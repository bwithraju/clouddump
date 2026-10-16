package chunks

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// Test 1: Deliberately modify a byte in a chunk file on disk after upload
func TestIntegrity_DeliberateByteModification(t *testing.T) {
	mgr, dirs := setupCluster(t)
	defer cleanupCluster(dirs)

	chunkSize := int64(100 * 1024) // 100 KB
	fileSize := int64(500 * 1024)   // 500 KB (5 chunks)
	payload := generateRandomBytes(t, fileSize)

	distributor, _ := NewChunkDistributor(mgr, chunkSize)
	res, err := distributor.Distribute(context.Background(), "file_integrity_mod", bytes.NewReader(payload), fileSize, nil)
	if err != nil {
		t.Fatalf("Distribute failed: %v", err)
	}

	// Pick chunk index 2 to deliberately corrupt
	targetChunk := res.Chunks[2]
	nodeDir := dirs[targetChunk.StorageNodeID]
	chunkPath := filepath.Join(nodeDir, targetChunk.ProviderObjectID)

	originalChunkBytes, err := os.ReadFile(chunkPath)
	if err != nil {
		t.Fatalf("failed to read physical chunk file: %v", err)
	}

	// Flip a single byte in the middle of the chunk
	corruptedChunkBytes := make([]byte, len(originalChunkBytes))
	copy(corruptedChunkBytes, originalChunkBytes)
	corruptedChunkBytes[500] ^= 0xFF

	if err := os.WriteFile(chunkPath, corruptedChunkBytes, 0644); err != nil {
		t.Fatalf("failed to overwrite physical chunk with corrupted data: %v", err)
	}

	// Now attempt file reconstruction
	reconstructor, _ := NewFileReconstructor(mgr)
	var out bytes.Buffer
	_, err = reconstructor.ReconstructTo(context.Background(), res.Chunks, &out)

	// Must fail with ErrChunkCorrupted
	if err == nil {
		t.Fatalf("expected reconstruction to fail with corruption error, but it succeeded silently")
	}

	if !errors.Is(err, ErrChunkCorrupted) {
		t.Errorf("expected ErrChunkCorrupted, got: %v", err)
	}

	// Chunk status should now be marked ChunkStatusCorrupted
	if targetChunk.Status != ChunkStatusCorrupted {
		t.Errorf("expected chunk status %s, got %s", ChunkStatusCorrupted, targetChunk.Status)
	}
}

// Test 2: Deliberately truncate chunk on disk
func TestIntegrity_DeliberateTruncation(t *testing.T) {
	mgr, dirs := setupCluster(t)
	defer cleanupCluster(dirs)

	chunkSize := int64(100 * 1024)
	fileSize := int64(300 * 1024) // 3 chunks
	payload := generateRandomBytes(t, fileSize)

	distributor, _ := NewChunkDistributor(mgr, chunkSize)
	res, err := distributor.Distribute(context.Background(), "file_integrity_trunc", bytes.NewReader(payload), fileSize, nil)
	if err != nil {
		t.Fatalf("Distribute failed: %v", err)
	}

	// Truncate chunk 1 by 1024 bytes
	targetChunk := res.Chunks[1]
	nodeDir := dirs[targetChunk.StorageNodeID]
	chunkPath := filepath.Join(nodeDir, targetChunk.ProviderObjectID)

	origBytes, _ := os.ReadFile(chunkPath)
	truncatedBytes := origBytes[:len(origBytes)-1024]
	_ = os.WriteFile(chunkPath, truncatedBytes, 0644)

	reconstructor, _ := NewFileReconstructor(mgr)
	var out bytes.Buffer
	_, err = reconstructor.ReconstructTo(context.Background(), res.Chunks, &out)

	if err == nil {
		t.Fatalf("expected error for truncated chunk, got nil")
	}

	if !errors.Is(err, ErrChunkCorrupted) {
		t.Errorf("expected ErrChunkCorrupted for truncated chunk, got: %v", err)
	}
}

// Test 3: Deliberately append garbage data to chunk on disk
func TestIntegrity_DeliberateGarbageAppend(t *testing.T) {
	mgr, dirs := setupCluster(t)
	defer cleanupCluster(dirs)

	chunkSize := int64(100 * 1024)
	fileSize := int64(200 * 1024) // 2 chunks
	payload := generateRandomBytes(t, fileSize)

	distributor, _ := NewChunkDistributor(mgr, chunkSize)
	res, err := distributor.Distribute(context.Background(), "file_integrity_append", bytes.NewReader(payload), fileSize, nil)
	if err != nil {
		t.Fatalf("Distribute failed: %v", err)
	}

	// Append 128 bytes of garbage to chunk 0
	targetChunk := res.Chunks[0]
	nodeDir := dirs[targetChunk.StorageNodeID]
	chunkPath := filepath.Join(nodeDir, targetChunk.ProviderObjectID)

	f, err := os.OpenFile(chunkPath, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatalf("failed opening chunk for append: %v", err)
	}
	_, _ = f.Write([]byte("MALICIOUS_EXTRA_DATA_INJECTED_INTO_CHUNK"))
	_ = f.Close()

	reconstructor, _ := NewFileReconstructor(mgr)
	var out bytes.Buffer
	_, err = reconstructor.ReconstructTo(context.Background(), res.Chunks, &out)

	if err == nil {
		t.Fatalf("expected error for oversized modified chunk, got nil")
	}
	if !errors.Is(err, ErrChunkCorrupted) {
		t.Errorf("expected ErrChunkCorrupted, got: %v", err)
	}
}

