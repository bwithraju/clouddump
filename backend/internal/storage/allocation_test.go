package storage

import (
	"errors"
	"os"
	"testing"
)

// Test 1: Equal nodes - chunks distribute evenly as capacity reduces
func TestAllocation_EqualNodes(t *testing.T) {
	mgr := NewStorageManager(NewMaxAvailableStrategy())

	nodeA, dirA := createTestNode(t, "nodeA", "Node A", 100*1024*1024)
	defer os.RemoveAll(dirA)
	nodeB, dirB := createTestNode(t, "nodeB", "Node B", 100*1024*1024)
	defer os.RemoveAll(dirB)
	nodeC, dirC := createTestNode(t, "nodeC", "Node C", 100*1024*1024)
	defer os.RemoveAll(dirC)
	nodeD, dirD := createTestNode(t, "nodeD", "Node D", 100*1024*1024)
	defer os.RemoveAll(dirD)

	_ = mgr.RegisterNode(nodeA)
	_ = mgr.RegisterNode(nodeB)
	_ = mgr.RegisterNode(nodeC)
	_ = mgr.RegisterNode(nodeD)

	chunkSize := int64(10 * 1024 * 1024) // 10 MB
	counts := make(map[string]int)

	// Allocate 8 chunks
	for i := 0; i < 8; i++ {
		selected, err := mgr.AllocateAndReserveNode(chunkSize)
		if err != nil {
			t.Fatalf("chunk %d allocation failed: %v", i, err)
		}
		counts[selected.ID]++
	}

	// Each of the 4 equal nodes should receive exactly 2 chunks
	for _, id := range []string{"nodeA", "nodeB", "nodeC", "nodeD"} {
		if counts[id] != 2 {
			t.Errorf("expected node %s to receive 2 chunks, got %d", id, counts[id])
		}
	}
}

// Test 2: Unequal nodes (Node A = 10 GB, Node B = 20 GB, Node C = 5 GB, Node D = 15 GB)
func TestAllocation_UnequalNodes(t *testing.T) {
	mgr := NewStorageManager(NewMaxAvailableStrategy())

	// Scale down by 100 for unit test: 100 MB, 200 MB, 50 MB, 150 MB (proportions: 10, 20, 5, 15)
	nodeA, dirA := createTestNode(t, "nodeA", "Node A", 100*1024*1024)
	defer os.RemoveAll(dirA)
	nodeB, dirB := createTestNode(t, "nodeB", "Node B", 200*1024*1024)
	defer os.RemoveAll(dirB)
	nodeC, dirC := createTestNode(t, "nodeC", "Node C", 50*1024*1024)
	defer os.RemoveAll(dirC)
	nodeD, dirD := createTestNode(t, "nodeD", "Node D", 150*1024*1024)
	defer os.RemoveAll(dirD)

	_ = mgr.RegisterNode(nodeA)
	_ = mgr.RegisterNode(nodeB)
	_ = mgr.RegisterNode(nodeC)
	_ = mgr.RegisterNode(nodeD)

	chunkSize := int64(10 * 1024 * 1024) // 10 MB
	counts := make(map[string]int)

	// Allocate 20 chunks (total 200 MB)
	for i := 0; i < 20; i++ {
		selected, err := mgr.AllocateAndReserveNode(chunkSize)
		if err != nil {
			t.Fatalf("chunk %d allocation failed: %v", i, err)
		}
		counts[selected.ID]++
	}

	// Node B (largest: 200MB) must receive the most chunks
	// Node D (150MB) second most
	// Node A (100MB) third
	// Node C (50MB) least
	t.Logf("Allocation distribution across unequal nodes: %+v", counts)

	if counts["nodeB"] < counts["nodeD"] {
		t.Errorf("nodeB (%d chunks) should have >= nodeD (%d chunks)", counts["nodeB"], counts["nodeD"])
	}
	if counts["nodeD"] < counts["nodeA"] {
		t.Errorf("nodeD (%d chunks) should have >= nodeA (%d chunks)", counts["nodeD"], counts["nodeA"])
	}
	if counts["nodeA"] < counts["nodeC"] {
		t.Errorf("nodeA (%d chunks) should have >= nodeC (%d chunks)", counts["nodeA"], counts["nodeC"])
	}

	// Node C must definitely have fewer chunks than Node B
	if counts["nodeB"] <= counts["nodeC"] {
		t.Errorf("expected nodeB to have significantly more chunks than nodeC, got B=%d, C=%d",
			counts["nodeB"], counts["nodeC"])
	}
}

