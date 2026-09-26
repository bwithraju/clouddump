package chunks

import (
	"errors"
	"time"
)

var (
	// ErrInsufficientPoolStorage is returned when the combined storage pool cannot accommodate the file.
	ErrInsufficientPoolStorage = errors.New("insufficient total pool storage for file")
	// ErrChunkChecksumMismatch is returned when an extracted chunk's SHA-256 does not match its metadata.
	ErrChunkChecksumMismatch = errors.New("chunk checksum mismatch")
	// ErrChunkNotFound is returned when a requested chunk is missing from metadata or storage.
	ErrChunkNotFound = errors.New("chunk not found")
	// ErrInvalidChunkSize is returned when an invalid chunk size is specified.
	ErrInvalidChunkSize = errors.New("chunk size must be greater than zero")
	// ErrEmptyFile is returned when an upload of 0 bytes is rejected.
	ErrEmptyFile = errors.New("file size must be greater than zero")
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

// ChunkMetadata represents the internal state and placement of a single chunk.
type ChunkMetadata struct {
	ID               string      `json:"id"`
	FileID           string      `json:"file_id"`
	ChunkIndex       int         `json:"chunk_index"`
	Size             int64       `json:"size"`
	Checksum         string      `json:"checksum"` // SHA-256 hex string
	StorageNodeID    string      `json:"storage_node_id"`
	ProviderObjectID string      `json:"provider_object_id"`
	Status           ChunkStatus `json:"status"`
	CreatedAt        time.Time   `json:"created_at"`
}

// FileStatus represents the lifecycle state of a logical file.
type FileStatus string

const (
	FileStatusUploading FileStatus = "UPLOADING"
	FileStatusCompleted FileStatus = "COMPLETED"
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
