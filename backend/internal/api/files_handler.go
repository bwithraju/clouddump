package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/bwithraju/clouddump/backend/internal/chunks"
	"github.com/bwithraju/clouddump/backend/internal/database"
	"github.com/bwithraju/clouddump/backend/internal/storage"
)

// ChunkDistributorInterface abstracts streaming chunk distribution (sequential or parallel worker pool).
type ChunkDistributorInterface interface {
	Distribute(ctx context.Context, fileID string, source io.Reader, fileSize int64, callback chunks.ChunkProgressCallback) (*chunks.DistributionResult, error)
}

// FileHandler coordinates file HTTP endpoints and streaming operations.
type FileHandler struct {
	storageManager   *storage.StorageManager
	chunkDistributor ChunkDistributorInterface
	reconstructor    *chunks.FileReconstructor
	repo             database.Repository
	defaultChunkSize int64
}

// NewFileHandler constructs a new FileHandler.
func NewFileHandler(
	mgr *storage.StorageManager,
	distributor ChunkDistributorInterface,
	reconstructor *chunks.FileReconstructor,
	repo database.Repository,
	defaultChunkSize int64,
) *FileHandler {
	if defaultChunkSize <= 0 {
		defaultChunkSize = chunks.DefaultChunkSize
	}
	return &FileHandler{
		storageManager:   mgr,
		chunkDistributor: distributor,
		reconstructor:    reconstructor,
		repo:             repo,
		defaultChunkSize: defaultChunkSize,
	}
}

// Upload handles POST /api/files/upload supporting both multipart/form-data and raw streams.
func (h *FileHandler) Upload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx := r.Context()
	userID := r.Header.Get("X-User-ID")
	if userID == "" {
		userID = "default_user"
	}

	var filename string
	var fileSize int64
	var mimeType string
	var fileStream io.Reader

	contentType := r.Header.Get("Content-Type")

	// 1. Handle multipart/form-data via streaming MultipartReader (zero full-file disk/RAM buffering)
	if strings.HasPrefix(contentType, "multipart/form-data") {
		mr, err := r.MultipartReader()
		if err != nil {
			http.Error(w, fmt.Sprintf("invalid multipart payload: %v", err), http.StatusBadRequest)
			return
		}

		for {
			part, err := mr.NextPart()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				http.Error(w, fmt.Sprintf("failed reading multipart stream: %v", err), http.StatusBadRequest)
				return
			}

			if part.FormName() == "file" {
				filename = part.FileName()
				mimeType = part.Header.Get("Content-Type")
				if mimeType == "" {
					mimeType = "application/octet-stream"
				}

				// Check size from header or query param
				sizeHeader := r.Header.Get("X-File-Size")
				if sizeHeader != "" {
					fileSize, _ = strconv.ParseInt(sizeHeader, 10, 64)
				}
				if fileSize <= 0 && r.URL.Query().Get("size") != "" {
					fileSize, _ = strconv.ParseInt(r.URL.Query().Get("size"), 10, 64)
				}

				fileStream = part
				break
			}
		}
	} else {
		// 2. Direct raw streaming upload
		filename = r.Header.Get("X-Filename")
		if filename == "" {
			filename = r.URL.Query().Get("filename")
		}
		mimeType = r.Header.Get("Content-Type")
		if mimeType == "" {
			mimeType = "application/octet-stream"
		}
		fileSize = r.ContentLength
		if fileSize <= 0 && r.Header.Get("X-File-Size") != "" {
			fileSize, _ = strconv.ParseInt(r.Header.Get("X-File-Size"), 10, 64)
		}
		fileStream = r.Body
	}

	if filename == "" {
		filename = fmt.Sprintf("file_%d.bin", time.Now().Unix())
	}
	filename = filepath.Base(filename)

	if fileSize <= 0 {
		http.Error(w, "invalid or missing file size (must provide Content-Length, X-File-Size, or ?size=)", http.StatusBadRequest)
		return
	}

	// Step 1: Create initial file record in UPLOADING status
	fileID := fmt.Sprintf("file_%d_%s", time.Now().UnixNano(), sanitizeID(filename))
	totalChunks, err := chunks.CalculateTotalChunks(fileSize, h.defaultChunkSize)
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid chunk calculation: %v", err), http.StatusBadRequest)
		return
	}

	fileRecord := &database.FileRecord{
		ID:          fileID,
		UserID:      userID,
		Filename:    filename,
		Size:        fileSize,
		MimeType:    mimeType,
		ChunkSize:   h.defaultChunkSize,
		TotalChunks: totalChunks,
		Status:      string(chunks.FileStatusUploading),
	}

	if err := h.repo.Files().CreateFile(ctx, fileRecord); err != nil {
		http.Error(w, fmt.Sprintf("failed to create file record: %v", err), http.StatusInternalServerError)
		return
	}

	// Step 2: Stream and distribute chunks across storage nodes
	result, err := h.chunkDistributor.Distribute(ctx, fileID, fileStream, fileSize, nil)
	if err != nil {
		_ = h.repo.Files().UpdateFileStatus(context.Background(), fileID, string(chunks.FileStatusFailed))
		if errors.Is(err, chunks.ErrInsufficientPoolStorage) || errors.Is(err, storage.ErrInsufficientNodeCapacity) {
			http.Error(w, fmt.Sprintf("insufficient storage pool capacity: %v", err), http.StatusInsufficientStorage)
			return
		}
		http.Error(w, fmt.Sprintf("file distribution failed: %v", err), http.StatusInternalServerError)
		return
	}

	// Step 3: Batch persist chunk metadata into database
	chunkRecords := make([]*database.ChunkRecord, 0, len(result.Chunks))
	for _, chk := range result.Chunks {
		chunkRecords = append(chunkRecords, &database.ChunkRecord{
			ID:               chk.ID,
			FileID:           fileID,
			ChunkIndex:       chk.ChunkIndex,
			Size:             chk.Size,
			Checksum:         chk.Checksum,
			StorageNodeID:    chk.StorageNodeID,
			ProviderObjectID: chk.ProviderObjectID,
			Status:           string(chk.Status),
			CreatedAt:        chk.CreatedAt,
		})
	}

	if err := h.repo.Chunks().CreateChunksBatch(ctx, chunkRecords); err != nil {
		_ = h.repo.Files().UpdateFileStatus(context.Background(), fileID, string(chunks.FileStatusFailed))
		http.Error(w, fmt.Sprintf("failed persisting chunk metadata: %v", err), http.StatusInternalServerError)
		return
	}

	// Step 4: Mark file COMPLETED
	if err := h.repo.Files().UpdateFileStatus(ctx, fileID, string(chunks.FileStatusCompleted)); err != nil {
		http.Error(w, fmt.Sprintf("failed finalizing file status: %v", err), http.StatusInternalServerError)
		return
	}
	fileRecord.Status = string(chunks.FileStatusCompleted)

	// Step 5: Respond with HTTP 201 Created
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"id":           fileID,
		"filename":     filename,
		"size":         fileSize,
		"mime_type":    mimeType,
		"chunk_size":   h.defaultChunkSize,
		"total_chunks": totalChunks,
		"status":       fileRecord.Status,
		"mapping":      result.Mapping,
		"distribution": result.NodeDistribution,
	})
}

