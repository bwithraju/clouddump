package chunks

import (
	"bytes"
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
	FileID            string           `json:"file_id"`
	TotalSize         int64            `json:"total_size"`
	ChunkSize         int64            `json:"chunk_size"`
	TotalChunks       int              `json:"total_chunks"`
	ReplicationFactor int              `json:"replication_factor"`
	Status            FileStatus       `json:"status"`
	Chunks            []*ChunkMetadata `json:"chunks"`
	Mapping           ChunkMapping     `json:"mapping"`
	NodeDistribution  map[string]int   `json:"node_distribution"` // storage_node_id -> count of chunks (primary + replicas)
}

// ChunkDistributor coordinates the end-to-end pipeline:
// File Chunker -> Storage Manager -> Storage Provider -> Storage Node
type ChunkDistributor struct {
	storageManager    *storage.StorageManager
	chunkSize         int64
	replicationFactor int
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
		storageManager:    mgr,
		chunkSize:         chunkSize,
		replicationFactor: 1, // default factor = 1
	}, nil
}

// SetReplicationFactor sets the default replication factor for this distributor.
func (d *ChunkDistributor) SetReplicationFactor(rf int) {
	if rf < 1 {
		rf = 1
	}
	d.replicationFactor = rf
}

// ReplicationFactor returns the current default replication factor.
func (d *ChunkDistributor) ReplicationFactor() int {
	if d.replicationFactor < 1 {
		return 1
	}
	return d.replicationFactor
}

// Distribute streams a file, segments it into ordered chunks, asks StorageManager for optimal
// nodes, and stores chunks directly into each node's storage without intermediate storage.
// Uses the distributor's default replication factor.
func (d *ChunkDistributor) Distribute(
	ctx context.Context,
	fileID string,
	sourceReader io.Reader,
	fileSize int64,
	callback ChunkProgressCallback,
) (*DistributionResult, error) {
	return d.DistributeWithReplication(ctx, fileID, sourceReader, fileSize, d.replicationFactor, callback)
}

