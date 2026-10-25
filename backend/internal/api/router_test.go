package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bwithraju/clouddump/backend/internal/database"
	"github.com/bwithraju/clouddump/backend/internal/jobs"
)

func TestHealthCheck(t *testing.T) {
	router := NewRouter()

	endpoints := []string{"/health", "/api/health"}
	for _, ep := range endpoints {
		req := httptest.NewRequest(http.MethodGet, ep, nil)
		rec := httptest.NewRecorder()

		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("endpoint %s returned status %d, expected %d", ep, rec.Code, http.StatusOK)
		}

		var resp HealthResponse
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("failed to decode response for %s: %v", ep, err)
		}

		if resp.Status != "healthy" {
			t.Errorf("expected status 'healthy', got '%s'", resp.Status)
		}
		if resp.Service != "clouddump-backend" {
			t.Errorf("expected service 'clouddump-backend', got '%s'", resp.Service)
		}
	}
}

func TestCORSHeaders(t *testing.T) {
	router := NewRouter(RouterConfig{
		AllowedOrigins: []string{"http://localhost:3000", "https://app.example.com"},
	})

	req := httptest.NewRequest(http.MethodOptions, "/api/health", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Errorf("expected OPTIONS status %d, got %d", http.StatusNoContent, rec.Code)
	}

	allowOrigin := rec.Header().Get("Access-Control-Allow-Origin")
	if allowOrigin != "http://localhost:3000" {
		t.Errorf("expected Access-Control-Allow-Origin 'http://localhost:3000', got '%s'", allowOrigin)
	}

	allowCreds := rec.Header().Get("Access-Control-Allow-Credentials")
	if allowCreds != "true" {
		t.Errorf("expected Access-Control-Allow-Credentials 'true', got '%s'", allowCreds)
	}
}

func TestReadinessCheck(t *testing.T) {
	memRepo := database.NewMemoryRepository()
	memQueue := jobs.NewMemoryJobQueue()

	router := NewRouter(RouterConfig{
		Repo:     memRepo,
		JobQueue: memQueue,
	})

	endpoints := []string{"/ready", "/api/ready"}
	for _, ep := range endpoints {
		req := httptest.NewRequest(http.MethodGet, ep, nil)
		rec := httptest.NewRecorder()

		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("endpoint %s returned status %d, expected %d", ep, rec.Code, http.StatusOK)
		}

		var resp map[string]interface{}
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("failed to decode readiness response for %s: %v", ep, err)
		}

		if resp["status"] != "ready" {
			t.Errorf("expected status 'ready', got '%v'", resp["status"])
		}

		checks, ok := resp["checks"].(map[string]interface{})
		if !ok {
			t.Fatalf("expected checks map in response")
		}
		if checks["database"] != "ready" {
			t.Errorf("expected database check 'ready', got '%v'", checks["database"])
		}
		if checks["job_queue"] != "ready" {
			t.Errorf("expected job_queue check 'ready', got '%v'", checks["job_queue"])
		}
	}
}
