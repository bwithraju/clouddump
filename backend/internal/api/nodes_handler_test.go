package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bwithraju/clouddump/backend/internal/storage"
)

func TestNodesAPI_ListAndStats(t *testing.T) {
	_, mgr, _, dirs := setupAPITestEnvironment(t, 100*1024)
	defer cleanupAPITestDirs(dirs)

	nh := NewNodesHandler(mgr)
	r := NewRouter(RouterConfig{NodesHandler: nh})

	// Test GET /api/nodes
	req := httptest.NewRequest(http.MethodGet, "/api/nodes", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var nodes []storage.NodeStats
	if err := json.NewDecoder(rec.Body).Decode(&nodes); err != nil {
		t.Fatalf("failed to decode nodes: %v", err)
	}

	if len(nodes) != 3 {
		t.Errorf("expected 3 nodes, got %d", len(nodes))
	}

	// Test GET /api/nodes/stats
	reqStats := httptest.NewRequest(http.MethodGet, "/api/nodes/stats", nil)
	recStats := httptest.NewRecorder()
	r.ServeHTTP(recStats, reqStats)

	if recStats.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", recStats.Code)
	}

	var stats storage.PoolStats
	if err := json.NewDecoder(recStats.Body).Decode(&stats); err != nil {
		t.Fatalf("failed to decode pool stats: %v", err)
	}

	if stats.NodeCount != 3 {
		t.Errorf("expected 3 nodes, got %d", stats.NodeCount)
	}
	if stats.TotalCapacity <= 0 {
		t.Errorf("expected positive total capacity, got %d", stats.TotalCapacity)
	}
}
