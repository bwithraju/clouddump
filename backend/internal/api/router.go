package api

import (
	"encoding/json"
	"net/http"
	"time"
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
	FileHandler  *FileHandler
	NodesHandler *NodesHandler
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
			Version:   "1.0.0-phase10",
			Timestamp: time.Now().UTC(),
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	}

	// Register health endpoints
	mux.HandleFunc("GET /health", healthHandler)
	mux.HandleFunc("GET /api/health", healthHandler)

	if len(configs) > 0 {
		cfg := configs[0]

		// Register file endpoints if FileHandler is provided
		if cfg.FileHandler != nil {
			fh := cfg.FileHandler
			mux.HandleFunc("POST /api/files/upload", fh.Upload)
			mux.HandleFunc("GET /api/files/upload", func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			})
			mux.HandleFunc("GET /api/files", fh.List)
			mux.HandleFunc("GET /api/files/{id}", fh.GetDetails)
			mux.HandleFunc("GET /api/files/{id}/status", fh.GetStatus)
			mux.HandleFunc("GET /api/files/{id}/download", fh.Download)
			mux.HandleFunc("DELETE /api/files/{id}", fh.Delete)
		}

		// Register node endpoints if NodesHandler is provided
		if cfg.NodesHandler != nil {
			nh := cfg.NodesHandler
			mux.HandleFunc("GET /api/nodes", nh.List)
			mux.HandleFunc("GET /api/nodes/stats", nh.Stats)
		}
	}

	return withCORS(mux)
}

// withCORS adds CORS headers for frontend integration.
func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Filename, X-File-Size, X-User-ID")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}