// List handles GET /api/files.
func (h *FileHandler) List(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID := r.Header.Get("X-User-ID")
	if userID == "" {
		userID = "default_user"
	}

	fileList, err := h.repo.Files().ListFilesByUser(r.Context(), userID)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to list files: %v", err), http.StatusInternalServerError)
		return
	}

	if fileList == nil {
		fileList = []*database.FileRecord{}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(fileList)
}

// GetDetails handles GET /api/files/:id.
func (h *FileHandler) GetDetails(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	fileID := r.PathValue("id")
	if fileID == "" {
		http.Error(w, "missing file id", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	file, err := h.repo.Files().GetFile(ctx, fileID)
	if err != nil {
		if errors.Is(err, database.ErrNotFound) {
			http.Error(w, "file not found", http.StatusNotFound)
			return
		}
		http.Error(w, fmt.Sprintf("database error: %v", err), http.StatusInternalServerError)
		return
	}

	chunkRecords, err := h.repo.Chunks().ListChunksByFile(ctx, fileID)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to get chunks: %v", err), http.StatusInternalServerError)
		return
	}

	// Compute node chunk distribution
	distribution := make(map[string]int)
	for _, c := range chunkRecords {
		distribution[c.StorageNodeID]++
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"file":         file,
		"chunks":       chunkRecords,
		"distribution": distribution,
	})
}

// GetStatus handles GET /api/files/:id/status.
func (h *FileHandler) GetStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	fileID := r.PathValue("id")
	if fileID == "" {
		http.Error(w, "missing file id", http.StatusBadRequest)
		return
	}

	file, err := h.repo.Files().GetFile(r.Context(), fileID)
	if err != nil {
		if errors.Is(err, database.ErrNotFound) {
			http.Error(w, "file not found", http.StatusNotFound)
			return
		}
		http.Error(w, fmt.Sprintf("database error: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"id":           file.ID,
		"filename":     file.Filename,
		"size":         file.Size,
		"status":       file.Status,
		"total_chunks": file.TotalChunks,
		"updated_at":   file.UpdatedAt,
	})
}

