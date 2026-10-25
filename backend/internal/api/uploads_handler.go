package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/bwithraju/clouddump/backend/internal/auth"
	"github.com/bwithraju/clouddump/backend/internal/chunks"
	"github.com/bwithraju/clouddump/backend/internal/uploads"
)

// UploadsHandler exposes HTTP REST endpoints for resumable upload sessions.
type UploadsHandler struct {
	uploadManager *uploads.UploadManager
}

// NewUploadsHandler constructs an UploadsHandler.
func NewUploadsHandler(mgr *uploads.UploadManager) *UploadsHandler {
	return &UploadsHandler{
		uploadManager: mgr,
	}
}

// InitSessionRequest payload for initializing a resumable upload session.
type InitSessionRequest struct {
	Filename  string `json:"filename"`
	Size      int64  `json:"size"`
	MimeType  string `json:"mime_type"`
	ChunkSize int64  `json:"chunk_size"`
}

// InitSession handles POST /api/uploads - creates a new resumable upload session.
func (h *UploadsHandler) InitSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req InitSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid request payload: %v", err), http.StatusBadRequest)
		return
	}

	if req.Filename == "" {
		http.Error(w, "filename is required", http.StatusBadRequest)
		return
	}
	if req.Size <= 0 {
		http.Error(w, "size must be greater than zero", http.StatusBadRequest)
		return
	}

	userID := auth.GetEffectiveUserID(r)
	if userID == "" {
		userID = "default_user"
	}

	session, err := h.uploadManager.CreateSession(r.Context(), userID, req.Filename, req.MimeType, req.Size, req.ChunkSize)
	if err != nil {
		if errors.Is(err, chunks.ErrInsufficientPoolStorage) {
			http.Error(w, err.Error(), http.StatusInsufficientStorage)
			return
		}
		http.Error(w, fmt.Sprintf("failed to create upload session: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(session.Summary())
}

// GetSession handles GET /api/uploads/{id} - returns live progress and completed chunks.
func (h *UploadsHandler) GetSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	uploadID := r.PathValue("id")
	if uploadID == "" {
		http.Error(w, "upload id required", http.StatusBadRequest)
		return
	}

	session, err := h.uploadManager.GetSession(uploadID)
	if err != nil {
		if errors.Is(err, uploads.ErrSessionNotFound) {
			http.Error(w, "upload session not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	userID := auth.GetEffectiveUserID(r)
	if userID != "" && session.UserID != "" && session.UserID != userID {
		http.Error(w, "forbidden: access denied", http.StatusForbidden)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(session.Summary())
}

// UploadChunk handles PUT /api/uploads/{id}/chunks/{index} or POST /api/uploads/{id}/chunks/{index}.
func (h *UploadsHandler) UploadChunk(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut && r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	uploadID := r.PathValue("id")
	indexStr := r.PathValue("index")

	if uploadID == "" || indexStr == "" {
		http.Error(w, "upload id and chunk index are required", http.StatusBadRequest)
		return
	}

	chunkIndex, err := strconv.Atoi(indexStr)
	if err != nil || chunkIndex < 0 {
		http.Error(w, "invalid chunk index", http.StatusBadRequest)
		return
	}

	declaredSize := r.ContentLength

	session, err := h.uploadManager.GetSession(uploadID)
	if err != nil {
		if errors.Is(err, uploads.ErrSessionNotFound) {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	userID := auth.GetEffectiveUserID(r)
	if userID != "" && session.UserID != "" && session.UserID != userID {
		http.Error(w, "forbidden: access denied", http.StatusForbidden)
		return
	}

	meta, err := h.uploadManager.StoreChunk(r.Context(), uploadID, chunkIndex, r.Body, declaredSize)
	if err != nil {
		if errors.Is(err, uploads.ErrSessionNotFound) {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		if errors.Is(err, uploads.ErrSessionCancelled) || errors.Is(err, uploads.ErrSessionCompleted) {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		if errors.Is(err, uploads.ErrInvalidChunkIndex) || errors.Is(err, uploads.ErrChunkSizeMismatch) {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		http.Error(w, fmt.Sprintf("chunk upload failed: %v", err), http.StatusInternalServerError)
		return
	}

	session, _ = h.uploadManager.GetSession(uploadID)
	summary := session.Summary()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"upload_id":       uploadID,
		"chunk_index":     chunkIndex,
		"chunk_id":        meta.ID,
		"size":            meta.Size,
		"checksum":        meta.Checksum,
		"storage_node_id": meta.StorageNodeID,
		"status":          meta.Status,
		"completed_count": summary.CompletedCount,
		"total_chunks":    summary.TotalChunks,
		"is_completed":    summary.Status == uploads.UploadStatusCompleted,
	})
}

// CancelSession handles POST /api/uploads/{id}/cancel or DELETE /api/uploads/{id}.
func (h *UploadsHandler) CancelSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodDelete {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	uploadID := r.PathValue("id")
	if uploadID == "" {
		http.Error(w, "upload id required", http.StatusBadRequest)
		return
	}

	session, err := h.uploadManager.GetSession(uploadID)
	if err != nil {
		if errors.Is(err, uploads.ErrSessionNotFound) {
			http.Error(w, "upload session not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	userID := auth.GetEffectiveUserID(r)
	if userID != "" && session.UserID != "" && session.UserID != userID {
		http.Error(w, "forbidden: access denied", http.StatusForbidden)
		return
	}

	if err := h.uploadManager.CancelSession(r.Context(), uploadID); err != nil {
		if errors.Is(err, uploads.ErrSessionNotFound) {
			http.Error(w, "upload session not found", http.StatusNotFound)
			return
		}
		http.Error(w, fmt.Sprintf("failed to cancel upload session: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status":    "CANCELLED",
		"upload_id": uploadID,
	})
}

// ListSessions handles GET /api/uploads.
func (h *UploadsHandler) ListSessions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID := auth.GetEffectiveUserID(r)
	if userID == "" {
		userID = "default_user"
	}
	sessions := h.uploadManager.ListSessions(userID)

	summaries := make([]uploads.SessionSummary, 0, len(sessions))
	for _, s := range sessions {
		summaries = append(summaries, s.Summary())
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(summaries)
}
