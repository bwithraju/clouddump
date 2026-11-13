package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/bwithraju/clouddump/backend/internal/auth"
	"github.com/bwithraju/clouddump/backend/internal/chunks"
	"github.com/bwithraju/clouddump/backend/internal/database"
	"github.com/bwithraju/clouddump/backend/internal/providers"
	"github.com/bwithraju/clouddump/backend/internal/storage"
)

// OAuthHandler coordinates OAuth 2.0 flows, Google Drive storage node creation,
// and Google Drive file browsing / importing into the distributed storage pool.
type OAuthHandler struct {
	oauthService     *auth.OAuthService
	storageManager   *storage.StorageManager
	repo             database.Repository
	chunkDistributor ChunkDistributorInterface
	defaultChunkSize int64
}

// NewOAuthHandler constructs an OAuthHandler.
func NewOAuthHandler(
	oauthService *auth.OAuthService,
	mgr *storage.StorageManager,
	repo database.Repository,
	distributor ...ChunkDistributorInterface,
) *OAuthHandler {
	var d ChunkDistributorInterface
	if len(distributor) > 0 && distributor[0] != nil {
		d = distributor[0]
	} else if mgr != nil {
		d, _ = chunks.NewChunkDistributor(mgr, chunks.DefaultChunkSize)
	}

	return &OAuthHandler{
		oauthService:     oauthService,
		storageManager:   mgr,
		repo:             repo,
		chunkDistributor: d,
		defaultChunkSize: chunks.DefaultChunkSize,
	}
}

// AuthURLResponse payload for GET /api/oauth/google/auth-url.
type AuthURLResponse struct {
	AuthURL    string `json:"auth_url"`
	Configured bool   `json:"configured"`
}

// GetAuthURL handles GET /api/oauth/google/auth-url.
// Credentials remain strictly on the backend.
func (h *OAuthHandler) GetAuthURL(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	conf := h.oauthService.GetGoogleConfig()
	if !conf.IsConfigured() {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(AuthURLResponse{
			AuthURL:    "",
			Configured: false,
		})
		return
	}

	userID := auth.GetEffectiveUserID(r)
	if userID == "" {
		userID = "default_user"
	}

	state := r.URL.Query().Get("state")
	if state == "" {
		state = fmt.Sprintf("%s:%d", userID, time.Now().UnixNano())
	}

	authURL := conf.AuthCodeURL(state)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(AuthURLResponse{
		AuthURL:    authURL,
		Configured: true,
	})
}

// CallbackRequest payload for POST /api/oauth/google/callback.
type CallbackRequest struct {
	Code  string `json:"code"`
	State string `json:"state"`
}

