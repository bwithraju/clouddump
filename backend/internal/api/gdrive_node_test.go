package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bwithraju/clouddump/backend/internal/auth"
	"github.com/bwithraju/clouddump/backend/internal/chunks"
	"github.com/bwithraju/clouddump/backend/internal/database"
	"github.com/bwithraju/clouddump/backend/internal/providers"
	"github.com/bwithraju/clouddump/backend/internal/storage"
	"github.com/bwithraju/clouddump/backend/internal/uploads"
	"github.com/bwithraju/clouddump/backend/internal/workers"
	"golang.org/x/oauth2"
)

type gdriveTestEnvironment struct {
	router         http.Handler
	storageManager *storage.StorageManager
	repo           database.Repository
	authService    *auth.AuthService
	oauthService   *auth.OAuthService
	gdriveServer   *httptest.Server
	storedChunks   map[string][]byte
}

func setupGDriveTestEnvironment(t *testing.T) *gdriveTestEnvironment {
	storedChunks := make(map[string][]byte)
	var uploadCounter int64

	// Mock Google Drive and OAuth Server
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Mock OAuth token exchange: POST /oauth2/v4/token
		if r.Method == http.MethodPost && (strings.Contains(r.URL.Path, "/token") || strings.Contains(r.URL.Path, "/oauth")) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"access_token":  "mock_drive_access_token_12345",
				"refresh_token": "mock_drive_refresh_token_67890",
				"token_type":    "Bearer",
				"expires_in":    3600,
			})
			return
		}

		// Mock Drive Quota: GET /about
		if r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/about") {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"storageQuota": map[string]string{
					"limit": "100000000", // ~100 MB
					"usage": "10000000",  // ~10 MB
				},
			})
			return
		}

		// Mock Resumable Upload Initiation: POST /upload/drive/v3/files?uploadType=resumable
		if r.Method == http.MethodPost && strings.Contains(r.URL.RawQuery, "uploadType=resumable") {
			id := atomic.AddInt64(&uploadCounter, 1)
			location := fmt.Sprintf("http://%s/upload-session/%d", r.Host, id)
			w.Header().Set("Location", location)
			w.WriteHeader(http.StatusOK)
			return
		}

		// Mock Put Chunk: PUT /upload-session/:id
		if r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/upload-session/") {
			idStr := strings.TrimPrefix(r.URL.Path, "/upload-session/")
			data, err := io.ReadAll(r.Body)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			objID := "gdrive_obj_" + idStr
			storedChunks[objID] = data

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"id":   objID,
				"name": "chunk_" + idStr,
				"size": fmt.Sprintf("%d", len(data)),
			})
			return
		}

		// Mock Download Chunk: GET /files/:id?alt=media
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

		// Mock Delete Chunk: DELETE /files/:id
		if r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/files/") {
			objID := strings.TrimPrefix(r.URL.Path, "/files/")
			delete(storedChunks, objID)
			w.WriteHeader(http.StatusNoContent)
			return
		}

		http.NotFound(w, r)
	}))

	chunkSize := int64(64 * 1024) // 64 KB
	mgr := storage.NewStorageManager(storage.NewMaxAvailableStrategy())
	repo := database.NewMemoryRepository()
	sessionStore := auth.NewSessionStore(24 * time.Hour)
	authService := auth.NewAuthService(repo.Users(), sessionStore)
	authHandler := NewAuthHandler(authService)

	// Configure OAuth pointing to mock endpoints
	customOAuthConf := auth.NewCustomGoogleOAuthConfig(
		"test-client-id",
		"test-client-secret",
		"http://localhost:8080/api/oauth/google/callback",
		[]string{auth.GoogleDriveFileScope},
		oauth2.Endpoint{
			AuthURL:  mockServer.URL + "/oauth/auth",
			TokenURL: mockServer.URL + "/token",
		},
	)

	oauthService := auth.NewOAuthService(customOAuthConf, repo.OAuthTokens())
	oauthHandler := NewOAuthHandler(oauthService, mgr, repo)

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
		OAuthHandler:   oauthHandler,
	})

	return &gdriveTestEnvironment{
		router:         router,
		storageManager: mgr,
		repo:           repo,
		authService:    authService,
		oauthService:   oauthService,
		gdriveServer:   mockServer,
		storedChunks:   storedChunks,
	}
}