// Test 4: ReconstructedReader streaming detects deliberate corruption
func TestIntegrity_StreamingReaderDetectsCorruption(t *testing.T) {
	mgr, dirs := setupCluster(t)
	defer cleanupCluster(dirs)

	chunkSize := int64(100 * 1024)
	fileSize := int64(300 * 1024) // 3 chunks
	payload := generateRandomBytes(t, fileSize)

	distributor, _ := NewChunkDistributor(mgr, chunkSize)
	res, err := distributor.Distribute(context.Background(), "file_stream_corrupt", bytes.NewReader(payload), fileSize, nil)
	if err != nil {
		t.Fatalf("Distribute failed: %v", err)
	}

	// Corrupt chunk 1
	targetChunk := res.Chunks[1]
	nodeDir := dirs[targetChunk.StorageNodeID]
	chunkPath := filepath.Join(nodeDir, targetChunk.ProviderObjectID)

	origBytes, _ := os.ReadFile(chunkPath)
	origBytes[10] ^= 0xAA
	_ = os.WriteFile(chunkPath, origBytes, 0644)

	reconstructor, _ := NewFileReconstructor(mgr)
	reader, err := reconstructor.NewReconstructedReader(context.Background(), res.Chunks)
	if err != nil {
		t.Fatalf("NewReconstructedReader failed: %v", err)
	}
	defer reader.Close()

	_, err = io.ReadAll(reader)
	if err == nil {
		t.Fatalf("expected ReconstructedReader to return corruption error, got nil")
	}
	if !errors.Is(err, ErrChunkCorrupted) {
		t.Errorf("expected ErrChunkCorrupted, got: %v", err)
	}
}

// Test 5: Standalone VerifyChunk and VerifyAllChunks audit
func TestIntegrity_StandaloneVerificationAudit(t *testing.T) {
	mgr, dirs := setupCluster(t)
	defer cleanupCluster(dirs)

	chunkSize := int64(100 * 1024)
	fileSize := int64(300 * 1024) // 3 chunks
	payload := generateRandomBytes(t, fileSize)

	distributor, _ := NewChunkDistributor(mgr, chunkSize)
	res, err := distributor.Distribute(context.Background(), "file_audit_test", bytes.NewReader(payload), fileSize, nil)
	if err != nil {
		t.Fatalf("Distribute failed: %v", err)
	}

	reconstructor, _ := NewFileReconstructor(mgr)

	// Before tampering: audit must pass on all chunks
	healthy, err := reconstructor.VerifyAllChunks(context.Background(), res.Chunks)
	if err != nil || !healthy {
		t.Fatalf("expected all chunks to be healthy initially, got healthy=%v, err=%v", healthy, err)
	}

	// Verify single untampered chunk
	valid, err := reconstructor.VerifyChunk(context.Background(), res.Chunks[0])
	if err != nil || !valid {
		t.Fatalf("expected chunk 0 to be valid, got valid=%v, err=%v", valid, err)
	}

	// Now tamper chunk 2
	tamperedChunk := res.Chunks[2]
	nodeDir := dirs[tamperedChunk.StorageNodeID]
	chunkPath := filepath.Join(nodeDir, tamperedChunk.ProviderObjectID)

	orig, _ := os.ReadFile(chunkPath)
	orig[200] ^= 0x42
	_ = os.WriteFile(chunkPath, orig, 0644)

	// Standalone verify on tampered chunk must return false and ErrChunkCorrupted
	valid, err = reconstructor.VerifyChunk(context.Background(), tamperedChunk)
	if valid || err == nil {
		t.Fatalf("expected VerifyChunk on tampered chunk to fail, got valid=%v, err=%v", valid, err)
	}
	if !errors.Is(err, ErrChunkCorrupted) {
		t.Errorf("expected ErrChunkCorrupted, got: %v", err)
	}

	// Full audit should report corruption
	allHealthy, auditErr := reconstructor.VerifyAllChunks(context.Background(), res.Chunks)
	if allHealthy || auditErr == nil {
		t.Fatalf("expected VerifyAllChunks to fail after tampering, got healthy=%v", allHealthy)
	}
}
