package workers

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/bwithraju/clouddump/backend/internal/chunks"
	"github.com/bwithraju/clouddump/backend/internal/storage"
)

const (
	DefaultWorkerCount = 4
	MaxAllowedWorkers  = 32
)

// ChunkTask encapsulates data and coordinates for a single chunk to be stored.
type ChunkTask struct {
	Index        int
	FileID       string
	Data         []byte
	ExpectedSize int64
}

// ChunkResult captures the outcome of storing a single chunk by a worker.
type ChunkResult struct {
	Index     int
	ChunkMeta *chunks.ChunkMetadata
	NodeID    string
	ObjectID  string
	Written   int64
	Err       error
}

// ParallelUploader coordinates parallel chunk distribution across storage nodes with controlled concurrency.
type ParallelUploader struct {
	storageManager *storage.StorageManager
	chunkSize      int64
	workerCount    int
	mu             sync.RWMutex
}

// ResolveWorkerCount determines the worker count from parameter, environment, or default.
func ResolveWorkerCount(requested int) int {
	if requested > 0 {
		if requested > MaxAllowedWorkers {
			return MaxAllowedWorkers
		}
		return requested
	}

	envVal := os.Getenv("MAX_UPLOAD_WORKERS")
	if envVal != "" {
		if parsed, err := strconv.Atoi(envVal); err == nil && parsed > 0 {
			if parsed > MaxAllowedWorkers {
				return MaxAllowedWorkers
			}
			return parsed
		}
	}

	return DefaultWorkerCount
}

// NewParallelUploader initializes a ParallelUploader with controlled worker pool size.
func NewParallelUploader(mgr *storage.StorageManager, chunkSize int64, workerCount int) (*ParallelUploader, error) {
	if mgr == nil {
		return nil, errors.New("storage manager cannot be nil")
	}
	if chunkSize <= 0 {
		chunkSize = chunks.DefaultChunkSize
	}

	workers := ResolveWorkerCount(workerCount)

	return &ParallelUploader{
		storageManager: mgr,
		chunkSize:      chunkSize,
		workerCount:    workers,
	}, nil
}

// WorkerCount returns the active number of worker goroutines.
func (u *ParallelUploader) WorkerCount() int {
	u.mu.RLock()
	defer u.mu.RUnlock()
	return u.workerCount
}

// SetWorkerCount updates the active worker pool size.
func (u *ParallelUploader) SetWorkerCount(n int) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.workerCount = ResolveWorkerCount(n)
}

