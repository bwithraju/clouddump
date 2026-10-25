package chunks

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"log"
	"sort"

	"github.com/bwithraju/clouddump/backend/internal/storage"
)

// VerifyingReader streams chunk data while calculating SHA-256 on the fly.
// Upon reaching EOF, it verifies the checksum against expectedChecksum.
// If corrupted, it marks chunk.Status = ChunkStatusCorrupted and returns ErrChunkCorrupted.
type VerifyingReader struct {
	rc               io.ReadCloser
	hasher           hash.Hash
	expectedChecksum string
	chunk            *ChunkMetadata
	verified         bool
	bytesRead        int64
}

// NewVerifyingReader constructs a VerifyingReader for a chunk.
func NewVerifyingReader(rc io.ReadCloser, expectedChecksum string, chunk *ChunkMetadata) *VerifyingReader {
	return &VerifyingReader{
		rc:               rc,
		hasher:           sha256.New(),
		expectedChecksum: expectedChecksum,
		chunk:            chunk,
	}
}

func (vr *VerifyingReader) Read(p []byte) (int, error) {
	n, err := vr.rc.Read(p)
	if n > 0 {
		vr.hasher.Write(p[:n])
		vr.bytesRead += int64(n)
	}

	if errors.Is(err, io.EOF) {
		if !vr.verified {
			vr.verified = true
			actualChecksum := hex.EncodeToString(vr.hasher.Sum(nil))
			if vr.expectedChecksum != "" && actualChecksum != vr.expectedChecksum {
				if vr.chunk != nil {
					vr.chunk.Status = ChunkStatusCorrupted
				}
				chunkIdx := -1
				if vr.chunk != nil {
					chunkIdx = vr.chunk.ChunkIndex
				}
				return n, fmt.Errorf("%w: chunk index %d checksum mismatch (expected %s, got %s)",
					ErrChunkCorrupted, chunkIdx, vr.expectedChecksum, actualChecksum)
			}
		}
	}

	return n, err
}

func (vr *VerifyingReader) Close() error {
	return vr.rc.Close()
}

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

// RetrieveChunkStream attempts to open a streaming read handle for the chunk,
// querying the primary storage node first and automatically falling back to replicas
// if the primary node is offline or returns an error.
func (r *FileReconstructor) RetrieveChunkStream(ctx context.Context, chunk *ChunkMetadata) (io.ReadCloser, string, error) {
	if chunk == nil {
		return nil, "", errors.New("cannot retrieve nil chunk")
	}

	// 1. Attempt primary node if online
	primaryNode, err := r.storageManager.GetNode(chunk.StorageNodeID)
	if err == nil && primaryNode.IsOnline() {
		rc, err := r.storageManager.GetChunk(ctx, chunk.StorageNodeID, chunk.ProviderObjectID)
		if err == nil {
			return rc, chunk.StorageNodeID, nil
		}
		log.Printf("[CloudDump] Primary node %s failed to get chunk %d: %v. Attempting replicas...", chunk.StorageNodeID, chunk.ChunkIndex, err)
	}

	// 2. Attempt replicas in order
	for _, replica := range chunk.Replicas {
		if replica == nil {
			continue
		}
		repNode, err := r.storageManager.GetNode(replica.StorageNodeID)
		if err == nil && repNode.IsOnline() {
			rc, err := r.storageManager.GetChunk(ctx, replica.StorageNodeID, replica.ProviderObjectID)
			if err == nil {
				log.Printf("[CloudDump] Successfully retrieved chunk %d from replica node %s (object: %s)",
					chunk.ChunkIndex, replica.StorageNodeID, replica.ProviderObjectID)
				return rc, replica.StorageNodeID, nil
			}
		}
	}

	return nil, "", fmt.Errorf("%w: chunk index %d (node %s, object %s) unavailable and no online replicas succeeded",
		ErrMissingChunk, chunk.ChunkIndex, chunk.StorageNodeID, chunk.ProviderObjectID)
}

