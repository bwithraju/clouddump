package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/bwithraju/clouddump/backend/internal/api"
	"github.com/bwithraju/clouddump/backend/internal/chunks"
	"github.com/bwithraju/clouddump/backend/internal/database"
	"github.com/bwithraju/clouddump/backend/internal/providers"
	"github.com/bwithraju/clouddump/backend/internal/storage"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	// 1. Determine storage root
	storageBase := os.Getenv("STORAGE_ROOT")
	if storageBase == "" {
		if _, err := os.Stat("storage"); err == nil {
			storageBase = "storage"
		} else if _, err := os.Stat("../storage"); err == nil {
			storageBase = "../storage"
		} else {
			storageBase = filepath.Join(os.TempDir(), "clouddump_storage")
		}
	}

	// 2. Initialize Storage Manager & Register Nodes
	mgr := storage.NewStorageManager(storage.NewMaxAvailableStrategy())

	nodeConfigs := []struct {
		id       string
		name     string
		subDir   string
		capacity int64
	}{
		{"node1", "Local Storage Node 1", "node1", 500 * 1024 * 1024},
		{"node2", "Local Storage Node 2", "node2", 1024 * 1024 * 1024},
		{"node3", "Local Storage Node 3", "node3", 2048 * 1024 * 1024},
		{"node4", "Local Storage Node 4", "node4", 700 * 1024 * 1024},
	}

	for _, cfg := range nodeConfigs {
		dir := filepath.Join(storageBase, cfg.subDir)
		_ = os.MkdirAll(dir, 0755)
		provider, err := providers.NewLocalStorageProvider(dir, cfg.capacity)
		if err != nil {
			log.Fatalf("Failed to initialize provider for %s: %v", cfg.id, err)
		}
		node, err := storage.NewStorageNode(cfg.id, cfg.name, provider)
		if err != nil {
			log.Fatalf("Failed to create storage node %s: %v", cfg.id, err)
		}
		if err := mgr.RegisterNode(node); err != nil {
			log.Fatalf("Failed to register storage node %s: %v", cfg.id, err)
		}
	}

	// 3. Initialize Database Repository (PostgreSQL if DATABASE_URL is set, else in-memory)
	var repo database.Repository
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL != "" {
		db, err := database.ConnectURL(dbURL)
		if err != nil {
			log.Printf("[Warning] Failed to connect to PostgreSQL at %s (%v). Falling back to in-memory metadata repository.", dbURL, err)
			repo = database.NewMemoryRepository()
		} else {
			log.Println("Connected to PostgreSQL metadata repository.")
			_ = database.Migrate(context.Background(), db)
			repo = database.NewPostgresRepository(db)
		}
	} else {
		log.Println("DATABASE_URL not set; using fast in-memory metadata repository.")
		repo = database.NewMemoryRepository()
	}

	// 4. Initialize Core Pipeline Components
	chunkDistributor, err := chunks.NewChunkDistributor(mgr, chunks.DefaultChunkSize)
	if err != nil {
		log.Fatalf("Failed to initialize chunk distributor: %v", err)
	}

	fileReconstructor, err := chunks.NewFileReconstructor(mgr)
	if err != nil {
		log.Fatalf("Failed to initialize file reconstructor: %v", err)
	}

	// 5. Construct Handlers and Router
	fileHandler := api.NewFileHandler(mgr, chunkDistributor, fileReconstructor, repo, chunks.DefaultChunkSize)
	nodesHandler := api.NewNodesHandler(mgr)

	routerConfig := api.RouterConfig{
		FileHandler:  fileHandler,
		NodesHandler: nodesHandler,
	}

	handler := api.NewRouter(routerConfig)

	addr := fmt.Sprintf(":%s", port)
	srv := &http.Server{
		Addr:         addr,
		Handler:      handler,
		ReadTimeout:  60 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// Start server in background goroutine
	go func() {
		log.Printf("CloudDump Backend Server starting on http://localhost:%s...", port)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("Server listen failed: %v\n", err)
		}
	}()

	// Wait for interrupt signal for graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down CloudDump server gracefully...")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("Server forced to shutdown: %v\n", err)
	}

	log.Println("CloudDump server exited successfully.")
}
