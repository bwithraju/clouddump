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
	"strings"
	"testing"
	"time"

	"github.com/bwithraju/clouddump/backend/internal/auth"
	"github.com/bwithraju/clouddump/backend/internal/chunks"
	"github.com/bwithraju/clouddump/backend/internal/database"
	"github.com/bwithraju/clouddump/backend/internal/providers"
	"github.com/bwithraju/clouddump/backend/internal/storage"
	"github.com/bwithraju/clouddump/backend/internal/uploads"
	"github.com/bwithraju/clouddump/backend/internal/workers"
)

type authTestEnv struct {
	router         http.Handler
	storageManager *storage.StorageManager
	repo           database.Repository
	authService    *auth.AuthService
	uploadManager  *uploads.UploadManager
	dirs           map[string]string
}

func setupAuthTestEnv(t *testing.T) *authTestEnv {
	chunkSize := int64(64 * 1024) // 64 KB chunks
	mgr := storage.NewStorageManager(storage.NewMaxAvailableStrategy())

	tempDir1, _ := os.MkdirTemp("", "auth_node1_*")
	tempDir2, _ := os.MkdirTemp("", "auth_node2_*")

	p1, _ := providers.NewLocalStorageProvider(tempDir1, 20*1024*1024)
	p2, _ := providers.NewLocalStorageProvider(tempDir2, 20*1024*1024)

	n1, _ := storage.NewStorageNode("node1", "Shared Node 1", p1)
	n2, _ := storage.NewStorageNode("node2", "Shared Node 2", p2)

	_ = mgr.RegisterNode(n1)
	_ = mgr.RegisterNode(n2)

	dirs := map[string]string{
		"node1": tempDir1,
		"node2": tempDir2,
	}

	repo := database.NewMemoryRepository()
	sessionStore := auth.NewSessionStore(1 * time.Hour)
	authService := auth.NewAuthService(repo.Users(), sessionStore)
	authHandler := NewAuthHandler(authService)

	distributor, _ := workers.NewParallelUploader(mgr, chunkSize, 2)
	reconstructor, _ := chunks.NewFileReconstructor(mgr)
	fileHandler := NewFileHandler(mgr, distributor, reconstructor, repo, chunkSize)
	nodesHandler := NewNodesHandler(mgr, repo)
	uploadManager := uploads.NewUploadManager(mgr, repo, chunkSize)
	uploadsHandler := NewUploadsHandler(uploadManager)

	router := NewRouter(RouterConfig{
		FileHandler:    fileHandler,
		NodesHandler:   nodesHandler,
		UploadsHandler: uploadsHandler,
		AuthHandler:    authHandler,
		AuthService:    authService,
	})

	return &authTestEnv{
		router:         router,
		storageManager: mgr,
		repo:           repo,
		authService:    authService,
		uploadManager:  uploadManager,
		dirs:           dirs,
	}
}

func cleanupAuthTestEnv(env *authTestEnv) {
	for _, d := range env.dirs {
		_ = os.RemoveAll(d)
	}
}

