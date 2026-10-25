package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/bwithraju/clouddump/backend/internal/auth"
	"github.com/bwithraju/clouddump/backend/internal/database"
	"github.com/bwithraju/clouddump/backend/internal/providers"
	"github.com/bwithraju/clouddump/backend/internal/storage"
	"github.com/bwithraju/clouddump/backend/internal/uploads"
	"os"
)

func TestSystemAPI_ActivityAndMonitoring(t *testing.T) {
	mgr := storage.NewStorageManager(storage.NewMaxAvailableStrategy())

	tempDir1, _ := os.MkdirTemp("", "sys_node1_*")
	defer os.RemoveAll(tempDir1)
	p1, _ := providers.NewLocalStorageProvider(tempDir1, 50*1024*1024)
	n1, _ := storage.NewStorageNode("sys_node_1", "Monitoring Node 1", p1)
	_ = mgr.RegisterNode(n1)

	repo := database.NewMemoryRepository()
	sessionStore := auth.NewSessionStore(1 * time.Hour)
	authService := auth.NewAuthService(repo.Users(), sessionStore)
	authHandler := NewAuthHandler(authService)
	uploadManager := uploads.NewUploadManager(mgr, repo, 64*1024)

	systemHandler := NewSystemHandler(time.Now().Add(-10*time.Minute), mgr, repo, uploadManager, nil)

	router := NewRouter(RouterConfig{
		AuthHandler:   authHandler,
		AuthService:   authService,
		SystemHandler: systemHandler,
	})

	cookie := helperRegisterAndLogin(t, router, "monitor@example.com", "password123", "Monitor User")

	// 1. Query /api/system/activity
	req := httptest.NewRequest(http.MethodGet, "/api/system/activity", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK from /api/system/activity, got %d: %s", rec.Code, rec.Body.String())
	}

	var act SystemActivityResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &act); err != nil {
		t.Fatalf("failed unmarshaling activity response: %v", err)
	}

	if act.UptimeSeconds < 500 {
		t.Errorf("expected uptime >= 500s, got %d", act.UptimeSeconds)
	}
	if act.PoolStats.NodeCount != 1 {
		t.Errorf("expected 1 node, got %d", act.PoolStats.NodeCount)
	}
	if len(act.FailedOperations) != 0 {
		t.Errorf("expected 0 failed operations initially, got %d", len(act.FailedOperations))
	}

	// 2. Simulate Node Offline -> Verify /api/system/activity surfaces the failed operation alert
	n1.SetSimulatedOffline(true)

	req2 := httptest.NewRequest(http.MethodGet, "/api/system/activity", nil)
	req2.AddCookie(cookie)
	rec2 := httptest.NewRecorder()
	router.ServeHTTP(rec2, req2)

	var act2 SystemActivityResponse
	_ = json.Unmarshal(rec2.Body.Bytes(), &act2)

	if len(act2.FailedOperations) != 1 {
		t.Fatalf("expected 1 failed operation for offline node, got %d", len(act2.FailedOperations))
	}
	if act2.FailedOperations[0].Type != "NODE_OFFLINE" {
		t.Errorf("expected NODE_OFFLINE failed operation, got %s", act2.FailedOperations[0].Type)
	}

	// Restore node
	n1.SetSimulatedOffline(false)
}
