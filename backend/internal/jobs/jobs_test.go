package jobs

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bwithraju/clouddump/backend/internal/chunks"
	"github.com/bwithraju/clouddump/backend/internal/database"
	"github.com/bwithraju/clouddump/backend/internal/providers"
	"github.com/bwithraju/clouddump/backend/internal/storage"
	"github.com/bwithraju/clouddump/backend/internal/uploads"
)

type testJobEnv struct {
	queue          JobQueue
	pool           *JobWorkerPool
	storageManager *storage.StorageManager
	uploadManager  *uploads.UploadManager
	repo           database.Repository
	dirs           map[string]string
}

func setupTestJobEnv(t *testing.T) *testJobEnv {
	chunkSize := int64(64 * 1024)
	mgr := storage.NewStorageManager(storage.NewMaxAvailableStrategy())

	dir1, _ := os.MkdirTemp("", "job_node1_*")
	dir2, _ := os.MkdirTemp("", "job_node2_*")

	p1, _ := providers.NewLocalStorageProvider(dir1, 50*1024*1024)
	p2, _ := providers.NewLocalStorageProvider(dir2, 50*1024*1024)

	n1, _ := storage.NewStorageNode("node1", "Storage Node 1", p1)
	n2, _ := storage.NewStorageNode("node2", "Storage Node 2", p2)

	_ = mgr.RegisterNode(n1)
	_ = mgr.RegisterNode(n2)

	repo := database.NewMemoryRepository()
	uploadManager := uploads.NewUploadManager(mgr, repo, chunkSize)
	queue := NewMemoryJobQueue()
	pool := NewJobWorkerPool(queue, 2)

	RegisterDefaultHandlers(pool, WorkerDependencies{
		StorageManager: mgr,
		UploadManager:  uploadManager,
		Repo:           repo,
	})

	pool.Start(context.Background())

	return &testJobEnv{
		queue:          queue,
		pool:           pool,
		storageManager: mgr,
		uploadManager:  uploadManager,
		repo:           repo,
		dirs: map[string]string{
			"node1": dir1,
			"node2": dir2,
		},
	}
}

func cleanupTestJobEnv(env *testJobEnv) {
	env.pool.Stop()
	_ = env.queue.Close()
	for _, d := range env.dirs {
		_ = os.RemoveAll(d)
	}
}

