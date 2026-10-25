package uploads

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/bwithraju/clouddump/backend/internal/chunks"
	"github.com/bwithraju/clouddump/backend/internal/database"
	"github.com/bwithraju/clouddump/backend/internal/providers"
	"github.com/bwithraju/clouddump/backend/internal/storage"
)

func setupTestUploadManager(t *testing.T, chunkSize int64) (*UploadManager, *storage.StorageManager, database.Repository, map[string]string) {
	mgr := storage.NewStorageManager(storage.NewMaxAvailableStrategy())

	d1, _ := os.MkdirTemp("", "up_node1_*")
	d2, _ := os.MkdirTemp("", "up_node2_*")
	d3, _ := os.MkdirTemp("", "up_node3_*")

	p1, _ := providers.NewLocalStorageProvider(d1, 50*1024*1024)
	p2, _ := providers.NewLocalStorageProvider(d2, 50*1024*1024)
	p3, _ := providers.NewLocalStorageProvider(d3, 50*1024*1024)

	n1, _ := storage.NewStorageNode("node1", "Node 1", p1)
	n2, _ := storage.NewStorageNode("node2", "Node 2", p2)
	n3, _ := storage.NewStorageNode("node3", "Node 3", p3)

	_ = mgr.RegisterNode(n1)
	_ = mgr.RegisterNode(n2)
	_ = mgr.RegisterNode(n3)

	dirs := map[string]string{
		"node1": d1,
		"node2": d2,
		"node3": d3,
	}

	repo := database.NewMemoryRepository()
	upMgr := NewUploadManager(mgr, repo, chunkSize)

	return upMgr, mgr, repo, dirs
}

func cleanupDirs(dirs map[string]string) {
	for _, d := range dirs {
		_ = os.RemoveAll(d)
	}
}

// Test: Interruption after several chunks and resuming from the interrupted point
func TestUploadManager_InterruptionAndResume(t *testing.T) {
	chunkSize := int64(10 * 1024) // 10 KB
	upMgr, _, repo, dirs := setupTestUploadManager(t, chunkSize)
	defer cleanupDirs(dirs)

	ctx := context.Background()
	totalChunks := 6
	fileSize := chunkSize*int64(totalChunks) - 2048 // 5 full chunks + 1 partial chunk

	session, err := upMgr.CreateSession(ctx, "user1", "resume_test.dat", "application/octet-stream", fileSize, chunkSize)
	if err != nil {
		t.Fatalf("failed to create upload session: %v", err)
	}

	if session.TotalChunks != totalChunks {
		t.Fatalf("expected %d total chunks, got %d", totalChunks, session.TotalChunks)
	}

	// 1. Upload first 3 chunks (chunks 0, 1, 2)
	for i := 0; i < 3; i++ {
		payload := bytes.Repeat([]byte{byte(i + 1)}, int(chunkSize))
		meta, err := upMgr.StoreChunk(ctx, session.UploadID, i, bytes.NewReader(payload), chunkSize)
		if err != nil {
			t.Fatalf("failed storing chunk %d: %v", i, err)
		}
		if meta.ChunkIndex != i {
			t.Errorf("expected chunk index %d, got %d", i, meta.ChunkIndex)
		}
	}

	// Verify interruption state: exactly 3 completed chunks, next expected is 3
	summary := session.Summary()
	if summary.CompletedCount != 3 {
		t.Errorf("expected 3 completed chunks, got %d", summary.CompletedCount)
	}
	if summary.NextExpected != 3 {
		t.Errorf("expected next expected chunk to be 3, got %d", summary.NextExpected)
	}
	if summary.Status != UploadStatusUploading {
		t.Errorf("expected status UPLOADING, got %v", summary.Status)
	}

	// Verify file in repo is still in UPLOADING status
	fileRec, _ := repo.Files().GetFile(ctx, session.FileID)
	if fileRec.Status != string(chunks.FileStatusUploading) {
		t.Errorf("file status in db should be UPLOADING, got %s", fileRec.Status)
	}

	// 2. Resume upload: upload remaining chunks (chunks 3, 4, 5)
	for i := summary.NextExpected; i < session.TotalChunks; i++ {
		expectedSize := upMgr.ExpectedChunkSize(session, i)
		payload := bytes.Repeat([]byte{byte(i + 1)}, int(expectedSize))
		_, err := upMgr.StoreChunk(ctx, session.UploadID, i, bytes.NewReader(payload), expectedSize)
		if err != nil {
			t.Fatalf("failed storing resumed chunk %d: %v", i, err)
		}
	}

	// Verify completed state
	finalSummary := session.Summary()
	if finalSummary.CompletedCount != totalChunks {
		t.Errorf("expected %d completed chunks, got %d", totalChunks, finalSummary.CompletedCount)
	}
	if finalSummary.Status != UploadStatusCompleted {
		t.Errorf("expected status COMPLETED, got %v", finalSummary.Status)
	}

	// Verify file in repo is now COMPLETED
	updatedFile, _ := repo.Files().GetFile(ctx, session.FileID)
	if updatedFile.Status != string(chunks.FileStatusCompleted) {
		t.Errorf("expected file status COMPLETED in repo, got %s", updatedFile.Status)
	}
}