// Download handles GET /api/files/:id/download.
func (h *FileHandler) Download(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	fileID := r.PathValue("id")
	if fileID == "" {
		http.Error(w, "missing file id", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	file, err := h.repo.Files().GetFile(ctx, fileID)
	if err != nil {
		if errors.Is(err, database.ErrNotFound) {
			http.Error(w, "file not found", http.StatusNotFound)
			return
		}
		http.Error(w, fmt.Sprintf("database error: %v", err), http.StatusInternalServerError)
		return
	}

	if file.Status == string(chunks.FileStatusDegraded) {
		http.Error(w, "file is degraded: one or more storage nodes containing required chunks are currently offline", http.StatusServiceUnavailable)
		return
	}

	if file.Status != string(chunks.FileStatusCompleted) {
		http.Error(w, fmt.Sprintf("file is not available for download, current status: %s", file.Status), http.StatusBadRequest)
		return
	}

	chunkRecords, err := h.repo.Chunks().ListChunksByFile(ctx, fileID)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to retrieve chunks: %v", err), http.StatusInternalServerError)
		return
	}

	// Verify all nodes holding chunks are online
	for _, c := range chunkRecords {
		node, err := h.storageManager.GetNode(c.StorageNodeID)
		if err != nil || !node.IsOnline() {
			http.Error(w, fmt.Sprintf("file is degraded: storage node %s is currently offline", c.StorageNodeID), http.StatusServiceUnavailable)
			return
		}
	}

	chunkMetas := make([]*chunks.ChunkMetadata, 0, len(chunkRecords))
	for _, c := range chunkRecords {
		chunkMetas = append(chunkMetas, &chunks.ChunkMetadata{
			ID:               c.ID,
			FileID:           c.FileID,
			ChunkIndex:       c.ChunkIndex,
			Size:             c.Size,
			Checksum:         c.Checksum,
			StorageNodeID:    c.StorageNodeID,
			ProviderObjectID: c.ProviderObjectID,
			Status:           chunks.ChunkStatus(c.Status),
			CreatedAt:        c.CreatedAt,
		})
	}

	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", file.Filename))
	w.Header().Set("Content-Type", file.MimeType)
	w.Header().Set("Content-Length", strconv.FormatInt(file.Size, 10))

	// Stream reconstruction directly to client response
	written, err := h.reconstructor.ReconstructTo(ctx, chunkMetas, w)
	if err != nil {
		// If error happens before body bytes are flushed or midway
		if errors.Is(err, chunks.ErrChunkCorrupted) {
			http.Error(w, fmt.Sprintf("data integrity failure during reconstruction: %v", err), http.StatusInternalServerError)
			return
		}
		if errors.Is(err, chunks.ErrMissingChunk) {
			http.Error(w, fmt.Sprintf("missing chunk during reconstruction: %v", err), http.StatusInternalServerError)
			return
		}
		return
	}

	_ = written
}

// Delete handles DELETE /api/files/:id.
func (h *FileHandler) Delete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	fileID := r.PathValue("id")
	if fileID == "" {
		http.Error(w, "missing file id", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	file, err := h.repo.Files().GetFile(ctx, fileID)
	if err != nil {
		if errors.Is(err, database.ErrNotFound) {
			http.Error(w, "file not found", http.StatusNotFound)
			return
		}
		http.Error(w, fmt.Sprintf("database error: %v", err), http.StatusInternalServerError)
		return
	}

	// Mark status as DELETING
	_ = h.repo.Files().UpdateFileStatus(ctx, fileID, string(chunks.FileStatusDeleting))

	// Retrieve chunks and delete each chunk from its storage node
	chunkRecords, err := h.repo.Chunks().ListChunksByFile(ctx, fileID)
	if err == nil {
		for _, c := range chunkRecords {
			_ = h.storageManager.DeleteChunk(ctx, c.StorageNodeID, c.ProviderObjectID)
		}
	}

	// Remove records from database
	_ = h.repo.Chunks().DeleteChunksByFile(ctx, fileID)
	if err := h.repo.Files().DeleteFile(ctx, fileID); err != nil {
		http.Error(w, fmt.Sprintf("failed deleting file record: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":   "DELETED",
		"id":       fileID,
		"filename": file.Filename,
	})
}

func sanitizeID(name string) string {
	var b strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteRune('_')
		}
	}
	res := b.String()
	if len(res) > 20 {
		res = res[:20]
	}
	return res
}