// Callback handles both:
// 1. GET /api/oauth/google/callback (Browser OAuth 2.0 redirect from Google)
// 2. POST /api/oauth/google/callback (Programmatic code exchange with JSON payload)
func (h *OAuthHandler) Callback(w http.ResponseWriter, r *http.Request) {
	frontendURL := os.Getenv("FRONTEND_URL")
	if frontendURL == "" {
		frontendURL = "http://localhost:3000"
	}
	frontendURL = strings.TrimRight(frontendURL, "/")

	// Case 1: Browser GET Redirect from Google
	if r.Method == http.MethodGet {
		errParam := r.URL.Query().Get("error")
		if errParam != "" {
			http.Redirect(w, r, fmt.Sprintf("%s/nodes?gdrive_error=%s", frontendURL, url.QueryEscape(errParam)), http.StatusTemporaryRedirect)
			return
		}

		code := r.URL.Query().Get("code")
		if code == "" {
			http.Redirect(w, r, fmt.Sprintf("%s/nodes?gdrive_error=missing_authorization_code", frontendURL), http.StatusTemporaryRedirect)
			return
		}

		state := r.URL.Query().Get("state")
		userID := auth.GetEffectiveUserID(r)
		if userID == "" && state != "" {
			parts := strings.Split(state, ":")
			if len(parts) > 0 && parts[0] != "" {
				userID = parts[0]
			}
		}
		if userID == "" {
			userID = "default_user"
		}

		tok, err := h.oauthService.GetGoogleConfig().ExchangeCode(r.Context(), code)
		if err != nil {
			http.Redirect(w, r, fmt.Sprintf("%s/nodes?gdrive_error=%s", frontendURL, url.QueryEscape(err.Error())), http.StatusTemporaryRedirect)
			return
		}

		if err := h.oauthService.SaveUserToken(r.Context(), userID, auth.ProviderGoogleDrive, tok); err != nil {
			http.Redirect(w, r, fmt.Sprintf("%s/nodes?gdrive_error=%s", frontendURL, url.QueryEscape(err.Error())), http.StatusTemporaryRedirect)
			return
		}

		// Auto-register a default Google Drive storage node if none exists yet for this user
		nodeID := fmt.Sprintf("gdrive_%s", sanitizeID(userID))
		if existing, err := h.storageManager.GetNode(nodeID); err != nil || existing == nil {
			if client, err := h.oauthService.GetAuthenticatedClient(r.Context(), userID, auth.ProviderGoogleDrive); err == nil {
				if driveProvider, err := providers.NewGoogleDriveProvider(providers.GoogleDriveConfig{Client: client}); err == nil {
					if node, err := storage.NewStorageNode(nodeID, "Google Drive Node", driveProvider); err == nil {
						node.SetUserID(userID)
						_ = h.storageManager.RegisterNode(node)
						totCap, _ := driveProvider.GetTotalCapacity(r.Context())
						availCap, _ := driveProvider.GetAvailableCapacity(r.Context())
						now := time.Now().UTC()
						_ = h.repo.Nodes().CreateNode(r.Context(), &database.StorageNodeRecord{
							ID:                node.ID,
							UserID:            userID,
							Provider:          "google_drive",
							Name:              node.Name,
							TotalCapacity:     totCap,
							UsedCapacity:      totCap - availCap,
							AvailableCapacity: availCap,
							Status:            string(storage.NodeStatusOnline),
							CreatedAt:         now,
							UpdatedAt:         now,
						})
					}
				}
			}
		}

		http.Redirect(w, r, fmt.Sprintf("%s/nodes?gdrive=connected", frontendURL), http.StatusTemporaryRedirect)
		return
	}

	// Case 2: Programmatic POST with JSON Body
	if r.Method == http.MethodPost {
		userID := auth.GetEffectiveUserID(r)
		if userID == "" {
			userID = "default_user"
		}

		var req CallbackRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
			return
		}

		if req.Code == "" {
			http.Error(w, "authorization code is required", http.StatusBadRequest)
			return
		}

		tok, err := h.oauthService.GetGoogleConfig().ExchangeCode(r.Context(), req.Code)
		if err != nil {
			http.Error(w, fmt.Sprintf("token exchange failed: %v", err), http.StatusBadGateway)
			return
		}

		if err := h.oauthService.SaveUserToken(r.Context(), userID, auth.ProviderGoogleDrive, tok); err != nil {
			http.Error(w, fmt.Sprintf("failed to save token: %v", err), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status":     "CONNECTED",
			"provider":   auth.ProviderGoogleDrive,
			"expires_at": tok.Expiry,
		})
		return
	}

	http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
}

// Status handles GET /api/oauth/google/status.
func (h *OAuthHandler) Status(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID := auth.GetEffectiveUserID(r)
	if userID == "" {
		userID = "default_user"
	}

	tok, err := h.oauthService.GetUserToken(r.Context(), userID, auth.ProviderGoogleDrive)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"connected": false,
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"connected":  true,
		"provider":   auth.ProviderGoogleDrive,
		"expires_at": tok.Expiry,
	})
}

// Disconnect handles POST /api/oauth/google/disconnect.
func (h *OAuthHandler) Disconnect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID := auth.GetEffectiveUserID(r)
	if userID == "" {
		userID = "default_user"
	}

	_ = h.oauthService.DeleteUserToken(r.Context(), userID, auth.ProviderGoogleDrive)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status":  "DISCONNECTED",
		"message": "Google Drive disconnected successfully",
	})
}