// Test: Already completed chunks must not be duplicated or re-stored
func TestUploadManager_AlreadyCompletedChunksNotDuplicated(t *testing.T) {
	chunkSize := int64(10 * 1024)
	upMgr, mgr, repo, dirs := setupTestUploadManager(t, chunkSize)
	defer cleanupDirs(dirs)

	ctx := context.Background()
	totalChunks := 3
	fileSize := chunkSize * int64(totalChunks)

	session, err := upMgr.CreateSession(ctx, "user1", "idempotent.dat", "application/octet-stream", fileSize, chunkSize)
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	// Store chunk 0
	payload := bytes.Repeat([]byte("A"), int(chunkSize))
	meta1, err := upMgr.StoreChunk(ctx, session.UploadID, 0, bytes.NewReader(payload), chunkSize)
	if err != nil {
		t.Fatalf("first upload of chunk 0 failed: %v", err)
	}

	usedBefore := mgr.GetPoolStats(ctx).UsedCapacity

	// Attempt duplicate upload of chunk 0 with same or different data
	duplicatePayload := bytes.Repeat([]byte("B"), int(chunkSize))
	meta2, err := upMgr.StoreChunk(ctx, session.UploadID, 0, bytes.NewReader(duplicatePayload), chunkSize)
	if err != nil {
		t.Fatalf("duplicate upload should not fail: %v", err)
	}

	usedAfter := mgr.GetPoolStats(ctx).UsedCapacity

	// Verify capacity was NOT allocated twice
	if usedAfter != usedBefore {
		t.Errorf("storage capacity changed on duplicate chunk upload: before %d, after %d", usedBefore, usedAfter)
	}

	// Verify returned metadata matches original
	if meta2.ID != meta1.ID || meta2.Checksum != meta1.Checksum || meta2.StorageNodeID != meta1.StorageNodeID {
		t.Errorf("duplicate chunk did not return existing chunk metadata")
	}

	// Verify only 1 chunk record exists for chunk 0 in repo
	chunksList, _ := repo.Chunks().ListChunksByFile(ctx, session.FileID)
	if len(chunksList) != 1 {
		t.Errorf("expected exactly 1 chunk record, found %d", len(chunksList))
	}
}

