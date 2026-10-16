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

// ChunkMapping represents the exact mapping:
// chunk_index -> storage_node_id -> provider_object_id
type ChunkMapping map[int]map[string]string

// DistributionResult captures the verified outcome of chunk distribution across storage nodes.
type DistributionResult struct {
	FileID           string           `json:"file_id"`
	TotalSize        int64            `json:"total_size"`
	ChunkSize        int64            `json:"chunk_size"`
	TotalChunks      int              `json:"total_chunks"`
	Status           FileStatus       `json:"status"`
	Chunks           []*ChunkMetadata `json:"chunks"`
	Mapping          ChunkMapping     `json:"mapping"`
	NodeDistribution map[string]int   `json:"node_distribution"` // storage_node_id -> count of chunks
}

// ChunkDistributor coordinates the end-to-end pipeline:
// File Chunker -> Storage Manager -> Storage Provider -> Storage Node
type ChunkDistributor struct {
	storageManager *storage.StorageManager
	chunkSize      int64
}

// NewChunkDistributor initializes a ChunkDistributor with a given StorageManager and chunk size.
func NewChunkDistributor(mgr *storage.StorageManager, chunkSize int64) (*ChunkDistributor, error) {
	if mgr == nil {
		return nil, fmt.Errorf("storage manager cannot be nil")
	}
	if chunkSize <= 0 {
		chunkSize = DefaultChunkSize
	}
	return &ChunkDistributor{
		storageManager: mgr,
		chunkSize:      chunkSize,
	}, nil
}

// Distribute streams a file, segments it into ordered chunks, asks StorageManager for optimal
// nodes, and stores chunks directly into each node's storage without intermediate storage.
// Returns a complete mapping: chunk_index -> storage_node -> object_id.
// If any chunk fails, previously stored chunks are rolled back and an error is returned.
func (d *ChunkDistributor) Distribute(
	ctx context.Context,
	fileID string,
	sourceReader io.Reader,
	fileSize int64,
	callback ChunkProgressCallback,
) (*DistributionResult, error) {
	if fileSize <= 0 {
		return nil, ErrEmptyFile
	}

	totalChunks, err := CalculateTotalChunks(fileSize, d.chunkSize)
	if err != nil {
		return nil, err
	}

	// 1. Pre-flight check on overall pool capacity
	poolStats := d.storageManager.GetPoolStats(ctx)
	if poolStats.AvailableCapacity < fileSize {
		return nil, fmt.Errorf("%w: file requires %d bytes, pool available %d bytes",
			ErrInsufficientPoolStorage, fileSize, poolStats.AvailableCapacity)
	}

	storedChunks := make([]*ChunkMetadata, 0, totalChunks)
	mapping := make(ChunkMapping)
	nodeDistribution := make(map[string]int)
	var totalBytesUploaded int64

	// Rollback helper on partial failure
	rollback := func() {
		log.Printf("[CloudDump] Rolling back %d chunks for incomplete file %s", len(storedChunks), fileID)
		for _, chunk := range storedChunks {
			_ = d.storageManager.DeleteChunk(context.Background(), chunk.StorageNodeID, chunk.ProviderObjectID)
		}
	}

	for i := 0; i < totalChunks; i++ {
		select {
		case <-ctx.Done():
			rollback()
			return nil, ctx.Err()
		default:
		}

		expectedSize := ExpectedChunkSize(fileSize, d.chunkSize, i)

		// 2. Ask StorageManager for an appropriate node considering available capacity
		targetNode, err := d.storageManager.SelectNodeForChunk(expectedSize)
		if err != nil {
			rollback()
			return nil, fmt.Errorf("storage manager failed to allocate node for chunk %d/%d: %w", i+1, totalChunks, err)
		}

		// 3. Generate unique chunk and object identifier
		chunkID := fmt.Sprintf("chk_%s_%04d", fileID, i)

		// 4. Stream directly to node storage with on-the-fly SHA-256 calculation
		hasher := sha256.New()
		limitReader := io.LimitReader(sourceReader, expectedSize)
		teeReader := io.TeeReader(limitReader, hasher)

		objectID, written, err := d.storageManager.StoreChunk(ctx, targetNode.ID, chunkID, teeReader, expectedSize)
		if err != nil {
			rollback()
			return nil, fmt.Errorf("failed storing chunk %d on node %s: %w", i, targetNode.ID, err)
		}

		if written != expectedSize {
			rollback()
			return nil, fmt.Errorf("incomplete chunk %d: expected %d bytes, wrote %d bytes", i, expectedSize, written)
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

		// 5. Build mapping: chunk_index -> storage_node -> object_id
		if mapping[i] == nil {
			mapping[i] = make(map[string]string)
		}
		mapping[i][targetNode.ID] = objectID
		nodeDistribution[targetNode.ID]++

		if callback != nil {
			callback(chunkMeta, i+1, totalChunks, totalBytesUploaded, fileSize)
		}
	}

	// 6. Guarantee: Never report file as COMPLETED if any required chunk is missing
	if len(storedChunks) != totalChunks {
		rollback()
		return nil, fmt.Errorf("chunk distribution incomplete: stored %d/%d required chunks", len(storedChunks), totalChunks)
	}

	return &DistributionResult{
		FileID:           fileID,
		TotalSize:        fileSize,
		ChunkSize:        d.chunkSize,
		TotalChunks:      totalChunks,
		Status:           FileStatusCompleted,
		Chunks:           storedChunks,
		Mapping:          mapping,
		NodeDistribution: nodeDistribution,
	}, nil
}
