package uploads

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bwithraju/clouddump/backend/internal/chunks"
	"github.com/bwithraju/clouddump/backend/internal/database"
	"github.com/bwithraju/clouddump/backend/internal/storage"
)

var (
	ErrSessionNotFound   = errors.New("upload session not found")
	ErrSessionCompleted  = errors.New("upload session has already completed")
	ErrSessionCancelled  = errors.New("upload session was cancelled")
	ErrInvalidChunkIndex = errors.New("chunk index out of bounds")
	ErrChunkSizeMismatch = errors.New("chunk size does not match expected byte length")
)

var (
	sessionCounter uint64
	nonAlphaNumRegex = regexp.MustCompile(`[^a-zA-Z0-9_]`)
)

func sanitizeID(input string) string {
	cleaned := nonAlphaNumRegex.ReplaceAllString(input, "_")
	cleaned = strings.Trim(cleaned, "_")
	if len(cleaned) > 20 {
		cleaned = cleaned[:20]
	}
	if cleaned == "" {
		cleaned = "f"
	}
	return cleaned
}

// UploadManager manages resumable upload sessions, chunk-level idempotency, and storage coordination.
type UploadManager struct {
	storageManager   *storage.StorageManager
	repo             database.Repository
	defaultChunkSize int64
	sessions         map[string]*UploadSession
	mu               sync.RWMutex
}

// NewUploadManager constructs a new UploadManager.
func NewUploadManager(mgr *storage.StorageManager, repo database.Repository, defaultChunkSize int64) *UploadManager {
	if defaultChunkSize <= 0 {
		defaultChunkSize = chunks.DefaultChunkSize
	}
	return &UploadManager{
		storageManager:   mgr,
		repo:             repo,
		defaultChunkSize: defaultChunkSize,
		sessions:         make(map[string]*UploadSession),
	}
}

// CreateSession initializes a new resumable upload session and creates the corresponding file record.
func (m *UploadManager) CreateSession(
	ctx context.Context,
	userID string,
	filename string,
	mimeType string,
	totalSize int64,
	chunkSize int64,
) (*UploadSession, error) {
	if totalSize <= 0 {
		return nil, errors.New("total size must be greater than zero")
	}
	if chunkSize <= 0 {
		chunkSize = m.defaultChunkSize
	}
	if userID == "" {
		userID = "default_user"
	}
	if filename == "" {
		filename = fmt.Sprintf("upload_%d.bin", time.Now().Unix())
	}
	filename = filepath.Base(filename)
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}

	// Verify aggregate capacity in storage pool
	poolStats := m.storageManager.GetPoolStats(ctx)
	if poolStats.AvailableCapacity < totalSize {
		return nil, fmt.Errorf("%w: required %d bytes, available %d bytes",
			chunks.ErrInsufficientPoolStorage, totalSize, poolStats.AvailableCapacity)
	}

	totalChunks, err := chunks.CalculateTotalChunks(totalSize, chunkSize)
	if err != nil {
		return nil, fmt.Errorf("failed to calculate chunks: %w", err)
	}

	now := time.Now().UTC()
	counter := atomic.AddUint64(&sessionCounter, 1)
	uploadID := fmt.Sprintf("upload_%d_%d_%s", now.UnixNano(), counter, sanitizeID(filename))
	fileID := fmt.Sprintf("file_%d_%d_%s", now.UnixNano(), counter, sanitizeID(filename))

	// Create initial file record in database
	fileRecord := &database.FileRecord{
		ID:          fileID,
		UserID:      userID,
		Filename:    filename,
		Size:        totalSize,
		MimeType:    mimeType,
		ChunkSize:   chunkSize,
		TotalChunks: totalChunks,
		Status:      string(chunks.FileStatusUploading),
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if err := m.repo.Files().CreateFile(ctx, fileRecord); err != nil {
		return nil, fmt.Errorf("failed to create file record in database: %w", err)
	}

	session := &UploadSession{
		UploadID:        uploadID,
		FileID:          fileID,
		UserID:          userID,
		Filename:        filename,
		MimeType:        mimeType,
		TotalSize:       totalSize,
		ChunkSize:       chunkSize,
		TotalChunks:     totalChunks,
		CompletedChunks: make(map[int]string),
		FailedChunks:    make(map[int]string),
		Status:          UploadStatusInitialized,
		CreatedAt:       now,
		UpdatedAt:       now,
	}

	m.mu.Lock()
	m.sessions[uploadID] = session
	m.mu.Unlock()

	return session, nil
}