// CreateDriveNodeRequest payload for POST /api/nodes/google-drive.
type CreateDriveNodeRequest struct {
	NodeID   string `json:"node_id"`
	Name     string `json:"name"`
	FolderID string `json:"folder_id"`
}

// CreateDriveNode handles POST /api/nodes/google-drive.
// Instantiates a GoogleDriveProvider using the user's stored OAuth credentials,
// wraps it in a StorageNode, registers it with StorageManager and metadata repository.
func (h *OAuthHandler) CreateDriveNode(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx := r.Context()
	userID := auth.GetEffectiveUserID(r)
	if userID == "" {
		userID = "default_user"
	}

	var req CreateDriveNodeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
		return
	}

	if req.Name == "" {
		req.Name = "Google Drive Storage Node"
	}
	if req.NodeID == "" {
		req.NodeID = fmt.Sprintf("gdrive_%d", time.Now().UnixNano())
	}

	// 1. Retrieve user's authenticated HTTP client
	client, err := h.oauthService.GetAuthenticatedClient(ctx, userID, auth.ProviderGoogleDrive)
	if err != nil {
		if errors.Is(err, auth.ErrOAuthTokenNotFound) {
			http.Error(w, "Google Drive account is not authorized. Link account first via OAuth.", http.StatusBadRequest)
			return
		}
		http.Error(w, fmt.Sprintf("failed getting authenticated client: %v", err), http.StatusInternalServerError)
		return
	}

	// 2. Instantiate GoogleDriveProvider
	driveProvider, err := providers.NewGoogleDriveProvider(providers.GoogleDriveConfig{
		Client:   client,
		FolderID: req.FolderID,
	})
	if err != nil {
		http.Error(w, fmt.Sprintf("failed creating drive provider: %v", err), http.StatusInternalServerError)
		return
	}

	// 3. Probe provider via HealthCheck & query capacity
	healthy, err := driveProvider.HealthCheck(ctx)
	if err != nil || !healthy {
		http.Error(w, fmt.Sprintf("Google Drive health check failed: %v", err), http.StatusBadGateway)
		return
	}

	totalCap, _ := driveProvider.GetTotalCapacity(ctx)
	availCap, _ := driveProvider.GetAvailableCapacity(ctx)

	// 4. Create and Register StorageNode
	node, err := storage.NewStorageNode(req.NodeID, req.Name, driveProvider)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed creating storage node: %v", err), http.StatusInternalServerError)
		return
	}
	node.SetUserID(userID)

	if err := h.storageManager.RegisterNode(node); err != nil {
		if errors.Is(err, storage.ErrDuplicateNodeID) {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		http.Error(w, fmt.Sprintf("failed registering node: %v", err), http.StatusInternalServerError)
		return
	}

	// 5. Persist node in Database
	now := time.Now().UTC()
	nodeRecord := &database.StorageNodeRecord{
		ID:                node.ID,
		UserID:            userID,
		Provider:          "google_drive",
		Name:              node.Name,
		TotalCapacity:     totalCap,
		UsedCapacity:      totalCap - availCap,
		AvailableCapacity: availCap,
		Status:            string(storage.NodeStatusOnline),
		CreatedAt:         now,
		UpdatedAt:         now,
	}

	if err := h.repo.Nodes().CreateNode(ctx, nodeRecord); err != nil {
		// Log warning but don't fail since node is live in memory pool
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(node.GetStats())
}

var (
	defaultDriveAPIBase = "https://www.googleapis.com/drive/v3"
)

// SetDriveAPIBaseURLForTest allows tests to point Google Drive REST calls to a mock server.
func SetDriveAPIBaseURLForTest(apiBase string) {
	defaultDriveAPIBase = strings.TrimRight(apiBase, "/")
}

// DriveFileItem represents file metadata retrieved from Google Drive v3 API.
type DriveFileItem struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	MimeType      string `json:"mime_type"`
	Size          int64  `json:"size"`
	ModifiedTime  string `json:"modified_time"`
	IconLink      string `json:"icon_link,omitempty"`
	ThumbnailLink string `json:"thumbnail_link,omitempty"`
	IsFolder      bool   `json:"is_folder"`
}

