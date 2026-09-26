package providers

import (
	"context"
	"errors"
	"io"
)

var (
	// ErrInsufficientCapacity is returned when the provider does not have enough capacity for the requested chunk.
	ErrInsufficientCapacity = errors.New("insufficient storage capacity on provider")
	// ErrChunkNotFound is returned when the requested chunk object does not exist.
	ErrChunkNotFound = errors.New("chunk not found on provider")
	// ErrInvalidChunkID is returned when chunk ID violates safety/naming constraints.
	ErrInvalidChunkID = errors.New("invalid chunk identifier")
	// ErrNodeOffline is returned when attempting an operation on an offline provider.
	ErrNodeOffline = errors.New("storage provider is offline or unreachable")
)

// StorageProvider defines the universal interface for independent storage backends.
// Implementations include LocalStorageProvider, GoogleDriveProvider, S3Provider, etc.
type StorageProvider interface {
	// ProviderType returns the unique identifier of the provider type (e.g., "local", "google_drive").
	ProviderType() string

	// StoreChunk streams a chunk into the provider.
	// It writes the contents of r without buffering the full chunk in RAM.
	// Returns the provider-specific objectID (which may be path, file ID, key) and written bytes.
	StoreChunk(ctx context.Context, chunkID string, r io.Reader, size int64) (objectID string, bytesWritten int64, err error)

	// GetChunk opens a streaming read handle to the chunk identified by objectID.
	// Caller is responsible for closing the returned io.ReadCloser.
	GetChunk(ctx context.Context, objectID string) (io.ReadCloser, error)

	// DeleteChunk permanently removes the chunk object from the provider.
	DeleteChunk(ctx context.Context, objectID string) error

	// GetTotalCapacity returns the total configured or quota capacity of the provider in bytes.
	GetTotalCapacity(ctx context.Context) (int64, error)

	// GetAvailableCapacity returns the remaining available capacity in bytes.
	GetAvailableCapacity(ctx context.Context) (int64, error)

	// HealthCheck verifies whether the provider is online, reachable, and writable.
	HealthCheck(ctx context.Context) (bool, error)
}
