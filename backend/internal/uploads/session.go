package uploads

import (
	"sync"
	"time"
)

// UploadStatus represents the lifecycle state of a resumable upload session.
type UploadStatus string

const (
	UploadStatusInitialized UploadStatus = "INITIALIZED"
	UploadStatusUploading   UploadStatus = "UPLOADING"
	UploadStatusCompleted   UploadStatus = "COMPLETED"
	UploadStatusFailed      UploadStatus = "FAILED"
	UploadStatusCancelled   UploadStatus = "CANCELLED"
)

// UploadSession tracks chunk-level progress, status, and failure metrics for a file upload.
type UploadSession struct {
	UploadID        string                 `json:"upload_id"`
	FileID          string                 `json:"file_id"`
	UserID          string                 `json:"user_id"`
	Filename        string                 `json:"filename"`
	MimeType        string                 `json:"mime_type"`
	TotalSize       int64                  `json:"total_size"`
	ChunkSize       int64                  `json:"chunk_size"`
	TotalChunks     int                    `json:"total_chunks"`
	CompletedChunks map[int]string         `json:"completed_chunks"` // chunk_index -> chunk_id
	FailedChunks    map[int]string         `json:"failed_chunks"`    // chunk_index -> error message
	Status          UploadStatus           `json:"status"`
	CreatedAt       time.Time              `json:"created_at"`
	UpdatedAt       time.Time              `json:"updated_at"`
	mu              sync.RWMutex           `json:"-"`
}

// SessionSummary provides a client-facing JSON snapshot of session progress.
type SessionSummary struct {
	UploadID         string          `json:"upload_id"`
	FileID           string          `json:"file_id"`
	Filename         string          `json:"filename"`
	TotalSize        int64           `json:"total_size"`
	ChunkSize        int64           `json:"chunk_size"`
	TotalChunks      int             `json:"total_chunks"`
	CompletedCount   int             `json:"completed_count"`
	CompletedIndices []int           `json:"completed_indices"`
	FailedCount      int             `json:"failed_count"`
	FailedChunks     map[int]string  `json:"failed_chunks"`
	Status           UploadStatus    `json:"status"`
	ProgressPercent  float64         `json:"progress_percent"`
	NextExpected     int             `json:"next_expected"`
	CreatedAt        time.Time       `json:"created_at"`
	UpdatedAt        time.Time       `json:"updated_at"`
}

// Summary returns an aggregated snapshot of the session state.
func (s *UploadSession) Summary() SessionSummary {
	s.mu.RLock()
	defer s.mu.RUnlock()

	completedIndices := make([]int, 0, len(s.CompletedChunks))
	for idx := range s.CompletedChunks {
		completedIndices = append(completedIndices, idx)
	}

	failedCopy := make(map[int]string, len(s.FailedChunks))
	for k, v := range s.FailedChunks {
		failedCopy[k] = v
	}

	completedCount := len(s.CompletedChunks)
	var progress float64
	if s.TotalChunks > 0 {
		progress = float64(completedCount) / float64(s.TotalChunks) * 100.0
	}

	// Find the smallest missing chunk index
	nextExpected := s.TotalChunks
	for i := 0; i < s.TotalChunks; i++ {
		if _, ok := s.CompletedChunks[i]; !ok {
			nextExpected = i
			break
		}
	}

	return SessionSummary{
		UploadID:         s.UploadID,
		FileID:           s.FileID,
		Filename:         s.Filename,
		TotalSize:        s.TotalSize,
		ChunkSize:        s.ChunkSize,
		TotalChunks:      s.TotalChunks,
		CompletedCount:   completedCount,
		CompletedIndices: completedIndices,
		FailedCount:      len(failedCopy),
		FailedChunks:     failedCopy,
		Status:           s.Status,
		ProgressPercent:  progress,
		NextExpected:     nextExpected,
		CreatedAt:        s.CreatedAt,
		UpdatedAt:        s.UpdatedAt,
	}
}

// IsChunkCompleted checks if a given chunk index was already successfully stored.
func (s *UploadSession) IsChunkCompleted(index int) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, exists := s.CompletedChunks[index]
	return exists
}

// RecordChunkSuccess records a successfully stored chunk and clears any prior failure record.
func (s *UploadSession) RecordChunkSuccess(index int, chunkID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.FailedChunks, index)
	s.CompletedChunks[index] = chunkID
	s.UpdatedAt = time.Now().UTC()
	s.Status = UploadStatusUploading

	if len(s.CompletedChunks) == s.TotalChunks {
		s.Status = UploadStatusCompleted
		return true
	}
	return false
}

// RecordChunkFailure records an error for a specific chunk without aborting the entire session.
func (s *UploadSession) RecordChunkFailure(index int, errStr string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.FailedChunks[index] = errStr
	s.UpdatedAt = time.Now().UTC()
}

// SetStatus updates the session status.
func (s *UploadSession) SetStatus(status UploadStatus) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.Status = status
	s.UpdatedAt = time.Now().UTC()
}
