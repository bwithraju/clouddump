package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/bwithraju/clouddump/backend/internal/jobs"
)

// JobsHandler serves endpoints for querying job statuses and triggering background processing.
type JobsHandler struct {
	queue jobs.JobQueue
}

// NewJobsHandler constructs a JobsHandler.
func NewJobsHandler(queue jobs.JobQueue) *JobsHandler {
	return &JobsHandler{
		queue: queue,
	}
}

// List handles GET /api/jobs - returns recent background jobs.
func (h *JobsHandler) List(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	limit := 50
	if lStr := r.URL.Query().Get("limit"); lStr != "" {
		if l, err := strconv.Atoi(lStr); err == nil && l > 0 {
			limit = l
		}
	}

	jobList, err := h.queue.ListJobs(r.Context(), limit)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed listing jobs: %v", err), http.StatusInternalServerError)
		return
	}

	if jobList == nil {
		jobList = []*jobs.Job{}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(jobList)
}

// Get handles GET /api/jobs/{id} - returns job details and status.
func (h *JobsHandler) Get(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	jobID := r.PathValue("id")
	if jobID == "" {
		http.Error(w, "job id required", http.StatusBadRequest)
		return
	}

	job, err := h.queue.GetJob(r.Context(), jobID)
	if err != nil {
		if errors.Is(err, jobs.ErrJobNotFound) {
			http.Error(w, "job not found", http.StatusNotFound)
			return
		}
		http.Error(w, fmt.Sprintf("failed retrieving job: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(job)
}

// TriggerJobRequest payload for POST /api/jobs/trigger.
type TriggerJobRequest struct {
	Type       jobs.JobType    `json:"type"`
	Payload    json.RawMessage `json:"payload,omitempty"`
	MaxRetries int             `json:"max_retries,omitempty"`
}

// Trigger handles POST /api/jobs/trigger - manually enqueues a background job.
func (h *JobsHandler) Trigger(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req TriggerJobRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
		return
	}

	if req.Type == "" {
		http.Error(w, "job type is required", http.StatusBadRequest)
		return
	}

	var payload interface{} = req.Payload
	if len(req.Payload) == 0 {
		payload = map[string]string{"triggered_by": "api"}
	}

	job, err := jobs.NewJob(req.Type, payload, req.MaxRetries)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed creating job: %v", err), http.StatusBadRequest)
		return
	}

	if err := h.queue.Enqueue(r.Context(), job); err != nil {
		http.Error(w, fmt.Sprintf("failed enqueuing job: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(job)
}
