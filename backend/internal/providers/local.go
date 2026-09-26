package providers

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"
)

var validChunkIDRegex = regexp.MustCompile(`^[a-zA-Z0-9_\-]+$`)

// LocalStorageProvider implements StorageProvider backed by a local filesystem directory.
type LocalStorageProvider struct {
	baseDir       string
	totalCapacity int64
	usedCapacity  int64
	mu            sync.RWMutex
}

// NewLocalStorageProvider initializes a local directory-backed storage provider.
func NewLocalStorageProvider(baseDir string, totalCapacityBytes int64) (*LocalStorageProvider, error) {
	if totalCapacityBytes <= 0 {
		return nil, fmt.Errorf("total capacity must be positive, got: %d", totalCapacityBytes)
	}

	cleanDir := filepath.Clean(baseDir)
	if err := os.MkdirAll(cleanDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create base directory %s: %w", cleanDir, err)
	}

	p := &LocalStorageProvider{
		baseDir:       cleanDir,
		totalCapacity: totalCapacityBytes,
	}

	// Calculate initially used bytes from existing files in the directory
	usedBytes, err := p.scanUsedBytes()
	if err != nil {
		return nil, fmt.Errorf("failed to scan existing directory usage: %w", err)
	}
	p.usedCapacity = usedBytes

	return p, nil
}

// ProviderType returns "local".
func (p *LocalStorageProvider) ProviderType() string {
	return "local"
}

// scanUsedBytes sums the sizes of all files in baseDir.
func (p *LocalStorageProvider) scanUsedBytes() (int64, error) {
	var total int64
	entries, err := os.ReadDir(p.baseDir)
	if err != nil {
		return 0, err
	}

	for _, entry := range entries {
		if !entry.IsDir() && entry.Name() != ".gitkeep" && entry.Name() != ".health_probe" {
			info, err := entry.Info()
			if err != nil {
				continue
			}
			total += info.Size()
		}
	}
	return total, nil
}

// validateChunkID ensures the chunk ID cannot be used for directory traversal attacks.
func (p *LocalStorageProvider) validateChunkID(chunkID string) error {
	if chunkID == "" || len(chunkID) > 128 || !validChunkIDRegex.MatchString(chunkID) {
		return fmt.Errorf("%w: chunk ID must be alphanumeric and between 1-128 characters", ErrInvalidChunkID)
	}
	return nil
}

// StoreChunk streams a chunk directly to disk via a temporary file and atomically renames it.
func (p *LocalStorageProvider) StoreChunk(ctx context.Context, chunkID string, r io.Reader, size int64) (string, int64, error) {
	select {
	case <-ctx.Done():
		return "", 0, ctx.Err()
	default:
	}

	if err := p.validateChunkID(chunkID); err != nil {
		return "", 0, err
	}

	p.mu.Lock()
	if p.totalCapacity-p.usedCapacity < size {
		p.mu.Unlock()
		return "", 0, fmt.Errorf("%w: requested %d bytes, available %d bytes",
			ErrInsufficientCapacity, size, p.totalCapacity-p.usedCapacity)
	}
	// Temporarily reserve capacity during write
	p.usedCapacity += size
	p.mu.Unlock()

	// Handle rollback if write fails
	success := false
	defer func() {
		if !success {
			p.mu.Lock()
			p.usedCapacity -= size
			p.mu.Unlock()
		}
	}()

	filename := fmt.Sprintf("%s.chunk", chunkID)
	targetPath := filepath.Join(p.baseDir, filename)
	tmpPath := filepath.Join(p.baseDir, fmt.Sprintf(".tmp_%s_%d", chunkID, time.Now().UnixNano()))

	// Create and stream to temp file
	tmpFile, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return "", 0, fmt.Errorf("failed to create temporary chunk file: %w", err)
	}

	// Stream write in chunks without holding everything in memory
	written, copyErr := io.Copy(tmpFile, r)
	closeErr := tmpFile.Close()

	if copyErr != nil {
		_ = os.Remove(tmpPath)
		return "", 0, fmt.Errorf("failed to stream write chunk: %w", copyErr)
	}
	if closeErr != nil {
		_ = os.Remove(tmpPath)
		return "", 0, fmt.Errorf("failed to flush chunk file: %w", closeErr)
	}

	// Verify size if expected size was provided (> 0)
	if size > 0 && written != size {
		_ = os.Remove(tmpPath)
		return "", 0, fmt.Errorf("chunk size mismatch: expected %d bytes, wrote %d bytes", size, written)
	}

	// Atomic rename to final path
	if err := os.Rename(tmpPath, targetPath); err != nil {
		_ = os.Remove(tmpPath)
		return "", 0, fmt.Errorf("failed to finalize chunk file: %w", err)
	}

	// If size was not provided up-front, adjust used capacity with actual written bytes
	if size == 0 && written > 0 {
		p.mu.Lock()
		p.usedCapacity += written
		p.mu.Unlock()
	}

	success = true
	// Return the relative objectID within the node
	return filename, written, nil
}

// GetChunk returns a streaming ReadCloser for the chunk identified by objectID.
func (p *LocalStorageProvider) GetChunk(ctx context.Context, objectID string) (io.ReadCloser, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	cleanName := filepath.Base(objectID)
	if cleanName != objectID || objectID == "." || objectID == ".." {
		return nil, fmt.Errorf("%w: invalid objectID path", ErrInvalidChunkID)
	}

	targetPath := filepath.Join(p.baseDir, cleanName)
	file, err := os.Open(targetPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: %s", ErrChunkNotFound, objectID)
		}
		return nil, fmt.Errorf("failed to open chunk: %w", err)
	}

	return file, nil
}

// DeleteChunk permanently deletes a chunk file and restores available capacity.
func (p *LocalStorageProvider) DeleteChunk(ctx context.Context, objectID string) error {
	cleanName := filepath.Base(objectID)
	if cleanName != objectID || objectID == "." || objectID == ".." {
		return fmt.Errorf("%w: invalid objectID path", ErrInvalidChunkID)
	}

	targetPath := filepath.Join(p.baseDir, cleanName)

	p.mu.Lock()
	defer p.mu.Unlock()

	info, err := os.Stat(targetPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // idempotent deletion
		}
		return fmt.Errorf("failed to stat chunk for deletion: %w", err)
	}

	chunkSize := info.Size()
	if err := os.Remove(targetPath); err != nil {
		return fmt.Errorf("failed to remove chunk file: %w", err)
	}

	if p.usedCapacity >= chunkSize {
		p.usedCapacity -= chunkSize
	} else {
		p.usedCapacity = 0
	}

	return nil
}

// GetTotalCapacity returns the configured total capacity in bytes.
func (p *LocalStorageProvider) GetTotalCapacity(ctx context.Context) (int64, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.totalCapacity, nil
}

// GetAvailableCapacity returns the remaining capacity in bytes.
func (p *LocalStorageProvider) GetAvailableCapacity(ctx context.Context) (int64, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	available := p.totalCapacity - p.usedCapacity
	if available < 0 {
		return 0, nil
	}
	return available, nil
}

// HealthCheck verifies that the directory exists and is writable.
func (p *LocalStorageProvider) HealthCheck(ctx context.Context) (bool, error) {
	select {
	case <-ctx.Done():
		return false, ctx.Err()
	default:
	}

	probePath := filepath.Join(p.baseDir, ".health_probe")
	if err := os.WriteFile(probePath, []byte("ok"), 0644); err != nil {
		return false, fmt.Errorf("write probe failed: %w", err)
	}
	_ = os.Remove(probePath)
	return true, nil
}