// 1. Test Valid Login and Session Cookie
func TestAuth_ValidLoginAndSessionCookie(t *testing.T) {
	env := setupAuthTestEnv(t)
	defer cleanupAuthTestEnv(env)

	// Register account
	regBody, _ := json.Marshal(map[string]string{
		"email":    "alice@example.com",
		"password": "supersecretpassword",
		"name":     "Alice",
	})
	regReq := httptest.NewRequest(http.MethodPost, "/api/auth/register", bytes.NewReader(regBody))
	regReq.Header.Set("Content-Type", "application/json")
	regRec := httptest.NewRecorder()
	env.router.ServeHTTP(regRec, regReq)

	if regRec.Code != http.StatusCreated {
		t.Fatalf("expected registration 201 Created, got %d: %s", regRec.Code, regRec.Body.String())
	}

	// Verify password hash is NOT returned
	if strings.Contains(regRec.Body.String(), "password_hash") || strings.Contains(regRec.Body.String(), "supersecretpassword") {
		t.Fatalf("registration exposed sensitive password hash or plaintext")
	}

	// Login with valid credentials
	loginBody, _ := json.Marshal(map[string]string{
		"email":    "alice@example.com",
		"password": "supersecretpassword",
	})
	loginReq := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(loginBody))
	loginReq.Header.Set("Content-Type", "application/json")
	loginRec := httptest.NewRecorder()
	env.router.ServeHTTP(loginRec, loginReq)

	if loginRec.Code != http.StatusOK {
		t.Fatalf("expected login 200 OK, got %d: %s", loginRec.Code, loginRec.Body.String())
	}

	// Verify HTTP-only cookie
	cookies := loginRec.Result().Cookies()
	var sessionCookie *http.Cookie
	for _, c := range cookies {
		if c.Name == auth.SessionCookieName {
			sessionCookie = c
			break
		}
	}
	if sessionCookie == nil {
		t.Fatalf("expected %s cookie in response, none found", auth.SessionCookieName)
	}
	if !sessionCookie.HttpOnly {
		t.Errorf("expected session cookie to be HttpOnly")
	}
	if sessionCookie.Value == "" {
		t.Errorf("session cookie value was empty")
	}

	// Verify /api/auth/me returns authenticated user
	meReq := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	meReq.AddCookie(sessionCookie)
	meRec := httptest.NewRecorder()
	env.router.ServeHTTP(meRec, meReq)

	if meRec.Code != http.StatusOK {
		t.Fatalf("expected /api/auth/me 200 OK, got %d: %s", meRec.Code, meRec.Body.String())
	}

	var meResp struct {
		User struct {
			ID    string `json:"id"`
			Email string `json:"email"`
			Name  string `json:"name"`
		} `json:"user"`
	}
	if err := json.NewDecoder(meRec.Body).Decode(&meResp); err != nil {
		t.Fatalf("failed decoding /api/auth/me response: %v", err)
	}
	if meResp.User.Email != "alice@example.com" {
		t.Errorf("expected email 'alice@example.com', got '%s'", meResp.User.Email)
	}
	if meResp.User.Name != "Alice" {
		t.Errorf("expected name 'Alice', got '%s'", meResp.User.Name)
	}
}

// 2. Test Invalid Login
func TestAuth_InvalidLogin(t *testing.T) {
	env := setupAuthTestEnv(t)
	defer cleanupAuthTestEnv(env)

	// Create user
	_, _, err := env.authService.Register(context.Background(), "bob@example.com", "correctpassword123", "Bob")
	if err != nil {
		t.Fatalf("failed to register bob: %v", err)
	}

	// Case 1: Wrong password
	wrongPassBody, _ := json.Marshal(map[string]string{
		"email":    "bob@example.com",
		"password": "wrongpassword999",
	})
	req1 := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(wrongPassBody))
	req1.Header.Set("Content-Type", "application/json")
	rec1 := httptest.NewRecorder()
	env.router.ServeHTTP(rec1, req1)

	if rec1.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 Unauthorized for wrong password, got %d", rec1.Code)
	}

	// Case 2: Non-existent email
	nonExistentBody, _ := json.Marshal(map[string]string{
		"email":    "nonexistent@example.com",
		"password": "anyPassword123",
	})
	req2 := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(nonExistentBody))
	req2.Header.Set("Content-Type", "application/json")
	rec2 := httptest.NewRecorder()
	env.router.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 Unauthorized for nonexistent user, got %d", rec2.Code)
	}
}

// 3. Test Protected Routes Reject Unauthenticated Requests
func TestAuth_ProtectedRoutesRejectUnauthenticated(t *testing.T) {
	env := setupAuthTestEnv(t)
	defer cleanupAuthTestEnv(env)

	protectedEndpoints := []struct {
		method string
		url    string
	}{
		{http.MethodGet, "/api/files"},
		{http.MethodPost, "/api/files/upload"},
		{http.MethodGet, "/api/files/test_file_id"},
		{http.MethodGet, "/api/nodes"},
		{http.MethodGet, "/api/nodes/stats"},
		{http.MethodPost, "/api/uploads"},
		{http.MethodGet, "/api/uploads"},
		{http.MethodGet, "/api/auth/me"},
	}

	for _, ep := range protectedEndpoints {
		req := httptest.NewRequest(ep.method, ep.url, nil)
		rec := httptest.NewRecorder()
		env.router.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("[%s %s] expected 401 Unauthorized without credentials, got %d", ep.method, ep.url, rec.Code)
		}
	}
}

