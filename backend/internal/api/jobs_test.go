package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/bwithraju/clouddump/backend/internal/auth"
	"github.com/bwithraju/clouddump/backend/internal/database"
	"github.com/bwithraju/clouddump/backend/internal/jobs"
	"github.com/bwithraju/clouddump/backend/internal/storage"
	"github.com/bwithraju/clouddump/backend/internal/uploads"
)

func TestJobsAPI_ListGetAndTrigger(t *testing.T) {
	queue := jobs.NewMemoryJobQueue()
	mgr := storage.NewStorageManager(storage.NewMaxAvailableStrategy())
	repo := database.NewMemoryRepository()
	uploadManager := uploads.NewUploadManager(mgr, repo, 64*1024)
	workerPool := jobs.NewJobWorkerPool(queue, 2)

	jobs.RegisterDefaultHandlers(workerPool, jobs.WorkerDependencies{
		StorageManager: mgr,
		UploadManager:  uploadManager,
		Repo:           repo,
	})
	workerPool.Start(context.Background())
	defer workerPool.Stop()
	defer queue.Close()

	sessionStore := auth.NewSessionStore(24 * time.Hour)
	authService := auth.NewAuthService(repo.Users(), sessionStore)
	authHandler := NewAuthHandler(authService)
	jobsHandler := NewJobsHandler(queue)

	router := NewRouter(RouterConfig{
		AuthHandler: authHandler,
		AuthService: authService,
		JobsHandler: jobsHandler,
	})

	ctx := context.Background()
	_, session, err := authService.Register(ctx, "jobtester@example.com", "password123", "Job Tester")
	if err != nil {
		t.Fatalf("failed registering user: %v", err)
	}

	// 1. Trigger Job via POST /api/jobs/trigger
	triggerPayload, _ := json.Marshal(map[string]interface{}{
		"type": string(jobs.JobTypeNodeHealthCheck),
		"payload": map[string]string{
			"triggered_by": "api_test",
		},
		"max_retries": 3,
	})
	triggerReq := httptest.NewRequest(http.MethodPost, "/api/jobs/trigger", bytes.NewReader(triggerPayload))
	triggerReq.Header.Set("Content-Type", "application/json")
	triggerReq.Header.Set("Authorization", "Bearer "+session.Token)
	triggerRec := httptest.NewRecorder()
	router.ServeHTTP(triggerRec, triggerReq)

	if triggerRec.Code != http.StatusAccepted {
		t.Fatalf("expected 202 Accepted, got %d: %s", triggerRec.Code, triggerRec.Body.String())
	}

	var triggeredJob jobs.Job
	if err := json.NewDecoder(triggerRec.Body).Decode(&triggeredJob); err != nil {
		t.Fatalf("failed decoding triggered job response: %v", err)
	}
	if triggeredJob.ID == "" {
		t.Fatalf("expected non-empty job ID")
	}

	// 2. Poll GET /api/jobs/:id until completed
	deadline := time.Now().Add(5 * time.Second)
	var finalJob *jobs.Job
	for time.Now().Before(deadline) {
		getReq := httptest.NewRequest(http.MethodGet, "/api/jobs/"+triggeredJob.ID, nil)
		getReq.Header.Set("Authorization", "Bearer "+session.Token)
		getRec := httptest.NewRecorder()
		router.ServeHTTP(getRec, getReq)

		if getRec.Code == http.StatusOK {
			var j jobs.Job
			_ = json.NewDecoder(getRec.Body).Decode(&j)
			if j.Status == jobs.JobStatusCompleted {
				finalJob = &j
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
	}

	if finalJob == nil || finalJob.Status != jobs.JobStatusCompleted {
		t.Fatalf("expected job to be COMPLETED via GET /api/jobs/:id, got: %+v", finalJob)
	}

	// 3. List Jobs via GET /api/jobs
	listReq := httptest.NewRequest(http.MethodGet, "/api/jobs?limit=10", nil)
	listReq.Header.Set("Authorization", "Bearer "+session.Token)
	listRec := httptest.NewRecorder()
	router.ServeHTTP(listRec, listReq)

	if listRec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for list jobs, got %d", listRec.Code)
	}

	var jobsList []*jobs.Job
	if err := json.NewDecoder(listRec.Body).Decode(&jobsList); err != nil {
		t.Fatalf("failed decoding job list: %v", err)
	}
	if len(jobsList) == 0 {
		t.Errorf("expected at least 1 job in list, got 0")
	}
}
