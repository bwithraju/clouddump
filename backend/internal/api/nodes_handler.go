package api

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/bwithraju/clouddump/backend/internal/auth"
	"github.com/bwithraju/clouddump/backend/internal/database"
	"github.com/bwithraju/clouddump/backend/internal/storage"
)

// NodesHandler serves endpoints for querying storage node metrics, pool status, and failure simulations.
type NodesHandler struct {
	storageManager *storage.StorageManager
	repo           database.Repository
}

// NewNodesHandler constructs a NodesHandler.
func NewNodesHandler(mgr *storage.StorageManager, repo database.Repository) *NodesHandler {
	return &NodesHandler{
		storageManager: mgr,
		repo:           repo,
	}
}

// List handles GET /api/nodes - returns all registered storage nodes with their metrics.
func (h *NodesHandler) List(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID := auth.GetEffectiveUserID(r)
	stats := h.storageManager.GetPoolStatsForUser(r.Context(), userID)
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

	userID := auth.GetEffectiveUserID(r)
	stats := h.storageManager.GetPoolStatsForUser(r.Context(), userID)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(stats)
}

// SimulateFailureRequest payload for toggling simulated offline status on a storage node.
type SimulateFailureRequest struct {
	Offline bool `json:"offline"`
}

// SimulateFailure handles POST /api/nodes/{id}/simulate-failure.
func (h *NodesHandler) SimulateFailure(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	nodeID := r.PathValue("id")
	if nodeID == "" {
		http.Error(w, "missing node id", http.StatusBadRequest)
		return
	}

	node, err := h.storageManager.GetNode(nodeID)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to find node: %v", err), http.StatusNotFound)
		return
	}

	userID := auth.GetEffectiveUserID(r)
	if userID != "" && node.UserID != "" && node.UserID != userID {
		http.Error(w, "forbidden: access denied to storage node", http.StatusForbidden)
		return
	}

	var req SimulateFailureRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
		return
	}

	if err := h.storageManager.SetNodeSimulatedOffline(nodeID, req.Offline); err != nil {
		http.Error(w, fmt.Sprintf("failed to update node status: %v", err), http.StatusNotFound)
		return
	}

	// Reconcile affected files and chunks
	reconcileResult, _ := h.storageManager.ReconcileNodeFailures(r.Context(), h.repo)

	node, _ = h.storageManager.GetNode(nodeID)
	nodeStats := node.GetStats()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"node":            nodeStats,
		"offline":         req.Offline,
		"degraded_files":  reconcileResult.DegradedFiles,
		"recovered_files": reconcileResult.RecoveredFiles,
	})
}

// Reconcile handles POST /api/nodes/reconcile - triggers a full health check and file status reconciliation.
func (h *NodesHandler) Reconcile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	result, err := h.storageManager.ReconcileNodeFailures(r.Context(), h.repo)
	if err != nil {
		http.Error(w, fmt.Sprintf("reconciliation failed: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(result)
}
