package storage

import (
	"bytes"
	"context"
	"errors"
	"os"
	"testing"
)

// TestStorage_MissingNodeEdgeCases verifies behavior when querying or acting on non-existent nodes.
func TestStorage_MissingNodeEdgeCases(t *testing.T) {
	mgr := NewStorageManager(NewMaxAvailableStrategy())

	// 1. Get non-existent node
	_, err := mgr.GetNode("ghost_node_123")
	if !errors.Is(err, ErrNodeNotFound) {
		t.Fatalf("expected ErrNodeNotFound, got: %v", err)
	}

	// 2. Unregister non-existent node
	err = mgr.UnregisterNode("ghost_node_123")
	if !errors.Is(err, ErrNodeNotFound) {
		t.Fatalf("expected ErrNodeNotFound on unregister, got: %v", err)
	}

	// 3. Store chunk on non-existent node
	_, _, err = mgr.StoreChunk(context.Background(), "ghost_node_123", "chunk_1", bytes.NewReader([]byte("test")), 4)
	if !errors.Is(err, ErrNodeNotFound) {
		t.Fatalf("expected ErrNodeNotFound on StoreChunk, got: %v", err)
	}

	// 4. Retrieve chunk from non-existent node
	_, err = mgr.GetChunk(context.Background(), "ghost_node_123", "chunk_1")
	if !errors.Is(err, ErrNodeNotFound) {
		t.Fatalf("expected ErrNodeNotFound on GetChunk, got: %v", err)
	}

	// 5. Delete chunk on non-existent node
	err = mgr.DeleteChunk(context.Background(), "ghost_node_123", "chunk_1")
	if !errors.Is(err, ErrNodeNotFound) {
		t.Fatalf("expected ErrNodeNotFound on DeleteChunk, got: %v", err)
	}
}

// TestStorage_InsufficientCapacityAndAllocationEdgeCases verifies node selection under tight capacity constraints.
func TestStorage_InsufficientCapacityAndAllocationEdgeCases(t *testing.T) {
	mgr := NewStorageManager(NewMaxAvailableStrategy())

	node1, dir1 := createTestNode(t, "n1", "Node 1", 100*1024) // 100 KB
	defer os.RemoveAll(dir1)
	node2, dir2 := createTestNode(t, "n2", "Node 2", 200*1024) // 200 KB
	defer os.RemoveAll(dir2)

	_ = mgr.RegisterNode(node1)
	_ = mgr.RegisterNode(node2)

	// 1. Requesting chunk larger than any node capacity (300 KB > 200 KB)
	_, err := mgr.SelectNodeForChunk(300 * 1024)
	if !errors.Is(err, ErrNoAvailableNodes) {
		t.Fatalf("expected ErrNoAvailableNodes for chunk larger than any node, got: %v", err)
	}

	// 2. Requesting 150 KB chunk: Node 1 (100 KB) cannot fit it, Node 2 (200 KB) must be chosen
	selected, err := mgr.SelectNodeForChunk(150 * 1024)
	if err != nil {
		t.Fatalf("SelectNodeForChunk failed: %v", err)
	}
	if selected.ID != "n2" {
		t.Errorf("expected node n2 to be selected, got %s", selected.ID)
	}

	// 3. Mark Node 2 OFFLINE -> only Node 1 remains (100 KB capacity)
	node2.SetSimulatedOffline(true)
	_, err = mgr.SelectNodeForChunk(150 * 1024)
	if !errors.Is(err, ErrNoAvailableNodes) {
		t.Fatalf("expected ErrNoAvailableNodes when only node capable of storing is offline, got: %v", err)
	}

	// 4. Mark all nodes OFFLINE -> any allocation fails
	node1.SetSimulatedOffline(true)
	_, err = mgr.SelectNodeForChunk(10 * 1024)
	if !errors.Is(err, ErrNoAvailableNodes) {
		t.Fatalf("expected ErrNoAvailableNodes when all nodes are offline, got: %v", err)
	}
}

// TestStorage_ExclusionAndTiedCapacities verifies allocation when nodes are excluded (replication) or have tied capacity.
func TestStorage_ExclusionAndTiedCapacities(t *testing.T) {
	mgr := NewStorageManager(NewMaxAvailableStrategy())

	// Two nodes with identical capacity (500 KB)
	nodeA, dirA := createTestNode(t, "nodeA", "Node A", 500*1024)
	defer os.RemoveAll(dirA)
	nodeB, dirB := createTestNode(t, "nodeB", "Node B", 500*1024)
	defer os.RemoveAll(dirB)

	_ = mgr.RegisterNode(nodeA)
	_ = mgr.RegisterNode(nodeB)

	// Exclude nodeA (e.g. for replica placement) -> nodeB must be selected
	selected, err := mgr.SelectNodeForChunk(100*1024, "nodeA")
	if err != nil {
		t.Fatalf("SelectNodeForChunk failed with exclusion: %v", err)
	}
	if selected.ID != "nodeB" {
		t.Errorf("expected nodeB when nodeA is excluded, got %s", selected.ID)
	}

	// Exclude both nodes -> no suitable node
	_, err = mgr.SelectNodeForChunk(100*1024, "nodeA", "nodeB")
	if !errors.Is(err, ErrNoAvailableNodes) {
		t.Fatalf("expected ErrNoAvailableNodes when all nodes are excluded, got: %v", err)
	}
}

// TestStorage_CapacityAccountingAndRefund verifies capacity deductions and refunds after chunk storage and deletion.
func TestStorage_CapacityAccountingAndRefund(t *testing.T) {
	mgr := NewStorageManager(NewMaxAvailableStrategy())

	node, dir := createTestNode(t, "n_acct", "Accounting Node", 1000*1024)
	defer os.RemoveAll(dir)
	_ = mgr.RegisterNode(node)

	ctx := context.Background()
	chunkData := bytes.Repeat([]byte("M"), 250*1024) // 250 KB

	objID, written, err := mgr.StoreChunk(ctx, "n_acct", "chktestrefundchunk", bytes.NewReader(chunkData), int64(len(chunkData)))
	if err != nil {
		t.Fatalf("StoreChunk failed: %v", err)
	}
	if written != int64(len(chunkData)) {
		t.Errorf("expected written %d, got %d", len(chunkData), written)
	}

	stats := node.GetStats()
	if stats.UsedCapacity != 250*1024 {
		t.Errorf("expected 250 KB used, got %d", stats.UsedCapacity)
	}
	if stats.AvailableCapacity != 750*1024 {
		t.Errorf("expected 750 KB available, got %d", stats.AvailableCapacity)
	}

	// Delete chunk and verify capacity is restored
	err = mgr.DeleteChunk(ctx, "n_acct", objID)
	if err != nil {
		t.Fatalf("DeleteChunk failed: %v", err)
	}

	afterStats := node.GetStats()
	if afterStats.UsedCapacity != 0 {
		t.Errorf("expected 0 used after deletion, got %d", afterStats.UsedCapacity)
	}
	if afterStats.AvailableCapacity != 1000*1024 {
		t.Errorf("expected 1000 KB available after deletion, got %d", afterStats.AvailableCapacity)
	}
}