// Test 3: Node with insufficient space must not receive chunk
func TestAllocation_NodeWithInsufficientSpace(t *testing.T) {
	mgr := NewStorageManager(NewMaxAvailableStrategy())

	// Node small has only 40 MB
	nodeSmall, dirSmall := createTestNode(t, "node_small", "Small Node", 40*1024*1024)
	defer os.RemoveAll(dirSmall)
	// Node large has 200 MB
	nodeLarge, dirLarge := createTestNode(t, "node_large", "Large Node", 200*1024*1024)
	defer os.RemoveAll(dirLarge)

	_ = mgr.RegisterNode(nodeSmall)
	_ = mgr.RegisterNode(nodeLarge)

	chunkSize := int64(50 * 1024 * 1024) // 50 MB (> 40 MB available on node_small)

	// Allocate 3 chunks of 50 MB
	for i := 0; i < 3; i++ {
		selected, err := mgr.AllocateAndReserveNode(chunkSize)
		if err != nil {
			t.Fatalf("allocation %d failed: %v", i, err)
		}
		if selected.ID == "node_small" {
			t.Fatalf("node_small was allocated chunk %d even though its available capacity is less than chunk size", i)
		}
		if selected.ID != "node_large" {
			t.Errorf("expected node_large, got %s", selected.ID)
		}
	}

	// node_small available capacity should still be 40 MB untouched
	stats := nodeSmall.GetStats()
	if stats.UsedCapacity != 0 {
		t.Errorf("expected 0 used capacity on node_small, got %d", stats.UsedCapacity)
	}
}

// Test 4: All nodes full returns clear insufficient-storage error
func TestAllocation_AllNodesFull(t *testing.T) {
	mgr := NewStorageManager(NewMaxAvailableStrategy())

	node1, dir1 := createTestNode(t, "node1", "Node 1", 30*1024*1024)
	defer os.RemoveAll(dir1)
	node2, dir2 := createTestNode(t, "node2", "Node 2", 20*1024*1024)
	defer os.RemoveAll(dir2)

	_ = mgr.RegisterNode(node1)
	_ = mgr.RegisterNode(node2)

	// Consume all space on both nodes
	_ = node1.ReserveCapacity(30 * 1024 * 1024)
	_ = node2.ReserveCapacity(20 * 1024 * 1024)

	// Now try to allocate a 5 MB chunk
	chunkSize := int64(5 * 1024 * 1024)
	_, err := mgr.AllocateAndReserveNode(chunkSize)
	if err == nil {
		t.Fatalf("expected error when all nodes full, got nil")
	}

	if !errors.Is(err, ErrInsufficientNodeCapacity) && !errors.Is(err, ErrNoAvailableNodes) {
		t.Errorf("expected ErrInsufficientNodeCapacity or ErrNoAvailableNodes, got: %v", err)
	}
}

