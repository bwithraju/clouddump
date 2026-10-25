package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/bwithraju/clouddump/backend/internal/auth"
)

// Helper to register and log in a test user, returning the session cookie.
func helperRegisterAndLogin(t *testing.T, router http.Handler, email, password, name string) *http.Cookie {
	t.Helper()
	regBody, _ := json.Marshal(map[string]string{
		"email":    email,
		"password": password,
		"name":     name,
	})
	regReq := httptest.NewRequest(http.MethodPost, "/api/auth/register", bytes.NewReader(regBody))
	regReq.Header.Set("Content-Type", "application/json")
	regRec := httptest.NewRecorder()
	router.ServeHTTP(regRec, regReq)

	if regRec.Code != http.StatusCreated {
		t.Fatalf("failed registering user %s: code %d, body: %s", email, regRec.Code, regRec.Body.String())
	}

	loginBody, _ := json.Marshal(map[string]string{
		"email":    email,
		"password": password,
	})
	loginReq := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(loginBody))
	loginReq.Header.Set("Content-Type", "application/json")
	loginRec := httptest.NewRecorder()
	router.ServeHTTP(loginRec, loginReq)

	if loginRec.Code != http.StatusOK {
		t.Fatalf("failed logging in user %s: code %d, body: %s", email, loginRec.Code, loginRec.Body.String())
	}

	for _, c := range loginRec.Result().Cookies() {
		if c.Name == auth.SessionCookieName {
			return c
		}
	}
	t.Fatalf("session cookie not found for %s", email)
	return nil
}

// 1. Edge Case: Empty File (0 bytes) returns HTTP 400 Bad Request
func TestAPI_EdgeCase_EmptyFile(t *testing.T) {
	env := setupAuthTestEnv(t)
	defer cleanupAuthTestEnv(env)

	cookie := helperRegisterAndLogin(t, env.router, "empty@example.com", "password123", "Empty User")

	// Direct stream upload of 0 bytes
	req := httptest.NewRequest(http.MethodPost, "/api/files/upload", bytes.NewReader([]byte{}))
	req.AddCookie(cookie)
	req.Header.Set("X-Filename", "empty.txt")
	req.Header.Set("X-File-Size", "0")
	rec := httptest.NewRecorder()
	env.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected HTTP 400 Bad Request for 0-byte file, got: %d", rec.Code)
	}

	// Resumable upload initialization with size 0
	initBody, _ := json.Marshal(map[string]any{
		"filename":   "empty_resumable.txt",
		"size":       0,
		"chunk_size": 64 * 1024,
	})
	initReq := httptest.NewRequest(http.MethodPost, "/api/uploads", bytes.NewReader(initBody))
	initReq.AddCookie(cookie)
	initReq.Header.Set("Content-Type", "application/json")
	initRec := httptest.NewRecorder()
	env.router.ServeHTTP(initRec, initReq)

	if initRec.Code != http.StatusBadRequest {
		t.Fatalf("expected HTTP 400 Bad Request for 0-byte resumable init, got: %d", initRec.Code)
	}
}

