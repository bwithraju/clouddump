package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bwithraju/clouddump/backend/internal/uploads"
)

func setupUploadsAPITestEnvironment(t *testing.T, chunkSize int64) (http.Handler, *uploads.UploadManager, map[string]string) {
	_, mgr, repo, dirs := setupAPITestEnvironment(t, chunkSize)

	uploadMgr := uploads.NewUploadManager(mgr, repo, chunkSize)
	uh := NewUploadsHandler(uploadMgr)
	router := NewRouter(RouterConfig{UploadsHandler: uh})

	return router, uploadMgr, dirs
}

func TestUploadsAPI_LifecycleAndResume(t *testing.T) {
	chunkSize := int64(20 * 1024) // 20 KB
	router, _, dirs := setupUploadsAPITestEnvironment(t, chunkSize)
	defer cleanupAPITestDirs(dirs)

	totalChunks := 4
	totalSize := chunkSize * int64(totalChunks)

	// 1. Initialize resumable upload session
	initPayload, _ := json.Marshal(InitSessionRequest{
		Filename:  "large_video.mp4",
		Size:      totalSize,
		MimeType:  "video/mp4",
		ChunkSize: chunkSize,
	})

	initReq := httptest.NewRequest(http.MethodPost, "/api/uploads", bytes.NewReader(initPayload))
	initRec := httptest.NewRecorder()
	router.ServeHTTP(initRec, initReq)

	if initRec.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created on init, got %d: %s", initRec.Code, initRec.Body.String())
	}

	var sessionSummary uploads.SessionSummary
	if err := json.NewDecoder(initRec.Body).Decode(&sessionSummary); err != nil {
		t.Fatalf("failed decoding session summary: %v", err)
	}

	uploadID := sessionSummary.UploadID
	if sessionSummary.TotalChunks != totalChunks {
		t.Errorf("expected %d total chunks, got %d", totalChunks, sessionSummary.TotalChunks)
	}

	// 2. Upload chunks 0 and 1 (simulate interruption)
	for i := 0; i < 2; i++ {
		payload := bytes.Repeat([]byte{byte(i + 1)}, int(chunkSize))
		chunkReq := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/api/uploads/%s/chunks/%d", uploadID, i), bytes.NewReader(payload))
		chunkRec := httptest.NewRecorder()
		router.ServeHTTP(chunkRec, chunkReq)

		if chunkRec.Code != http.StatusOK {
			t.Fatalf("failed uploading chunk %d: %d - %s", i, chunkRec.Code, chunkRec.Body.String())
		}
	}

	// 3. Query status API to inspect progress & missing chunks
	statusReq := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/uploads/%s", uploadID), nil)
	statusRec := httptest.NewRecorder()
	router.ServeHTTP(statusRec, statusReq)

	if statusRec.Code != http.StatusOK {
		t.Fatalf("status request failed with %d", statusRec.Code)
	}

	var progress uploads.SessionSummary
	_ = json.NewDecoder(statusRec.Body).Decode(&progress)

	if progress.CompletedCount != 2 {
		t.Errorf("expected 2 completed chunks, got %d", progress.CompletedCount)
	}
	if progress.NextExpected != 2 {
		t.Errorf("expected next expected chunk to be 2, got %d", progress.NextExpected)
	}
	if progress.ProgressPercent != 50.0 {
		t.Errorf("expected 50%% progress, got %f", progress.ProgressPercent)
	}

	// 4. Test Idempotency: Re-uploading chunk 0 should not fail or duplicate
	dupPayload := bytes.Repeat([]byte("A"), int(chunkSize))
	dupReq := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/api/uploads/%s/chunks/0", uploadID), bytes.NewReader(dupPayload))
	dupRec := httptest.NewRecorder()
	router.ServeHTTP(dupRec, dupReq)

	if dupRec.Code != http.StatusOK {
		t.Fatalf("re-uploading chunk 0 should succeed idempotently, got %d", dupRec.Code)
	}

	// 5. Resume and upload remaining chunks 2 and 3
	for i := progress.NextExpected; i < totalChunks; i++ {
		payload := bytes.Repeat([]byte{byte(i + 1)}, int(chunkSize))
		chunkReq := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/api/uploads/%s/chunks/%d", uploadID, i), bytes.NewReader(payload))
		chunkRec := httptest.NewRecorder()
		router.ServeHTTP(chunkRec, chunkReq)

		if chunkRec.Code != http.StatusOK {
			t.Fatalf("failed uploading resumed chunk %d: %d", i, chunkRec.Code)
		}

		var chunkResp map[string]interface{}
		_ = json.NewDecoder(chunkRec.Body).Decode(&chunkResp)
		if i == totalChunks-1 {
			if chunkResp["is_completed"] != true {
				t.Errorf("expected is_completed to be true for final chunk")
			}
		}
	}

	// 6. Verify final status is COMPLETED
	finalReq := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/uploads/%s", uploadID), nil)
	finalRec := httptest.NewRecorder()
	router.ServeHTTP(finalRec, finalReq)

	var finalSummary uploads.SessionSummary
	_ = json.NewDecoder(finalRec.Body).Decode(&finalSummary)

	if finalSummary.Status != uploads.UploadStatusCompleted {
		t.Errorf("expected session COMPLETED, got %v", finalSummary.Status)
	}
	if finalSummary.CompletedCount != totalChunks {
		t.Errorf("expected all %d chunks completed, got %d", totalChunks, finalSummary.CompletedCount)
	}
}

func TestUploadsAPI_CancelSession(t *testing.T) {
	chunkSize := int64(20 * 1024)
	router, _, dirs := setupUploadsAPITestEnvironment(t, chunkSize)
	defer cleanupAPITestDirs(dirs)

	initPayload, _ := json.Marshal(InitSessionRequest{
		Filename:  "cancel_me.bin",
		Size:      chunkSize * 3,
		MimeType:  "application/octet-stream",
		ChunkSize: chunkSize,
	})

	initReq := httptest.NewRequest(http.MethodPost, "/api/uploads", bytes.NewReader(initPayload))
	initRec := httptest.NewRecorder()
	router.ServeHTTP(initRec, initReq)

	var summary uploads.SessionSummary
	_ = json.NewDecoder(initRec.Body).Decode(&summary)

	// Upload chunk 0
	chunkReq := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/api/uploads/%s/chunks/0", summary.UploadID), bytes.NewReader(bytes.Repeat([]byte("X"), int(chunkSize))))
	chunkRec := httptest.NewRecorder()
	router.ServeHTTP(chunkRec, chunkReq)

	// Cancel
	cancelReq := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/uploads/%s/cancel", summary.UploadID), nil)
	cancelRec := httptest.NewRecorder()
	router.ServeHTTP(cancelRec, cancelReq)

	if cancelRec.Code != http.StatusOK {
		t.Fatalf("expected 200 on cancel, got %d", cancelRec.Code)
	}

	// Further chunk uploads should fail with 409 Conflict
	afterReq := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/api/uploads/%s/chunks/1", summary.UploadID), bytes.NewReader(bytes.Repeat([]byte("Y"), int(chunkSize))))
	afterRec := httptest.NewRecorder()
	router.ServeHTTP(afterRec, afterReq)

	if afterRec.Code != http.StatusConflict {
		t.Errorf("expected 409 Conflict on uploading to cancelled session, got %d", afterRec.Code)
	}
}
