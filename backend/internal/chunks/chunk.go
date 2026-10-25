package chunks

import (
	"errors"
	"time"
)

var (
	// ErrInsufficientPoolStorage is returned when the combined storage pool cannot accommodate the file.
	ErrInsufficientPoolStorage = errors.New("insufficient total pool storage for file")
	// ErrChunkCorrupted is returned when an extracted chunk's SHA-256 does not match its expected metadata.
	ErrChunkCorrupted = errors.New("data integrity error: chunk is corrupted (checksum mismatch)")
	// ErrChunkChecksumMismatch is an alias for ErrChunkCorrupted.
	ErrChunkChecksumMismatch = ErrChunkCorrupted
	// ErrChunkNotFound is returned when a requested chunk is missing from metadata or storage.
	ErrChunkNotFound = errors.New("chunk not found")
	// ErrInvalidChunkSize is returned when an invalid chunk size is specified.
	ErrInvalidChunkSize = errors.New("chunk size must be greater than zero")
	// ErrEmptyFile is returned when an upload of 0 bytes is rejected.
	ErrEmptyFile = errors.New("file size must be greater than zero")
	// ErrMissingChunk is returned when a required chunk in the sequence is missing or inaccessible.
	ErrMissingChunk = errors.New("missing chunk: required chunk not found or inaccessible")
	// ErrIncompleteFile is returned when file chunk list is incomplete or out of sequence.
	ErrIncompleteFile = errors.New("incomplete file: chunk sequence is broken or missing elements")
)

// DefaultChunkSize is 256 MB in bytes (268,435,456 bytes).
const DefaultChunkSize int64 = 256 * 1024 * 1024

// ChunkStatus represents the operational status of an individual chunk.
type ChunkStatus string

const (
	ChunkStatusPending     ChunkStatus = "PENDING"
	ChunkStatusStored      ChunkStatus = "STORED"
	ChunkStatusCorrupted   ChunkStatus = "CORRUPTED"
	ChunkStatusUnavailable ChunkStatus = "UNAVAILABLE"
)

// ChunkReplica represents a replica location of a chunk on a secondary storage node.
type ChunkReplica struct {
	StorageNodeID    string      `json:"storage_node_id"`
	ProviderObjectID string      `json:"provider_object_id"`
	Status           ChunkStatus `json:"status"`
}

// ChunkMetadata represents the internal state and placement of a single chunk.
type ChunkMetadata struct {
	ID               string          `json:"id"`
	FileID           string          `json:"file_id"`
	ChunkIndex       int             `json:"chunk_index"`
	Size             int64           `json:"size"`
	Checksum         string          `json:"checksum"` // SHA-256 hex string
	StorageNodeID    string          `json:"storage_node_id"`
	ProviderObjectID string          `json:"provider_object_id"`
	Status           ChunkStatus     `json:"status"`
	Replicas         []*ChunkReplica `json:"replicas,omitempty"`
	CreatedAt        time.Time       `json:"created_at"`
}

// AddReplica registers a replica location for this chunk.
func (c *ChunkMetadata) AddReplica(nodeID, objectID string, status ChunkStatus) {
	c.Replicas = append(c.Replicas, &ChunkReplica{
		StorageNodeID:    nodeID,
		ProviderObjectID: objectID,
		Status:           status,
	})
}

// AllStorageNodes returns the list of all storage node IDs hosting this chunk (primary + replicas).
func (c *ChunkMetadata) AllStorageNodes() []string {
	nodes := make([]string, 0, 1+len(c.Replicas))
	if c.StorageNodeID != "" {
		nodes = append(nodes, c.StorageNodeID)
	}
	for _, r := range c.Replicas {
		if r != nil && r.StorageNodeID != "" {
			nodes = append(nodes, r.StorageNodeID)
		}
	}
	return nodes
}

// FileStatus represents the lifecycle state of a logical file.
type FileStatus string

const (
	FileStatusUploading FileStatus = "UPLOADING"
	FileStatusCompleted FileStatus = "COMPLETED"
	FileStatusDegraded  FileStatus = "DEGRADED"
	FileStatusFailed    FileStatus = "FAILED"
	FileStatusDeleting  FileStatus = "DELETING"
	FileStatusDeleted   FileStatus = "DELETED"
)

// FileMetadata represents a logical user file distributed across storage nodes.
type FileMetadata struct {
	ID          string           `json:"id"`
	UserID      string           `json:"user_id"`
	Filename    string           `json:"filename"`
	Size        int64            `json:"size"`
	MimeType    string           `json:"mime_type"`
	ChunkSize   int64            `json:"chunk_size"`
	TotalChunks int              `json:"total_chunks"`
	Status      FileStatus       `json:"status"`
	Chunks      []*ChunkMetadata `json:"chunks"`
	CreatedAt   time.Time        `json:"created_at"`
	UpdatedAt   time.Time        `json:"updated_at"`
}

// CalculateTotalChunks computes the expected number of chunks for a given file size and chunk size.
func CalculateTotalChunks(fileSize, chunkSize int64) (int, error) {
	if chunkSize <= 0 {
		return 0, ErrInvalidChunkSize
	}
	if fileSize <= 0 {
		return 0, ErrEmptyFile
	}
	total := int((fileSize + chunkSize - 1) / chunkSize)
	return total, nil
}

// ExpectedChunkSize returns the expected byte count for a given chunk index.
func ExpectedChunkSize(fileSize, chunkSize int64, chunkIndex int) int64 {
	offset := int64(chunkIndex) * chunkSize
	remaining := fileSize - offset
	if remaining <= 0 {
		return 0
	}
	if remaining < chunkSize {
		return remaining
	}
	return chunkSize
}
