package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestGoogleDriveProvider_FullLifecycle(t *testing.T) {
	storedChunks := make(map[string][]byte)
	var uploadCounter int64

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 1. Quota check: GET /about
		if r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/about") {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"storageQuota": map[string]string{
					"limit": "50000000", // ~50 MB
					"usage": "10000000", // ~10 MB
				},
			})
			return
		}

		// 2. Initiate Resumable Upload: POST /upload/drive/v3/files?uploadType=resumable
		if r.Method == http.MethodPost && strings.Contains(r.URL.RawQuery, "uploadType=resumable") {
			id := atomic.AddInt64(&uploadCounter, 1)
			location := fmt.Sprintf("http://%s/upload-session/%d", r.Host, id)
			w.Header().Set("Location", location)
			w.WriteHeader(http.StatusOK)
			return
		}

		// 3. Put Chunk Payload: PUT /upload-session/:id
		if r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/upload-session/") {
			idStr := strings.TrimPrefix(r.URL.Path, "/upload-session/")
			payload, err := io.ReadAll(r.Body)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			objID := "gdrive_file_" + idStr
			storedChunks[objID] = payload

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"id":   objID,
				"name": "chunk_" + idStr,
				"size": fmt.Sprintf("%d", len(payload)),
			})
			return
		}

		// 4. Download Chunk: GET /files/:id?alt=media
		if r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/files/") {
			objID := strings.TrimPrefix(r.URL.Path, "/files/")
			data, exists := storedChunks[objID]
			if !exists {
				http.Error(w, "File not found", http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", "application/octet-stream")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(data)
			return
		}

		// 5. Delete Chunk: DELETE /files/:id
		if r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/files/") {
			objID := strings.TrimPrefix(r.URL.Path, "/files/")
			delete(storedChunks, objID)
			w.WriteHeader(http.StatusNoContent)
			return
		}

		http.NotFound(w, r)
	}))
	defer server.Close()

	ctx := context.Background()
	provider, err := NewGoogleDriveProvider(GoogleDriveConfig{
		Client:        server.Client(),
		APIBaseURL:    server.URL,
		UploadBaseURL: server.URL,
		BaseBackoff:   10 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("failed to create GoogleDriveProvider: %v", err)
	}

	if provider.ProviderType() != "google_drive" {
		t.Errorf("expected provider type 'google_drive', got '%s'", provider.ProviderType())
	}

	// Test Quota Capacity
	total, err := provider.GetTotalCapacity(ctx)
	if err != nil {
		t.Fatalf("failed getting total capacity: %v", err)
	}
	if total != 50000000 {
		t.Errorf("expected total 50000000, got %d", total)
	}

	avail, err := provider.GetAvailableCapacity(ctx)
	if err != nil {
		t.Fatalf("failed getting available capacity: %v", err)
	}
	if avail != 40000000 {
		t.Errorf("expected available 40000000, got %d", avail)
	}

	// Test HealthCheck
	healthy, err := provider.HealthCheck(ctx)
	if err != nil || !healthy {
		t.Fatalf("expected provider to be healthy, got: %v (healthy: %v)", err, healthy)
	}

	// Test StoreChunk
	chunkContent := []byte("Hello Google Drive Distributed Storage Chunk Data!")
	chunkID := "chk_gdrive_001"
	objID, written, err := provider.StoreChunk(ctx, chunkID, bytes.NewReader(chunkContent), int64(len(chunkContent)))
	if err != nil {
		t.Fatalf("StoreChunk failed: %v", err)
	}
	if objID == "" {
		t.Fatalf("empty object ID returned")
	}
	if written != int64(len(chunkContent)) {
		t.Errorf("expected %d bytes written, got %d", len(chunkContent), written)
	}

	// Test GetChunk
	rc, err := provider.GetChunk(ctx, objID)
	if err != nil {
		t.Fatalf("GetChunk failed: %v", err)
	}
	downloaded, err := io.ReadAll(rc)
	_ = rc.Close()
	if err != nil {
		t.Fatalf("reading downloaded chunk failed: %v", err)
	}
	if !bytes.Equal(chunkContent, downloaded) {
		t.Errorf("downloaded content mismatch: expected %q, got %q", string(chunkContent), string(downloaded))
	}

	// Test DeleteChunk
	if err := provider.DeleteChunk(ctx, objID); err != nil {
		t.Fatalf("DeleteChunk failed: %v", err)
	}

	// Verify chunk is gone (GetChunk returns ErrChunkNotFound)
	_, err = provider.GetChunk(ctx, objID)
	if err != ErrChunkNotFound {
		t.Errorf("expected ErrChunkNotFound after deletion, got %v", err)
	}
}

