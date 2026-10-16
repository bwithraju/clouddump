package chunks

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/bwithraju/clouddump/backend/internal/storage"
)

func TestChunkDistributor_PhysicalPlacementAndMapping(t *testing.T) {
	mgr := storage.NewStorageManager(storage.NewMaxAvailableStrategy())

	// 1. Create 4 local nodes with different capacities:
	// Node 1: 500 KB
	// Node 2: 1000 KB
	// Node 3: 2000 KB
	// Node 4: 700 KB
	// Total pool: 4200 KB
	node1, dir1 := setupTestNode(t, "node1", "Node 1 (500KB)", 500*1024)
	defer os.RemoveAll(dir1)
	node2, dir2 := setupTestNode(t, "node2", "Node 2 (1000KB)", 1000*1024)
	defer os.RemoveAll(dir2)
	node3, dir3 := setupTestNode(t, "node3", "Node 3 (2000KB)", 2000*1024)
	defer os.RemoveAll(dir3)
	node4, dir4 := setupTestNode(t, "node4", "Node 4 (700KB)", 700*1024)
	defer os.RemoveAll(dir4)

	_ = mgr.RegisterNode(node1)
	_ = mgr.RegisterNode(node2)
	_ = mgr.RegisterNode(node3)
	_ = mgr.RegisterNode(node4)

	nodeDirs := map[string]string{
		"node1": dir1,
		"node2": dir2,
		"node3": dir3,
		"node4": dir4,
	}

	// 2. Setup distributor with chunk size of 250 KB
	chunkSize := int64(250 * 1024)
	distributor, err := NewChunkDistributor(mgr, chunkSize)
	if err != nil {
		t.Fatalf("NewChunkDistributor failed: %v", err)
	}

	// 3. Upload a 2500 KB file (10 chunks of 250 KB)
	fileSize := int64(2500 * 1024)
	payload := bytes.Repeat([]byte("M"), int(fileSize))
	fileID := "test_dist_file_001"

	ctx := context.Background()
	result, err := distributor.Distribute(ctx, fileID, bytes.NewReader(payload), fileSize, nil)
	if err != nil {
		t.Fatalf("Distribute failed: %v", err)
	}

	// 4. Verify result metadata
	if result.Status != FileStatusCompleted {
		t.Errorf("expected status %s, got %s", FileStatusCompleted, result.Status)
	}
	if result.TotalChunks != 10 {
		t.Errorf("expected 10 chunks, got %d", result.TotalChunks)
	}
	if len(result.Chunks) != 10 {
		t.Errorf("expected 10 chunks in slice, got %d", len(result.Chunks))
	}

	// 5. Verify mapping: chunk_index -> storage_node -> object_id
	if len(result.Mapping) != 10 {
		t.Fatalf("expected mapping for 10 chunks, got %d", len(result.Mapping))
	}

	for i := 0; i < 10; i++ {
		nodeMap, exists := result.Mapping[i]
		if !exists {
			t.Fatalf("missing mapping for chunk_index %d", i)
		}
		if len(nodeMap) != 1 {
			t.Fatalf("chunk_index %d should map to exactly 1 node, got %d", i, len(nodeMap))
		}

		for nodeID, objectID := range nodeMap {
			nodeDir, ok := nodeDirs[nodeID]
			if !ok {
				t.Fatalf("unknown nodeID %s in mapping", nodeID)
			}

			// 6. Verify chunk physically exists on disk under that node's directory
			expectedPhysicalPath := filepath.Join(nodeDir, objectID)
			info, statErr := os.Stat(expectedPhysicalPath)
			if statErr != nil {
				t.Fatalf("chunk file physically missing on disk at %s: %v", expectedPhysicalPath, statErr)
			}
			if info.Size() != chunkSize {
				t.Errorf("chunk on disk has unexpected size: expected %d, got %d", chunkSize, info.Size())
			}
		}
	}

	// 7. Verify allocation matches available capacity
	// Node 3 (2000 KB) should hold the most chunks
	// Node 1 (500 KB) should hold the fewest chunks
	t.Logf("Node chunk distribution: %+v", result.NodeDistribution)

	if result.NodeDistribution["node3"] <= result.NodeDistribution["node1"] {
		t.Errorf("expected node3 (2000KB) to hold more chunks than node1 (500KB), got node3=%d, node1=%d",
			result.NodeDistribution["node3"], result.NodeDistribution["node1"])
	}

	// Verify no node capacity was exceeded
	for _, n := range []*storage.StorageNode{node1, node2, node3, node4} {
		stats := n.GetStats()
		if stats.UsedCapacity > stats.TotalCapacity {
			t.Errorf("node %s exceeded total capacity: used=%d, total=%d", n.ID, stats.UsedCapacity, stats.TotalCapacity)
		}
		if stats.AvailableCapacity < 0 {
			t.Errorf("node %s has negative available capacity: %d", n.ID, stats.AvailableCapacity)
		}
	}
}

func TestChunkDistributor_PartialFailureRollback(t *testing.T) {
	mgr := storage.NewStorageManager(storage.NewMaxAvailableStrategy())

	node1, dir1 := setupTestNode(t, "node1", "Node 1", 2000*1024)
	defer os.RemoveAll(dir1)
	_ = mgr.RegisterNode(node1)

	chunkSize := int64(100 * 1024)
	distributor, _ := NewChunkDistributor(mgr, chunkSize)

	// File of 500 KB (5 chunks)
	fileSize := int64(500 * 1024)
	payload := bytes.Repeat([]byte("F"), int(fileSize))

	ctx, cancel := context.WithCancel(context.Background())

	// Cancel context after 2nd chunk
	callback := func(chunk *ChunkMetadata, current, total int, uploaded, totalB int64) {
		if current == 2 {
			cancel()
		}
	}

	_, err := distributor.Distribute(ctx, "fail_file", bytes.NewReader(payload), fileSize, callback)
	if err == nil {
		t.Fatalf("expected error on canceled distribution, got nil")
	}

	// Verify rollback: no chunk files should remain in dir1
	entries, err := os.ReadDir(dir1)
	if err != nil {
		t.Fatalf("failed reading node directory: %v", err)
	}

	chunkFileCount := 0
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".chunk" {
			chunkFileCount++
		}
	}

	if chunkFileCount != 0 {
		t.Errorf("expected 0 chunk files after rollback, found %d lingering chunk files", chunkFileCount)
	}
}
