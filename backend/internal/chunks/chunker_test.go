package chunks

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/bwithraju/clouddump/backend/internal/providers"
	"github.com/bwithraju/clouddump/backend/internal/storage"
)

// setupTestNode initializes a temporary local directory and wraps it in a StorageNode.
func setupTestNode(t *testing.T, id, name string, capacity int64) (*storage.StorageNode, string) {
	tempDir, err := os.MkdirTemp("", fmt.Sprintf("chunk_test_node_%s_*", id))
	if err != nil {
		t.Fatalf("failed to create temp dir for node %s: %v", id, err)
	}

	provider, err := providers.NewLocalStorageProvider(tempDir, capacity)
	if err != nil {
		t.Fatalf("failed to create provider for node %s: %v", id, err)
	}

	node, err := storage.NewStorageNode(id, name, provider)
	if err != nil {
		t.Fatalf("failed to create storage node %s: %v", id, err)
	}

	return node, tempDir
}

// computeSHA256 returns the hex-encoded SHA-256 hash of a byte slice.
func computeSHA256(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

// Test 1: File smaller than chunk size
func TestChunker_FileSmallerThanChunkSize(t *testing.T) {
	mgr := storage.NewStorageManager(storage.NewMaxAvailableStrategy())
	node, dir := setupTestNode(t, "node1", "Node 1", 10*1024*1024)
	defer os.RemoveAll(dir)
	_ = mgr.RegisterNode(node)

	chunkSize := int64(256 * 1024) // 256 KB
	chunker, err := NewChunker(mgr, chunkSize)
	if err != nil {
		t.Fatalf("NewChunker failed: %v", err)
	}

	fileSize := int64(50 * 1024) // 50 KB (< 256 KB)
	payload := bytes.Repeat([]byte("A"), int(fileSize))
	expectedChecksum := computeSHA256(payload)

	chunks, err := chunker.StreamAndDistribute(context.Background(), "file_small", bytes.NewReader(payload), fileSize, nil)
	if err != nil {
		t.Fatalf("StreamAndDistribute failed: %v", err)
	}

	if len(chunks) != 1 {
		t.Fatalf("expected exactly 1 chunk, got %d", len(chunks))
	}

	chunk := chunks[0]
	if chunk.ChunkIndex != 0 {
		t.Errorf("expected chunk index 0, got %d", chunk.ChunkIndex)
	}
	if chunk.Size != fileSize {
		t.Errorf("expected chunk size %d, got %d", fileSize, chunk.Size)
	}
	if chunk.Checksum != expectedChecksum {
		t.Errorf("checksum mismatch: expected %s, got %s", expectedChecksum, chunk.Checksum)
	}
	if chunk.Status != ChunkStatusStored {
		t.Errorf("expected status %s, got %s", ChunkStatusStored, chunk.Status)
	}
}

// Test 2: File exact chunk size
func TestChunker_ExactChunkSize(t *testing.T) {
	mgr := storage.NewStorageManager(storage.NewMaxAvailableStrategy())
	node, dir := setupTestNode(t, "node1", "Node 1", 10*1024*1024)
	defer os.RemoveAll(dir)
	_ = mgr.RegisterNode(node)

	chunkSize := int64(100 * 1024) // 100 KB
	chunker, _ := NewChunker(mgr, chunkSize)

	fileSize := int64(100 * 1024) // Exactly 100 KB
	payload := bytes.Repeat([]byte("B"), int(fileSize))
	expectedChecksum := computeSHA256(payload)

	chunks, err := chunker.StreamAndDistribute(context.Background(), "file_exact", bytes.NewReader(payload), fileSize, nil)
	if err != nil {
		t.Fatalf("StreamAndDistribute failed: %v", err)
	}

	if len(chunks) != 1 {
		t.Fatalf("expected exactly 1 chunk for exact chunk size, got %d", len(chunks))
	}
	if chunks[0].Size != chunkSize {
		t.Errorf("expected chunk size %d, got %d", chunkSize, chunks[0].Size)
	}
	if chunks[0].Checksum != expectedChecksum {
		t.Errorf("checksum mismatch: expected %s, got %s", expectedChecksum, chunks[0].Checksum)
	}
}

// Test 3: File larger than one chunk
func TestChunker_FileLargerThanOneChunk(t *testing.T) {
	mgr := storage.NewStorageManager(storage.NewMaxAvailableStrategy())
	node, dir := setupTestNode(t, "node1", "Node 1", 10*1024*1024)
	defer os.RemoveAll(dir)
	_ = mgr.RegisterNode(node)

	chunkSize := int64(100 * 1024) // 100 KB
	chunker, _ := NewChunker(mgr, chunkSize)

	fileSize := int64(200 * 1024) // Exactly 200 KB (2 chunks)
	part1 := bytes.Repeat([]byte("C"), 100*1024)
	part2 := bytes.Repeat([]byte("D"), 100*1024)
	fullPayload := append(part1, part2...)

	chunks, err := chunker.StreamAndDistribute(context.Background(), "file_two_chunks", bytes.NewReader(fullPayload), fileSize, nil)
	if err != nil {
		t.Fatalf("StreamAndDistribute failed: %v", err)
	}

	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(chunks))
	}

	if chunks[0].Size != 100*1024 || chunks[0].Checksum != computeSHA256(part1) {
		t.Errorf("chunk 0 metadata incorrect: size=%d, checksum=%s", chunks[0].Size, chunks[0].Checksum)
	}
	if chunks[1].Size != 100*1024 || chunks[1].Checksum != computeSHA256(part2) {
		t.Errorf("chunk 1 metadata incorrect: size=%d, checksum=%s", chunks[1].Size, chunks[1].Checksum)
	}
}

