package chunks

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"time"

	"github.com/bwithraju/clouddump/backend/internal/storage"
)

// Chunker handles streaming chunk segmentation, hashing, and distribution across storage nodes.
type Chunker struct {
	storageManager *storage.StorageManager
	chunkSize      int64
}

// NewChunker initializes a Chunker with a given StorageManager and configurable chunk size.
func NewChunker(mgr *storage.StorageManager, chunkSize int64) (*Chunker, error) {
	if mgr == nil {
		return nil, fmt.Errorf("storage manager cannot be nil")
	}
	if chunkSize <= 0 {
		chunkSize = DefaultChunkSize
	}
	return &Chunker{
		storageManager: mgr,
		chunkSize:      chunkSize,
	}, nil
}

// ChunkSize returns the configured chunk size in bytes.
func (c *Chunker) ChunkSize() int64 {
	return c.chunkSize
}

// ChunkProgressCallback is called after each chunk is successfully stored.
type ChunkProgressCallback func(chunk *ChunkMetadata, currentChunk, totalChunks int, bytesUploaded, totalBytes int64)

// StreamAndDistribute splits sourceReader into sequential chunks and stores them across storage nodes.
// Memory usage is strictly O(1) buffer size: data is streamed directly to the selected node's provider
// while simultaneously calculating the cryptographic SHA-256 hash.
func (c *Chunker) StreamAndDistribute(
	ctx context.Context,
	fileID string,
	sourceReader io.Reader,
	fileSize int64,
	callback ChunkProgressCallback,
) ([]*ChunkMetadata, error) {
	if fileSize <= 0 {
		return nil, ErrEmptyFile
	}

	totalChunks, err := CalculateTotalChunks(fileSize, c.chunkSize)
	if err != nil {
		return nil, err
	}

	// 1. Pre-flight capacity check: verify overall storage pool headroom
	poolStats := c.storageManager.GetPoolStats(ctx)
	if poolStats.AvailableCapacity < fileSize {
		return nil, fmt.Errorf("%w: file requires %d bytes, pool available %d bytes",
			ErrInsufficientPoolStorage, fileSize, poolStats.AvailableCapacity)
	}

	storedChunks := make([]*ChunkMetadata, 0, totalChunks)
	var totalBytesUploaded int64

	// Helper for clean rollback of stored chunks on failure
	rollback := func() {
		log.Printf("[CloudDump] Rolling back %d stored chunks for failed file %s", len(storedChunks), fileID)
		for _, chunk := range storedChunks {
			_ = c.storageManager.DeleteChunk(context.Background(), chunk.StorageNodeID, chunk.ProviderObjectID)
		}
	}

	for i := 0; i < totalChunks; i++ {
		select {
		case <-ctx.Done():
			rollback()
			return nil, ctx.Err()
		default:
		}

		expectedSize := ExpectedChunkSize(fileSize, c.chunkSize, i)

		// 2. Capacity-aware node selection
		targetNode, err := c.storageManager.SelectNodeForChunk(expectedSize)
		if err != nil {
			rollback()
			return nil, fmt.Errorf("node selection failed for chunk %d/%d (size %d bytes): %w",
				i+1, totalChunks, expectedSize, err)
		}

		// 3. Streaming + SHA-256 hashing without RAM accumulation
		chunkID := fmt.Sprintf("chk_%s_%04d", fileID, i)
		hasher := sha256.New()
		limitReader := io.LimitReader(sourceReader, expectedSize)
		teeReader := io.TeeReader(limitReader, hasher)

		objectID, written, err := c.storageManager.StoreChunk(ctx, targetNode.ID, chunkID, teeReader, expectedSize)
		if err != nil {
			rollback()
			return nil, fmt.Errorf("failed storing chunk %d on node %s: %w", i, targetNode.ID, err)
		}

		if written != expectedSize {
			rollback()
			return nil, fmt.Errorf("chunk %d incomplete: expected %d bytes, got %d bytes", i, expectedSize, written)
		}

		checksum := hex.EncodeToString(hasher.Sum(nil))
		totalBytesUploaded += written

		chunkMeta := &ChunkMetadata{
			ID:               chunkID,
			FileID:           fileID,
			ChunkIndex:       i,
			Size:             written,
			Checksum:         checksum,
			StorageNodeID:    targetNode.ID,
			ProviderObjectID: objectID,
			Status:           ChunkStatusStored,
			CreatedAt:        time.Now().UTC(),
		}

		storedChunks = append(storedChunks, chunkMeta)

		if callback != nil {
			callback(chunkMeta, i+1, totalChunks, totalBytesUploaded, fileSize)
		}
	}

	return storedChunks, nil
}