// 4. Test User A Accessing User B's File (Strict Isolation)
func TestAuth_TenantIsolation_UserACannotAccessUserBFile(t *testing.T) {
	env := setupAuthTestEnv(t)
	defer cleanupAuthTestEnv(env)

	ctx := context.Background()

	// Register User A and User B
	userA, sessionA, err := env.authService.Register(ctx, "usera@example.com", "passwordA123", "User A")
	if err != nil {
		t.Fatalf("failed registering User A: %v", err)
	}
	userB, sessionB, err := env.authService.Register(ctx, "userb@example.com", "passwordB123", "User B")
	if err != nil {
		t.Fatalf("failed registering User B: %v", err)
	}

	// User A uploads a file
	filePayload := bytes.Repeat([]byte("Secret data belonging exclusively to User A! "), 2000) // ~90KB
	bodyBuf := &bytes.Buffer{}
	mpWriter := multipart.NewWriter(bodyBuf)
	part, _ := mpWriter.CreateFormFile("file", "user_a_private.txt")
	_, _ = part.Write(filePayload)
	_ = mpWriter.Close()

	uploadReq := httptest.NewRequest(http.MethodPost, "/api/files/upload", bodyBuf)
	uploadReq.Header.Set("Content-Type", mpWriter.FormDataContentType())
	uploadReq.Header.Set("X-File-Size", fmt.Sprintf("%d", len(filePayload)))
	uploadReq.Header.Set("Authorization", "Bearer "+sessionA.Token)
	uploadRec := httptest.NewRecorder()
	env.router.ServeHTTP(uploadRec, uploadReq)

	if uploadRec.Code != http.StatusCreated {
		t.Fatalf("User A upload failed: %d - %s", uploadRec.Code, uploadRec.Body.String())
	}

	var uploadResp struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(uploadRec.Body).Decode(&uploadResp); err != nil {
		t.Fatalf("failed decoding upload response: %v", err)
	}
	userAFileID := uploadResp.ID
	if userAFileID == "" {
		t.Fatalf("empty file id returned")
	}

	// 1. User B tries GET /api/files/:id -> MUST be 403 Forbidden
	getReq := httptest.NewRequest(http.MethodGet, "/api/files/"+userAFileID, nil)
	getReq.Header.Set("Authorization", "Bearer "+sessionB.Token)
	getRec := httptest.NewRecorder()
	env.router.ServeHTTP(getRec, getReq)

	if getRec.Code != http.StatusForbidden {
		t.Errorf("User B accessing User A file details: expected 403 Forbidden, got %d: %s", getRec.Code, getRec.Body.String())
	}

	// 2. User B tries GET /api/files/:id/download -> MUST be 403 Forbidden
	dlReq := httptest.NewRequest(http.MethodGet, "/api/files/"+userAFileID+"/download", nil)
	dlReq.Header.Set("Authorization", "Bearer "+sessionB.Token)
	dlRec := httptest.NewRecorder()
	env.router.ServeHTTP(dlRec, dlReq)

	if dlRec.Code != http.StatusForbidden {
		t.Errorf("User B downloading User A file: expected 403 Forbidden, got %d: %s", dlRec.Code, dlRec.Body.String())
	}

	// 3. User B tries DELETE /api/files/:id -> MUST be 403 Forbidden
	delReq := httptest.NewRequest(http.MethodDelete, "/api/files/"+userAFileID, nil)
	delReq.Header.Set("Authorization", "Bearer "+sessionB.Token)
	delRec := httptest.NewRecorder()
	env.router.ServeHTTP(delRec, delReq)

	if delRec.Code != http.StatusForbidden {
		t.Errorf("User B deleting User A file: expected 403 Forbidden, got %d: %s", delRec.Code, delRec.Body.String())
	}

	// 4. User B lists files: User A's file MUST NOT be returned
	listReq := httptest.NewRequest(http.MethodGet, "/api/files", nil)
	listReq.Header.Set("Authorization", "Bearer "+sessionB.Token)
	listRec := httptest.NewRecorder()
	env.router.ServeHTTP(listRec, listReq)

	if listRec.Code != http.StatusOK {
		t.Fatalf("User B listing files failed: %d", listRec.Code)
	}

	var bFiles []*database.FileRecord
	if err := json.NewDecoder(listRec.Body).Decode(&bFiles); err != nil {
		t.Fatalf("failed decoding User B files: %v", err)
	}
	if len(bFiles) != 0 {
		t.Errorf("expected User B to see 0 files, got %d", len(bFiles))
	}

	// 5. User A CAN access their own file
	userAGetReq := httptest.NewRequest(http.MethodGet, "/api/files/"+userAFileID, nil)
	userAGetReq.Header.Set("Authorization", "Bearer "+sessionA.Token)
	userAGetRec := httptest.NewRecorder()
	env.router.ServeHTTP(userAGetRec, userAGetReq)

	if userAGetRec.Code != http.StatusOK {
		t.Errorf("User A accessing their own file expected 200 OK, got %d", userAGetRec.Code)
	}

	_ = userA
	_ = userB
}

