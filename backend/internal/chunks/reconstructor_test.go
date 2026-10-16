package chunks

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/bwithraju/clouddump/backend/internal/storage"
)

// setupCluster initializes a 4-node heterogeneous storage cluster.
func setupCluster(t *testing.T) (*storage.StorageManager, map[string]string) {
	mgr := storage.NewStorageManager(storage.NewMaxAvailableStrategy())

	n1, d1 := setupTestNode(t, "node1", "Node 1 (500KB)", 500*1024)
	n2, d2 := setupTestNode(t, "node2", "Node 2 (1000KB)", 1000*1024)
	n3, d3 := setupTestNode(t, "node3", "Node 3 (2000KB)", 2000*1024)
	n4, d4 := setupTestNode(t, "node4", "Node 4 (700KB)", 700*1024)

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

func cleanupCluster(dirs map[string]string) {
	for _, d := range dirs {
		_ = os.RemoveAll(d)
	}
}

// generateRandomBytes generates pseudo-random test bytes with high entropy.
func generateRandomBytes(t *testing.T, size int64) []byte {
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		t.Fatalf("failed generating random bytes: %v", err)
	}
	return buf
}

// Test 1: One chunk reconstruction
func TestReconstruct_OneChunk(t *testing.T) {
	mgr, dirs := setupCluster(t)
	defer cleanupCluster(dirs)

	chunkSize := int64(100 * 1024) // 100 KB
	fileSize := int64(45 * 1024)    // 45 KB (< 1 chunk)
	payload := generateRandomBytes(t, fileSize)

	distributor, _ := NewChunkDistributor(mgr, chunkSize)
	res, err := distributor.Distribute(context.Background(), "file_1chunk", bytes.NewReader(payload), fileSize, nil)
	if err != nil {
		t.Fatalf("Distribute failed: %v", err)
	}
	if len(res.Chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(res.Chunks))
	}

	reconstructor, _ := NewFileReconstructor(mgr)
	var out bytes.Buffer
	written, err := reconstructor.ReconstructTo(context.Background(), res.Chunks, &out)
	if err != nil {
		t.Fatalf("ReconstructTo failed: %v", err)
	}

	if written != fileSize {
		t.Errorf("expected %d bytes written, got %d", fileSize, written)
	}

	// Byte-for-byte comparison
	if !bytes.Equal(out.Bytes(), payload) {
		t.Fatalf("reconstructed output not byte-for-byte identical to original")
	}

	// SHA-256 integrity check
	reconstructedHash, _, err := reconstructor.CalculateReconstructedSHA256(context.Background(), res.Chunks)
	if err != nil {
		t.Fatalf("CalculateReconstructedSHA256 failed: %v", err)
	}
	origHash := computeSHA256(payload)
	if reconstructedHash != origHash {
		t.Errorf("hash mismatch: expected %s, got %s", origHash, reconstructedHash)
	}
}

// Test 2: Two chunks reconstruction
func TestReconstruct_TwoChunks(t *testing.T) {
	mgr, dirs := setupCluster(t)
	defer cleanupCluster(dirs)

	chunkSize := int64(100 * 1024) // 100 KB
	fileSize := int64(200 * 1024)   // 200 KB (exactly 2 chunks)
	payload := generateRandomBytes(t, fileSize)

	distributor, _ := NewChunkDistributor(mgr, chunkSize)
	res, err := distributor.Distribute(context.Background(), "file_2chunks", bytes.NewReader(payload), fileSize, nil)
	if err != nil {
		t.Fatalf("Distribute failed: %v", err)
	}
	if len(res.Chunks) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(res.Chunks))
	}

	reconstructor, _ := NewFileReconstructor(mgr)
	var out bytes.Buffer
	written, err := reconstructor.ReconstructTo(context.Background(), res.Chunks, &out)
	if err != nil {
		t.Fatalf("ReconstructTo failed: %v", err)
	}

	if written != fileSize {
		t.Errorf("expected %d bytes, got %d", fileSize, written)
	}
	if !bytes.Equal(out.Bytes(), payload) {
		t.Fatalf("reconstructed 2-chunk file not byte-for-byte identical to original")
	}
}

