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

// NewParallelUploader initializes a concurrent uploader pool.
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

// WorkerCount returns current active worker concurrency.
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

// Distribute processes chunks concurrently using a fixed worker pool of goroutines with factor 1.
func (u *ParallelUploader) Distribute(
	ctx context.Context,
	fileID string,
	source io.Reader,
	fileSize int64,
	callback chunks.ChunkProgressCallback,
) (*chunks.DistributionResult, error) {
	return u.DistributeWithReplication(ctx, fileID, source, fileSize, 1, callback)
}

// DistributeWithReplication processes chunks concurrently using workers, replicating across distinct nodes when replicationFactor > 1.
func (u *ParallelUploader) DistributeWithReplication(
	ctx context.Context,
	fileID string,
	source io.Reader,
	fileSize int64,
	replicationFactor int,
	callback chunks.ChunkProgressCallback,
) (*chunks.DistributionResult, error) {
	if fileSize <= 0 {
		return nil, chunks.ErrEmptyFile
	}
	if replicationFactor < 1 {
		replicationFactor = 1
	}

	totalChunks, err := chunks.CalculateTotalChunks(fileSize, u.chunkSize)
	if err != nil {
		return nil, err
	}

	// Pre-flight check on pool capacity considering replication
	totalRequired := fileSize * int64(replicationFactor)
	poolStats := u.storageManager.GetPoolStats(ctx)
	if poolStats.AvailableCapacity < totalRequired {
		return nil, fmt.Errorf("%w: file requires %d bytes, pool available %d bytes",
			chunks.ErrInsufficientPoolStorage, totalRequired, poolStats.AvailableCapacity)
	}

	if replicationFactor > poolStats.OnlineNodes {
		return nil, fmt.Errorf("cannot satisfy replication factor %d: only %d online nodes available",
			replicationFactor, poolStats.OnlineNodes)
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

				// Select and reserve primary node capacity atomically
				targetNode, err := u.storageManager.AllocateAndReserveNode(task.ExpectedSize)
				if err != nil {
					cancelProc()
					results <- ChunkResult{Index: task.Index, Err: fmt.Errorf("worker %d failed allocating primary node: %w", workerID, err)}
					continue
				}

				chunkID := fmt.Sprintf("chk_%s_%04d", task.FileID, task.Index)

				// Compute SHA-256 and store chunk
				hasher := sha256.New()
				hasher.Write(task.Data)
				checksum := hex.EncodeToString(hasher.Sum(nil))

				objectID, written, err := u.storageManager.StoreChunk(procCtx, targetNode.ID, chunkID, bytes.NewReader(task.Data), task.ExpectedSize)
				if err != nil {
					targetNode.ReleaseCapacity(task.ExpectedSize)
					cancelProc()
					results <- ChunkResult{Index: task.Index, Err: fmt.Errorf("worker %d failed storing primary chunk %d on %s: %w", workerID, task.Index, targetNode.ID, err)}
					continue
				}

				if written != task.ExpectedSize {
					targetNode.ReleaseCapacity(task.ExpectedSize)
					cancelProc()
					results <- ChunkResult{Index: task.Index, Err: fmt.Errorf("worker %d: size mismatch on chunk %d", workerID, task.Index)}
					continue
				}

				meta := &chunks.ChunkMetadata{
					ID:               chunkID,
					FileID:           task.FileID,
					ChunkIndex:       task.Index,
					Size:             written,
					Checksum:         checksum,
					StorageNodeID:    targetNode.ID,
					ProviderObjectID: objectID,
					Status:           chunks.ChunkStatusStored,
					Replicas:         make([]*chunks.ChunkReplica, 0),
					CreatedAt:        time.Now().UTC(),
				}

				// Store replicas if factor > 1
				if replicationFactor > 1 {
					excludedNodes := []string{targetNode.ID}
					var repErr error
					for r := 1; r < replicationFactor; r++ {
						repNode, err := u.storageManager.AllocateAndReserveNode(task.ExpectedSize, excludedNodes...)
						if err != nil {
							repErr = fmt.Errorf("worker %d failed allocating replica %d for chunk %d: %w", workerID, r, task.Index, err)
							break
						}
						excludedNodes = append(excludedNodes, repNode.ID)

						repChunkID := fmt.Sprintf("chk_%s_%04d_rep%d", task.FileID, task.Index, r)
						repObjID, repWritten, err := u.storageManager.StoreChunk(procCtx, repNode.ID, repChunkID, bytes.NewReader(task.Data), task.ExpectedSize)
						if err != nil || repWritten != task.ExpectedSize {
							repNode.ReleaseCapacity(task.ExpectedSize)
							repErr = fmt.Errorf("worker %d failed storing replica %d of chunk %d: %w", workerID, r, task.Index, err)
							break
						}
						meta.AddReplica(repNode.ID, repObjID, chunks.ChunkStatusStored)
					}
					if repErr != nil {
						cancelProc()
						results <- ChunkResult{Index: task.Index, Err: repErr}
						continue
					}
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
		for _, rep := range res.ChunkMeta.Replicas {
			mapping[res.Index][rep.StorageNodeID] = rep.ProviderObjectID
			nodeDistribution[rep.StorageNodeID]++
		}
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
		log.Printf("[CloudDump] Parallel upload failed (%v). Rolling back stored chunks for %s...", firstErr, fileID)
		for _, chunk := range storedChunks {
			if chunk != nil {
				_ = u.storageManager.DeleteChunk(context.Background(), chunk.StorageNodeID, chunk.ProviderObjectID)
				for _, rep := range chunk.Replicas {
					_ = u.storageManager.DeleteChunk(context.Background(), rep.StorageNodeID, rep.ProviderObjectID)
				}
			}
		}
		return nil, firstErr
	}

	// 5. Verify all chunks accounted for
	for i, c := range storedChunks {
		if c == nil {
			for _, chunk := range storedChunks {
				if chunk != nil {
					_ = u.storageManager.DeleteChunk(context.Background(), chunk.StorageNodeID, chunk.ProviderObjectID)
					for _, rep := range chunk.Replicas {
						_ = u.storageManager.DeleteChunk(context.Background(), rep.StorageNodeID, rep.ProviderObjectID)
					}
				}
			}
			return nil, fmt.Errorf("parallel upload incomplete: missing chunk %d of %d", i, totalChunks)
		}
	}

	return &chunks.DistributionResult{
		FileID:            fileID,
		TotalSize:         fileSize,
		ChunkSize:         u.chunkSize,
		TotalChunks:       totalChunks,
		ReplicationFactor: replicationFactor,
		Status:            chunks.FileStatusCompleted,
		Chunks:            storedChunks,
		Mapping:           mapping,
		NodeDistribution:  nodeDistribution,
	}, nil
}