// 5. Test Resumable Upload Session and Storage Node Tenant Isolation
func TestAuth_TenantIsolation_UploadSessionAndStorageNode(t *testing.T) {
	env := setupAuthTestEnv(t)
	defer cleanupAuthTestEnv(env)

	ctx := context.Background()

	userA, sessionA, _ := env.authService.Register(ctx, "user1@example.com", "pass123456", "User 1")
	userB, sessionB, _ := env.authService.Register(ctx, "user2@example.com", "pass123456", "User 2")

	// User A creates an upload session
	initPayload, _ := json.Marshal(map[string]interface{}{
		"filename":   "userA_document.pdf",
		"size":       128 * 1024,
		"chunk_size": 64 * 1024,
	})
	initReq := httptest.NewRequest(http.MethodPost, "/api/uploads", bytes.NewReader(initPayload))
	initReq.Header.Set("Content-Type", "application/json")
	initReq.Header.Set("Authorization", "Bearer "+sessionA.Token)
	initRec := httptest.NewRecorder()
	env.router.ServeHTTP(initRec, initReq)

	if initRec.Code != http.StatusCreated {
		t.Fatalf("User A session init failed: %d - %s", initRec.Code, initRec.Body.String())
	}

	var initResp uploads.SessionSummary
	_ = json.NewDecoder(initRec.Body).Decode(&initResp)
	uploadID := initResp.UploadID

	// User B attempts to access User A's upload session -> MUST be 403 Forbidden
	sessReq := httptest.NewRequest(http.MethodGet, "/api/uploads/"+uploadID, nil)
	sessReq.Header.Set("Authorization", "Bearer "+sessionB.Token)
	sessRec := httptest.NewRecorder()
	env.router.ServeHTTP(sessRec, sessReq)

	if sessRec.Code != http.StatusForbidden {
		t.Errorf("User B accessing User A upload session: expected 403 Forbidden, got %d", sessRec.Code)
	}

	// User B attempts to upload chunk to User A's session -> MUST be 403 Forbidden
	chunkData := bytes.Repeat([]byte("B"), 64*1024)
	chunkReq := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/api/uploads/%s/chunks/0", uploadID), bytes.NewReader(chunkData))
	chunkReq.Header.Set("Authorization", "Bearer "+sessionB.Token)
	chunkRec := httptest.NewRecorder()
	env.router.ServeHTTP(chunkRec, chunkReq)

	if chunkRec.Code != http.StatusForbidden {
		t.Errorf("User B uploading chunk to User A session: expected 403 Forbidden, got %d", chunkRec.Code)
	}

	// User B attempts to cancel User A's upload session -> MUST be 403 Forbidden
	cancelReq := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/uploads/%s/cancel", uploadID), nil)
	cancelReq.Header.Set("Authorization", "Bearer "+sessionB.Token)
	cancelRec := httptest.NewRecorder()
	env.router.ServeHTTP(cancelRec, cancelReq)

	if cancelRec.Code != http.StatusForbidden {
		t.Errorf("User B cancelling User A session: expected 403 Forbidden, got %d", cancelRec.Code)
	}

	// User-specific Storage Node isolation
	nodeADir, _ := os.MkdirTemp("", "node_a_*")
	env.dirs["nodeA"] = nodeADir
	providerA, _ := providers.NewLocalStorageProvider(nodeADir, 10*1024*1024)
	nodeA, _ := storage.NewStorageNode("user_a_private_node", "User A Private Node", providerA)
	nodeA.SetUserID(userA.ID)
	_ = env.storageManager.RegisterNode(nodeA)

	// User B attempts to simulate failure on User A's node -> MUST be 403 Forbidden
	failBody, _ := json.Marshal(map[string]bool{"offline": true})
	failReq := httptest.NewRequest(http.MethodPost, "/api/nodes/user_a_private_node/simulate-failure", bytes.NewReader(failBody))
	failReq.Header.Set("Content-Type", "application/json")
	failReq.Header.Set("Authorization", "Bearer "+sessionB.Token)
	failRec := httptest.NewRecorder()
	env.router.ServeHTTP(failRec, failReq)

	if failRec.Code != http.StatusForbidden {
		t.Errorf("User B tampering with User A storage node: expected 403 Forbidden, got %d", failRec.Code)
	}

	// User B listing nodes: User A's private node MUST NOT be in User B's list
	nodesListReq := httptest.NewRequest(http.MethodGet, "/api/nodes", nil)
	nodesListReq.Header.Set("Authorization", "Bearer "+sessionB.Token)
	nodesListRec := httptest.NewRecorder()
	env.router.ServeHTTP(nodesListRec, nodesListReq)

	if nodesListRec.Code != http.StatusOK {
		t.Fatalf("User B listing nodes failed: %d", nodesListRec.Code)
	}

	var bNodes []storage.NodeStats
	_ = json.NewDecoder(nodesListRec.Body).Decode(&bNodes)
	for _, n := range bNodes {
		if n.ID == "user_a_private_node" {
			t.Errorf("User A's private node was leaked in User B's node list!")
		}
	}

	_ = userB
}