// 1. Test OAuth Flow & Google Drive Node Creation
func TestGDrive_OAuthAndNodeCreation(t *testing.T) {
	env := setupGDriveTestEnvironment(t)
	defer env.gdriveServer.Close()

	ctx := context.Background()
	_, session, err := env.authService.Register(ctx, "drivetester@example.com", "password123", "Drive Tester")
	if err != nil {
		t.Fatalf("failed registering user: %v", err)
	}

	// 1. Check Auth URL
	authURLReq := httptest.NewRequest(http.MethodGet, "/api/oauth/google/auth-url", nil)
	authURLRec := httptest.NewRecorder()
	env.router.ServeHTTP(authURLRec, authURLReq)

	if authURLRec.Code != http.StatusOK {
		t.Fatalf("expected 200 for auth-url, got %d", authURLRec.Code)
	}
	var urlResp AuthURLResponse
	_ = json.NewDecoder(authURLRec.Body).Decode(&urlResp)
	if !urlResp.Configured || !strings.Contains(urlResp.AuthURL, "test-client-id") {
		t.Errorf("unexpected auth URL response: %+v", urlResp)
	}

	// 2. Exchange code via callback
	cbPayload, _ := json.Marshal(map[string]string{
		"code": "test_auth_code_xyz",
	})
	cbReq := httptest.NewRequest(http.MethodPost, "/api/oauth/google/callback", bytes.NewReader(cbPayload))
	cbReq.Header.Set("Content-Type", "application/json")
	cbReq.Header.Set("Authorization", "Bearer "+session.Token)
	cbRec := httptest.NewRecorder()
	env.router.ServeHTTP(cbRec, cbReq)

	if cbRec.Code != http.StatusOK {
		t.Fatalf("expected 200 for callback, got %d: %s", cbRec.Code, cbRec.Body.String())
	}

	// 3. Verify OAuth Status
	statusReq := httptest.NewRequest(http.MethodGet, "/api/oauth/google/status", nil)
	statusReq.Header.Set("Authorization", "Bearer "+session.Token)
	statusRec := httptest.NewRecorder()
	env.router.ServeHTTP(statusRec, statusReq)

	if statusRec.Code != http.StatusOK {
		t.Fatalf("expected 200 for status, got %d", statusRec.Code)
	}
	var statusResp map[string]interface{}
	_ = json.NewDecoder(statusRec.Body).Decode(&statusResp)
	if connected, ok := statusResp["connected"].(bool); !ok || !connected {
		t.Errorf("expected connected: true, got %+v", statusResp)
	}

	// 4. Create Google Drive Storage Node using customized provider pointing to mock server
	gdriveProvider, err := providers.NewGoogleDriveProvider(providers.GoogleDriveConfig{
		Client:        env.gdriveServer.Client(),
		APIBaseURL:    env.gdriveServer.URL,
		UploadBaseURL: env.gdriveServer.URL,
	})
	if err != nil {
		t.Fatalf("failed creating drive provider: %v", err)
	}

	gdriveNode, err := storage.NewStorageNode("gdrive_node_primary", "User Primary Google Drive", gdriveProvider)
	if err != nil {
		t.Fatalf("failed creating storage node: %v", err)
	}
	gdriveNode.SetUserID(session.UserID)
	if err := env.storageManager.RegisterNode(gdriveNode); err != nil {
		t.Fatalf("failed registering node: %v", err)
	}

	// Query nodes API
	nodesReq := httptest.NewRequest(http.MethodGet, "/api/nodes", nil)
	nodesReq.Header.Set("Authorization", "Bearer "+session.Token)
	nodesRec := httptest.NewRecorder()
	env.router.ServeHTTP(nodesRec, nodesReq)

	if nodesRec.Code != http.StatusOK {
		t.Fatalf("listing nodes failed: %d", nodesRec.Code)
	}

	var nodes []storage.NodeStats
	_ = json.NewDecoder(nodesRec.Body).Decode(&nodes)
	found := false
	for _, n := range nodes {
		if n.ID == "gdrive_node_primary" && n.ProviderType == "google_drive" {
			found = true
			if n.Status != storage.NodeStatusOnline {
				t.Errorf("expected node ONLINE, got %s", n.Status)
			}
			break
		}
	}
	if !found {
		t.Errorf("expected gdrive_node_primary in nodes response, got: %+v", nodes)
	}
}

