package storage

import (
	"bytes"
	"context"
	"os"
	"testing"
	"time"

	"github.com/bwithraju/clouddump/backend/internal/database"
)

func TestNodeFailure_HealthCheckAndOfflineDetection(t *testing.T) {
	ctx := context.Background()
	mgr := NewStorageManager(NewMaxAvailableStrategy())

	n1, dir1 := createTestNode(t, "node1", "Node 1", 10*1024*1024)
	defer os.RemoveAll(dir1)
	n2, dir2 := createTestNode(t, "node2", "Node 2", 10*1024*1024)
	defer os.RemoveAll(dir2)

	_ = mgr.RegisterNode(n1)
	_ = mgr.RegisterNode(n2)

	// Initially both are online
	stats := mgr.GetPoolStats(ctx)
	if stats.OnlineNodes != 2 || stats.OfflineNodes != 0 {
		t.Fatalf("expected 2 online, 0 offline, got online=%d offline=%d", stats.OnlineNodes, stats.OfflineNodes)
	}

	// 1. Node goes offline
	err := mgr.SetNodeSimulatedOffline("node1", true)
	if err != nil {
		t.Fatalf("failed to simulate offline: %v", err)
	}

	// Verify detection via health checks
	results := mgr.RunHealthChecks(ctx)
	if results["node1"] {
		t.Errorf("expected node1 to be reported unhealthy, got healthy")
	}
	if !results["node2"] {
		t.Errorf("expected node2 to be reported healthy, got unhealthy")
	}

	stats = mgr.GetPoolStats(ctx)
	if stats.OnlineNodes != 1 || stats.OfflineNodes != 1 {
		t.Errorf("expected 1 online, 1 offline, got online=%d offline=%d", stats.OnlineNodes, stats.OfflineNodes)
	}

	// 2. Node returns online
	err = mgr.SetNodeSimulatedOffline("node1", false)
	if err != nil {
		t.Fatalf("failed to restore online: %v", err)
	}

	results = mgr.RunHealthChecks(ctx)
	if !results["node1"] {
		t.Errorf("expected node1 to be reported healthy after restore")
	}

	stats = mgr.GetPoolStats(ctx)
	if stats.OnlineNodes != 2 || stats.OfflineNodes != 0 {
		t.Errorf("expected 2 online, 0 offline after restore, got online=%d offline=%d", stats.OnlineNodes, stats.OfflineNodes)
	}
}

func TestNodeFailure_ReconciliationImpactOnFiles(t *testing.T) {
	ctx := context.Background()
	mgr := NewStorageManager(NewMaxAvailableStrategy())
	repo := database.NewMemoryRepository()

	n1, dir1 := createTestNode(t, "node1", "Node 1", 10*1024*1024)
	defer os.RemoveAll(dir1)
	n2, dir2 := createTestNode(t, "node2", "Node 2", 10*1024*1024)
	defer os.RemoveAll(dir2)

	_ = mgr.RegisterNode(n1)
	_ = mgr.RegisterNode(n2)

	// Create file records
	fileID := "file-failure-test"
	_ = repo.Files().CreateFile(ctx, &database.FileRecord{
		ID:          fileID,
		UserID:      "default_user",
		Filename:    "document.pdf",
		Size:        2048,
		ChunkSize:   1024,
		TotalChunks: 2,
		Status:      "COMPLETED",
	})

	// Chunk 0 on node1, Chunk 1 on node2
	_ = repo.Chunks().CreateChunk(ctx, &database.ChunkRecord{
		ID:            "chk-0",
		FileID:        fileID,
		ChunkIndex:    0,
		Size:          1024,
		Checksum:      "dummy1",
		StorageNodeID: "node1",
		Status:        "STORED",
		CreatedAt:     time.Now().UTC(),
	})
	_ = repo.Chunks().CreateChunk(ctx, &database.ChunkRecord{
		ID:            "chk-1",
		FileID:        fileID,
		ChunkIndex:    1,
		Size:          1024,
		Checksum:      "dummy2",
		StorageNodeID: "node2",
		Status:        "STORED",
		CreatedAt:     time.Now().UTC(),
	})

	// 1. Node 1 fails
	_ = mgr.SetNodeSimulatedOffline("node1", true)

	recResult, err := mgr.ReconcileNodeFailures(ctx, repo)
	if err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}

	if len(recResult.OfflineNodes) != 1 || recResult.OfflineNodes[0] != "node1" {
		t.Errorf("expected offline node [node1], got %v", recResult.OfflineNodes)
	}
	if len(recResult.DegradedFiles) != 1 || recResult.DegradedFiles[0] != fileID {
		t.Errorf("expected degraded file [%s], got %v", fileID, recResult.DegradedFiles)
	}

	// Verify file status in repository is DEGRADED
	fileRec, err := repo.Files().GetFile(ctx, fileID)
	if err != nil {
		t.Fatalf("failed to get file: %v", err)
	}
	if fileRec.Status != "DEGRADED" {
		t.Errorf("expected file status DEGRADED, got %s", fileRec.Status)
	}

	// 2. Node 1 restored online
	_ = mgr.SetNodeSimulatedOffline("node1", false)

	recResult2, err := mgr.ReconcileNodeFailures(ctx, repo)
	if err != nil {
		t.Fatalf("reconcile restore failed: %v", err)
	}

	if len(recResult2.RecoveredFiles) != 1 || recResult2.RecoveredFiles[0] != fileID {
		t.Errorf("expected recovered file [%s], got %v", fileID, recResult2.RecoveredFiles)
	}

	fileRec2, err := repo.Files().GetFile(ctx, fileID)
	if err != nil {
		t.Fatalf("failed to get file: %v", err)
	}
	if fileRec2.Status != "COMPLETED" {
		t.Errorf("expected file status COMPLETED after recovery, got %s", fileRec2.Status)
	}
}

