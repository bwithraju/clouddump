package chunks

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// TestEdgeCase_EmptyFile verifies that 0-byte or negative size files are rejected with ErrEmptyFile.
func TestEdgeCase_EmptyFile(t *testing.T) {
	mgr, dirs := setupCluster(t)
	defer cleanupCluster(dirs)

	chunker, err := NewChunker(mgr, 64*1024)
	if err != nil {
		t.Fatalf("NewChunker failed: %v", err)
	}

	// 0-byte file
	_, err = chunker.StreamAndDistribute(context.Background(), "file_empty", bytes.NewReader([]byte{}), 0, nil)
	if !errors.Is(err, ErrEmptyFile) {
		t.Fatalf("expected ErrEmptyFile for 0-byte file, got: %v", err)
	}

	// Negative file size
	_, err = chunker.StreamAndDistribute(context.Background(), "file_negative", bytes.NewReader([]byte{}), -10, nil)
	if !errors.Is(err, ErrEmptyFile) {
		t.Fatalf("expected ErrEmptyFile for negative size, got: %v", err)
	}

	// Pool should have 0 bytes used
	stats := mgr.GetPoolStats(context.Background())
	if stats.UsedCapacity != 0 {
		t.Errorf("expected 0 used capacity after empty file rejection, got %d", stats.UsedCapacity)
	}
}

// TestEdgeCase_OneByteFile verifies that a 1-byte file creates exactly 1 chunk of size 1 and reconstructs correctly.
func TestEdgeCase_OneByteFile(t *testing.T) {
	mgr, dirs := setupCluster(t)
	defer cleanupCluster(dirs)

	chunkSize := int64(64 * 1024) // 64 KB chunk size
	chunker, err := NewChunker(mgr, chunkSize)
	if err != nil {
		t.Fatalf("NewChunker failed: %v", err)
	}

	singleByte := []byte{0x42}
	expectedChecksum := hex.EncodeToString(func() []byte {
		h := sha256.Sum256(singleByte)
		return h[:]
	}())

	chunks, err := chunker.StreamAndDistribute(context.Background(), "file_one_byte", bytes.NewReader(singleByte), 1, nil)
	if err != nil {
		t.Fatalf("StreamAndDistribute failed for 1-byte file: %v", err)
	}

	if len(chunks) != 1 {
		t.Fatalf("expected exactly 1 chunk, got %d", len(chunks))
	}

	if chunks[0].Size != 1 {
		t.Errorf("expected chunk size 1, got %d", chunks[0].Size)
	}
	if chunks[0].Checksum != expectedChecksum {
		t.Errorf("checksum mismatch: expected %s, got %s", expectedChecksum, chunks[0].Checksum)
	}

	// Reconstruct and verify exact payload
	reconstructor, err := NewFileReconstructor(mgr)
	if err != nil {
		t.Fatalf("NewFileReconstructor failed: %v", err)
	}

	var out bytes.Buffer
	written, err := reconstructor.ReconstructTo(context.Background(), chunks, &out)
	if err != nil {
		t.Fatalf("ReconstructTo failed: %v", err)
	}

	if written != 1 {
		t.Errorf("expected 1 byte written, got %d", written)
	}
	if !bytes.Equal(out.Bytes(), singleByte) {
		t.Errorf("reconstructed byte mismatch: expected %v, got %v", singleByte, out.Bytes())
	}
}

// TestEdgeCase_ExactChunkSize verifies that a file exactly equal to chunk size produces 1 chunk without empty trailers.
func TestEdgeCase_ExactChunkSize(t *testing.T) {
	mgr, dirs := setupCluster(t)
	defer cleanupCluster(dirs)

	chunkSize := int64(32 * 1024) // 32 KB
	chunker, _ := NewChunker(mgr, chunkSize)

	payload := generateRandomBytes(t, chunkSize)
	chunks, err := chunker.StreamAndDistribute(context.Background(), "file_exact", bytes.NewReader(payload), chunkSize, nil)
	if err != nil {
		t.Fatalf("StreamAndDistribute failed: %v", err)
	}

	if len(chunks) != 1 {
		t.Fatalf("expected exactly 1 chunk for exact chunk size, got %d", len(chunks))
	}
	if chunks[0].Size != chunkSize {
		t.Errorf("expected chunk size %d, got %d", chunkSize, chunks[0].Size)
	}

	reconstructor, _ := NewFileReconstructor(mgr)
	var out bytes.Buffer
	written, err := reconstructor.ReconstructTo(context.Background(), chunks, &out)
	if err != nil {
		t.Fatalf("reconstruction failed: %v", err)
	}

	if written != chunkSize {
		t.Errorf("expected %d bytes written, got %d", chunkSize, written)
	}
	if !bytes.Equal(out.Bytes(), payload) {
		t.Error("reconstructed payload does not match original")
	}
}

