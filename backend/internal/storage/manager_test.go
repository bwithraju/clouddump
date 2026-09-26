package storage

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"testing"

	"github.com/bwithraju/clouddump/backend/internal/providers"
)

func createTestNode(t *testing.T, id, name string, capacity int64) (*StorageNode, string) {
	tempDir, err := os.MkdirTemp("", "clouddump_mgr_test_"+id+"_*")
	if err != nil {
		t.Fatalf("failed to create temp dir for node %s: %v", id, err)
	}

	provider, err := providers.NewLocalStorageProvider(tempDir, capacity)
	if err != nil {
		t.Fatalf("failed to create provider for node %s: %v", id, err)
	}

	node, err := NewStorageNode(id, name, provider)
	if err != nil {
		t.Fatalf("failed to create storage node %s: %v", id, err)
	}

	return node, tempDir
}

func TestStorageManager_RegistrationAndPoolCapacity(t *testing.T) {
	mgr := NewStorageManager(NewMaxAvailableStrategy())

	node1, dir1 := createTestNode(t, "node1", "Node A (500MB)", 500*1024*1024)
	defer os.RemoveAll(dir1)
	node2, dir2 := createTestNode(t, "node2", "Node B (1GB)", 1000*1024*1024)
	defer os.RemoveAll(dir2)
	node3, dir3 := createTestNode(t, "node3", "Node C (2GB)", 2000*1024*1024)
	defer os.RemoveAll(dir3)
	node4, dir4 := createTestNode(t, "node4", "Node D (700MB)", 700*1024*1024)
	defer os.RemoveAll(dir4)

	// Register all 4 nodes
	for _, n := range []*StorageNode{node1, node2, node3, node4} {
		if err := mgr.RegisterNode(n); err != nil {
			t.Fatalf("failed to register node %s: %v", n.ID, err)
		}
	}

	// Verify duplicate registration error
	if err := mgr.RegisterNode(node1); !errors.Is(err, ErrDuplicateNodeID) {
		t.Errorf("expected ErrDuplicateNodeID, got: %v", err)
	}

	// Verify pool stats
	ctx := context.Background()
	stats := mgr.GetPoolStats(ctx)

	expectedTotal := int64((500 + 1000 + 2000 + 700) * 1024 * 1024)
	if stats.TotalCapacity != expectedTotal {
		t.Errorf("expected pool total %d, got %d", expectedTotal, stats.TotalCapacity)
	}
	if stats.AvailableCapacity != expectedTotal {
		t.Errorf("expected pool available %d, got %d", expectedTotal, stats.AvailableCapacity)
	}
	if stats.NodeCount != 4 || stats.OnlineNodes != 4 {
		t.Errorf("expected 4 nodes online, got count=%d, online=%d", stats.NodeCount, stats.OnlineNodes)
	}
}

func TestStorageManager_CapacityAwareNodeSelection(t *testing.T) {
	mgr := NewStorageManager(NewMaxAvailableStrategy())

	// Node 1: 50 MB, Node 2: 100 MB, Node 3: 200 MB
	node1, dir1 := createTestNode(t, "node1", "Node 1", 50*1024*1024)
	defer os.RemoveAll(dir1)
	node2, dir2 := createTestNode(t, "node2", "Node 2", 100*1024*1024)
	defer os.RemoveAll(dir2)
	node3, dir3 := createTestNode(t, "node3", "Node 3", 200*1024*1024)
	defer os.RemoveAll(dir3)

	_ = mgr.RegisterNode(node1)
	_ = mgr.RegisterNode(node2)
	_ = mgr.RegisterNode(node3)

	// Chunk of 20 MB -> MaxAvailableStrategy must pick Node 3 (has 200 MB free)
	selected, err := mgr.SelectNodeForChunk(20 * 1024 * 1024)
	if err != nil {
		t.Fatalf("SelectNodeForChunk failed: %v", err)
	}
	if selected.ID != "node3" {
		t.Errorf("expected node3 to be selected due to maximum available capacity, got %s", selected.ID)
	}

	// Exclude node3 -> must pick node2 (has 100 MB free)
	selectedWithout3, err := mgr.SelectNodeForChunk(20*1024*1024, "node3")
	if err != nil {
		t.Fatalf("SelectNodeForChunk with exclusion failed: %v", err)
	}
	if selectedWithout3.ID != "node2" {
		t.Errorf("expected node2 to be selected when node3 excluded, got %s", selectedWithout3.ID)
	}

	// Requesting 300 MB (exceeds all individual nodes) -> must fail with ErrNoAvailableNodes
	_, err = mgr.SelectNodeForChunk(300 * 1024 * 1024)
	if !errors.Is(err, ErrNoAvailableNodes) {
		t.Errorf("expected ErrNoAvailableNodes, got: %v", err)
	}
}

