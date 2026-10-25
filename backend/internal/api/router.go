package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/bwithraju/clouddump/backend/internal/auth"
)

// HealthResponse represents the health status of the CloudDump server.
type HealthResponse struct {
	Status    string    `json:"status"`
	Service   string    `json:"service"`
	Version   string    `json:"version"`
	Timestamp time.Time `json:"timestamp"`
}

// RouterConfig defines dependency injection parameters for NewRouter.
type RouterConfig struct {
	FileHandler    *FileHandler
	NodesHandler   *NodesHandler
	UploadsHandler *UploadsHandler
	AuthHandler    *AuthHandler
	AuthService    *auth.AuthService
	OAuthHandler   *OAuthHandler
}

// NewRouter sets up and returns the main HTTP handler for the CloudDump API.
func NewRouter(configs ...RouterConfig) http.Handler {
	mux := http.NewServeMux()

	healthHandler := func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		resp := HealthResponse{
			Status:    "healthy",
			Service:   "clouddump-backend",
			Version:   "1.0.0-phase15",
			Timestamp: time.Now().UTC(),
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	}

	// Register health endpoints
	mux.HandleFunc("GET /health", healthHandler)
	mux.HandleFunc("GET /api/health", healthHandler)

	var authService *auth.AuthService

	if len(configs) > 0 {
		cfg := configs[0]
		authService = cfg.AuthService

		// Auto-instantiate AuthHandler if AuthService provided but AuthHandler omitted
		ah := cfg.AuthHandler
		if ah == nil && authService != nil {
			ah = NewAuthHandler(authService)
		}

		// Register Auth endpoints
		if ah != nil {
			mux.HandleFunc("POST /api/auth/register", ah.Register)
			mux.HandleFunc("POST /api/auth/login", ah.Login)
			mux.HandleFunc("POST /api/auth/logout", ah.Logout)
			if authService != nil {
				mux.HandleFunc("GET /api/auth/me", auth.RequireAuth(authService, ah.Me))
			} else {
				mux.HandleFunc("GET /api/auth/me", ah.Me)
			}
		}

		wrapAuth := func(h http.HandlerFunc) http.HandlerFunc {
			if authService != nil {
				return auth.RequireAuth(authService, h)
			}
			return h
		}

		// Register file endpoints if FileHandler is provided
		if cfg.FileHandler != nil {
			fh := cfg.FileHandler
			mux.HandleFunc("POST /api/files/upload", wrapAuth(fh.Upload))
			mux.HandleFunc("GET /api/files/upload", func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			})
			mux.HandleFunc("GET /api/files", wrapAuth(fh.List))
			mux.HandleFunc("GET /api/files/{id}", wrapAuth(fh.GetDetails))
			mux.HandleFunc("GET /api/files/{id}/status", wrapAuth(fh.GetStatus))
			mux.HandleFunc("GET /api/files/{id}/download", wrapAuth(fh.Download))
			mux.HandleFunc("DELETE /api/files/{id}", wrapAuth(fh.Delete))
		}

		// Register node endpoints if NodesHandler is provided
		if cfg.NodesHandler != nil {
			nh := cfg.NodesHandler
			mux.HandleFunc("GET /api/nodes", wrapAuth(nh.List))
			mux.HandleFunc("GET /api/nodes/stats", wrapAuth(nh.Stats))
			mux.HandleFunc("POST /api/nodes/{id}/simulate-failure", wrapAuth(nh.SimulateFailure))
			mux.HandleFunc("POST /api/nodes/reconcile", wrapAuth(nh.Reconcile))
		}

		// Register resumable upload endpoints if UploadsHandler is provided
		if cfg.UploadsHandler != nil {
			uh := cfg.UploadsHandler
			mux.HandleFunc("POST /api/uploads", wrapAuth(uh.InitSession))
			mux.HandleFunc("GET /api/uploads", wrapAuth(uh.ListSessions))
			mux.HandleFunc("GET /api/uploads/{id}", wrapAuth(uh.GetSession))
			mux.HandleFunc("PUT /api/uploads/{id}/chunks/{index}", wrapAuth(uh.UploadChunk))
			mux.HandleFunc("POST /api/uploads/{id}/chunks/{index}", wrapAuth(uh.UploadChunk))
			mux.HandleFunc("DELETE /api/uploads/{id}", wrapAuth(uh.CancelSession))
			mux.HandleFunc("POST /api/uploads/{id}/cancel", wrapAuth(uh.CancelSession))
		}

		// Register OAuth and Google Drive endpoints if OAuthHandler is provided
		if cfg.OAuthHandler != nil {
			oh := cfg.OAuthHandler
			mux.HandleFunc("GET /api/oauth/google/auth-url", oh.GetAuthURL)
			mux.HandleFunc("POST /api/oauth/google/callback", wrapAuth(oh.Callback))
			mux.HandleFunc("GET /api/oauth/google/status", wrapAuth(oh.Status))
			mux.HandleFunc("POST /api/oauth/google/disconnect", wrapAuth(oh.Disconnect))
			mux.HandleFunc("POST /api/nodes/google-drive", wrapAuth(oh.CreateDriveNode))
		}
	}

	var handler http.Handler = mux
	if authService != nil {
		handler = auth.AuthenticateMiddleware(authService)(handler)
	}

	return withCORS(handler)
}

// withCORS adds CORS headers for frontend integration with support for credentials.
func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
		} else {
			w.Header().Set("Access-Control-Allow-Origin", "*")
		}
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Filename, X-File-Size, X-User-ID, X-Replication-Factor, Cookie")
		w.Header().Set("Access-Control-Expose-Headers", "Content-Disposition, Content-Length, Set-Cookie")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}