// 1. Test Job Status Lifecycle & Retry Handling
func TestJob_RetryAndFailureHandling(t *testing.T) {
	queue := NewMemoryJobQueue()
	pool := NewJobWorkerPool(queue, 1)

	var executionAttempts int32

	// Handler that fails twice then succeeds
	pool.RegisterHandler("flaky_task", func(ctx context.Context, job *Job) error {
		att := atomic.AddInt32(&executionAttempts, 1)
		if att < 3 {
			return errors.New("simulated transient error")
		}
		return nil
	})

	pool.Start(context.Background())
	defer pool.Stop()
	defer queue.Close()

	ctx := context.Background()
	job, err := NewJob("flaky_task", map[string]string{"foo": "bar"}, 3)
	if err != nil {
		t.Fatalf("failed creating job: %v", err)
	}

	if job.Status != JobStatusPending {
		t.Errorf("expected initial status PENDING, got %s", job.Status)
	}

	if err := queue.Enqueue(ctx, job); err != nil {
		t.Fatalf("failed enqueuing job: %v", err)
	}

	// Poll until completed
	deadline := time.Now().Add(5 * time.Second)
	var finalJob *Job
	for time.Now().Before(deadline) {
		finalJob, _ = queue.GetJob(ctx, job.ID)
		if finalJob != nil && finalJob.Status == JobStatusCompleted {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if finalJob == nil || finalJob.Status != JobStatusCompleted {
		t.Fatalf("expected job to eventually complete, got status: %v, attempts: %d, last error: %s",
			finalJob.Status, finalJob.Attempts, finalJob.LastError)
	}

	if atomic.LoadInt32(&executionAttempts) != 3 {
		t.Errorf("expected exactly 3 execution attempts, got %d", executionAttempts)
	}
	if finalJob.CompletedAt == nil {
		t.Errorf("expected CompletedAt timestamp to be set")
	}

	// Now test terminal failure when retries exhausted
	var permAttempts int32
	pool.RegisterHandler("perm_fail_task", func(ctx context.Context, job *Job) error {
		atomic.AddInt32(&permAttempts, 1)
		return errors.New("permanent failure")
	})

	failJob, _ := NewJob("perm_fail_task", map[string]string{"data": "test"}, 2)
	_ = queue.Enqueue(ctx, failJob)

	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		finalJob, _ = queue.GetJob(ctx, failJob.ID)
		if finalJob != nil && finalJob.Status == JobStatusFailed {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if finalJob == nil || finalJob.Status != JobStatusFailed {
		t.Fatalf("expected job to be FAILED, got: %v", finalJob.Status)
	}
	if finalJob.Attempts < 2 {
		t.Errorf("expected at least 2 attempts before failure, got %d", finalJob.Attempts)
	}
	if !strings.Contains(finalJob.LastError, "permanent failure") {
		t.Errorf("expected LastError to contain 'permanent failure', got: %s", finalJob.LastError)
	}
}

// 2. Test Task: Large Upload Background Processing
func TestJob_LargeUploadProcessing(t *testing.T) {
	env := setupTestJobEnv(t)
	defer cleanupTestJobEnv(env)

	ctx := context.Background()

	// Create file record in UPLOADING state
	fileID := "file_large_upload_123"
	file := &database.FileRecord{
		ID:          fileID,
		UserID:      "user1",
		Filename:    "large_video.mp4",
		Size:        128 * 1024,
		ChunkSize:   64 * 1024,
		TotalChunks: 2,
		Status:      string(chunks.FileStatusUploading),
	}
	_ = env.repo.Files().CreateFile(ctx, file)

	// Create 2 chunks
	_ = env.repo.Chunks().CreateChunk(ctx, &database.ChunkRecord{
		ID:               "chunk_0",
		FileID:           fileID,
		ChunkIndex:       0,
		Size:             64 * 1024,
		Checksum:         "hash0",
		StorageNodeID:    "node1",
		ProviderObjectID: "obj0",
		Status:           string(chunks.ChunkStatusStored),
	})
	_ = env.repo.Chunks().CreateChunk(ctx, &database.ChunkRecord{
		ID:               "chunk_1",
		FileID:           fileID,
		ChunkIndex:       1,
		Size:             64 * 1024,
		Checksum:         "hash1",
		StorageNodeID:    "node2",
		ProviderObjectID: "obj1",
		Status:           string(chunks.ChunkStatusStored),
	})

	job, err := NewJob(JobTypeLargeUploadProcessing, LargeUploadPayload{
		FileID:   fileID,
		UploadID: "upload_large_123",
		UserID:   "user1",
	}, 3)
	if err != nil {
		t.Fatalf("failed creating large upload job: %v", err)
	}

	_ = env.queue.Enqueue(ctx, job)

	// Wait for job completion
	deadline := time.Now().Add(5 * time.Second)
	var finalJob *Job
	for time.Now().Before(deadline) {
		finalJob, _ = env.queue.GetJob(ctx, job.ID)
		if finalJob != nil && finalJob.Status == JobStatusCompleted {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if finalJob == nil || finalJob.Status != JobStatusCompleted {
		t.Fatalf("expected job COMPLETED, got: %v (err: %s)", finalJob.Status, finalJob.LastError)
	}

	// Verify file status was updated to COMPLETED
	updatedFile, err := env.repo.Files().GetFile(ctx, fileID)
	if err != nil {
		t.Fatalf("failed fetching updated file: %v", err)
	}
	if updatedFile.Status != string(chunks.FileStatusCompleted) {
		t.Errorf("expected file status COMPLETED, got %s", updatedFile.Status)
	}

	// Idempotency check: Re-running the job should succeed without error
	_ = env.queue.Enqueue(ctx, job)
	time.Sleep(200 * time.Millisecond)
	finalJob2, _ := env.queue.GetJob(ctx, job.ID)
	if finalJob2.Status != JobStatusCompleted {
		t.Errorf("expected idempotent rerun to be COMPLETED, got %s", finalJob2.Status)
	}
}

// 3. Test Task: Background Chunk Replication (Idempotency, No Duplicate Chunks)
func TestJob_ReplicationWorker(t *testing.T) {
	env := setupTestJobEnv(t)
	defer cleanupTestJobEnv(env)

	ctx := context.Background()

	// Store a chunk on Node 1
	chunkID := "chunk_to_replicate_001"
	chunkData := []byte("Replication test chunk payload for background workers")
	objID, _, err := env.storageManager.StoreChunk(ctx, "node1", chunkID, strings.NewReader(string(chunkData)), int64(len(chunkData)))
	if err != nil {
		t.Fatalf("failed storing initial chunk on node1: %v", err)
	}

	chunkRecord := &database.ChunkRecord{
		ID:               chunkID,
		FileID:           "file_rep_999",
		ChunkIndex:       0,
		Size:             int64(len(chunkData)),
		Checksum:         "dummy_checksum",
		StorageNodeID:    "node1",
		ProviderObjectID: objID,
		Status:           string(chunks.ChunkStatusStored),
		Replicas:         nil,
	}
	_ = env.repo.Chunks().CreateChunk(ctx, chunkRecord)

	// Enqueue Replication Job (Target factor = 2)
	job, err := NewJob(JobTypeReplication, ReplicationPayload{
		FileID:                  "file_rep_999",
		ChunkID:                 chunkID,
		TargetReplicationFactor: 2,
	}, 3)
	if err != nil {
		t.Fatalf("failed creating replication job: %v", err)
	}

	_ = env.queue.Enqueue(ctx, job)

	// Wait for job completion
	deadline := time.Now().Add(5 * time.Second)
	var finalJob *Job
	for time.Now().Before(deadline) {
		finalJob, _ = env.queue.GetJob(ctx, job.ID)
		if finalJob != nil && finalJob.Status == JobStatusCompleted {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if finalJob == nil || finalJob.Status != JobStatusCompleted {
		t.Fatalf("expected replication job COMPLETED, got %v (err: %s)", finalJob.Status, finalJob.LastError)
	}

	// Verify replica was placed on Node 2 (Distinct from Node 1)
	updatedChunk, _ := env.repo.Chunks().GetChunk(ctx, chunkID)
	if len(updatedChunk.Replicas) != 1 {
		t.Fatalf("expected 1 replica recorded, got %d", len(updatedChunk.Replicas))
	}
	replica := updatedChunk.Replicas[0]
	if replica.StorageNodeID == "node1" {
		t.Errorf("replica placed on same node as primary (node1)!")
	}
	if replica.StorageNodeID != "node2" {
		t.Errorf("expected replica on node2, got %s", replica.StorageNodeID)
	}

	// Verify replica content can be retrieved from node2
	stream, err := env.storageManager.GetChunk(ctx, replica.StorageNodeID, replica.ProviderObjectID)
	if err != nil {
		t.Fatalf("failed retrieving chunk replica from node2: %v", err)
	}
	repBytes, _ := io.ReadAll(stream)
	_ = stream.Close()

	if string(repBytes) != string(chunkData) {
		t.Errorf("replica data mismatch: %s vs %s", string(repBytes), string(chunkData))
	}

	// Idempotency: Enqueue the replication job again -> MUST NOT duplicate chunk creation
	job2, _ := NewJob(JobTypeReplication, ReplicationPayload{
		FileID:                  "file_rep_999",
		ChunkID:                 chunkID,
		TargetReplicationFactor: 2,
	}, 3)
	_ = env.queue.Enqueue(ctx, job2)

	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		finalJob, _ = env.queue.GetJob(ctx, job2.ID)
		if finalJob != nil && finalJob.Status == JobStatusCompleted {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	updatedChunk2, _ := env.repo.Chunks().GetChunk(ctx, chunkID)
	if len(updatedChunk2.Replicas) != 1 {
		t.Errorf("idempotency violation: duplicate replica created! count = %d", len(updatedChunk2.Replicas))
	}
}

// 4. Test Task: Node Health Check Worker
func TestJob_NodeHealthCheck(t *testing.T) {
	env := setupTestJobEnv(t)
	defer cleanupTestJobEnv(env)

	ctx := context.Background()

	// Simulate node 1 offline
	_ = env.storageManager.SetNodeSimulatedOffline("node1", true)

	job, err := NewJob(JobTypeNodeHealthCheck, HealthCheckPayload{TriggeredBy: "test_runner"}, 3)
	if err != nil {
		t.Fatalf("failed creating health check job: %v", err)
	}

	_ = env.queue.Enqueue(ctx, job)

	deadline := time.Now().Add(5 * time.Second)
	var finalJob *Job
	for time.Now().Before(deadline) {
		finalJob, _ = env.queue.GetJob(ctx, job.ID)
		if finalJob != nil && finalJob.Status == JobStatusCompleted {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if finalJob == nil || finalJob.Status != JobStatusCompleted {
		t.Fatalf("expected health check job COMPLETED, got %v (err: %s)", finalJob.Status, finalJob.LastError)
	}

	// Node 1 should now be recorded as OFFLINE
	node1, _ := env.storageManager.GetNode("node1")
	if node1.IsOnline() {
		t.Errorf("expected node1 to be OFFLINE after health check job")
	}

	// Restore node 1
	_ = env.storageManager.SetNodeSimulatedOffline("node1", false)
}

// 5. Test Task: Cleanup Abandoned Uploads
func TestJob_CleanupAbandonedUploads(t *testing.T) {
	env := setupTestJobEnv(t)
	defer cleanupTestJobEnv(env)

	ctx := context.Background()

	// Initialize an upload session
	session, err := env.uploadManager.CreateSession(ctx, "user_abandon", "abandoned.iso", "application/octet-stream", 128*1024, 64*1024)
	if err != nil {
		t.Fatalf("failed creating session: %v", err)
	}

	// Upload chunk 0
	_, err = env.uploadManager.StoreChunk(ctx, session.UploadID, 0, strings.NewReader(strings.Repeat("X", 64*1024)), 64*1024)
	if err != nil {
		t.Fatalf("failed storing chunk 0: %v", err)
	}

	// Artificially age the session updated_at timestamp to 2 hours ago
	session.SetStatus(uploads.UploadStatusUploading)
	session.UpdatedAt = time.Now().UTC().Add(-2 * time.Hour)

	// Enqueue cleanup job with threshold = 1 hour
	job, err := NewJob(JobTypeCleanupAbandoned, CleanupAbandonedPayload{
		OlderThanSeconds: 3600, // 1 hour
		UserID:           "user_abandon",
	}, 3)
	if err != nil {
		t.Fatalf("failed creating cleanup job: %v", err)
	}

	_ = env.queue.Enqueue(ctx, job)

	deadline := time.Now().Add(5 * time.Second)
	var finalJob *Job
	for time.Now().Before(deadline) {
		finalJob, _ = env.queue.GetJob(ctx, job.ID)
		if finalJob != nil && finalJob.Status == JobStatusCompleted {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if finalJob == nil || finalJob.Status != JobStatusCompleted {
		t.Fatalf("expected cleanup job COMPLETED, got %v", finalJob.Status)
	}

	// Session should now be CANCELLED
	s, _ := env.uploadManager.GetSession(session.UploadID)
	if s.Status != uploads.UploadStatusCancelled {
		t.Errorf("expected abandoned session to be CANCELLED, got %s", s.Status)
	}
}

// 6. Test Task: Failed Chunk Retry
func TestJob_FailedChunkRetry(t *testing.T) {
	env := setupTestJobEnv(t)
	defer cleanupTestJobEnv(env)

	ctx := context.Background()

	// Primary chunk on Node 1, Replica on Node 2
	chunkID := "failed_chunk_retry_001"
	chunkData := []byte("Payload for recovering a failed chunk from replica")

	objID1, _, _ := env.storageManager.StoreChunk(ctx, "node1", chunkID, strings.NewReader(string(chunkData)), int64(len(chunkData)))
	objID2, _, _ := env.storageManager.StoreChunk(ctx, "node2", chunkID+"_rep", strings.NewReader(string(chunkData)), int64(len(chunkData)))

	// Register extra Node 3 for relocation
	dir3, _ := os.MkdirTemp("", "job_node3_*")
	env.dirs["node3"] = dir3
	p3, _ := providers.NewLocalStorageProvider(dir3, 50*1024*1024)
	n3, _ := storage.NewStorageNode("node3", "Storage Node 3", p3)
	_ = env.storageManager.RegisterNode(n3)

	chunkRecord := &database.ChunkRecord{
		ID:               chunkID,
		FileID:           "file_failed_chk_1",
		ChunkIndex:       0,
		Size:             int64(len(chunkData)),
		Checksum:         "test_chksum",
		StorageNodeID:    "node1",
		ProviderObjectID: objID1,
		Status:           string(chunks.ChunkStatusStored),
		Replicas: []database.ChunkReplicaRecord{
			{
				StorageNodeID:    "node2",
				ProviderObjectID: objID2,
				Status:           string(chunks.ChunkStatusStored),
			},
		},
	}
	_ = env.repo.Chunks().CreateChunk(ctx, chunkRecord)

	// Simulate node 1 offline (primary lost!)
	_ = env.storageManager.SetNodeSimulatedOffline("node1", true)
	_, _ = env.storageManager.GetNode("node1")

	// Trigger failed chunk retry job
	job, err := NewJob(JobTypeFailedChunkRetry, FailedChunkRetryPayload{
		FileID:  "file_failed_chk_1",
		ChunkID: chunkID,
	}, 3)
	if err != nil {
		t.Fatalf("failed creating retry job: %v", err)
	}

	_ = env.queue.Enqueue(ctx, job)

	deadline := time.Now().Add(5 * time.Second)
	var finalJob *Job
	for time.Now().Before(deadline) {
		finalJob, _ = env.queue.GetJob(ctx, job.ID)
		if finalJob != nil && finalJob.Status == JobStatusCompleted {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if finalJob == nil || finalJob.Status != JobStatusCompleted {
		t.Fatalf("expected failed chunk retry job COMPLETED, got %v (err: %s)", finalJob.Status, finalJob.LastError)
	}

	// Verify chunk was relocated from replica on node2 to online node3
	updatedChunk, _ := env.repo.Chunks().GetChunk(ctx, chunkID)
	if updatedChunk.StorageNodeID != "node3" {
		t.Errorf("expected chunk to be relocated to node3, got %s", updatedChunk.StorageNodeID)
	}

	// Verify content on node3
	stream, err := env.storageManager.GetChunk(ctx, "node3", updatedChunk.ProviderObjectID)
	if err != nil {
		t.Fatalf("failed retrieving recovered chunk on node3: %v", err)
	}
	b, _ := io.ReadAll(stream)
	_ = stream.Close()

	if string(b) != string(chunkData) {
		t.Errorf("recovered chunk data mismatch")
	}
}