// 6. Test Logout
func TestAuth_Logout(t *testing.T) {
	env := setupAuthTestEnv(t)
	defer cleanupAuthTestEnv(env)

	ctx := context.Background()

	// Register user
	_, session, err := env.authService.Register(ctx, "charlie@example.com", "charliepass123", "Charlie")
	if err != nil {
		t.Fatalf("failed registering charlie: %v", err)
	}

	// Ensure /api/auth/me works before logout
	meReq1 := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	meReq1.AddCookie(&http.Cookie{
		Name:  auth.SessionCookieName,
		Value: session.Token,
	})
	meRec1 := httptest.NewRecorder()
	env.router.ServeHTTP(meRec1, meReq1)

	if meRec1.Code != http.StatusOK {
		t.Fatalf("expected /api/auth/me 200 OK before logout, got %d", meRec1.Code)
	}

	// Call POST /api/auth/logout
	logoutReq := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	logoutReq.AddCookie(&http.Cookie{
		Name:  auth.SessionCookieName,
		Value: session.Token,
	})
	logoutRec := httptest.NewRecorder()
	env.router.ServeHTTP(logoutRec, logoutReq)

	if logoutRec.Code != http.StatusOK {
		t.Fatalf("expected /api/auth/logout 200 OK, got %d", logoutRec.Code)
	}

	// Verify cookie is cleared with MaxAge: -1
	cookies := logoutRec.Result().Cookies()
	var clearedCookie *http.Cookie
	for _, c := range cookies {
		if c.Name == auth.SessionCookieName {
			clearedCookie = c
			break
		}
	}
	if clearedCookie == nil {
		t.Fatalf("expected cleared session cookie in logout response")
	}
	if clearedCookie.MaxAge >= 0 && clearedCookie.Value != "" {
		t.Errorf("session cookie was not invalidated (MaxAge=%d, Value=%s)", clearedCookie.MaxAge, clearedCookie.Value)
	}

	// Ensure subsequent call to /api/auth/me with revoked token returns 401 Unauthorized
	meReq2 := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	meReq2.Header.Set("Authorization", "Bearer "+session.Token)
	meRec2 := httptest.NewRecorder()
	env.router.ServeHTTP(meRec2, meReq2)

	if meRec2.Code != http.StatusUnauthorized {
		t.Errorf("expected /api/auth/me 401 Unauthorized after logout, got %d", meRec2.Code)
	}
}