// 2. Test End-to-End File Upload, Download, and Delete on Google Drive Storage Provider
func TestGDrive_EndToEndFileLifecycle(t *testing.T) {
	env := setupGDriveTestEnvironment(t)
	defer env.gdriveServer.Close()

	ctx := context.Background()
	_, session, err := env.authService.Register(ctx, "e2e_tester@example.com", "password123", "E2E Tester")
	if err != nil {
		t.Fatalf("failed registering user: %v", err)
	}

	// Register Google Drive Storage Node in the pool
	driveProvider, err := providers.NewGoogleDriveProvider(providers.GoogleDriveConfig{
		Client:        env.gdriveServer.Client(),
		APIBaseURL:    env.gdriveServer.URL,
		UploadBaseURL: env.gdriveServer.URL,
	})
	if err != nil {
		t.Fatalf("failed creating drive provider: %v", err)
	}

	driveNode, err := storage.NewStorageNode("gdrive_node_e2e", "E2E Google Drive", driveProvider)
	if err != nil {
		t.Fatalf("failed creating storage node: %v", err)
	}
	driveNode.SetUserID(session.UserID)
	if err := env.storageManager.RegisterNode(driveNode); err != nil {
		t.Fatalf("failed registering node: %v", err)
	}

	// 1. Upload File (150 KB = 3 chunks: 64KB, 64KB, 22KB)
	fileContent := bytes.Repeat([]byte("CloudDump Google Drive integration test payload! "), 3000)
	bodyBuf := &bytes.Buffer{}
	mpWriter := multipart.NewWriter(bodyBuf)
	part, _ := mpWriter.CreateFormFile("file", "gdrive_test.txt")
	_, _ = part.Write(fileContent)
	_ = mpWriter.Close()

	uploadReq := httptest.NewRequest(http.MethodPost, "/api/files/upload", bodyBuf)
	uploadReq.Header.Set("Content-Type", mpWriter.FormDataContentType())
	uploadReq.Header.Set("X-File-Size", fmt.Sprintf("%d", len(fileContent)))
	uploadReq.Header.Set("Authorization", "Bearer "+session.Token)
	uploadRec := httptest.NewRecorder()
	env.router.ServeHTTP(uploadRec, uploadReq)

	if uploadRec.Code != http.StatusCreated {
		t.Fatalf("upload failed: %d - %s", uploadRec.Code, uploadRec.Body.String())
	}

	var uploadResp struct {
		ID          string `json:"id"`
		TotalChunks int    `json:"total_chunks"`
		Status      string `json:"status"`
	}
	if err := json.NewDecoder(uploadRec.Body).Decode(&uploadResp); err != nil {
		t.Fatalf("failed decoding upload response: %v", err)
	}

	if uploadResp.Status != "COMPLETED" {
		t.Errorf("expected file status COMPLETED, got %s", uploadResp.Status)
	}
	if uploadResp.TotalChunks != 3 {
		t.Errorf("expected 3 total chunks, got %d", uploadResp.TotalChunks)
	}

	// Verify chunks were stored on the mock Google Drive server
	if len(env.storedChunks) < 3 {
		t.Errorf("expected at least 3 chunks stored in Drive, got %d", len(env.storedChunks))
	}

	// 2. Download File and Verify Reconstruction
	dlReq := httptest.NewRequest(http.MethodGet, "/api/files/"+uploadResp.ID+"/download", nil)
	dlReq.Header.Set("Authorization", "Bearer "+session.Token)
	dlRec := httptest.NewRecorder()
	env.router.ServeHTTP(dlRec, dlReq)

	if dlRec.Code != http.StatusOK {
		t.Fatalf("download failed: %d - %s", dlRec.Code, dlRec.Body.String())
	}

	downloadedBytes := dlRec.Body.Bytes()
	if !bytes.Equal(fileContent, downloadedBytes) {
		t.Errorf("reconstructed file content does not match original (len %d vs %d)", len(fileContent), len(downloadedBytes))
	}

	// 3. Delete File and Verify Chunks are deleted from Google Drive
	delReq := httptest.NewRequest(http.MethodDelete, "/api/files/"+uploadResp.ID, nil)
	delReq.Header.Set("Authorization", "Bearer "+session.Token)
	delRec := httptest.NewRecorder()
	env.router.ServeHTTP(delRec, delReq)

	if delRec.Code != http.StatusOK {
		t.Fatalf("delete failed: %d - %s", delRec.Code, delRec.Body.String())
	}

	if len(env.storedChunks) != 0 {
		t.Errorf("expected all chunks deleted from Drive, remaining: %d", len(env.storedChunks))
	}
}