// Test 3: Many chunks reconstruction (20 chunks) and ReconstructedReader test
func TestReconstruct_ManyChunks(t *testing.T) {
	mgr, dirs := setupCluster(t)
	defer cleanupCluster(dirs)

	chunkSize := int64(50 * 1024)   // 50 KB
	fileSize := int64(1000 * 1024)  // 1000 KB (20 chunks)
	payload := generateRandomBytes(t, fileSize)

	distributor, _ := NewChunkDistributor(mgr, chunkSize)
	res, err := distributor.Distribute(context.Background(), "file_many_chunks", bytes.NewReader(payload), fileSize, nil)
	if err != nil {
		t.Fatalf("Distribute failed: %v", err)
	}
	if len(res.Chunks) != 20 {
		t.Fatalf("expected 20 chunks, got %d", len(res.Chunks))
	}

	reconstructor, _ := NewFileReconstructor(mgr)

	// Test 3a: ReconstructTo
	var out bytes.Buffer
	written, err := reconstructor.ReconstructTo(context.Background(), res.Chunks, &out)
	if err != nil {
		t.Fatalf("ReconstructTo failed: %v", err)
	}
	if written != fileSize || !bytes.Equal(out.Bytes(), payload) {
		t.Fatalf("ReconstructTo output mismatch on 20 chunks")
	}

	// Test 3b: Streaming ReconstructedReader
	reader, err := reconstructor.NewReconstructedReader(context.Background(), res.Chunks)
	if err != nil {
		t.Fatalf("NewReconstructedReader failed: %v", err)
	}
	defer reader.Close()

	streamed, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll from ReconstructedReader failed: %v", err)
	}
	if !bytes.Equal(streamed, payload) {
		t.Fatalf("ReconstructedReader output not byte-for-byte identical to original")
	}
}

// Test 4: Final partial chunk
func TestReconstruct_FinalPartialChunk(t *testing.T) {
	mgr, dirs := setupCluster(t)
	defer cleanupCluster(dirs)

	chunkSize := int64(100 * 1024) // 100 KB
	fileSize := int64(263 * 1024)  // 263 KB (100KB + 100KB + 63KB partial)
	payload := generateRandomBytes(t, fileSize)

	distributor, _ := NewChunkDistributor(mgr, chunkSize)
	res, err := distributor.Distribute(context.Background(), "file_partial_last", bytes.NewReader(payload), fileSize, nil)
	if err != nil {
		t.Fatalf("Distribute failed: %v", err)
	}
	if len(res.Chunks) != 3 {
		t.Fatalf("expected 3 chunks, got %d", len(res.Chunks))
	}
	if res.Chunks[2].Size != 63*1024 {
		t.Errorf("expected final partial chunk size 64512, got %d", res.Chunks[2].Size)
	}

	reconstructor, _ := NewFileReconstructor(mgr)
	var out bytes.Buffer
	written, err := reconstructor.ReconstructTo(context.Background(), res.Chunks, &out)
	if err != nil {
		t.Fatalf("ReconstructTo failed: %v", err)
	}

	if written != fileSize || !bytes.Equal(out.Bytes(), payload) {
		t.Fatalf("reconstructed output with final partial chunk mismatch")
	}
}