// GetSession retrieves an active or historic upload session by ID.
func (m *UploadManager) GetSession(uploadID string) (*UploadSession, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	session, exists := m.sessions[uploadID]
	if !exists {
		return nil, fmt.Errorf("%w: %s", ErrSessionNotFound, uploadID)
	}
	return session, nil
}

// ExpectedChunkSize calculates the precise byte size for a given chunk index.
func (m *UploadManager) ExpectedChunkSize(session *UploadSession, index int) int64 {
	if index < session.TotalChunks-1 {
		return session.ChunkSize
	}
	// Last chunk
	lastSize := session.TotalSize - int64(session.TotalChunks-1)*session.ChunkSize
	return lastSize
}

// StoreChunk idempotently stores a single chunk of an active upload session.
func (m *UploadManager) StoreChunk(
	ctx context.Context,
	uploadID string,
	chunkIndex int,
	stream io.Reader,
	declaredSize int64,
) (*chunks.ChunkMetadata, error) {
	session, err := m.GetSession(uploadID)
	if err != nil {
		return nil, err
	}

	session.mu.RLock()
	currentStatus := session.Status
	session.mu.RUnlock()

	if currentStatus == UploadStatusCancelled {
		return nil, ErrSessionCancelled
	}
	if currentStatus == UploadStatusCompleted {
		return nil, ErrSessionCompleted
	}

	if chunkIndex < 0 || chunkIndex >= session.TotalChunks {
		return nil, fmt.Errorf("%w: index %d, total %d", ErrInvalidChunkIndex, chunkIndex, session.TotalChunks)
	}

	expectedSize := m.ExpectedChunkSize(session, chunkIndex)
	if declaredSize > 0 && declaredSize != expectedSize {
		return nil, fmt.Errorf("%w: declared %d, expected %d", ErrChunkSizeMismatch, declaredSize, expectedSize)
	}

	// 1. Idempotency Check: Do NOT duplicate already completed chunks
	if session.IsChunkCompleted(chunkIndex) {
		chunkID := session.CompletedChunks[chunkIndex]
		// Chunk is already safely stored on a node; return existing record
		existingRecord, err := m.repo.Chunks().GetChunk(ctx, chunkID)
		if err == nil && existingRecord != nil {
			return &chunks.ChunkMetadata{
				ID:               existingRecord.ID,
				FileID:           existingRecord.FileID,
				ChunkIndex:       existingRecord.ChunkIndex,
				Size:             existingRecord.Size,
				Checksum:         existingRecord.Checksum,
				StorageNodeID:    existingRecord.StorageNodeID,
				ProviderObjectID: existingRecord.ProviderObjectID,
				Status:           chunks.ChunkStatus(existingRecord.Status),
				CreatedAt:        existingRecord.CreatedAt,
			}, nil
		}
	}

	// 2. Read chunk payload and calculate SHA-256
	var buf bytes.Buffer
	hasher := sha256.New()
	mw := io.MultiWriter(&buf, hasher)

	copied, err := io.CopyN(mw, stream, expectedSize)
	if err != nil && !errors.Is(err, io.EOF) {
		session.RecordChunkFailure(chunkIndex, fmt.Sprintf("stream read failure: %v", err))
		return nil, fmt.Errorf("failed reading chunk data: %w", err)
	}
	if copied != expectedSize {
		session.RecordChunkFailure(chunkIndex, fmt.Sprintf("size mismatch: read %d, expected %d", copied, expectedSize))
		return nil, fmt.Errorf("%w: read %d bytes, expected %d bytes", ErrChunkSizeMismatch, copied, expectedSize)
	}

	checksum := hex.EncodeToString(hasher.Sum(nil))

	// 3. Select and reserve a capacity-aware storage node
	node, err := m.storageManager.AllocateAndReserveNode(expectedSize)
	if err != nil {
		session.RecordChunkFailure(chunkIndex, fmt.Sprintf("node allocation failure: %v", err))
		return nil, fmt.Errorf("storage node allocation failed: %w", err)
	}

	chunkID := fmt.Sprintf("chk_%s_%d", session.FileID, chunkIndex)

	// 4. Store chunk directly into designated node
	objectID, _, err := m.storageManager.StoreChunk(ctx, node.ID, chunkID, bytes.NewReader(buf.Bytes()), expectedSize)
	if err != nil {
		// Roll back node capacity reservation and record failure
		node.ReleaseCapacity(expectedSize)
		session.RecordChunkFailure(chunkIndex, fmt.Sprintf("node store failure (%s): %v", node.ID, err))
		return nil, fmt.Errorf("failed storing chunk on node %s: %w", node.ID, err)
	}

	now := time.Now().UTC()
	chunkRecord := &database.ChunkRecord{
		ID:               chunkID,
		FileID:           session.FileID,
		ChunkIndex:       chunkIndex,
		Size:             expectedSize,
		Checksum:         checksum,
		StorageNodeID:    node.ID,
		ProviderObjectID: objectID,
		Status:           string(chunks.ChunkStatusStored),
		CreatedAt:        now,
	}

	// Save chunk record to database
	if err := m.repo.Chunks().CreateChunk(ctx, chunkRecord); err != nil {
		// Rollback physical chunk
		_ = m.storageManager.DeleteChunk(ctx, node.ID, objectID)
		node.ReleaseCapacity(expectedSize)
		session.RecordChunkFailure(chunkIndex, fmt.Sprintf("database chunk creation failed: %v", err))
		return nil, fmt.Errorf("failed saving chunk metadata: %w", err)
	}

	// 5. Update session tracking
	isComplete := session.RecordChunkSuccess(chunkIndex, chunkID)

	if isComplete {
		// All chunks are present! Mark file as completed in repository
		_ = m.repo.Files().UpdateFileStatus(ctx, session.FileID, string(chunks.FileStatusCompleted))
	}

	return &chunks.ChunkMetadata{
		ID:               chunkID,
		FileID:           session.FileID,
		ChunkIndex:       chunkIndex,
		Size:             expectedSize,
		Checksum:         checksum,
		StorageNodeID:    node.ID,
		ProviderObjectID: objectID,
		Status:           chunks.ChunkStatusStored,
		CreatedAt:        now,
	}, nil
}

// CancelSession terminates an upload, cleans up any already-stored chunks, and restores node capacities.
func (m *UploadManager) CancelSession(ctx context.Context, uploadID string) error {
	session, err := m.GetSession(uploadID)
	if err != nil {
		return err
	}

	session.SetStatus(UploadStatusCancelled)

	// Fetch all stored chunks for this file
	chunkRecords, err := m.repo.Chunks().ListChunksByFile(ctx, session.FileID)
	if err == nil {
		for _, c := range chunkRecords {
			_ = m.storageManager.DeleteChunk(ctx, c.StorageNodeID, c.ProviderObjectID)
		}
		_ = m.repo.Chunks().DeleteChunksByFile(ctx, session.FileID)
	}

	// Mark file as FAILED
	_ = m.repo.Files().UpdateFileStatus(ctx, session.FileID, string(chunks.FileStatusFailed))

	return nil
}

// ListSessions returns all active and recent upload sessions.
func (m *UploadManager) ListSessions(userID string) []*UploadSession {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]*UploadSession, 0, len(m.sessions))
	for _, s := range m.sessions {
		if userID == "" || s.UserID == userID {
			result = append(result, s)
		}
	}
	return result
}