// 2. Edge Case: 1 Byte File roundtrip (upload, download, checksum verification)
func TestAPI_EdgeCase_OneByteFile(t *testing.T) {
	env := setupAuthTestEnv(t)
	defer cleanupAuthTestEnv(env)

	cookie := helperRegisterAndLogin(t, env.router, "onebyte@example.com", "password123", "One Byte User")

	singleByte := []byte{0x7F}
	req := httptest.NewRequest(http.MethodPost, "/api/files/upload", bytes.NewReader(singleByte))
	req.AddCookie(cookie)
	req.Header.Set("X-Filename", "byte.bin")
	req.Header.Set("X-File-Size", "1")
	rec := httptest.NewRecorder()
	env.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected HTTP 201 Created for 1-byte file, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		ID          string `json:"id"`
		TotalChunks int    `json:"total_chunks"`
		Size        int64  `json:"size"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)

	if resp.TotalChunks != 1 {
		t.Errorf("expected 1 chunk, got %d", resp.TotalChunks)
	}
	if resp.Size != 1 {
		t.Errorf("expected size 1, got %d", resp.Size)
	}

	// Download and verify exact 1 byte returned
	dlReq := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/files/%s/download", resp.ID), nil)
	dlReq.AddCookie(cookie)
	dlRec := httptest.NewRecorder()
	env.router.ServeHTTP(dlRec, dlReq)

	if dlRec.Code != http.StatusOK {
		t.Fatalf("expected download 200 OK, got %d: %s", dlRec.Code, dlRec.Body.String())
	}
	if !bytes.Equal(dlRec.Body.Bytes(), singleByte) {
		t.Errorf("downloaded content mismatch: expected %v, got %v", singleByte, dlRec.Body.Bytes())
	}
}

// 3. Edge Case: Exact Chunk Size boundary
func TestAPI_EdgeCase_ExactChunkSize(t *testing.T) {
	env := setupAuthTestEnv(t)
	defer cleanupAuthTestEnv(env)

	cookie := helperRegisterAndLogin(t, env.router, "exact@example.com", "password123", "Exact User")

	chunkSize := 64 * 1024 // 64 KB (matches test env chunkSize)
	payload := bytes.Repeat([]byte("E"), chunkSize)

	req := httptest.NewRequest(http.MethodPost, "/api/files/upload", bytes.NewReader(payload))
	req.AddCookie(cookie)
	req.Header.Set("X-Filename", "exact_chunk.bin")
	req.Header.Set("X-File-Size", strconv.Itoa(chunkSize))
	rec := httptest.NewRecorder()
	env.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created for exact chunk file, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		ID          string `json:"id"`
		TotalChunks int    `json:"total_chunks"`
		Size        int64  `json:"size"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)

	if resp.TotalChunks != 1 {
		t.Errorf("expected exactly 1 chunk for exact chunk size, got %d", resp.TotalChunks)
	}

	// Download and verify
	dlReq := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/files/%s/download", resp.ID), nil)
	dlReq.AddCookie(cookie)
	dlRec := httptest.NewRecorder()
	env.router.ServeHTTP(dlRec, dlReq)

	if dlRec.Code != http.StatusOK {
		t.Fatalf("download failed: %d", dlRec.Code)
	}
	if !bytes.Equal(dlRec.Body.Bytes(), payload) {
		t.Error("downloaded bytes do not match exact chunk payload")
	}
}