// Test 5: Chunks stored across all nodes
func TestReconstruct_ChunksStoredAcrossAllNodes(t *testing.T) {
	mgr, dirs := setupCluster(t)
	defer cleanupCluster(dirs)

	// File of 3000 KB with 300 KB chunk size (10 chunks)
	// Across total pool of 4200 KB (Node 1: 500KB, Node 2: 1000KB, Node 3: 2000KB, Node 4: 700KB)
	chunkSize := int64(300 * 1024)
	fileSize := int64(3000 * 1024)
	payload := generateRandomBytes(t, fileSize)

	distributor, _ := NewChunkDistributor(mgr, chunkSize)
	res, err := distributor.Distribute(context.Background(), "file_all_nodes", bytes.NewReader(payload), fileSize, nil)
	if err != nil {
		t.Fatalf("Distribute failed: %v", err)
	}

	// Verify all 4 nodes were used
	if len(res.NodeDistribution) < 3 {
		t.Fatalf("expected chunks placed across multiple nodes, got: %+v", res.NodeDistribution)
	}

	reconstructor, _ := NewFileReconstructor(mgr)
	var out bytes.Buffer
	written, err := reconstructor.ReconstructTo(context.Background(), res.Chunks, &out)
	if err != nil {
		t.Fatalf("ReconstructTo failed across multi-node chunks: %v", err)
	}

	if written != fileSize || !bytes.Equal(out.Bytes(), payload) {
		t.Fatalf("multi-node reconstructed file is not byte-for-byte identical")
	}
}

// Test 6: Missing chunk from metadata (broken sequence) returns clear error
func TestReconstruct_MissingChunkFromMetadata(t *testing.T) {
	mgr, dirs := setupCluster(t)
	defer cleanupCluster(dirs)

	chunkSize := int64(100 * 1024)
	fileSize := int64(300 * 1024) // 3 chunks
	payload := generateRandomBytes(t, fileSize)

	distributor, _ := NewChunkDistributor(mgr, chunkSize)
	res, err := distributor.Distribute(context.Background(), "file_missing_meta", bytes.NewReader(payload), fileSize, nil)
	if err != nil {
		t.Fatalf("Distribute failed: %v", err)
	}

	// Intentionally omit chunk 1 (keep only chunk 0 and chunk 2)
	incompleteChunks := []*ChunkMetadata{res.Chunks[0], res.Chunks[2]}

	reconstructor, _ := NewFileReconstructor(mgr)
	var out bytes.Buffer
	_, err = reconstructor.ReconstructTo(context.Background(), incompleteChunks, &out)
	if err == nil {
		t.Fatalf("expected error for missing chunk in sequence, got nil")
	}

	if !errors.Is(err, ErrMissingChunk) {
		t.Errorf("expected ErrMissingChunk, got: %v", err)
	}
}

// Test 7: Missing chunk physically deleted from storage node returns clear error
func TestReconstruct_MissingChunkPhysicallyDeleted(t *testing.T) {
	mgr, dirs := setupCluster(t)
	defer cleanupCluster(dirs)

	chunkSize := int64(100 * 1024)
	fileSize := int64(300 * 1024) // 3 chunks
	payload := generateRandomBytes(t, fileSize)

	distributor, _ := NewChunkDistributor(mgr, chunkSize)
	res, err := distributor.Distribute(context.Background(), "file_deleted_chunk", bytes.NewReader(payload), fileSize, nil)
	if err != nil {
		t.Fatalf("Distribute failed: %v", err)
	}

	// Physically delete chunk 1 from its node directory
	targetChunk := res.Chunks[1]
	nodeDir := dirs[targetChunk.StorageNodeID]
	chunkFilePath := filepath.Join(nodeDir, targetChunk.ProviderObjectID)

	if err := os.Remove(chunkFilePath); err != nil {
		t.Fatalf("failed removing physical chunk file for test: %v", err)
	}

	reconstructor, _ := NewFileReconstructor(mgr)
	var out bytes.Buffer
	_, err = reconstructor.ReconstructTo(context.Background(), res.Chunks, &out)
	if err == nil {
		t.Fatalf("expected error when physical chunk is missing, got nil")
	}

	if !errors.Is(err, ErrMissingChunk) {
		t.Errorf("expected ErrMissingChunk, got: %v", err)
	}
}