// Test 4: Final partial chunk
func TestChunker_FinalPartialChunk(t *testing.T) {
	mgr := storage.NewStorageManager(storage.NewMaxAvailableStrategy())
	node, dir := setupTestNode(t, "node1", "Node 1", 10*1024*1024)
	defer os.RemoveAll(dir)
	_ = mgr.RegisterNode(node)

	chunkSize := int64(100 * 1024) // 100 KB
	chunker, _ := NewChunker(mgr, chunkSize)

	fileSize := int64(250 * 1024) // 250 KB (100 KB + 100 KB + 50 KB partial)
	p1 := bytes.Repeat([]byte("1"), 100*1024)
	p2 := bytes.Repeat([]byte("2"), 100*1024)
	p3 := bytes.Repeat([]byte("3"), 50*1024)
	fullPayload := append(append(p1, p2...), p3...)

	chunks, err := chunker.StreamAndDistribute(context.Background(), "file_partial_last", bytes.NewReader(fullPayload), fileSize, nil)
	if err != nil {
		t.Fatalf("StreamAndDistribute failed: %v", err)
	}

	if len(chunks) != 3 {
		t.Fatalf("expected 3 chunks, got %d", len(chunks))
	}

	// Verify first two full chunks
	if chunks[0].Size != 100*1024 || chunks[1].Size != 100*1024 {
		t.Errorf("full chunk sizes incorrect: chunk0=%d, chunk1=%d", chunks[0].Size, chunks[1].Size)
	}
	// Verify final partial chunk
	if chunks[2].Size != 50*1024 {
		t.Errorf("final partial chunk size expected 51200, got %d", chunks[2].Size)
	}
	if chunks[2].Checksum != computeSHA256(p3) {
		t.Errorf("final partial chunk checksum mismatch: expected %s, got %s", computeSHA256(p3), chunks[2].Checksum)
	}
}

// Test 5: Insufficient storage across pool
func TestChunker_InsufficientStorage(t *testing.T) {
	mgr := storage.NewStorageManager(storage.NewMaxAvailableStrategy())
	// Node has only 150 KB capacity
	node, dir := setupTestNode(t, "node_small", "Small Node", 150*1024)
	defer os.RemoveAll(dir)
	_ = mgr.RegisterNode(node)

	chunker, _ := NewChunker(mgr, 50*1024)

	// Attempt to upload 300 KB (> 150 KB pool capacity)
	fileSize := int64(300 * 1024)
	payload := bytes.Repeat([]byte("X"), int(fileSize))

	_, err := chunker.StreamAndDistribute(context.Background(), "file_overflow", bytes.NewReader(payload), fileSize, nil)
	if !errors.Is(err, ErrInsufficientPoolStorage) {
		t.Fatalf("expected ErrInsufficientPoolStorage, got: %v", err)
	}

	// Verify node capacity was not consumed
	stats := mgr.GetPoolStats(context.Background())
	if stats.UsedCapacity != 0 {
		t.Errorf("expected 0 used capacity after pre-flight rejection, got %d", stats.UsedCapacity)
	}
}