// Test: Failed chunk retry clears failure and completes session
func TestUploadManager_FailedChunkRetry(t *testing.T) {
	chunkSize := int64(10 * 1024)
	upMgr, _, _, dirs := setupTestUploadManager(t, chunkSize)
	defer cleanupDirs(dirs)

	ctx := context.Background()
	fileSize := chunkSize * 2

	session, err := upMgr.CreateSession(ctx, "user1", "retry_test.dat", "application/octet-stream", fileSize, chunkSize)
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	// 1. Chunk 0 uploaded successfully
	payload0 := bytes.Repeat([]byte("0"), int(chunkSize))
	_, err = upMgr.StoreChunk(ctx, session.UploadID, 0, bytes.NewReader(payload0), chunkSize)
	if err != nil {
		t.Fatalf("chunk 0 upload failed: %v", err)
	}

	// 2. Chunk 1 fails due to truncated stream (size mismatch)
	truncatedPayload := bytes.Repeat([]byte("1"), int(chunkSize)-100)
	_, err = upMgr.StoreChunk(ctx, session.UploadID, 1, bytes.NewReader(truncatedPayload), chunkSize)
	if err == nil {
		t.Fatalf("expected error on truncated chunk stream, got nil")
	}

	summary := session.Summary()
	if summary.FailedCount != 1 {
		t.Errorf("expected 1 failed chunk in summary, got %d", summary.FailedCount)
	}
	if _, ok := summary.FailedChunks[1]; !ok {
		t.Errorf("expected chunk 1 to be marked in FailedChunks")
	}

	// 3. Retry chunk 1 with correct full payload
	correctPayload1 := bytes.Repeat([]byte("1"), int(chunkSize))
	meta1, err := upMgr.StoreChunk(ctx, session.UploadID, 1, bytes.NewReader(correctPayload1), chunkSize)
	if err != nil {
		t.Fatalf("retried chunk 1 upload failed: %v", err)
	}
	if meta1.ChunkIndex != 1 {
		t.Errorf("expected chunk index 1, got %d", meta1.ChunkIndex)
	}

	// Verify failed record was cleared and session completed
	summaryAfter := session.Summary()
	if summaryAfter.FailedCount != 0 {
		t.Errorf("expected 0 failed chunks after retry, got %d", summaryAfter.FailedCount)
	}
	if summaryAfter.Status != UploadStatusCompleted {
		t.Errorf("expected status COMPLETED, got %v", summaryAfter.Status)
	}
}

// Test: Cancelled upload rolls back physical chunks and frees capacity
func TestUploadManager_CancelledUploadRollback(t *testing.T) {
	chunkSize := int64(10 * 1024)
	upMgr, _, repo, dirs := setupTestUploadManager(t, chunkSize)
	defer cleanupDirs(dirs)

	ctx := context.Background()
	totalChunks := 4
	fileSize := chunkSize * int64(totalChunks)

	session, err := upMgr.CreateSession(ctx, "user1", "cancel_test.dat", "application/octet-stream", fileSize, chunkSize)
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	// Upload first 2 chunks
	for i := 0; i < 2; i++ {
		payload := bytes.Repeat([]byte{byte(i + 1)}, int(chunkSize))
		_, err := upMgr.StoreChunk(ctx, session.UploadID, i, bytes.NewReader(payload), chunkSize)
		if err != nil {
			t.Fatalf("failed storing chunk %d: %v", i, err)
		}
	}

	// Confirm chunks are on disk
	chunksBeforeCancel, _ := repo.Chunks().ListChunksByFile(ctx, session.FileID)
	if len(chunksBeforeCancel) != 2 {
		t.Fatalf("expected 2 chunk records in db, got %d", len(chunksBeforeCancel))
	}
	for _, c := range chunksBeforeCancel {
		nodeDir := dirs[c.StorageNodeID]
		chunkPath := filepath.Join(nodeDir, c.ProviderObjectID)
		if _, err := os.Stat(chunkPath); os.IsNotExist(err) {
			t.Fatalf("chunk file %s does not exist on disk before cancel", chunkPath)
		}
	}

	// Cancel upload
	if err := upMgr.CancelSession(ctx, session.UploadID); err != nil {
		t.Fatalf("cancel failed: %v", err)
	}

	// Verify session status is CANCELLED
	if session.Summary().Status != UploadStatusCancelled {
		t.Errorf("expected session status CANCELLED, got %v", session.Summary().Status)
	}

	// Verify physical files were deleted from storage nodes
	for _, c := range chunksBeforeCancel {
		nodeDir := dirs[c.StorageNodeID]
		chunkPath := filepath.Join(nodeDir, c.ProviderObjectID)
		if _, err := os.Stat(chunkPath); !os.IsNotExist(err) {
			t.Errorf("chunk file %s still exists on disk after cancel!", chunkPath)
		}
	}

	// Verify cannot upload chunks to a cancelled session
	dummyPayload := bytes.Repeat([]byte("X"), int(chunkSize))
	_, err = upMgr.StoreChunk(ctx, session.UploadID, 2, bytes.NewReader(dummyPayload), chunkSize)
	if !errors.Is(err, ErrSessionCancelled) {
		t.Errorf("expected ErrSessionCancelled, got %v", err)
	}
}
