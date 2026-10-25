package api

import (
	"encoding/json"
	"net/http"

	"github.com/bwithraju/clouddump/backend/internal/storage"
)

// NodesHandler serves endpoints for querying storage node metrics and pool status.
type NodesHandler struct {
	storageManager *storage.StorageManager
}

// NewNodesHandler constructs a NodesHandler.
func NewNodesHandler(mgr *storage.StorageManager) *NodesHandler {
	return &NodesHandler{
		storageManager: mgr,
	}
}

// List handles GET /api/nodes - returns all registered storage nodes with their metrics.
func (h *NodesHandler) List(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	stats := h.storageManager.GetPoolStats(r.Context())
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(stats.Nodes)
}

// Stats handles GET /api/nodes/stats - returns aggregated pool metrics (total, used, free, online).
func (h *NodesHandler) Stats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	stats := h.storageManager.GetPoolStats(r.Context())
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(stats)
}