// Test 5: Offline node is skipped even if it has massive capacity
func TestAllocation_OfflineNodeSkipped(t *testing.T) {
	mgr := NewStorageManager(NewMaxAvailableStrategy())

	// Giant node (10 GB) but offline
	nodeGiant, dirGiant := createTestNode(t, "node_giant", "Giant Offline Node", 10*1024*1024*1024)
	defer os.RemoveAll(dirGiant)
	nodeGiant.mu.Lock()
	nodeGiant.Status = NodeStatusOffline
	nodeGiant.mu.Unlock()

	// Modest node (100 MB) online
	nodeModest, dirModest := createTestNode(t, "node_modest", "Modest Online Node", 100*1024*1024)
	defer os.RemoveAll(dirModest)

	_ = mgr.RegisterNode(nodeGiant)
	_ = mgr.RegisterNode(nodeModest)

	chunkSize := int64(10 * 1024 * 1024) // 10 MB
	selected, err := mgr.AllocateAndReserveNode(chunkSize)
	if err != nil {
		t.Fatalf("failed to allocate chunk: %v", err)
	}

	if selected.ID != "node_modest" {
		t.Errorf("expected online node_modest to be selected, but offline node_giant was picked: %s", selected.ID)
	}
}

// Test 6: Multiple chunks allocation with batch planning
func TestAllocation_MultipleChunksBatch(t *testing.T) {
	mgr := NewStorageManager(NewMaxAvailableStrategy())

	node1, dir1 := createTestNode(t, "node1", "Node 1", 100*1024*1024)
	defer os.RemoveAll(dir1)
	node2, dir2 := createTestNode(t, "node2", "Node 2", 100*1024*1024)
	defer os.RemoveAll(dir2)

	_ = mgr.RegisterNode(node1)
	_ = mgr.RegisterNode(node2)

	// Batch of 6 chunks of 25 MB each (total 150 MB across 200 MB pool)
	chunkSizes := []int64{
		25 * 1024 * 1024,
		25 * 1024 * 1024,
		25 * 1024 * 1024,
		25 * 1024 * 1024,
		25 * 1024 * 1024,
		25 * 1024 * 1024,
	}

	allocatedNodes, err := mgr.AllocateBatch(chunkSizes)
	if err != nil {
		t.Fatalf("AllocateBatch failed: %v", err)
	}

	if len(allocatedNodes) != len(chunkSizes) {
		t.Fatalf("expected %d allocated nodes, got %d", len(chunkSizes), len(allocatedNodes))
	}

	// Verify each node has 75 MB reserved and 25 MB available
	stats1 := node1.GetStats()
	stats2 := node2.GetStats()

	if stats1.UsedCapacity != 75*1024*1024 || stats2.UsedCapacity != 75*1024*1024 {
		t.Errorf("batch allocation unequal: node1 used=%d, node2 used=%d", stats1.UsedCapacity, stats2.UsedCapacity)
	}
}

// Test 7: Exact capacity boundary
func TestAllocation_ExactCapacityBoundary(t *testing.T) {
	mgr := NewStorageManager(NewMaxAvailableStrategy())

	// Node with exactly 256 MB capacity
	exactCapacity := int64(256 * 1024 * 1024)
	node, dir := createTestNode(t, "node_exact", "Exact Node", exactCapacity)
	defer os.RemoveAll(dir)
	_ = mgr.RegisterNode(node)

	// Allocate exact 256 MB chunk
	chunkSize := exactCapacity
	selected, err := mgr.AllocateAndReserveNode(chunkSize)
	if err != nil {
		t.Fatalf("allocation at exact capacity boundary failed: %v", err)
	}

	if selected.ID != "node_exact" {
		t.Errorf("unexpected selected node: %s", selected.ID)
	}

	// Available capacity should now be exactly 0
	stats := node.GetStats()
	if stats.AvailableCapacity != 0 {
		t.Errorf("expected available capacity 0, got %d", stats.AvailableCapacity)
	}
	if stats.UsedCapacity != exactCapacity {
		t.Errorf("expected used capacity %d, got %d", exactCapacity, stats.UsedCapacity)
	}

	// Any subsequent allocation must fail with insufficient capacity
	_, err = mgr.AllocateAndReserveNode(1)
	if err == nil {
		t.Fatalf("expected failure when attempting to allocate to full node, got nil")
	}
	if !errors.Is(err, ErrInsufficientNodeCapacity) && !errors.Is(err, ErrNoAvailableNodes) {
		t.Errorf("expected ErrInsufficientNodeCapacity or ErrNoAvailableNodes, got %v", err)
	}
}