func TestStorageManager_StoreGetDeleteChunk(t *testing.T) {
	mgr := NewStorageManager(NewMaxAvailableStrategy())

	node1, dir1 := createTestNode(t, "node1", "Node 1", 500*1024*1024)
	defer os.RemoveAll(dir1)
	_ = mgr.RegisterNode(node1)

	ctx := context.Background()
	chunkData := []byte("CloudDump Chunk Content Stream Test")
	chunkID := "chunk_file1_idx0"

	// Store
	objectID, written, err := mgr.StoreChunk(ctx, "node1", chunkID, bytes.NewReader(chunkData), int64(len(chunkData)))
	if err != nil {
		t.Fatalf("StoreChunk failed: %v", err)
	}
	if written != int64(len(chunkData)) {
		t.Errorf("expected written %d bytes, got %d", len(chunkData), written)
	}

	// Get
	rc, err := mgr.GetChunk(ctx, "node1", objectID)
	if err != nil {
		t.Fatalf("GetChunk failed: %v", err)
	}

	readData, err := io.ReadAll(rc)
	_ = rc.Close() // Explicitly close file handle so Windows allows file deletion
	if err != nil {
		t.Fatalf("failed reading chunk data: %v", err)
	}
	if !bytes.Equal(readData, chunkData) {
		t.Errorf("read chunk data does not match stored content")
	}

	// Delete
	if err := mgr.DeleteChunk(ctx, "node1", objectID); err != nil {
		t.Fatalf("DeleteChunk failed: %v", err)
	}

	// Read after delete -> must fail
	_, err = mgr.GetChunk(ctx, "node1", objectID)
	if err == nil {
		t.Errorf("expected error after chunk deletion, got nil")
	}
}

func TestStorageManager_OfflineNodeHandling(t *testing.T) {
	mgr := NewStorageManager(NewMaxAvailableStrategy())

	node1, dir1 := createTestNode(t, "node1", "Node 1", 500*1024*1024)
	defer os.RemoveAll(dir1)
	_ = mgr.RegisterNode(node1)

	// Simulate node going offline
	node1.mu.Lock()
	node1.Status = NodeStatusOffline
	node1.mu.Unlock()

	// SelectNodeForChunk should skip offline node
	_, err := mgr.SelectNodeForChunk(10 * 1024 * 1024)
	if !errors.Is(err, ErrNoAvailableNodes) {
		t.Errorf("expected ErrNoAvailableNodes for offline node, got: %v", err)
	}

	// StoreChunk on offline node should fail
	ctx := context.Background()
	_, _, err = mgr.StoreChunk(ctx, "node1", "test_chunk", bytes.NewReader([]byte("data")), 4)
	if !errors.Is(err, ErrNodeUnavailable) {
		t.Errorf("expected ErrNodeUnavailable, got: %v", err)
	}
}

func TestStorageManager_HealthChecks(t *testing.T) {
	mgr := NewStorageManager(NewMaxAvailableStrategy())

	node1, dir1 := createTestNode(t, "node1", "Node 1", 500*1024*1024)
	defer os.RemoveAll(dir1)
	_ = mgr.RegisterNode(node1)

	ctx := context.Background()
	results := mgr.RunHealthChecks(ctx)

	if healthy, exists := results["node1"]; !exists || !healthy {
		t.Errorf("expected node1 to be healthy, got exists=%v, healthy=%v", exists, healthy)
	}
}
