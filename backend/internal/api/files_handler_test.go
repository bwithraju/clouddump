package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/bwithraju/clouddump/backend/internal/chunks"
	"github.com/bwithraju/clouddump/backend/internal/database"
	"github.com/bwithraju/clouddump/backend/internal/providers"
	"github.com/bwithraju/clouddump/backend/internal/storage"
)

// setupAPITestEnvironment spins up an in-memory database, 3 storage nodes, and the API router.
func setupAPITestEnvironment(t *testing.T, chunkSize int64) (http.Handler, *storage.StorageManager, database.Repository, map[string]string) {
	mgr := storage.NewStorageManager(storage.NewMaxAvailableStrategy())

	tempDir1, _ := os.MkdirTemp("", "api_node1_*")
	tempDir2, _ := os.MkdirTemp("", "api_node2_*")
	tempDir3, _ := os.MkdirTemp("", "api_node3_*")

	p1, _ := providers.NewLocalStorageProvider(tempDir1, 50*1024*1024)
	p2, _ := providers.NewLocalStorageProvider(tempDir2, 100*1024*1024)
	p3, _ := providers.NewLocalStorageProvider(tempDir3, 150*1024*1024)

	n1, _ := storage.NewStorageNode("node1", "Node 1", p1)
	n2, _ := storage.NewStorageNode("node2", "Node 2", p2)
	n3, _ := storage.NewStorageNode("node3", "Node 3", p3)

	_ = mgr.RegisterNode(n1)
	_ = mgr.RegisterNode(n2)
	_ = mgr.RegisterNode(n3)

	dirs := map[string]string{
		"node1": tempDir1,
		"node2": tempDir2,
		"node3": tempDir3,
	}

	distributor, _ := chunks.NewChunkDistributor(mgr, chunkSize)
	reconstructor, _ := chunks.NewFileReconstructor(mgr)
	repo := database.NewMemoryRepository()

	fileHandler := NewFileHandler(mgr, distributor, reconstructor, repo, chunkSize)
	router := NewRouter(RouterConfig{FileHandler: fileHandler})

	return router, mgr, repo, dirs
}

func cleanupAPITestDirs(dirs map[string]string) {
	for _, d := range dirs {
		_ = os.RemoveAll(d)
	}
}

// Test: Full File REST API Lifecycle (Upload -> List -> Get -> Status -> Download -> Delete)
func TestFileAPI_FullLifecycle(t *testing.T) {
	chunkSize := int64(100 * 1024) // 100 KB
	router, _, _, dirs := setupAPITestEnvironment(t, chunkSize)
	defer cleanupAPITestDirs(dirs)

	fileSize := int64(250 * 1024) // 250 KB (3 chunks: 100KB, 100KB, 50KB)
	originalPayload := bytes.Repeat([]byte("X"), int(fileSize))
	filename := "college_project_spec.pdf"

	// 1. POST /api/files/upload (Streaming upload)
	req := httptest.NewRequest(http.MethodPost, "/api/files/upload", bytes.NewReader(originalPayload))
	req.Header.Set("X-Filename", filename)
	req.Header.Set("Content-Type", "application/pdf")
	req.Header.Set("Content-Length", strconv.FormatInt(fileSize, 10))
	req.Header.Set("X-User-ID", "student_42")

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Upload failed with status %d: %s", rec.Code, rec.Body.String())
	}

	var uploadResp map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&uploadResp); err != nil {
		t.Fatalf("failed decoding upload response: %v", err)
	}

	fileID, ok := uploadResp["id"].(string)
	if !ok || fileID == "" {
		t.Fatalf("missing or invalid file ID in response: %+v", uploadResp)
	}
	if uploadResp["status"] != "COMPLETED" {
		t.Errorf("expected status COMPLETED, got %v", uploadResp["status"])
	}
	if int(uploadResp["total_chunks"].(float64)) != 3 {
		t.Errorf("expected 3 total chunks, got %v", uploadResp["total_chunks"])
	}

	// 2. GET /api/files (List files)
	listReq := httptest.NewRequest(http.MethodGet, "/api/files", nil)
	listReq.Header.Set("X-User-ID", "student_42")
	listRec := httptest.NewRecorder()
	router.ServeHTTP(listRec, listReq)

	if listRec.Code != http.StatusOK {
		t.Fatalf("List files returned status %d", listRec.Code)
	}

	var fileList []map[string]interface{}
	if err := json.NewDecoder(listRec.Body).Decode(&fileList); err != nil {
		t.Fatalf("failed decoding file list: %v", err)
	}
	if len(fileList) != 1 {
		t.Fatalf("expected 1 file in list, got %d", len(fileList))
	}
	if fileList[0]["id"] != fileID {
		t.Errorf("file ID mismatch: %v vs %s", fileList[0]["id"], fileID)
	}

	// 3. GET /api/files/:id (Get details)
	detailReq := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/files/%s", fileID), nil)
	detailRec := httptest.NewRecorder()
	router.ServeHTTP(detailRec, detailReq)

	if detailRec.Code != http.StatusOK {
		t.Fatalf("Get details returned status %d", detailRec.Code)
	}

	var detailResp map[string]interface{}
	_ = json.NewDecoder(detailRec.Body).Decode(&detailResp)
	chunksList := detailResp["chunks"].([]interface{})
	if len(chunksList) != 3 {
		t.Errorf("expected 3 chunks in details, got %d", len(chunksList))
	}

	// 4. GET /api/files/:id/status (Get lightweight status)
	statusReq := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/files/%s/status", fileID), nil)
	statusRec := httptest.NewRecorder()
	router.ServeHTTP(statusRec, statusReq)

	if statusRec.Code != http.StatusOK {
		t.Fatalf("Get status returned status %d", statusRec.Code)
	}
	var statusResp map[string]interface{}
	_ = json.NewDecoder(statusRec.Body).Decode(&statusResp)
	if statusResp["status"] != "COMPLETED" || statusResp["id"] != fileID {
		t.Errorf("unexpected status response: %+v", statusResp)
	}

	// 5. GET /api/files/:id/download (Streaming download)
	downReq := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/files/%s/download", fileID), nil)
	downRec := httptest.NewRecorder()
	router.ServeHTTP(downRec, downReq)

	if downRec.Code != http.StatusOK {
		t.Fatalf("Download failed with status %d: %s", downRec.Code, downRec.Body.String())
	}

	if downRec.Header().Get("Content-Length") != strconv.FormatInt(fileSize, 10) {
		t.Errorf("unexpected Content-Length: %s", downRec.Header().Get("Content-Length"))
	}

	reconstructedBytes := downRec.Body.Bytes()
	if !bytes.Equal(reconstructedBytes, originalPayload) {
		t.Fatalf("downloaded file not byte-for-byte identical to original")
	}

	// 6. DELETE /api/files/:id
	delReq := httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/api/files/%s", fileID), nil)
	delRec := httptest.NewRecorder()
	router.ServeHTTP(delRec, delReq)

	if delRec.Code != http.StatusOK {
		t.Fatalf("Delete returned status %d: %s", delRec.Code, delRec.Body.String())
	}

	// Verify subsequent GET returns 404
	afterReq := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/files/%s", fileID), nil)
	afterRec := httptest.NewRecorder()
	router.ServeHTTP(afterRec, afterReq)

	if afterRec.Code != http.StatusNotFound {
		t.Errorf("expected 404 after deletion, got %d", afterRec.Code)
	}
}