func TestNodeFailure_UploadSkipsOfflineNodes(t *testing.T) {
	mgr := NewStorageManager(NewMaxAvailableStrategy())

	// node1 has large capacity but will be offline
	n1, dir1 := createTestNode(t, "node1", "Big Node", 100*1024*1024)
	defer os.RemoveAll(dir1)
	// node2 has smaller capacity but will be online
	n2, dir2 := createTestNode(t, "node2", "Small Node", 10*1024*1024)
	defer os.RemoveAll(dir2)

	_ = mgr.RegisterNode(n1)
	_ = mgr.RegisterNode(n2)

	// Take node1 offline
	_ = mgr.SetNodeSimulatedOffline("node1", true)

	// Try allocating 3 chunks
	for i := 0; i < 3; i++ {
		selected, err := mgr.SelectNodeForChunk(1024)
		if err != nil {
			t.Fatalf("allocation failed during node failure: %v", err)
		}
		if selected.ID != "node2" {
			t.Errorf("expected allocation on online node2, got %s", selected.ID)
		}
	}

	// Now take node2 offline as well
	_ = mgr.SetNodeSimulatedOffline("node2", true)

	// Allocation must fail
	_, err := mgr.SelectNodeForChunk(1024)
	if err == nil {
		t.Errorf("expected allocation to fail when all nodes are offline, got nil error")
	}
}

func TestNodeFailure_ReadFromOfflineNodeFails(t *testing.T) {
	ctx := context.Background()
	mgr := NewStorageManager(NewMaxAvailableStrategy())

	n1, dir1 := createTestNode(t, "node1", "Node 1", 10*1024*1024)
	defer os.RemoveAll(dir1)
	_ = mgr.RegisterNode(n1)

	// Store a chunk
	chunkData := []byte("critical persistent content")
	objID, _, err := mgr.StoreChunk(ctx, "node1", "chunk_test_1", bytes.NewReader(chunkData), int64(len(chunkData)))
	if err != nil {
		t.Fatalf("failed to store chunk: %v", err)
	}

	// Verify reading succeeds while online
	rc, err := mgr.GetChunk(ctx, "node1", objID)
	if err != nil {
		t.Fatalf("failed to get chunk: %v", err)
	}
	_ = rc.Close()

	// Take node1 offline
	_ = mgr.SetNodeSimulatedOffline("node1", true)

	// Attempting to read chunk must fail with ErrNodeUnavailable or ErrNodeOffline
	_, err = mgr.GetChunk(ctx, "node1", objID)
	if err == nil {
		t.Fatalf("expected read to fail while node is offline, got nil error")
	}

	// Bring node1 back online
	_ = mgr.SetNodeSimulatedOffline("node1", false)

	// Reading chunk must now succeed again byte-for-byte
	rc2, err := mgr.GetChunk(ctx, "node1", objID)
	if err != nil {
		t.Fatalf("failed to get chunk after node recovered: %v", err)
	}
	buf := new(bytes.Buffer)
	_, _ = buf.ReadFrom(rc2)
	_ = rc2.Close()

	if !bytes.Equal(buf.Bytes(), chunkData) {
		t.Fatalf("data mismatch after recovery: expected %q, got %q", chunkData, buf.Bytes())
	}
}