// 4. Edge Case: Final Partial Chunk
func TestAPI_EdgeCase_FinalPartialChunk(t *testing.T) {
	env := setupAuthTestEnv(t)
	defer cleanupAuthTestEnv(env)

	cookie := helperRegisterAndLogin(t, env.router, "partial@example.com", "password123", "Partial User")

	// 64 KB chunk size: 64 KB + 64 KB + 20 KB = 148 KB (3 chunks)
	fileSize := 148 * 1024
	payload := bytes.Repeat([]byte("P"), fileSize)

	req := httptest.NewRequest(http.MethodPost, "/api/files/upload", bytes.NewReader(payload))
	req.AddCookie(cookie)
	req.Header.Set("X-Filename", "partial_chunk.bin")
	req.Header.Set("X-File-Size", strconv.Itoa(fileSize))
	rec := httptest.NewRecorder()
	env.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("upload failed: %d: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		ID          string `json:"id"`
		TotalChunks int    `json:"total_chunks"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)

	if resp.TotalChunks != 3 {
		t.Fatalf("expected 3 chunks (2 full + 1 partial), got %d", resp.TotalChunks)
	}

	// Download and verify
	dlReq := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/files/%s/download", resp.ID), nil)
	dlReq.AddCookie(cookie)
	dlRec := httptest.NewRecorder()
	env.router.ServeHTTP(dlRec, dlReq)

	if dlRec.Code != http.StatusOK {
		t.Fatalf("download failed: %d", dlRec.Code)
	}
	if !bytes.Equal(dlRec.Body.Bytes(), payload) {
		t.Error("downloaded bytes do not match partial chunk payload")
	}
}

// 5. Edge Case: Insufficient Capacity
func TestAPI_EdgeCase_InsufficientCapacity(t *testing.T) {
	env := setupAuthTestEnv(t)
	defer cleanupAuthTestEnv(env)

	cookie := helperRegisterAndLogin(t, env.router, "nospace@example.com", "password123", "No Space User")

	// Pool capacity in setupAuthTestEnv is 20MB + 20MB = 40MB
	// Attempt to upload 50MB file
	hugeSize := int64(50 * 1024 * 1024)
	req := httptest.NewRequest(http.MethodPost, "/api/files/upload", bytes.NewReader([]byte("dummy")))
	req.AddCookie(cookie)
	req.ContentLength = hugeSize
	req.Header.Set("X-Filename", "huge.bin")
	req.Header.Set("X-File-Size", strconv.FormatInt(hugeSize, 10))
	rec := httptest.NewRecorder()
	env.router.ServeHTTP(rec, req)

	if rec.Code == http.StatusCreated || rec.Code == http.StatusOK {
		t.Fatalf("expected failure for file exceeding pool capacity, got HTTP %d", rec.Code)
	}
}

// 6. Edge Case: Corrupted Chunk Detection on Download
func TestAPI_EdgeCase_CorruptedChunkDetection(t *testing.T) {
	env := setupAuthTestEnv(t)
	defer cleanupAuthTestEnv(env)

	cookie := helperRegisterAndLogin(t, env.router, "corrupt@example.com", "password123", "Corrupt User")

	fileSize := 64 * 1024
	payload := bytes.Repeat([]byte("Q"), fileSize)

	req := httptest.NewRequest(http.MethodPost, "/api/files/upload", bytes.NewReader(payload))
	req.AddCookie(cookie)
	req.Header.Set("X-Filename", "integrity_test.bin")
	req.Header.Set("X-File-Size", strconv.Itoa(fileSize))
	rec := httptest.NewRecorder()
	env.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("upload failed: %d", rec.Code)
	}

	var resp struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)

	// Fetch file chunks from repo to locate physical chunk file
	chunksList, err := env.repo.Chunks().ListChunksByFile(context.Background(), resp.ID)
	if err != nil || len(chunksList) == 0 {
		t.Fatalf("failed retrieving chunk record: %v", err)
	}

	chk := chunksList[0]
	nodeDir := env.dirs[chk.StorageNodeID]
	chunkPath := filepath.Join(nodeDir, chk.ProviderObjectID)

	// Deliberately corrupt the physical chunk file
	data, err := os.ReadFile(chunkPath)
	if err != nil {
		t.Fatalf("failed reading chunk file: %v", err)
	}
	data[50] ^= 0xFF
	if err := os.WriteFile(chunkPath, data, 0644); err != nil {
		t.Fatalf("failed corrupting chunk file: %v", err)
	}

	// Attempt download: must fail due to checksum corruption
	dlReq := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/files/%s/download", resp.ID), nil)
	dlReq.AddCookie(cookie)
	dlRec := httptest.NewRecorder()
	env.router.ServeHTTP(dlRec, dlReq)

	if dlRec.Code == http.StatusOK && bytes.Equal(dlRec.Body.Bytes(), payload) {
		t.Fatalf("expected download to fail with integrity violation, but it served original payload")
	}
}

// 7. Edge Case: Duplicate Request Handling (Duplicate Registration and Duplicate Chunk Upload)
func TestAPI_EdgeCase_DuplicateRequests(t *testing.T) {
	env := setupAuthTestEnv(t)
	defer cleanupAuthTestEnv(env)

	// 1. Duplicate user registration should return 409 Conflict
	userJSON, _ := json.Marshal(map[string]string{
		"email":    "dup@example.com",
		"password": "password123",
		"name":     "Dup User",
	})
	r1 := httptest.NewRequest(http.MethodPost, "/api/auth/register", bytes.NewReader(userJSON))
	r1.Header.Set("Content-Type", "application/json")
	w1 := httptest.NewRecorder()
	env.router.ServeHTTP(w1, r1)
	if w1.Code != http.StatusCreated {
		t.Fatalf("first registration failed: %d", w1.Code)
	}

	r2 := httptest.NewRequest(http.MethodPost, "/api/auth/register", bytes.NewReader(userJSON))
	r2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	env.router.ServeHTTP(w2, r2)
	if w2.Code != http.StatusConflict {
		t.Fatalf("expected 409 Conflict on duplicate registration, got: %d", w2.Code)
	}

	// Log in to test resumable duplicate chunk upload
	cookie := helperRegisterAndLogin(t, env.router, "dup_chunk@example.com", "password123", "Dup Chunk User")

	// Init upload session
	initBody, _ := json.Marshal(map[string]any{
		"filename":   "dup_test.bin",
		"size":       128 * 1024,
		"chunk_size": 64 * 1024,
	})
	iReq := httptest.NewRequest(http.MethodPost, "/api/uploads", bytes.NewReader(initBody))
	iReq.AddCookie(cookie)
	iReq.Header.Set("Content-Type", "application/json")
	iRec := httptest.NewRecorder()
	env.router.ServeHTTP(iRec, iReq)

	var session struct {
		UploadID string `json:"upload_id"`
	}
	_ = json.Unmarshal(iRec.Body.Bytes(), &session)

	chunkPayload := bytes.Repeat([]byte("D"), 64*1024)

	// Upload chunk 0 first time
	c1Req := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/api/uploads/%s/chunks/0", session.UploadID), bytes.NewReader(chunkPayload))
	c1Req.AddCookie(cookie)
	c1Req.Header.Set("Content-Type", "application/octet-stream")
	c1Rec := httptest.NewRecorder()
	env.router.ServeHTTP(c1Rec, c1Req)
	if c1Rec.Code != http.StatusOK {
		t.Fatalf("first chunk upload failed: %d", c1Rec.Code)
	}

	// Upload chunk 0 second time (duplicate request) - should succeed idempotently without error
	c2Req := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/api/uploads/%s/chunks/0", session.UploadID), bytes.NewReader(chunkPayload))
	c2Req.AddCookie(cookie)
	c2Req.Header.Set("Content-Type", "application/octet-stream")
	c2Rec := httptest.NewRecorder()
	env.router.ServeHTTP(c2Rec, c2Req)
	if c2Rec.Code != http.StatusOK {
		t.Fatalf("duplicate chunk upload failed: %d", c2Rec.Code)
	}
}

// 8. Edge Case: Interrupted and Resumed Upload Workflow
func TestAPI_EdgeCase_InterruptedAndResumedUpload(t *testing.T) {
	env := setupAuthTestEnv(t)
	defer cleanupAuthTestEnv(env)

	cookie := helperRegisterAndLogin(t, env.router, "resume@example.com", "password123", "Resume User")

	chunkSize := 64 * 1024
	totalChunks := 4
	totalSize := totalChunks * chunkSize
	part0 := bytes.Repeat([]byte("0"), chunkSize)
	part1 := bytes.Repeat([]byte("1"), chunkSize)
	part2 := bytes.Repeat([]byte("2"), chunkSize)
	part3 := bytes.Repeat([]byte("3"), chunkSize)

	// 1. Initialize session
	initBody, _ := json.Marshal(map[string]any{
		"filename":   "resume_flow.bin",
		"size":       totalSize,
		"chunk_size": chunkSize,
	})
	initReq := httptest.NewRequest(http.MethodPost, "/api/uploads", bytes.NewReader(initBody))
	initReq.AddCookie(cookie)
	initReq.Header.Set("Content-Type", "application/json")
	initRec := httptest.NewRecorder()
	env.router.ServeHTTP(initRec, initReq)

	if initRec.Code != http.StatusCreated {
		t.Fatalf("init upload failed: %d", initRec.Code)
	}

	var session struct {
		UploadID string `json:"upload_id"`
		FileID   string `json:"file_id"`
	}
	_ = json.Unmarshal(initRec.Body.Bytes(), &session)

	// 2. Upload chunks 0 and 1, then simulate client disconnect / interruption
	for idx, chunkData := range [][]byte{part0, part1} {
		uReq := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/api/uploads/%s/chunks/%d", session.UploadID, idx), bytes.NewReader(chunkData))
		uReq.AddCookie(cookie)
		uRec := httptest.NewRecorder()
		env.router.ServeHTTP(uRec, uReq)
		if uRec.Code != http.StatusOK {
			t.Fatalf("chunk %d upload failed: %d", idx, uRec.Code)
		}
	}

	// 3. Client checks upload status before resuming
	statusReq := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/uploads/%s", session.UploadID), nil)
	statusReq.AddCookie(cookie)
	statusRec := httptest.NewRecorder()
	env.router.ServeHTTP(statusRec, statusReq)

	var status struct {
		CompletedCount   int   `json:"completed_count"`
		CompletedIndices []int `json:"completed_indices"`
		NextExpected     int   `json:"next_expected"`
	}
	_ = json.Unmarshal(statusRec.Body.Bytes(), &status)

	if status.CompletedCount != 2 {
		t.Fatalf("expected 2 completed chunks, got %d", status.CompletedCount)
	}
	if status.NextExpected != 2 {
		t.Fatalf("expected next expected chunk 2, got %d", status.NextExpected)
	}

	// 4. Resume upload: send remaining chunks 2 and 3
	for idx, chunkData := range [][]byte{part2, part3} {
		actualIdx := idx + 2
		uReq := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/api/uploads/%s/chunks/%d", session.UploadID, actualIdx), bytes.NewReader(chunkData))
		uReq.AddCookie(cookie)
		uRec := httptest.NewRecorder()
		env.router.ServeHTTP(uRec, uReq)
		if uRec.Code != http.StatusOK {
			t.Fatalf("chunk %d upload failed: %d", actualIdx, uRec.Code)
		}
	}

	// 5. Verify final status is COMPLETED
	finalReq := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/uploads/%s", session.UploadID), nil)
	finalReq.AddCookie(cookie)
	finalRec := httptest.NewRecorder()
	env.router.ServeHTTP(finalRec, finalReq)

	var finalStatus struct {
		Status         string `json:"status"`
		CompletedCount int    `json:"completed_count"`
	}
	_ = json.Unmarshal(finalRec.Body.Bytes(), &finalStatus)

	if finalStatus.Status != "COMPLETED" {
		t.Fatalf("expected final status COMPLETED, got %s", finalStatus.Status)
	}
	if finalStatus.CompletedCount != 4 {
		t.Fatalf("expected 4 completed chunks, got %d", finalStatus.CompletedCount)
	}

	// 6. Download the complete file and verify full content reconstruction
	expectedFull := append(append(append(part0, part1...), part2...), part3...)
	dlReq := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/files/%s/download", session.FileID), nil)
	dlReq.AddCookie(cookie)
	dlRec := httptest.NewRecorder()
	env.router.ServeHTTP(dlRec, dlReq)

	if dlRec.Code != http.StatusOK {
		t.Fatalf("download failed: %d", dlRec.Code)
	}
	if !bytes.Equal(dlRec.Body.Bytes(), expectedFull) {
		t.Error("reconstructed file does not match original bytes after resume")
	}
}

// 9. Integration Test: File Delete & Physical Cleanup
func TestAPI_Integration_FileDelete(t *testing.T) {
	env := setupAuthTestEnv(t)
	defer cleanupAuthTestEnv(env)

	cookie := helperRegisterAndLogin(t, env.router, "delete@example.com", "password123", "Delete User")

	// Upload file
	payload := bytes.Repeat([]byte("DEL"), 10*1024)
	req := httptest.NewRequest(http.MethodPost, "/api/files/upload", bytes.NewReader(payload))
	req.AddCookie(cookie)
	req.Header.Set("X-Filename", "delete_me.txt")
	req.Header.Set("X-File-Size", strconv.Itoa(len(payload)))
	rec := httptest.NewRecorder()
	env.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("upload failed: %d", rec.Code)
	}

	var fileResp struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &fileResp)

	// Fetch chunk locations
	chunksList, err := env.repo.Chunks().ListChunksByFile(context.Background(), fileResp.ID)
	if err != nil || len(chunksList) == 0 {
		t.Fatalf("failed retrieving chunks for file: %v", err)
	}

	chunkFilePaths := make([]string, 0, len(chunksList))
	for _, chk := range chunksList {
		p := filepath.Join(env.dirs[chk.StorageNodeID], chk.ProviderObjectID)
		chunkFilePaths = append(chunkFilePaths, p)
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("expected physical chunk file to exist at %s", p)
		}
	}

	// Delete file via API
	delReq := httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/api/files/%s", fileResp.ID), nil)
	delReq.AddCookie(cookie)
	delRec := httptest.NewRecorder()
	env.router.ServeHTTP(delRec, delReq)

	if delRec.Code != http.StatusOK && delRec.Code != http.StatusNoContent {
		t.Fatalf("expected 200/204 on delete, got: %d", delRec.Code)
	}

	// Verify file is gone from repository
	_, err = env.repo.Files().GetFile(context.Background(), fileResp.ID)
	if err == nil {
		t.Fatalf("expected file to be deleted from database, but it was found")
	}

	// Verify physical chunk files were deleted from storage nodes
	for _, p := range chunkFilePaths {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("expected physical chunk file at %s to be deleted, but it still exists", p)
		}
	}
}

// 10. Integration Test: Node Failure & Replica Failover via API
func TestAPI_Integration_NodeFailureAndReplicaFailover(t *testing.T) {
	env := setupAuthTestEnv(t)
	defer cleanupAuthTestEnv(env)

	cookie := helperRegisterAndLogin(t, env.router, "failover@example.com", "password123", "Failover User")

	fileSize := 64 * 1024
	payload := bytes.Repeat([]byte("REPL"), fileSize/4)

	// Upload with replication factor = 2
	req := httptest.NewRequest(http.MethodPost, "/api/files/upload", bytes.NewReader(payload))
	req.AddCookie(cookie)
	req.Header.Set("X-Filename", "replicated_failover.bin")
	req.Header.Set("X-File-Size", strconv.Itoa(fileSize))
	req.Header.Set("X-Replication-Factor", "2")
	rec := httptest.NewRecorder()
	env.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("replicated upload failed: %d: %s", rec.Code, rec.Body.String())
	}

	var fileResp struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &fileResp)

	chunksList, err := env.repo.Chunks().ListChunksByFile(context.Background(), fileResp.ID)
	if err != nil || len(chunksList) == 0 {
		t.Fatalf("failed fetching chunk records: %v", err)
	}

	primaryNodeID := chunksList[0].StorageNodeID

	// Simulate outage on primary node
	primaryNode, err := env.storageManager.GetNode(primaryNodeID)
	if err != nil {
		t.Fatalf("failed getting primary node: %v", err)
	}
	primaryNode.SetSimulatedOffline(true)

	// Download file while primary node is OFFLINE - should seamlessly reconstruct from replica
	dlReq := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/files/%s/download", fileResp.ID), nil)
	dlReq.AddCookie(cookie)
	dlRec := httptest.NewRecorder()
	env.router.ServeHTTP(dlRec, dlReq)

	if dlRec.Code != http.StatusOK {
		t.Fatalf("download during primary node outage failed: %d: %s", dlRec.Code, dlRec.Body.String())
	}
	if !bytes.Equal(dlRec.Body.Bytes(), payload) {
		t.Error("downloaded content from replica does not match original payload")
	}

	// Restore primary node
	primaryNode.SetSimulatedOffline(false)
}
