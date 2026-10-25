package workers

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/bwithraju/clouddump/backend/internal/providers"
	"github.com/bwithraju/clouddump/backend/internal/storage"
)

func setupTestEnvironment(t *testing.T) (*storage.StorageManager, map[string]string) {
	mgr := storage.NewStorageManager(storage.NewMaxAvailableStrategy())

	d1, _ := os.MkdirTemp("", "worker_node1_*")
	d2, _ := os.MkdirTemp("", "worker_node2_*")
	d3, _ := os.MkdirTemp("", "worker_node3_*")
	d4, _ := os.MkdirTemp("", "worker_node4_*")

	p1, _ := providers.NewLocalStorageProvider(d1, 100*1024*1024)
	p2, _ := providers.NewLocalStorageProvider(d2, 100*1024*1024)
	p3, _ := providers.NewLocalStorageProvider(d3, 100*1024*1024)
	p4, _ := providers.NewLocalStorageProvider(d4, 100*1024*1024)

	n1, _ := storage.NewStorageNode("node1", "Node 1", p1)
	n2, _ := storage.NewStorageNode("node2", "Node 2", p2)
	n3, _ := storage.NewStorageNode("node3", "Node 3", p3)
	n4, _ := storage.NewStorageNode("node4", "Node 4", p4)

	_ = mgr.RegisterNode(n1)
	_ = mgr.RegisterNode(n2)
	_ = mgr.RegisterNode(n3)
	_ = mgr.RegisterNode(n4)

	dirs := map[string]string{
		"node1": d1,
		"node2": d2,
		"node3": d3,
		"node4": d4,
	}

	return mgr, dirs
}

func cleanupDirs(dirs map[string]string) {
	for _, d := range dirs {
		_ = os.RemoveAll(d)
	}
}

// Test: Parallel upload with 4 workers distributes chunks correctly
func TestParallelUploader_ControlledConcurrency(t *testing.T) {
	mgr, dirs := setupTestEnvironment(t)
	defer cleanupDirs(dirs)

	chunkSize := int64(50 * 1024) // 50 KB
	totalChunks := 12
	fileSize := chunkSize*int64(totalChunks) - 1024 // 11 full + 1 partial chunk
	workerCount := 4

	uploader, err := NewParallelUploader(mgr, chunkSize, workerCount)
	if err != nil {
		t.Fatalf("failed to create parallel uploader: %v", err)
	}

	if uploader.WorkerCount() != workerCount {
		t.Errorf("expected %d workers, got %d", workerCount, uploader.WorkerCount())
	}

	// Create test file payload with deterministic data
	payload := make([]byte, fileSize)
	for i := range payload {
		payload[i] = byte(i % 251)
	}

	ctx := context.Background()
	result, err := uploader.Distribute(ctx, "parallel_file_1", bytes.NewReader(payload), fileSize, nil)
	if err != nil {
		t.Fatalf("parallel upload failed: %v", err)
	}

	if result.TotalChunks != totalChunks {
		t.Fatalf("expected %d total chunks, got %d", totalChunks, result.TotalChunks)
	}

	// Verify all chunks are ordered, unique, and physically present
	seenIndices := make(map[int]bool)
	for i, chunk := range result.Chunks {
		if chunk.ChunkIndex != i {
			t.Errorf("chunk at position %d has index %d", i, chunk.ChunkIndex)
		}
		if seenIndices[chunk.ChunkIndex] {
			t.Errorf("duplicate chunk index %d detected", chunk.ChunkIndex)
		}
		seenIndices[chunk.ChunkIndex] = true

		// Check physical file on node
		nodeDir := dirs[chunk.StorageNodeID]
		chunkPath := filepath.Join(nodeDir, chunk.ProviderObjectID)
		data, err := os.ReadFile(chunkPath)
		if err != nil {
			t.Fatalf("chunk file %s missing on disk: %v", chunkPath, err)
		}

		if int64(len(data)) != chunk.Size {
			t.Errorf("chunk %d size mismatch: disk %d, metadata %d", i, len(data), chunk.Size)
		}

		// Verify SHA-256
		hasher := sha256.New()
		hasher.Write(data)
		expectedChecksum := hex.EncodeToString(hasher.Sum(nil))
		if chunk.Checksum != expectedChecksum {
			t.Errorf("chunk %d checksum mismatch: got %s, expected %s", i, chunk.Checksum, expectedChecksum)
		}
	}

	// Verify capacity accounting matches exactly
	poolStats := mgr.GetPoolStats(ctx)
	if poolStats.UsedCapacity != fileSize {
		t.Errorf("pool used capacity mismatch: expected %d, got %d", fileSize, poolStats.UsedCapacity)
	}
}

// Test: Cancellation support halts workers and rolls back stored chunks
func TestParallelUploader_CancellationRollback(t *testing.T) {
	mgr, dirs := setupTestEnvironment(t)
	defer cleanupDirs(dirs)

	chunkSize := int64(20 * 1024)
	fileSize := chunkSize * 20

	uploader, _ := NewParallelUploader(mgr, chunkSize, 4)

	// Create a context that cancels quickly
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately before upload starts

	payload := make([]byte, fileSize)
	_, err := uploader.Distribute(ctx, "cancelled_file", bytes.NewReader(payload), fileSize, nil)
	if err == nil {
		t.Fatalf("expected error on cancelled context, got nil")
	}

	// Verify capacity was not leaked
	poolStats := mgr.GetPoolStats(context.Background())
	if poolStats.UsedCapacity != 0 {
		t.Errorf("expected 0 used capacity after cancelled upload, got %d", poolStats.UsedCapacity)
	}
}

// Test: Worker failure propagation and rollback
func TestParallelUploader_WorkerErrorPropagation(t *testing.T) {
	mgr, dirs := setupTestEnvironment(t)
	defer cleanupDirs(dirs)

	chunkSize := int64(50 * 1024)
	fileSize := chunkSize * 6

	uploader, _ := NewParallelUploader(mgr, chunkSize, 4)

	// Provide a truncated stream (fewer bytes than declared fileSize)
	truncatedPayload := make([]byte, chunkSize*2) // only 2 chunks available
	_, err := uploader.Distribute(context.Background(), "truncated_file", bytes.NewReader(truncatedPayload), fileSize, nil)
	if err == nil {
		t.Fatalf("expected error on truncated stream, got nil")
	}

	// Verify all chunks rolled back and capacity restored
	poolStats := mgr.GetPoolStats(context.Background())
	if poolStats.UsedCapacity != 0 {
		t.Errorf("expected 0 used capacity after failed upload, got %d", poolStats.UsedCapacity)
	}
}

// Test: Worker pool respects environment variable MAX_UPLOAD_WORKERS
func TestParallelUploader_EnvWorkerConfiguration(t *testing.T) {
	os.Setenv("MAX_UPLOAD_WORKERS", "8")
	defer os.Unsetenv("MAX_UPLOAD_WORKERS")

	count := ResolveWorkerCount(0)
	if count != 8 {
		t.Errorf("expected 8 workers from env, got %d", count)
	}

	// Capped at MaxAllowedWorkers
	os.Setenv("MAX_UPLOAD_WORKERS", "999")
	capped := ResolveWorkerCount(0)
	if capped != MaxAllowedWorkers {
		t.Errorf("expected max allowed %d workers, got %d", MaxAllowedWorkers, capped)
	}
}
