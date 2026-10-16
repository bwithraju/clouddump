package chunks

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"sort"

	"github.com/bwithraju/clouddump/backend/internal/storage"
)

// FileReconstructor handles retrieving ordered chunks from distributed storage nodes
// and streaming the reconstructed file without buffering into RAM.
type FileReconstructor struct {
	storageManager *storage.StorageManager
}

// NewFileReconstructor creates a new reconstructor instance.
func NewFileReconstructor(mgr *storage.StorageManager) (*FileReconstructor, error) {
	if mgr == nil {
		return nil, errors.New("storage manager cannot be nil")
	}
	return &FileReconstructor{
		storageManager: mgr,
	}, nil
}

// OrderAndValidateChunks ensures chunks form a complete, continuous 0-indexed sequence.
func OrderAndValidateChunks(chunkList []*ChunkMetadata) ([]*ChunkMetadata, error) {
	if len(chunkList) == 0 {
		return nil, fmt.Errorf("%w: chunk list is empty", ErrIncompleteFile)
	}

	ordered := make([]*ChunkMetadata, len(chunkList))
	copy(ordered, chunkList)
	sort.Slice(ordered, func(i, j int) bool {
		return ordered[i].ChunkIndex < ordered[j].ChunkIndex
	})

	for i := 0; i < len(ordered); i++ {
		if ordered[i] == nil {
			return nil, fmt.Errorf("%w: nil chunk metadata at index %d", ErrMissingChunk, i)
		}
		if ordered[i].ChunkIndex != i {
			return nil, fmt.Errorf("%w: expected chunk index %d, found %d", ErrMissingChunk, i, ordered[i].ChunkIndex)
		}
	}

	return ordered, nil
}

// ReconstructTo streams the reconstructed file directly into dest in chunk_index order.
// Memory footprint is strictly bounded by io.Copy buffer size (~32KB).
// If any chunk is missing, unreadable, or corrupted, it fails fast with a clear error.
func (r *FileReconstructor) ReconstructTo(
	ctx context.Context,
	chunkList []*ChunkMetadata,
	dest io.Writer,
) (int64, error) {
	orderedChunks, err := OrderAndValidateChunks(chunkList)
	if err != nil {
		return 0, err
	}

	var totalWritten int64

	for _, chunk := range orderedChunks {
		select {
		case <-ctx.Done():
			return totalWritten, ctx.Err()
		default:
		}

		// Retrieve chunk from its designated storage node
		chunkRC, err := r.storageManager.GetChunk(ctx, chunk.StorageNodeID, chunk.ProviderObjectID)
		if err != nil {
			return totalWritten, fmt.Errorf("%w: chunk index %d (node %s, object %s): %v",
				ErrMissingChunk, chunk.ChunkIndex, chunk.StorageNodeID, chunk.ProviderObjectID, err)
		}

		// Stream chunk directly into dest
		written, copyErr := io.Copy(dest, chunkRC)
		closeErr := chunkRC.Close()

		if copyErr != nil {
			return totalWritten, fmt.Errorf("failed streaming chunk %d to destination: %w", chunk.ChunkIndex, copyErr)
		}
		if closeErr != nil {
			return totalWritten, fmt.Errorf("failed closing chunk %d reader: %w", chunk.ChunkIndex, closeErr)
		}

		if written != chunk.Size {
			return totalWritten, fmt.Errorf("%w: chunk %d size mismatch (expected %d bytes, read %d bytes)",
				ErrChunkChecksumMismatch, chunk.ChunkIndex, chunk.Size, written)
		}

		totalWritten += written
	}

	return totalWritten, nil
}

// ReconstructedReader provides an io.ReadCloser that lazily reads across distributed chunks sequentially.
type ReconstructedReader struct {
	ctx            context.Context
	storageManager *storage.StorageManager
	chunks         []*ChunkMetadata
	currentIndex   int
	currentReader  io.ReadCloser
}

// NewReconstructedReader creates a streaming ReadCloser over all chunks.
func (r *FileReconstructor) NewReconstructedReader(
	ctx context.Context,
	chunkList []*ChunkMetadata,
) (io.ReadCloser, error) {
	orderedChunks, err := OrderAndValidateChunks(chunkList)
	if err != nil {
		return nil, err
	}

	return &ReconstructedReader{
		ctx:            ctx,
		storageManager: r.storageManager,
		chunks:         orderedChunks,
		currentIndex:   0,
	}, nil
}

func (rr *ReconstructedReader) Read(p []byte) (int, error) {
	for {
		select {
		case <-rr.ctx.Done():
			return 0, rr.ctx.Err()
		default:
		}

		// If we don't have an active reader and haven't exhausted chunks, open next
		if rr.currentReader == nil {
			if rr.currentIndex >= len(rr.chunks) {
				return 0, io.EOF
			}

			chunk := rr.chunks[rr.currentIndex]
			rc, err := rr.storageManager.GetChunk(rr.ctx, chunk.StorageNodeID, chunk.ProviderObjectID)
			if err != nil {
				return 0, fmt.Errorf("%w: chunk %d from node %s: %v",
					ErrMissingChunk, chunk.ChunkIndex, chunk.StorageNodeID, err)
			}
			rr.currentReader = rc
		}

		n, err := rr.currentReader.Read(p)
		if n > 0 {
			return n, nil
		}

		if errors.Is(err, io.EOF) {
			_ = rr.currentReader.Close()
			rr.currentReader = nil
			rr.currentIndex++
			continue
		}

		if err != nil {
			_ = rr.currentReader.Close()
			rr.currentReader = nil
			return 0, err
		}
	}
}

func (rr *ReconstructedReader) Close() error {
	if rr.currentReader != nil {
		err := rr.currentReader.Close()
		rr.currentReader = nil
		return err
	}
	return nil
}

// CalculateReconstructedSHA256 streams the reconstructed file and calculates its SHA-256 hash.
func (r *FileReconstructor) CalculateReconstructedSHA256(
	ctx context.Context,
	chunkList []*ChunkMetadata,
) (string, int64, error) {
	hasher := sha256.New()
	totalWritten, err := r.ReconstructTo(ctx, chunkList, hasher)
	if err != nil {
		return "", totalWritten, err
	}
	return hex.EncodeToString(hasher.Sum(nil)), totalWritten, nil
}