// Test 6: Multiple nodes with different capacities (500 KB, 1000 KB, 2000 KB, 700 KB)
func TestChunker_MultipleNodesWithDifferentCapacities(t *testing.T) {
	mgr := storage.NewStorageManager(storage.NewMaxAvailableStrategy())

	node1, dir1 := setupTestNode(t, "node1", "Node 1", 500*1024)
	defer os.RemoveAll(dir1)
	node2, dir2 := setupTestNode(t, "node2", "Node 2", 1000*1024)
	defer os.RemoveAll(dir2)
	node3, dir3 := setupTestNode(t, "node3", "Node 3", 2000*1024)
	defer os.RemoveAll(dir3)
	node4, dir4 := setupTestNode(t, "node4", "Node 4", 700*1024)
	defer os.RemoveAll(dir4)

	_ = mgr.RegisterNode(node1)
	_ = mgr.RegisterNode(node2)
	_ = mgr.RegisterNode(node3)
	_ = mgr.RegisterNode(node4)

	// Total pool: 4200 KB
	// Single file: 3000 KB (> 2000 KB largest single node!)
	// Chunk size: 300 KB -> 10 chunks of 300 KB
	chunkSize := int64(300 * 1024)
	chunker, _ := NewChunker(mgr, chunkSize)

	fileSize := int64(3000 * 1024)
	payload := bytes.Repeat([]byte("Z"), int(fileSize))

	nodeChunkCounts := make(map[string]int)
	callback := func(chunk *ChunkMetadata, current, total int, uploaded, totalB int64) {
		nodeChunkCounts[chunk.StorageNodeID]++
	}

	chunks, err := chunker.StreamAndDistribute(context.Background(), "file_distributed", bytes.NewReader(payload), fileSize, callback)
	if err != nil {
		t.Fatalf("StreamAndDistribute across multiple nodes failed: %v", err)
	}

	if len(chunks) != 10 {
		t.Fatalf("expected 10 chunks, got %d", len(chunks))
	}

	// Verify chunks are distributed across multiple distinct nodes
	if len(nodeChunkCounts) < 3 {
		t.Errorf("expected chunks to be distributed across at least 3 nodes, but only used %d nodes: %+v",
			len(nodeChunkCounts), nodeChunkCounts)
	}

	// Verify no node's capacity was exceeded
	for _, n := range []*storage.StorageNode{node1, node2, node3, node4} {
		stats := n.GetStats()
		if stats.UsedCapacity > stats.TotalCapacity {
			t.Errorf("node %s exceeded capacity: used %d > total %d", n.ID, stats.UsedCapacity, stats.TotalCapacity)
		}
		if stats.AvailableCapacity < 0 {
			t.Errorf("node %s has negative available capacity: %d", n.ID, stats.AvailableCapacity)
		}
	}

	// Verify pool metrics after upload
	poolStats := mgr.GetPoolStats(context.Background())
	if poolStats.UsedCapacity != fileSize {
		t.Errorf("expected pool used capacity %d, got %d", fileSize, poolStats.UsedCapacity)
	}
	expectedAvailable := int64(4200*1024) - fileSize
	if poolStats.AvailableCapacity != expectedAvailable {
		t.Errorf("expected pool available %d, got %d", expectedAvailable, poolStats.AvailableCapacity)
	}
}

// Test 7: Rollback on canceled context midway
func TestChunker_RollbackOnCancellation(t *testing.T) {
	mgr := storage.NewStorageManager(storage.NewMaxAvailableStrategy())
	node, dir := setupTestNode(t, "node1", "Node 1", 10*1024*1024)
	defer os.RemoveAll(dir)
	_ = mgr.RegisterNode(node)

	chunker, _ := NewChunker(mgr, 100*1024)

	ctx, cancel := context.WithCancel(context.Background())

	fileSize := int64(500 * 1024)
	payload := bytes.Repeat([]byte("R"), int(fileSize))

	callback := func(chunk *ChunkMetadata, current, total int, uploaded, totalB int64) {
		if current == 2 {
			cancel() // cancel after 2nd chunk
		}
	}

	_, err := chunker.StreamAndDistribute(ctx, "file_canceled", bytes.NewReader(payload), fileSize, callback)
	if err == nil {
		t.Fatalf("expected error on cancellation, got nil")
	}

	// Verify rollback cleaned up all chunks on node
	stats := mgr.GetPoolStats(context.Background())
	if stats.UsedCapacity != 0 {
		t.Errorf("expected 0 used capacity after rollback, got %d", stats.UsedCapacity)
	}
}
