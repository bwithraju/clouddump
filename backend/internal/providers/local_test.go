package providers

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"testing"
)

func TestLocalStorageProvider_StoreAndGet(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "clouddump_local_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	capacity := int64(1024 * 1024) // 1 MB
	p, err := NewLocalStorageProvider(tempDir, capacity)
	if err != nil {
		t.Fatalf("failed to initialize provider: %v", err)
	}

	ctx := context.Background()
	payload := []byte("CloudDump Chunk Streaming Test Payload 12345")
	chunkID := "chunk_test_001"

	objectID, written, err := p.StoreChunk(ctx, chunkID, bytes.NewReader(payload), int64(len(payload)))
	if err != nil {
		t.Fatalf("StoreChunk failed: %v", err)
	}

	if written != int64(len(payload)) {
		t.Errorf("expected written %d bytes, got %d", len(payload), written)
	}
	if objectID != "chunk_test_001.chunk" {
		t.Errorf("unexpected objectID: %s", objectID)
	}

	// Read chunk back
	rc, err := p.GetChunk(ctx, objectID)
	if err != nil {
		t.Fatalf("GetChunk failed: %v", err)
	}
	defer rc.Close()

	readData, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("failed to read from GetChunk: %v", err)
	}
	if !bytes.Equal(readData, payload) {
		t.Errorf("read data does not match payload")
	}

	// Verify capacity
	avail, err := p.GetAvailableCapacity(ctx)
	if err != nil {
		t.Fatalf("GetAvailableCapacity failed: %v", err)
	}
	if avail != capacity-int64(len(payload)) {
		t.Errorf("expected available %d, got %d", capacity-int64(len(payload)), avail)
	}
}

func TestLocalStorageProvider_CapacityEnforcement(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "clouddump_cap_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	capacity := int64(100) // Small 100-byte capacity
	p, err := NewLocalStorageProvider(tempDir, capacity)
	if err != nil {
		t.Fatalf("failed to initialize provider: %v", err)
	}

	ctx := context.Background()

	// Try to store 150 bytes -> must fail with ErrInsufficientCapacity
	oversized := make([]byte, 150)
	_, _, err = p.StoreChunk(ctx, "chunk_over", bytes.NewReader(oversized), 150)
	if !errors.Is(err, ErrInsufficientCapacity) {
		t.Fatalf("expected ErrInsufficientCapacity, got %v", err)
	}

	// Available capacity should remain unchanged (100)
	avail, _ := p.GetAvailableCapacity(ctx)
	if avail != 100 {
		t.Errorf("expected available 100, got %d", avail)
	}
}

func TestLocalStorageProvider_PathTraversalProtection(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "clouddump_traversal_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	p, err := NewLocalStorageProvider(tempDir, 1024*1024)
	if err != nil {
		t.Fatalf("failed to initialize provider: %v", err)
	}

	ctx := context.Background()
	maliciousIDs := []string{
		"../../etc/passwd",
		"sub/dir/chunk",
		"..\\windows\\system32",
		"",
		"chunk;rm -rf /",
	}

	for _, malID := range maliciousIDs {
		_, _, err := p.StoreChunk(ctx, malID, bytes.NewReader([]byte("test")), 4)
		if !errors.Is(err, ErrInvalidChunkID) {
			t.Errorf("expected ErrInvalidChunkID for ID %q, got: %v", malID, err)
		}
	}
}

func TestLocalStorageProvider_DeleteAndRestoreCapacity(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "clouddump_del_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	capacity := int64(1000)
	p, err := NewLocalStorageProvider(tempDir, capacity)
	if err != nil {
		t.Fatalf("failed to initialize provider: %v", err)
	}

	ctx := context.Background()
	payload := make([]byte, 400)
	objectID, _, err := p.StoreChunk(ctx, "chunk_del", bytes.NewReader(payload), 400)
	if err != nil {
		t.Fatalf("StoreChunk failed: %v", err)
	}

	availBefore, _ := p.GetAvailableCapacity(ctx)
	if availBefore != 600 {
		t.Errorf("expected available 600, got %d", availBefore)
	}

	// Delete chunk
	if err := p.DeleteChunk(ctx, objectID); err != nil {
		t.Fatalf("DeleteChunk failed: %v", err)
	}

	availAfter, _ := p.GetAvailableCapacity(ctx)
	if availAfter != 1000 {
		t.Errorf("expected capacity restored to 1000, got %d", availAfter)
	}

	// Reading deleted chunk should return ErrChunkNotFound
	_, err = p.GetChunk(ctx, objectID)
	if !errors.Is(err, ErrChunkNotFound) {
		t.Errorf("expected ErrChunkNotFound, got: %v", err)
	}
}

func TestLocalStorageProvider_HealthCheck(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "clouddump_health_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	p, err := NewLocalStorageProvider(tempDir, 1024*1024)
	if err != nil {
		t.Fatalf("failed to initialize provider: %v", err)
	}

	ctx := context.Background()
	healthy, err := p.HealthCheck(ctx)
	if err != nil || !healthy {
		t.Fatalf("expected healthy true and no error, got healthy=%v, err=%v", healthy, err)
	}
}