// ListDriveFilesResponse is the payload returned by GET /api/oauth/google/files.
type ListDriveFilesResponse struct {
	Files         []DriveFileItem `json:"files"`
	NextPageToken string          `json:"next_page_token,omitempty"`
}

// ListDriveFiles handles GET /api/oauth/google/files.
// Queries Google Drive v3 REST API to list the authenticated user's files.
func (h *OAuthHandler) ListDriveFiles(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx := r.Context()
	userID := auth.GetEffectiveUserID(r)
	if userID == "" {
		userID = "default_user"
	}

	client, err := h.oauthService.GetAuthenticatedClient(ctx, userID, auth.ProviderGoogleDrive)
	if err != nil {
		if errors.Is(err, auth.ErrOAuthTokenNotFound) {
			http.Error(w, "Google Drive account is not authorized. Please link account via OAuth first.", http.StatusUnauthorized)
			return
		}
		http.Error(w, fmt.Sprintf("failed getting authenticated client: %v", err), http.StatusInternalServerError)
		return
	}

	// Filter out trashed items; if search term given, match on name
	queryParts := []string{"trashed = false"}
	searchQuery := r.URL.Query().Get("q")
	if searchQuery != "" {
		escaped := strings.ReplaceAll(searchQuery, "'", "\\'")
		queryParts = append(queryParts, fmt.Sprintf("name contains '%s'", escaped))
	}

	q := strings.Join(queryParts, " and ")
	reqURL, _ := url.Parse(fmt.Sprintf("%s/files", defaultDriveAPIBase))
	qVals := reqURL.Query()
	qVals.Set("q", q)
	qVals.Set("pageSize", "50")
	qVals.Set("fields", "nextPageToken,files(id,name,mimeType,size,modifiedTime,iconLink,thumbnailLink)")
	qVals.Set("orderBy", "modifiedTime desc")
	if pt := r.URL.Query().Get("pageToken"); pt != "" {
		qVals.Set("pageToken", pt)
	}
	reqURL.RawQuery = qVals.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL.String(), nil)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed building request: %v", err), http.StatusInternalServerError)
		return
	}

	resp, err := client.Do(req)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed contacting Google Drive API: %v", err), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		http.Error(w, fmt.Sprintf("Google Drive API error (%d): %s", resp.StatusCode, string(body)), resp.StatusCode)
		return
	}

	var driveResult struct {
		NextPageToken string `json:"nextPageToken"`
		Files         []struct {
			ID            string `json:"id"`
			Name          string `json:"name"`
			MimeType      string `json:"mimeType"`
			Size          string `json:"size"`
			ModifiedTime  string `json:"modifiedTime"`
			IconLink      string `json:"iconLink"`
			ThumbnailLink string `json:"thumbnailLink"`
		} `json:"files"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&driveResult); err != nil {
		http.Error(w, fmt.Sprintf("failed decoding drive response: %v", err), http.StatusInternalServerError)
		return
	}

	items := make([]DriveFileItem, 0, len(driveResult.Files))
	for _, f := range driveResult.Files {
		var sz int64
		if f.Size != "" {
			sz, _ = strconv.ParseInt(f.Size, 10, 64)
		}
		isFolder := f.MimeType == "application/vnd.google-apps.folder"
		items = append(items, DriveFileItem{
			ID:            f.ID,
			Name:          f.Name,
			MimeType:      f.MimeType,
			Size:          sz,
			ModifiedTime:  f.ModifiedTime,
			IconLink:      f.IconLink,
			ThumbnailLink: f.ThumbnailLink,
			IsFolder:      isFolder,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(ListDriveFilesResponse{
		Files:         items,
		NextPageToken: driveResult.NextPageToken,
	})
}

// ImportDriveFileRequest payload for POST /api/oauth/google/import.
type ImportDriveFileRequest struct {
	FileID            string `json:"file_id"`
	Filename          string `json:"filename,omitempty"`
	ReplicationFactor int    `json:"replication_factor,omitempty"`
}

// ImportDriveFile handles POST /api/oauth/google/import.
// Streams a file directly from Google Drive v3, slices it into chunks, computes SHA-256 hashes,
// distributes chunks across the storage node pool, records chunk & file metadata, and marks the file COMPLETED.
func (h *OAuthHandler) ImportDriveFile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx := r.Context()
	userID := auth.GetEffectiveUserID(r)
	if userID == "" {
		userID = "default_user"
	}

	var req ImportDriveFileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
		return
	}

	if req.FileID == "" {
		http.Error(w, "file_id is required", http.StatusBadRequest)
		return
	}

	repFactor := req.ReplicationFactor
	if repFactor < 1 {
		repFactor = 1
	}

	client, err := h.oauthService.GetAuthenticatedClient(ctx, userID, auth.ProviderGoogleDrive)
	if err != nil {
		if errors.Is(err, auth.ErrOAuthTokenNotFound) {
			http.Error(w, "Google Drive account is not authorized. Link account via OAuth first.", http.StatusUnauthorized)
			return
		}
		http.Error(w, fmt.Sprintf("failed getting authenticated client: %v", err), http.StatusInternalServerError)
		return
	}

	// 1. Fetch file metadata from Google Drive v3
	metaURL := fmt.Sprintf("%s/files/%s?fields=id,name,size,mimeType", defaultDriveAPIBase, url.PathEscape(req.FileID))
	metaReq, err := http.NewRequestWithContext(ctx, http.MethodGet, metaURL, nil)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed creating metadata request: %v", err), http.StatusInternalServerError)
		return
	}

	metaResp, err := client.Do(metaReq)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed querying file metadata: %v", err), http.StatusBadGateway)
		return
	}
	defer metaResp.Body.Close()

	if metaResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(metaResp.Body)
		http.Error(w, fmt.Sprintf("file metadata query failed (%d): %s", metaResp.StatusCode, string(body)), metaResp.StatusCode)
		return
	}

	var meta struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		MimeType string `json:"mimeType"`
		Size     string `json:"size"`
	}
	if err := json.NewDecoder(metaResp.Body).Decode(&meta); err != nil {
		http.Error(w, fmt.Sprintf("failed decoding metadata: %v", err), http.StatusInternalServerError)
		return
	}

	filename := req.Filename
	if filename == "" {
		filename = meta.Name
	}
	filename = filepath.Base(filename)
	mimeType := meta.MimeType

	// 2. Open stream for file content (handling Google Docs/Sheets export to PDF if needed)
	var downloadURL string
	isGoogleDoc := strings.HasPrefix(mimeType, "application/vnd.google-apps.")
	if isGoogleDoc {
		downloadURL = fmt.Sprintf("%s/files/%s/export?mimeType=application/pdf", defaultDriveAPIBase, url.PathEscape(req.FileID))
		mimeType = "application/pdf"
		if !strings.HasSuffix(strings.ToLower(filename), ".pdf") {
			filename += ".pdf"
		}
	} else {
		downloadURL = fmt.Sprintf("%s/files/%s?alt=media", defaultDriveAPIBase, url.PathEscape(req.FileID))
	}

	downReq, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed creating download request: %v", err), http.StatusInternalServerError)
		return
	}

	downResp, err := client.Do(downReq)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed streaming file from Google Drive: %v", err), http.StatusBadGateway)
		return
	}
	defer downResp.Body.Close()

	if downResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(downResp.Body)
		http.Error(w, fmt.Sprintf("file download failed (%d): %s", downResp.StatusCode, string(body)), downResp.StatusCode)
		return
	}

	var fileSize int64
	if downResp.ContentLength > 0 {
		fileSize = downResp.ContentLength
	} else if meta.Size != "" {
		fileSize, _ = strconv.ParseInt(meta.Size, 10, 64)
	}

	var fileStream io.Reader = downResp.Body
	if fileSize <= 0 {
		buf, err := io.ReadAll(downResp.Body)
		if err != nil {
			http.Error(w, fmt.Sprintf("failed reading exported file stream: %v", err), http.StatusInternalServerError)
			return
		}
		fileSize = int64(len(buf))
		fileStream = bytes.NewReader(buf)
	}

	// 3. Verify storage pool has enough capacity for file + replicas
	poolStats := h.storageManager.GetPoolStats(ctx)
	if poolStats.AvailableCapacity < fileSize*int64(repFactor) {
		http.Error(w, fmt.Sprintf("insufficient storage pool capacity: required %d bytes, available %d bytes", fileSize*int64(repFactor), poolStats.AvailableCapacity), http.StatusInsufficientStorage)
		return
	}

	// 4. Create initial file record in UPLOADING status
	chunkSize := h.defaultChunkSize
	if chunkSize <= 0 {
		chunkSize = chunks.DefaultChunkSize
	}
	totalChunks, err := chunks.CalculateTotalChunks(fileSize, chunkSize)
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid chunk calculation: %v", err), http.StatusBadRequest)
		return
	}

	fileID := fmt.Sprintf("file_%d_%s", time.Now().UnixNano(), sanitizeID(filename))
	fileRecord := &database.FileRecord{
		ID:          fileID,
		UserID:      userID,
		Filename:    filename,
		Size:        fileSize,
		MimeType:    mimeType,
		ChunkSize:   chunkSize,
		TotalChunks: totalChunks,
		Status:      string(chunks.FileStatusUploading),
	}

	if err := h.repo.Files().CreateFile(ctx, fileRecord); err != nil {
		http.Error(w, fmt.Sprintf("failed creating file record: %v", err), http.StatusInternalServerError)
		return
	}

	// 5. Stream and distribute chunks across storage nodes with replication
	result, err := h.chunkDistributor.DistributeWithReplication(ctx, fileID, fileStream, fileSize, repFactor, nil)
	if err != nil {
		_ = h.repo.Files().UpdateFileStatus(context.Background(), fileID, string(chunks.FileStatusFailed))
		if errors.Is(err, chunks.ErrInsufficientPoolStorage) || errors.Is(err, storage.ErrInsufficientNodeCapacity) {
			http.Error(w, fmt.Sprintf("insufficient storage pool capacity: %v", err), http.StatusInsufficientStorage)
			return
		}
		http.Error(w, fmt.Sprintf("file distribution failed: %v", err), http.StatusInternalServerError)
		return
	}

	// 6. Batch persist chunk metadata into database including replicas
	chunkRecords := make([]*database.ChunkRecord, 0, len(result.Chunks))
	for _, chk := range result.Chunks {
		var reps []database.ChunkReplicaRecord
		for _, r := range chk.Replicas {
			reps = append(reps, database.ChunkReplicaRecord{
				StorageNodeID:    r.StorageNodeID,
				ProviderObjectID: r.ProviderObjectID,
				Status:           string(r.Status),
			})
		}
		chunkRecords = append(chunkRecords, &database.ChunkRecord{
			ID:               chk.ID,
			FileID:           fileID,
			ChunkIndex:       chk.ChunkIndex,
			Size:             chk.Size,
			Checksum:         chk.Checksum,
			StorageNodeID:    chk.StorageNodeID,
			ProviderObjectID: chk.ProviderObjectID,
			Status:           string(chk.Status),
			Replicas:         reps,
			CreatedAt:        chk.CreatedAt,
		})
	}

	if err := h.repo.Chunks().CreateChunksBatch(ctx, chunkRecords); err != nil {
		_ = h.repo.Files().UpdateFileStatus(context.Background(), fileID, string(chunks.FileStatusFailed))
		http.Error(w, fmt.Sprintf("failed persisting chunk metadata: %v", err), http.StatusInternalServerError)
		return
	}

	// 7. Mark file COMPLETED
	if err := h.repo.Files().UpdateFileStatus(ctx, fileID, string(chunks.FileStatusCompleted)); err != nil {
		http.Error(w, fmt.Sprintf("failed finalizing file status: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"id":                 fileID,
		"filename":           filename,
		"size":               fileSize,
		"mime_type":          mimeType,
		"chunk_size":         chunkSize,
		"total_chunks":       totalChunks,
		"replication_factor": result.ReplicationFactor,
		"status":             string(chunks.FileStatusCompleted),
		"source":             "google_drive",
		"google_drive_id":    req.FileID,
		"distribution":       result.NodeDistribution,
	})
}