// DistributeWithReplication streams a file, segments it into ordered chunks, and stores each chunk
// with the requested replication factor across distinct storage nodes.
func (d *ChunkDistributor) DistributeWithReplication(
	ctx context.Context,
	fileID string,
	sourceReader io.Reader,
	fileSize int64,
	replicationFactor int,
	callback ChunkProgressCallback,
) (*DistributionResult, error) {
	if fileSize <= 0 {
		return nil, ErrEmptyFile
	}
	if replicationFactor < 1 {
		replicationFactor = 1
	}

	totalChunks, err := CalculateTotalChunks(fileSize, d.chunkSize)
	if err != nil {
		return nil, err
	}

	// 1. Pre-flight check on overall pool capacity considering replication factor
	totalRequired := fileSize * int64(replicationFactor)
	poolStats := d.storageManager.GetPoolStats(ctx)
	if poolStats.AvailableCapacity < totalRequired {
		return nil, fmt.Errorf("%w: file with replication factor %d requires %d bytes, pool available %d bytes",
			ErrInsufficientPoolStorage, replicationFactor, totalRequired, poolStats.AvailableCapacity)
	}

	if replicationFactor > poolStats.OnlineNodes {
		return nil, fmt.Errorf("cannot satisfy replication factor %d: only %d online nodes available",
			replicationFactor, poolStats.OnlineNodes)
	}

	storedChunks := make([]*ChunkMetadata, 0, totalChunks)
	mapping := make(ChunkMapping)
	nodeDistribution := make(map[string]int)
	var totalBytesUploaded int64

	// Rollback helper on partial failure: cleans up primary and replica copies
	rollback := func() {
		log.Printf("[CloudDump] Rolling back %d chunks for incomplete file %s", len(storedChunks), fileID)
		for _, chunk := range storedChunks {
			_ = d.storageManager.DeleteChunk(context.Background(), chunk.StorageNodeID, chunk.ProviderObjectID)
			for _, rep := range chunk.Replicas {
				_ = d.storageManager.DeleteChunk(context.Background(), rep.StorageNodeID, rep.ProviderObjectID)
			}
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

		// Read chunk data into a single-chunk bounded buffer
		chunkBytes := make([]byte, expectedSize)
		if _, err := io.ReadFull(sourceReader, chunkBytes); err != nil {
			rollback()
			return nil, fmt.Errorf("failed reading chunk %d data: %w", i, err)
		}

		// Compute SHA-256 once for this chunk
		hasher := sha256.New()
		hasher.Write(chunkBytes)
		checksum := hex.EncodeToString(hasher.Sum(nil))

		// 2. Select primary node
		primaryNode, err := d.storageManager.SelectNodeForChunk(expectedSize)
		if err != nil {
			rollback()
			return nil, fmt.Errorf("storage manager failed to allocate primary node for chunk %d/%d: %w", i+1, totalChunks, err)
		}

		// 3. Store primary chunk
		chunkID := fmt.Sprintf("chk_%s_%04d", fileID, i)
		objectID, written, err := d.storageManager.StoreChunk(ctx, primaryNode.ID, chunkID, bytes.NewReader(chunkBytes), expectedSize)
		if err != nil {
			rollback()
			return nil, fmt.Errorf("failed storing primary chunk %d on node %s: %w", i, primaryNode.ID, err)
		}
		if written != expectedSize {
			rollback()
			return nil, fmt.Errorf("incomplete primary chunk %d: expected %d bytes, wrote %d bytes", i, expectedSize, written)
		}

		totalBytesUploaded += written

		chunkMeta := &ChunkMetadata{
			ID:               chunkID,
			FileID:           fileID,
			ChunkIndex:       i,
			Size:             written,
			Checksum:         checksum,
			StorageNodeID:    primaryNode.ID,
			ProviderObjectID: objectID,
			Status:           ChunkStatusStored,
			Replicas:         make([]*ChunkReplica, 0),
			CreatedAt:        time.Now().UTC(),
		}

		if mapping[i] == nil {
			mapping[i] = make(map[string]string)
		}
		mapping[i][primaryNode.ID] = objectID
		nodeDistribution[primaryNode.ID]++

		// 4. Store replicas if replication factor > 1
		if replicationFactor > 1 {
			excludedNodes := []string{primaryNode.ID}
			for r := 1; r < replicationFactor; r++ {
				replicaNode, err := d.storageManager.SelectNodeForChunk(expectedSize, excludedNodes...)
				if err != nil {
					rollback()
					return nil, fmt.Errorf("storage manager failed to allocate replica %d for chunk %d: %w", r, i, err)
				}
				excludedNodes = append(excludedNodes, replicaNode.ID)

				replicaChunkID := fmt.Sprintf("chk_%s_%04d_rep%d", fileID, i, r)
				repObjectID, repWritten, err := d.storageManager.StoreChunk(ctx, replicaNode.ID, replicaChunkID, bytes.NewReader(chunkBytes), expectedSize)
				if err != nil {
					rollback()
					return nil, fmt.Errorf("failed storing replica %d of chunk %d on node %s: %w", r, i, replicaNode.ID, err)
				}
				if repWritten != expectedSize {
					rollback()
					return nil, fmt.Errorf("incomplete replica %d of chunk %d: expected %d bytes, wrote %d bytes", r, i, expectedSize, repWritten)
				}

				chunkMeta.AddReplica(replicaNode.ID, repObjectID, ChunkStatusStored)
				mapping[i][replicaNode.ID] = repObjectID
				nodeDistribution[replicaNode.ID]++
			}
		}

		storedChunks = append(storedChunks, chunkMeta)

		if callback != nil {
			callback(chunkMeta, i+1, totalChunks, totalBytesUploaded, fileSize)
		}
	}

	// 5. Guarantee: Never report file as COMPLETED if any required chunk is missing
	if len(storedChunks) != totalChunks {
		rollback()
		return nil, fmt.Errorf("chunk distribution incomplete: stored %d/%d required chunks", len(storedChunks), totalChunks)
	}

	return &DistributionResult{
		FileID:            fileID,
		TotalSize:         fileSize,
		ChunkSize:         d.chunkSize,
		TotalChunks:       totalChunks,
		ReplicationFactor: replicationFactor,
		Status:            FileStatusCompleted,
		Chunks:            storedChunks,
		Mapping:           mapping,
		NodeDistribution:  nodeDistribution,
	}, nil
}