// 3. Test Failure Handling and Replication with Google Drive Node
func TestGDrive_FailureHandlingAndReplication(t *testing.T) {
	env := setupGDriveTestEnvironment(t)
	defer env.gdriveServer.Close()

	ctx := context.Background()
	_, session, _ := env.authService.Register(ctx, "rep_tester@example.com", "password123", "Rep Tester")

	// Create Google Drive Node (Primary)
	driveProvider, _ := providers.NewGoogleDriveProvider(providers.GoogleDriveConfig{
		Client:        env.gdriveServer.Client(),
		APIBaseURL:    env.gdriveServer.URL,
		UploadBaseURL: env.gdriveServer.URL,
	})
	driveNode, _ := storage.NewStorageNode("gdrive_rep_node", "Google Drive Replica Node", driveProvider)
	driveNode.SetUserID(session.UserID)
	_ = env.storageManager.RegisterNode(driveNode)

	// Create Local Node (Secondary for replication)
	localDir, _ := os.MkdirTemp("", "local_rep_*")
	defer os.RemoveAll(localDir)
	localProv, _ := providers.NewLocalStorageProvider(localDir, 50*1024*1024)
	localNode, _ := storage.NewStorageNode("local_rep_node", "Local Replica Node", localProv)
	localNode.SetUserID(session.UserID)
	_ = env.storageManager.RegisterNode(localNode)

	// Upload file with replication factor = 2 (each chunk stored on Drive + Local)
	filePayload := bytes.Repeat([]byte("High availability replicated across Google Drive! "), 1000)
	bodyBuf := &bytes.Buffer{}
	mpWriter := multipart.NewWriter(bodyBuf)
	part, _ := mpWriter.CreateFormFile("file", "replicated_doc.txt")
	_, _ = part.Write(filePayload)
	_ = mpWriter.Close()

	uploadReq := httptest.NewRequest(http.MethodPost, "/api/files/upload", bodyBuf)
	uploadReq.Header.Set("Content-Type", mpWriter.FormDataContentType())
	uploadReq.Header.Set("X-File-Size", fmt.Sprintf("%d", len(filePayload)))
	uploadReq.Header.Set("X-Replication-Factor", "2")
	uploadReq.Header.Set("Authorization", "Bearer "+session.Token)
	uploadRec := httptest.NewRecorder()
	env.router.ServeHTTP(uploadRec, uploadReq)

	if uploadRec.Code != http.StatusCreated {
		t.Fatalf("replicated upload failed: %d - %s", uploadRec.Code, uploadRec.Body.String())
	}

	var uploadResp struct {
		ID string `json:"id"`
	}
	_ = json.NewDecoder(uploadRec.Body).Decode(&uploadResp)

	// Take Google Drive node OFFLINE (simulate cloud provider outage)
	driveProvider.SetSimulatedOffline(true)
	_, _ = driveNode.CheckHealth(ctx)

	// File download MUST still succeed seamlessly by reading from local replica!
	dlReq := httptest.NewRequest(http.MethodGet, "/api/files/"+uploadResp.ID+"/download", nil)
	dlReq.Header.Set("Authorization", "Bearer "+session.Token)
	dlRec := httptest.NewRecorder()
	env.router.ServeHTTP(dlRec, dlReq)

	if dlRec.Code != http.StatusOK {
		t.Fatalf("failover download from replica failed: %d - %s", dlRec.Code, dlRec.Body.String())
	}

	if !bytes.Equal(filePayload, dlRec.Body.Bytes()) {
		t.Errorf("failover reconstructed payload mismatch")
	}

	// Bring Google Drive node back ONLINE
	driveProvider.SetSimulatedOffline(false)
	_, _ = driveNode.CheckHealth(ctx)
}
