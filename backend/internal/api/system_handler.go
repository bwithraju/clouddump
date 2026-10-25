package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"time"

	"github.com/bwithraju/clouddump/backend/internal/auth"
	"github.com/bwithraju/clouddump/backend/internal/database"
	"github.com/bwithraju/clouddump/backend/internal/jobs"
	"github.com/bwithraju/clouddump/backend/internal/storage"
	"github.com/bwithraju/clouddump/backend/internal/uploads"
)

// SystemHandler serves aggregated monitoring metrics, recent operations, and failed operation alerts.
type SystemHandler struct {
	startTime      time.Time
	storageManager *storage.StorageManager
	repo           database.Repository
	uploadManager  *uploads.UploadManager
	jobQueue       jobs.JobQueue
}

// NewSystemHandler constructs a new SystemHandler.
func NewSystemHandler(
	startTime time.Time,
	mgr *storage.StorageManager,
	repo database.Repository,
	uploadMgr *uploads.UploadManager,
	jobQueue jobs.JobQueue,
) *SystemHandler {
	if startTime.IsZero() {
		startTime = time.Now().UTC()
	}
	return &SystemHandler{
		startTime:      startTime,
		storageManager: mgr,
		repo:           repo,
		uploadManager:  uploadMgr,
		jobQueue:       jobQueue,
	}
}

// SystemOperation represents a recent system activity item.
type SystemOperation struct {
	ID        string `json:"id"`
	Type      string `json:"type"`      // "UPLOAD", "DOWNLOAD", "DELETE", "HEALTH_CHECK", "REPLICATION"
	Entity    string `json:"entity"`    // filename, node name, or job ID
	Status    string `json:"status"`    // "SUCCESS", "RUNNING", "FAILED", "WARNING"
	Details   string `json:"details"`
	Timestamp string `json:"timestamp"`
}

// FailedOperation represents an active system issue, degraded file, or failed background task.
type FailedOperation struct {
	ID        string `json:"id"`
	Type      string `json:"type"`      // "FILE_DEGRADED", "NODE_OFFLINE", "UPLOAD_FAILED", "JOB_FAILED"
	Entity    string `json:"entity"`
	Reason    string `json:"reason"`
	Severity  string `json:"severity"`  // "HIGH", "MEDIUM", "LOW"
	Timestamp string `json:"timestamp"`
}

// SystemActivityResponse payload for GET /api/system/activity.
type SystemActivityResponse struct {
	UptimeSeconds    int64               `json:"uptime_seconds"`
	PoolStats        storage.PoolStats   `json:"pool_stats"`
	TotalFiles       int                 `json:"total_files"`
	CompletedFiles   int                 `json:"completed_files"`
	DegradedFiles    int                 `json:"degraded_files"`
	FailedFiles      int                 `json:"failed_files"`
	ActiveUploads    int                 `json:"active_uploads"`
	RecentOperations []SystemOperation  `json:"recent_operations"`
	FailedOperations []FailedOperation  `json:"failed_operations"`
}

