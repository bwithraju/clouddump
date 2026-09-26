package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
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
	router := NewRouter()

	req := httptest.NewRequest(http.MethodOptions, "/api/health", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Errorf("expected OPTIONS status %d, got %d", http.StatusNoContent, rec.Code)
	}

	allowOrigin := rec.Header().Get("Access-Control-Allow-Origin")
	if allowOrigin != "*" {
		t.Errorf("expected Access-Control-Allow-Origin '*', got '%s'", allowOrigin)
	}
}
