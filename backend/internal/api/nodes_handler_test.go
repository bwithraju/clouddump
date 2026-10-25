package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bwithraju/clouddump/backend/internal/storage"
)

func TestNodesAPI_ListAndStats(t *testing.T) {
	_, mgr, repo, dirs := setupAPITestEnvironment(t, 100*1024)
	defer cleanupAPITestDirs(dirs)

	nh := NewNodesHandler(mgr, repo)
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

func TestNodesAPI_SimulateFailureToggle(t *testing.T) {
	_, mgr, repo, dirs := setupAPITestEnvironment(t, 100*1024)
	defer cleanupAPITestDirs(dirs)

	nh := NewNodesHandler(mgr, repo)
	r := NewRouter(RouterConfig{NodesHandler: nh})

	// 1. Simulate failure on node1
	failPayload, _ := json.Marshal(SimulateFailureRequest{Offline: true})
	req := httptest.NewRequest(http.MethodPost, "/api/nodes/node1/simulate-failure", bytes.NewReader(failPayload))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Verify node1 is OFFLINE
	statsReq := httptest.NewRequest(http.MethodGet, "/api/nodes/stats", nil)
	statsRec := httptest.NewRecorder()
	r.ServeHTTP(statsRec, statsReq)

	var stats storage.PoolStats
	_ = json.NewDecoder(statsRec.Body).Decode(&stats)

	if stats.OfflineNodes != 1 {
		t.Errorf("expected 1 offline node, got %d", stats.OfflineNodes)
	}
	if stats.OnlineNodes != 2 {
		t.Errorf("expected 2 online nodes, got %d", stats.OnlineNodes)
	}

	// 2. Restore node1 back ONLINE
	restorePayload, _ := json.Marshal(SimulateFailureRequest{Offline: false})
	reqRestore := httptest.NewRequest(http.MethodPost, "/api/nodes/node1/simulate-failure", bytes.NewReader(restorePayload))
	recRestore := httptest.NewRecorder()
	r.ServeHTTP(recRestore, reqRestore)

	if recRestore.Code != http.StatusOK {
		t.Fatalf("expected 200 on restore, got %d", recRestore.Code)
	}

	// Verify all nodes are online
	statsRec2 := httptest.NewRecorder()
	r.ServeHTTP(statsRec2, statsReq)
	var stats2 storage.PoolStats
	_ = json.NewDecoder(statsRec2.Body).Decode(&stats2)

	if stats2.OfflineNodes != 0 {
		t.Errorf("expected 0 offline nodes after restore, got %d", stats2.OfflineNodes)
	}
	if stats2.OnlineNodes != 3 {
		t.Errorf("expected 3 online nodes after restore, got %d", stats2.OnlineNodes)
	}
}