// GetActivity handles GET /api/system/activity - returns aggregated monitoring telemetry.
func (h *SystemHandler) GetActivity(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx := r.Context()
	userID := auth.GetEffectiveUserID(r)
	if userID == "" {
		userID = "default_user"
	}

	uptime := int64(time.Since(h.startTime).Seconds())
	poolStats := h.storageManager.GetPoolStatsForUser(ctx, userID)

	recentOps := make([]SystemOperation, 0)
	failedOps := make([]FailedOperation, 0)

	// 1. Inspect Storage Nodes for offline/degraded alerts
	for _, n := range poolStats.Nodes {
		if n.Status == storage.NodeStatusOffline {
			failedOps = append(failedOps, FailedOperation{
				ID:        fmt.Sprintf("node_%s_offline", n.ID),
				Type:      "NODE_OFFLINE",
				Entity:    n.Name,
				Reason:    fmt.Sprintf("Storage node %s is offline or unresponsive", n.ID),
				Severity:  "HIGH",
				Timestamp: n.LastHealthCheck.Format(time.RFC3339),
			})
		}
	}

	// 2. Inspect Files
	var totalFiles, completedFiles, degradedFiles, failedFiles int
	if h.repo != nil && h.repo.Files() != nil {
		fileList, err := h.repo.Files().ListFilesByUser(ctx, userID)
		if err == nil {
			totalFiles = len(fileList)
			for _, f := range fileList {
				switch f.Status {
				case "COMPLETED":
					completedFiles++
					recentOps = append(recentOps, SystemOperation{
						ID:        f.ID,
						Type:      "UPLOAD",
						Entity:    f.Filename,
						Status:    "SUCCESS",
						Details:   fmt.Sprintf("%d bytes stored across %d chunks", f.Size, f.TotalChunks),
						Timestamp: f.CreatedAt.Format(time.RFC3339),
					})
				case "DEGRADED":
					degradedFiles++
					failedOps = append(failedOps, FailedOperation{
						ID:        fmt.Sprintf("file_%s_degraded", f.ID),
						Type:      "FILE_DEGRADED",
						Entity:    f.Filename,
						Reason:    "One or more chunks are residing on offline storage nodes",
						Severity:  "HIGH",
						Timestamp: f.UpdatedAt.Format(time.RFC3339),
					})
				case "FAILED":
					failedFiles++
					failedOps = append(failedOps, FailedOperation{
						ID:        fmt.Sprintf("file_%s_failed", f.ID),
						Type:      "UPLOAD_FAILED",
						Entity:    f.Filename,
						Reason:    "File upload was not finalized or suffered chunk failure",
						Severity:  "MEDIUM",
						Timestamp: f.UpdatedAt.Format(time.RFC3339),
					})
				}
			}
		}
	}

	// 3. Inspect Upload Sessions
	var activeUploads int
	if h.uploadManager != nil {
		sessions := h.uploadManager.ListSessions(userID)
		for _, s := range sessions {
			summary := s.Summary()
			if summary.Status == "UPLOADING" || summary.Status == "INITIALIZED" {
				activeUploads++
				recentOps = append(recentOps, SystemOperation{
					ID:        summary.UploadID,
					Type:      "UPLOAD",
					Entity:    summary.Filename,
					Status:    "RUNNING",
					Details:   fmt.Sprintf("%d/%d chunks uploaded (%.0f%%)", summary.CompletedCount, summary.TotalChunks, summary.ProgressPercent),
					Timestamp: summary.UpdatedAt.Format(time.RFC3339),
				})
			} else if summary.Status == "FAILED" {
				failedOps = append(failedOps, FailedOperation{
					ID:        fmt.Sprintf("upload_%s_failed", summary.UploadID),
					Type:      "UPLOAD_FAILED",
					Entity:    summary.Filename,
					Reason:    fmt.Sprintf("Session failed with %d failed chunks", summary.FailedCount),
					Severity:  "MEDIUM",
					Timestamp: summary.UpdatedAt.Format(time.RFC3339),
				})
			}
		}
	}

	// 4. Inspect Background Jobs (if job queue configured)
	if h.jobQueue != nil {
		jobList, err := h.jobQueue.ListJobs(ctx, 30)
		if err == nil {
			for _, j := range jobList {
				if j.Status == jobs.JobStatusFailed {
					failedOps = append(failedOps, FailedOperation{
						ID:        j.ID,
						Type:      "JOB_FAILED",
						Entity:    string(j.Type),
						Reason:    j.LastError,
						Severity:  "MEDIUM",
						Timestamp: j.UpdatedAt.Format(time.RFC3339),
					})
				} else if j.Status == jobs.JobStatusCompleted {
					completedAtStr := j.UpdatedAt.Format(time.RFC3339)
					durationStr := "completed"
					if j.CompletedAt != nil {
						completedAtStr = j.CompletedAt.Format(time.RFC3339)
						durationStr = fmt.Sprintf("Duration: %v", j.CompletedAt.Sub(j.CreatedAt))
					}
					recentOps = append(recentOps, SystemOperation{
						ID:        j.ID,
						Type:      string(j.Type),
						Entity:    fmt.Sprintf("Job %s", j.ID),
						Status:    "SUCCESS",
						Details:   durationStr,
						Timestamp: completedAtStr,
					})
				} else if j.Status == jobs.JobStatusRunning || j.Status == jobs.JobStatusRetrying {
					recentOps = append(recentOps, SystemOperation{
						ID:        j.ID,
						Type:      string(j.Type),
						Entity:    fmt.Sprintf("Job %s", j.ID),
						Status:    "RUNNING",
						Details:   fmt.Sprintf("Attempt %d/%d", j.Attempts, j.MaxRetries),
						Timestamp: j.UpdatedAt.Format(time.RFC3339),
					})
				}
			}
		}
	}

	// Sort Recent Operations descending by timestamp
	sort.Slice(recentOps, func(i, j int) bool {
		return recentOps[i].Timestamp > recentOps[j].Timestamp
	})
	if len(recentOps) > 15 {
		recentOps = recentOps[:15]
	}

	// Sort Failed Operations descending by timestamp
	sort.Slice(failedOps, func(i, j int) bool {
		return failedOps[i].Timestamp > failedOps[j].Timestamp
	})
	if len(failedOps) > 15 {
		failedOps = failedOps[:15]
	}

	resp := SystemActivityResponse{
		UptimeSeconds:    uptime,
		PoolStats:        poolStats,
		TotalFiles:       totalFiles,
		CompletedFiles:   completedFiles,
		DegradedFiles:    degradedFiles,
		FailedFiles:      failedFiles,
		ActiveUploads:    activeUploads,
		RecentOperations: recentOps,
		FailedOperations: failedOps,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}
