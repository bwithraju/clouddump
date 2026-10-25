package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/bwithraju/clouddump/backend/internal/auth"
	"github.com/bwithraju/clouddump/backend/internal/database"
	"github.com/bwithraju/clouddump/backend/internal/providers"
	"github.com/bwithraju/clouddump/backend/internal/storage"
)

// OAuthHandler coordinates OAuth 2.0 flows and Google Drive storage node creation.
type OAuthHandler struct {
	oauthService   *auth.OAuthService
	storageManager *storage.StorageManager
	repo           database.Repository
}

// NewOAuthHandler constructs an OAuthHandler.
func NewOAuthHandler(
	oauthService *auth.OAuthService,
	mgr *storage.StorageManager,
	repo database.Repository,
) *OAuthHandler {
	return &OAuthHandler{
		oauthService:   oauthService,
		storageManager: mgr,
		repo:           repo,
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

	state := r.URL.Query().Get("state")
	if state == "" {
		state = fmt.Sprintf("state_%d", time.Now().UnixNano())
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

// Callback handles POST /api/oauth/google/callback.
// Exchanges the authorization code for tokens and saves them securely in the token store.
func (h *OAuthHandler) Callback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

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