// 7. Regression Test: Resumable Upload Session Authentication (Cookie and Bearer Header)
func TestAuth_UploadSessionAuthenticationRegression(t *testing.T) {
	env := setupAuthTestEnv(t)
	defer cleanupAuthTestEnv(env)

	ctx := context.Background()

	// Register user
	user, session, err := env.authService.Register(ctx, "uploader@example.com", "uploaderpassword123", "Uploader")
	if err != nil {
		t.Fatalf("failed registering uploader: %v", err)
	}

	uploadPayload := `{"filename":"dataset.bin","size":131072,"mime_type":"application/octet-stream","chunk_size":65536}`

	// Case 1: Unauthenticated POST /api/uploads MUST return 401 Unauthorized
	{
		req := httptest.NewRequest(http.MethodPost, "/api/uploads", strings.NewReader(uploadPayload))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		env.router.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected unauthenticated /api/uploads to return 401 Unauthorized, got %d (body: %s)", rec.Code, rec.Body.String())
		}

		var errResp map[string]string
		if err := json.Unmarshal(rec.Body.Bytes(), &errResp); err != nil {
			t.Fatalf("failed decoding error response: %v", err)
		}
		if errResp["error"] != "unauthorized" || errResp["message"] != "authentication required" {
			t.Errorf("expected unauthorized / authentication required, got: %v", errResp)
		}
	}

	// Case 2: POST /api/uploads with invalid Bearer token MUST return 401 Unauthorized
	{
		req := httptest.NewRequest(http.MethodPost, "/api/uploads", strings.NewReader(uploadPayload))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer invalid-or-expired-token-xyz")
		rec := httptest.NewRecorder()
		env.router.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected invalid token to return 401 Unauthorized, got %d", rec.Code)
		}
	}

	// Case 3: POST /api/uploads with valid session cookie MUST succeed (201 Created)
	var cookieSessionID string
	{
		req := httptest.NewRequest(http.MethodPost, "/api/uploads", strings.NewReader(uploadPayload))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(&http.Cookie{
			Name:  auth.SessionCookieName,
			Value: session.Token,
		})
		rec := httptest.NewRecorder()
		env.router.ServeHTTP(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("expected cookie-authenticated /api/uploads to return 201 Created, got %d (body: %s)", rec.Code, rec.Body.String())
		}

		var resp map[string]interface{}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed decoding session summary: %v", err)
		}
		cookieSessionID = resp["upload_id"].(string)
		if cookieSessionID == "" {
			t.Fatalf("expected non-empty upload_id in response")
		}
	}

	// Case 4: POST /api/uploads with valid Authorization: Bearer <token> MUST succeed (201 Created)
	var bearerSessionID string
	{
		req := httptest.NewRequest(http.MethodPost, "/api/uploads", strings.NewReader(uploadPayload))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+session.Token)
		rec := httptest.NewRecorder()
		env.router.ServeHTTP(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("expected bearer-authenticated /api/uploads to return 201 Created, got %d (body: %s)", rec.Code, rec.Body.String())
		}

		var resp map[string]interface{}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed decoding session summary: %v", err)
		}
		bearerSessionID = resp["upload_id"].(string)
		if bearerSessionID == "" {
			t.Fatalf("expected non-empty upload_id in response")
		}
	}

	// Case 5: PUT /api/uploads/{id}/chunks/{index} unauthenticated MUST return 401 Unauthorized
	chunkData := make([]byte, 65536)
	{
		req := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/api/uploads/%s/chunks/0", bearerSessionID), bytes.NewReader(chunkData))
		req.Header.Set("Content-Type", "application/octet-stream")
		rec := httptest.NewRecorder()
		env.router.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected unauthenticated chunk upload to return 401 Unauthorized, got %d", rec.Code)
		}
	}

	// Case 6: PUT /api/uploads/{id}/chunks/{index} with Bearer token MUST succeed (200 OK)
	{
		req := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/api/uploads/%s/chunks/0", bearerSessionID), bytes.NewReader(chunkData))
		req.Header.Set("Content-Type", "application/octet-stream")
		req.Header.Set("Authorization", "Bearer "+session.Token)
		rec := httptest.NewRecorder()
		env.router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected authenticated chunk upload to return 200 OK, got %d (body: %s)", rec.Code, rec.Body.String())
		}
	}

	// Case 7: GET /api/uploads/{id} unauthenticated MUST return 401 Unauthorized
	{
		req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/uploads/%s", bearerSessionID), nil)
		rec := httptest.NewRecorder()
		env.router.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected unauthenticated get upload session to return 401 Unauthorized, got %d", rec.Code)
		}
	}

	// Case 8: GET /api/uploads/{id} with Bearer token MUST return 200 OK with session progress
	{
		req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/uploads/%s", bearerSessionID), nil)
		req.Header.Set("Authorization", "Bearer "+session.Token)
		rec := httptest.NewRecorder()
		env.router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected authenticated get upload session to return 200 OK, got %d", rec.Code)
		}

		var summary map[string]interface{}
		if err := json.Unmarshal(rec.Body.Bytes(), &summary); err != nil {
			t.Fatalf("failed decoding session summary: %v", err)
		}
		if summary["completed_count"].(float64) != 1 {
			t.Errorf("expected completed_count=1, got %v", summary["completed_count"])
		}
	}

	_ = user
	_ = cookieSessionID
}