// Test Rate Limit and Exponential Backoff Handling
func TestGoogleDriveProvider_RateLimitAndRetry(t *testing.T) {
	var attempts int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		att := atomic.AddInt32(&attempts, 1)
		if att <= 2 {
			// First two attempts fail with 429 Too Many Requests
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error": {"message": "Rate Limit Exceeded"}}`))
			return
		}

		// Third attempt succeeds
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"storageQuota": map[string]string{
				"limit": "1000000",
				"usage": "200000",
			},
		})
	}))
	defer server.Close()

	ctx := context.Background()
	provider, _ := NewGoogleDriveProvider(GoogleDriveConfig{
		Client:        server.Client(),
		APIBaseURL:    server.URL,
		UploadBaseURL: server.URL,
		MaxRetries:    3,
		BaseBackoff:   10 * time.Millisecond,
	})

	total, err := provider.GetTotalCapacity(ctx)
	if err != nil {
		t.Fatalf("expected successful retry after 429s, got error: %v", err)
	}
	if total != 1000000 {
		t.Errorf("expected total 1000000, got %d", total)
	}
	if attempts < 3 {
		t.Errorf("expected at least 3 attempts with retry, got %d", attempts)
	}
}

// Test Error Handling and Simulated Outage
func TestGoogleDriveProvider_ErrorHandling(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "quota-fail") {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"error": {"errors": [{"reason": "storageQuotaExceeded"}]}}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	ctx := context.Background()
	provider, _ := NewGoogleDriveProvider(GoogleDriveConfig{
		Client:        server.Client(),
		APIBaseURL:    server.URL,
		UploadBaseURL: server.URL,
		BaseBackoff:   5 * time.Millisecond,
	})

	// 1. Invalid chunk ID validation
	_, _, err := provider.StoreChunk(ctx, "invalid/chunk..id!", strings.NewReader("data"), 4)
	if err == nil || !strings.Contains(err.Error(), "invalid chunk identifier") {
		t.Errorf("expected invalid chunk identifier error, got: %v", err)
	}

	// 2. Simulated Offline
	provider.SetSimulatedOffline(true)
	_, _, err = provider.StoreChunk(ctx, "chunk1", strings.NewReader("data"), 4)
	if err != ErrNodeOffline {
		t.Errorf("expected ErrNodeOffline when offline, got %v", err)
	}

	healthy, _ := provider.HealthCheck(ctx)
	if healthy {
		t.Errorf("expected provider to be unhealthy when offline")
	}

	provider.SetSimulatedOffline(false)

	// 3. Quota Exceeded error handling
	quotaProvider, _ := NewGoogleDriveProvider(GoogleDriveConfig{
		Client:        server.Client(),
		APIBaseURL:    server.URL + "/quota-fail",
		UploadBaseURL: server.URL + "/quota-fail",
		BaseBackoff:   5 * time.Millisecond,
		MaxRetries:    1,
	})
	_, _, err = quotaProvider.StoreChunk(ctx, "chunk2", strings.NewReader("data"), 4)
	if err == nil {
		t.Errorf("expected quota exceeded error, got nil")
	}
}