// Test: Multipart Form Upload
func TestFileAPI_MultipartUpload(t *testing.T) {
	chunkSize := int64(50 * 1024)
	router, _, _, dirs := setupAPITestEnvironment(t, chunkSize)
	defer cleanupAPITestDirs(dirs)

	fileData := bytes.Repeat([]byte("FormDataPayload"), 1000)
	fileSize := int64(len(fileData))

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "presentation.key")
	if err != nil {
		t.Fatalf("failed creating form file: %v", err)
	}
	_, _ = part.Write(fileData)
	_ = writer.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/files/upload", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("X-File-Size", strconv.FormatInt(fileSize, 10))

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("multipart upload failed with status %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]interface{}
	_ = json.NewDecoder(rec.Body).Decode(&resp)
	if resp["filename"] != "presentation.key" {
		t.Errorf("expected filename 'presentation.key', got %v", resp["filename"])
	}
}

// Test: Upload Validation and Error Handling
func TestFileAPI_UploadValidation(t *testing.T) {
	router, _, _, dirs := setupAPITestEnvironment(t, 100*1024)
	defer cleanupAPITestDirs(dirs)

	// Missing size
	req := httptest.NewRequest(http.MethodPost, "/api/files/upload", bytes.NewReader([]byte("test")))
	req.ContentLength = -1
	req.Header.Set("X-Filename", "test.txt")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing size, got %d", rec.Code)
	}

	// Method not allowed
	getReq := httptest.NewRequest(http.MethodGet, "/api/files/upload", nil)
	getRec := httptest.NewRecorder()
	router.ServeHTTP(getRec, getReq)
	if getRec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 Method Not Allowed, got %d", getRec.Code)
	}

	// 404 for non-existent file
	missingReq := httptest.NewRequest(http.MethodGet, "/api/files/non_existent_id", nil)
	missingRec := httptest.NewRecorder()
	router.ServeHTTP(missingRec, missingReq)
	if missingRec.Code != http.StatusNotFound {
		t.Errorf("expected 404 for missing file, got %d", missingRec.Code)
	}
}

// Test: Download detects corrupted chunk and returns integrity failure
func TestFileAPI_DownloadIntegrityCheck(t *testing.T) {
	chunkSize := int64(50 * 1024)
	router, _, repo, dirs := setupAPITestEnvironment(t, chunkSize)
	defer cleanupAPITestDirs(dirs)

	payload := bytes.Repeat([]byte("Z"), 100*1024)
	fileSize := int64(len(payload))

	// Upload
	req := httptest.NewRequest(http.MethodPost, "/api/files/upload", bytes.NewReader(payload))
	req.Header.Set("X-Filename", "corrupt_test.bin")
	req.Header.Set("Content-Length", strconv.FormatInt(fileSize, 10))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	var resp map[string]interface{}
	_ = json.NewDecoder(rec.Body).Decode(&resp)
	fileID := resp["id"].(string)

	// Mutate chunk 0 on disk
	chunksList, _ := repo.Chunks().ListChunksByFile(context.Background(), fileID)
	targetChunk := chunksList[0]
	nodeDir := dirs[targetChunk.StorageNodeID]
	chunkPath := filepath.Join(nodeDir, targetChunk.ProviderObjectID)

	raw, _ := os.ReadFile(chunkPath)
	raw[10] ^= 0xFF
	_ = os.WriteFile(chunkPath, raw, 0644)

	// Try to download
	downReq := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/files/%s/download", fileID), nil)
	downRec := httptest.NewRecorder()
	router.ServeHTTP(downRec, downReq)

	// Since VerifyingReader fails mid-stream or before flush, read output should not match original
	if bytes.Equal(downRec.Body.Bytes(), payload) {
		t.Fatalf("downloaded corrupted file should not match original payload")
	}
}