// TestEdgeCase_FinalPartialChunk verifies files with multiple full chunks followed by a partial chunk.
func TestEdgeCase_FinalPartialChunk(t *testing.T) {
	mgr, dirs := setupCluster(t)
	defer cleanupCluster(dirs)

	chunkSize := int64(30 * 1024) // 30 KB
	// File: 70 KB = 30 KB (chunk 0) + 30 KB (chunk 1) + 10 KB (chunk 2 partial)
	fileSize := int64(70 * 1024)
	payload := generateRandomBytes(t, fileSize)

	chunker, _ := NewChunker(mgr, chunkSize)
	chunks, err := chunker.StreamAndDistribute(context.Background(), "file_partial", bytes.NewReader(payload), fileSize, nil)
	if err != nil {
		t.Fatalf("StreamAndDistribute failed: %v", err)
	}

	if len(chunks) != 3 {
		t.Fatalf("expected 3 chunks, got %d", len(chunks))
	}

	if chunks[0].Size != 30*1024 {
		t.Errorf("chunk 0 size expected 30720, got %d", chunks[0].Size)
	}
	if chunks[1].Size != 30*1024 {
		t.Errorf("chunk 1 size expected 30720, got %d", chunks[1].Size)
	}
	if chunks[2].Size != 10*1024 {
		t.Errorf("chunk 2 size expected 10240, got %d", chunks[2].Size)
	}

	reconstructor, _ := NewFileReconstructor(mgr)
	var out bytes.Buffer
	written, err := reconstructor.ReconstructTo(context.Background(), chunks, &out)
	if err != nil {
		t.Fatalf("reconstruction failed: %v", err)
	}

	if written != fileSize {
		t.Errorf("expected %d bytes written, got %d", fileSize, written)
	}
	if !bytes.Equal(out.Bytes(), payload) {
		t.Error("reconstructed payload does not match original")
	}
}

// TestEdgeCase_InsufficientCapacity verifies that requests exceeding available capacity are cleanly rejected.
func TestEdgeCase_InsufficientCapacity(t *testing.T) {
	mgr, dirs := setupCluster(t)
	defer cleanupCluster(dirs)

	// Pool capacity is 500+1000+2000+700 = 4200 KB
	poolStats := mgr.GetPoolStats(context.Background())
	excessSize := poolStats.TotalCapacity + 1024*1024 // 1 MB more than total capacity

	chunker, _ := NewChunker(mgr, 128*1024)
	dummyReader := bytes.NewReader(make([]byte, 1024)) // Small reader with large declared size

	_, err := chunker.StreamAndDistribute(context.Background(), "file_overflow", dummyReader, excessSize, nil)
	if !errors.Is(err, ErrInsufficientPoolStorage) {
		t.Fatalf("expected ErrInsufficientPoolStorage, got: %v", err)
	}

	// Verify no storage capacity was consumed
	afterStats := mgr.GetPoolStats(context.Background())
	if afterStats.UsedCapacity != 0 {
		t.Errorf("expected 0 used capacity after rejected upload, got %d", afterStats.UsedCapacity)
	}
}

// TestEdgeCase_CorruptedChunk verifies that checksum mismatch is detected when a chunk is altered.
func TestEdgeCase_CorruptedChunk(t *testing.T) {
	mgr, dirs := setupCluster(t)
	defer cleanupCluster(dirs)

	chunkSize := int64(50 * 1024)
	fileSize := int64(100 * 1024)
	payload := generateRandomBytes(t, fileSize)

	chunker, _ := NewChunker(mgr, chunkSize)
	chunks, err := chunker.StreamAndDistribute(context.Background(), "file_corrupt_test", bytes.NewReader(payload), fileSize, nil)
	if err != nil {
		t.Fatalf("StreamAndDistribute failed: %v", err)
	}

	// Corrupt chunk 1 by flipping a byte on disk
	c1 := chunks[1]
	nodeDir := dirs[c1.StorageNodeID]
	chunkPath := filepath.Join(nodeDir, c1.ProviderObjectID)

	data, err := os.ReadFile(chunkPath)
	if err != nil {
		t.Fatalf("failed to read chunk file: %v", err)
	}
	data[100] ^= 0xAA
	if err := os.WriteFile(chunkPath, data, 0644); err != nil {
		t.Fatalf("failed to write corrupted chunk: %v", err)
	}

	// Attempt reconstruction - should fail with ErrChunkCorrupted
	reconstructor, _ := NewFileReconstructor(mgr)
	var out bytes.Buffer
	_, err = reconstructor.ReconstructTo(context.Background(), chunks, &out)
	if !errors.Is(err, ErrChunkCorrupted) {
		t.Fatalf("expected ErrChunkCorrupted, got: %v", err)
	}
}

// TestEdgeCase_InterruptedUpload verifies that context cancellation rolls back stored chunks and frees capacity.
func TestEdgeCase_InterruptedUpload(t *testing.T) {
	mgr, dirs := setupCluster(t)
	defer cleanupCluster(dirs)

	chunkSize := int64(50 * 1024)
	fileSize := int64(200 * 1024) // 4 chunks
	payload := generateRandomBytes(t, fileSize)

	ctx, cancel := context.WithCancel(context.Background())

	chunker, _ := NewChunker(mgr, chunkSize)
	callback := func(chunk *ChunkMetadata, current, total int, uploaded, totalB int64) {
		if current == 2 {
			cancel() // abort midway after 2nd chunk
		}
	}

	_, err := chunker.StreamAndDistribute(ctx, "file_interrupted", bytes.NewReader(payload), fileSize, callback)
	if err == nil {
		t.Fatalf("expected error on context cancellation, got nil")
	}

	// Verify all chunks were removed and node capacity was restored to 0 used
	stats := mgr.GetPoolStats(context.Background())
	if stats.UsedCapacity != 0 {
		t.Errorf("expected 0 used capacity after interrupted upload rollback, got %d", stats.UsedCapacity)
	}
}