// Distribute processes chunks concurrently using a fixed worker pool of goroutines.
// Chunks are read sequentially from the stream without buffering the whole file in RAM,
// and dispatched to worker goroutines up to workerCount buffer capacity.
func (u *ParallelUploader) Distribute(
	ctx context.Context,
	fileID string,
	source io.Reader,
	fileSize int64,
	callback chunks.ChunkProgressCallback,
) (*chunks.DistributionResult, error) {
	if fileSize <= 0 {
		return nil, chunks.ErrEmptyFile
	}

	totalChunks, err := chunks.CalculateTotalChunks(fileSize, u.chunkSize)
	if err != nil {
		return nil, err
	}

	// Pre-flight check on pool capacity
	poolStats := u.storageManager.GetPoolStats(ctx)
	if poolStats.AvailableCapacity < fileSize {
		return nil, fmt.Errorf("%w: file requires %d bytes, pool available %d bytes",
			chunks.ErrInsufficientPoolStorage, fileSize, poolStats.AvailableCapacity)
	}

	u.mu.RLock()
	workers := u.workerCount
	u.mu.RUnlock()

	// If single chunk or 1 worker requested, use sequential path directly
	if workers <= 1 || totalChunks == 1 {
		workers = 1
	}

	// Create cancellable context for worker failure cascade
	procCtx, cancelProc := context.WithCancel(ctx)
	defer cancelProc()

	// Bounded channels: jobs buffer matches worker count to enforce O(1) RAM usage
	jobs := make(chan ChunkTask, workers)
	results := make(chan ChunkResult, totalChunks)

	// 1. Launch fixed set of Worker Goroutines (no unbounded goroutines)
	var workerWg sync.WaitGroup
	for w := 0; w < workers; w++ {
		workerWg.Add(1)
		go func(workerID int) {
			defer workerWg.Done()
			for task := range jobs {
				// Check if another worker already encountered an unrecoverable failure
				select {
				case <-procCtx.Done():
					results <- ChunkResult{Index: task.Index, Err: procCtx.Err()}
					continue
				default:
				}

				// Select and reserve node capacity atomically
				targetNode, err := u.storageManager.AllocateAndReserveNode(task.ExpectedSize)
				if err != nil {
					cancelProc()
					results <- ChunkResult{Index: task.Index, Err: fmt.Errorf("worker %d failed allocating node: %w", workerID, err)}
					continue
				}

				chunkID := fmt.Sprintf("chk_%s_%04d", task.FileID, task.Index)

				// Compute SHA-256 and store chunk
				hasher := sha256.New()
				teeReader := io.TeeReader(bytes.NewReader(task.Data), hasher)

				objectID, written, err := u.storageManager.StoreChunk(procCtx, targetNode.ID, chunkID, teeReader, task.ExpectedSize)
				if err != nil {
					targetNode.ReleaseCapacity(task.ExpectedSize)
					cancelProc()
					results <- ChunkResult{Index: task.Index, Err: fmt.Errorf("worker %d failed storing chunk %d on %s: %w", workerID, task.Index, targetNode.ID, err)}
					continue
				}

				if written != task.ExpectedSize {
					targetNode.ReleaseCapacity(task.ExpectedSize)
					cancelProc()
					results <- ChunkResult{Index: task.Index, Err: fmt.Errorf("worker %d: size mismatch on chunk %d", workerID, task.Index)}
					continue
				}

				checksum := hex.EncodeToString(hasher.Sum(nil))

				meta := &chunks.ChunkMetadata{
					ID:               chunkID,
					FileID:           task.FileID,
					ChunkIndex:       task.Index,
					Size:             written,
					Checksum:         checksum,
					StorageNodeID:    targetNode.ID,
					ProviderObjectID: objectID,
					Status:           chunks.ChunkStatusStored,
					CreatedAt:        time.Now().UTC(),
				}

				results <- ChunkResult{
					Index:     task.Index,
					ChunkMeta: meta,
					NodeID:    targetNode.ID,
					ObjectID:  objectID,
					Written:   written,
				}
			}
		}(w)
	}

	// 2. Feeder Goroutine: Reads chunks sequentially from source stream and feeds jobs channel
	var feedErr error
	var feederWg sync.WaitGroup
	feederWg.Add(1)

	go func() {
		defer feederWg.Done()
		defer close(jobs)

		for i := 0; i < totalChunks; i++ {
			select {
			case <-procCtx.Done():
				feedErr = procCtx.Err()
				return
			default:
			}

			expectedSize := chunks.ExpectedChunkSize(fileSize, u.chunkSize, i)
			chunkBuf := make([]byte, expectedSize)

			n, err := io.ReadFull(source, chunkBuf)
			if err != nil {
				feedErr = fmt.Errorf("feeder failed reading chunk %d: expected %d bytes, got %d: %w", i, expectedSize, n, err)
				cancelProc()
				return
			}

			task := ChunkTask{
				Index:        i,
				FileID:       fileID,
				Data:         chunkBuf,
				ExpectedSize: expectedSize,
			}

			select {
			case <-procCtx.Done():
				feedErr = procCtx.Err()
				return
			case jobs <- task:
			}
		}
	}()

	// Closer goroutine: waits for workers to finish, then closes results channel
	go func() {
		workerWg.Wait()
		close(results)
	}()

	// 3. Collector: Aggregates results, tracks progress, and executes rollback if any worker failed
	storedChunks := make([]*chunks.ChunkMetadata, totalChunks)
	mapping := make(chunks.ChunkMapping)
	nodeDistribution := make(map[string]int)
	var totalBytesUploaded int64
	var firstErr error

	for res := range results {
		if res.Err != nil {
			if firstErr == nil {
				firstErr = res.Err
			}
			continue
		}

		storedChunks[res.Index] = res.ChunkMeta

		if mapping[res.Index] == nil {
			mapping[res.Index] = make(map[string]string)
		}
		mapping[res.Index][res.NodeID] = res.ObjectID
		nodeDistribution[res.NodeID]++
		totalBytesUploaded += res.Written

		if callback != nil && firstErr == nil {
			callback(res.ChunkMeta, res.Index+1, totalChunks, totalBytesUploaded, fileSize)
		}
	}

	feederWg.Wait()

	// 4. Error Propagation & Rollback
	if feedErr != nil && firstErr == nil {
		firstErr = feedErr
	}

	if firstErr != nil {
		// Roll back any chunks successfully written by other workers
		log.Printf("[CloudDump] Parallel upload failed (%v). Rolling back stored chunks for %s...", firstErr, fileID)
		for _, chunk := range storedChunks {
			if chunk != nil {
				_ = u.storageManager.DeleteChunk(context.Background(), chunk.StorageNodeID, chunk.ProviderObjectID)
			}
		}
		return nil, firstErr
	}

	// 5. Verify all chunks accounted for
	for i, c := range storedChunks {
		if c == nil {
			// Roll back
			for _, chunk := range storedChunks {
				if chunk != nil {
					_ = u.storageManager.DeleteChunk(context.Background(), chunk.StorageNodeID, chunk.ProviderObjectID)
				}
			}
			return nil, fmt.Errorf("parallel upload incomplete: missing chunk %d of %d", i, totalChunks)
		}
	}

	return &chunks.DistributionResult{
		FileID:           fileID,
		TotalSize:        fileSize,
		ChunkSize:        u.chunkSize,
		TotalChunks:      totalChunks,
		Status:           chunks.FileStatusCompleted,
		Chunks:           storedChunks,
		Mapping:          mapping,
		NodeDistribution: nodeDistribution,
	}, nil
}