// ReconstructTo streams the reconstructed file directly into dest in chunk_index order.
// Verifies SHA-256 checksum for each chunk as it streams.
// If any chunk is corrupted, missing, or altered, it halts and returns a clear error.
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

		// Retrieve chunk from primary node or online replica
		chunkRC, _, err := r.RetrieveChunkStream(ctx, chunk)
		if err != nil {
			return totalWritten, err
		}

		// Wrap in verifying reader to validate SHA-256 on the fly
		verifyingRC := NewVerifyingReader(chunkRC, chunk.Checksum, chunk)

		// Stream chunk directly into dest
		written, copyErr := io.Copy(dest, verifyingRC)
		closeErr := verifyingRC.Close()

		if copyErr != nil {
			return totalWritten, fmt.Errorf("failed streaming chunk %d to destination: %w", chunk.ChunkIndex, copyErr)
		}
		if closeErr != nil {
			return totalWritten, fmt.Errorf("failed closing chunk %d reader: %w", chunk.ChunkIndex, closeErr)
		}

		if written != chunk.Size {
			if chunk != nil {
				chunk.Status = ChunkStatusCorrupted
			}
			return totalWritten, fmt.Errorf("%w: chunk %d size mismatch (expected %d bytes, read %d bytes)",
				ErrChunkCorrupted, chunk.ChunkIndex, chunk.Size, written)
		}

		totalWritten += written
	}

	return totalWritten, nil
}

// ReconstructedReader provides an io.ReadCloser that lazily reads and verifies across distributed chunks sequentially.
type ReconstructedReader struct {
	ctx           context.Context
	reconstructor *FileReconstructor
	chunks        []*ChunkMetadata
	currentIndex  int
	currentReader io.ReadCloser
}

// NewReconstructedReader creates a streaming ReadCloser over all chunks with on-the-fly checksum validation.
func (r *FileReconstructor) NewReconstructedReader(
	ctx context.Context,
	chunkList []*ChunkMetadata,
) (io.ReadCloser, error) {
	orderedChunks, err := OrderAndValidateChunks(chunkList)
	if err != nil {
		return nil, err
	}

	return &ReconstructedReader{
		ctx:           ctx,
		reconstructor: r,
		chunks:        orderedChunks,
		currentIndex:  0,
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
			rc, _, err := rr.reconstructor.RetrieveChunkStream(rr.ctx, chunk)
			if err != nil {
				return 0, err
			}
			rr.currentReader = NewVerifyingReader(rc, chunk.Checksum, chunk)
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

// VerifyChunk independently checks the physical data of a chunk against its expected SHA-256 checksum.
func (r *FileReconstructor) VerifyChunk(ctx context.Context, chunk *ChunkMetadata) (bool, error) {
	if chunk == nil {
		return false, errors.New("cannot verify nil chunk")
	}

	rc, _, err := r.RetrieveChunkStream(ctx, chunk)
	if err != nil {
		chunk.Status = ChunkStatusUnavailable
		return false, fmt.Errorf("%w: failed to open chunk %d: %v",
			ErrMissingChunk, chunk.ChunkIndex, err)
	}
	defer rc.Close()

	hasher := sha256.New()
	written, err := io.Copy(hasher, rc)
	if err != nil {
		return false, fmt.Errorf("failed reading chunk data: %w", err)
	}

	actualChecksum := hex.EncodeToString(hasher.Sum(nil))
	if written != chunk.Size || actualChecksum != chunk.Checksum {
		chunk.Status = ChunkStatusCorrupted
		return false, fmt.Errorf("%w: chunk %d checksum expected %s, got %s (size expected %d, read %d)",
			ErrChunkCorrupted, chunk.ChunkIndex, chunk.Checksum, actualChecksum, chunk.Size, written)
	}

	chunk.Status = ChunkStatusStored
	return true, nil
}

// VerifyAllChunks audits the integrity of all chunks for a file across their storage nodes.
func (r *FileReconstructor) VerifyAllChunks(ctx context.Context, chunks []*ChunkMetadata) (bool, error) {
	for _, chunk := range chunks {
		healthy, err := r.VerifyChunk(ctx, chunk)
		if err != nil || !healthy {
			return false, err
		}
	}
	return true, nil
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
