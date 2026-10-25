package jobs

import (
	"encoding/json"
	"fmt"
	"time"
)

// JobType defines the categories of asynchronous background operations.
type JobType string

const (
	JobTypeLargeUploadProcessing JobType = "large_upload_processing"
	JobTypeReplication          JobType = "replication"
	JobTypeNodeHealthCheck      JobType = "node_health_check"
	JobTypeCleanupAbandoned     JobType = "cleanup_abandoned_uploads"
	JobTypeFailedChunkRetry     JobType = "failed_chunk_retry"
)

// JobStatus represents the state of a job in the queue lifecycle.
type JobStatus string

const (
	JobStatusPending   JobStatus = "PENDING"
	JobStatusRunning   JobStatus = "RUNNING"
	JobStatusCompleted JobStatus = "COMPLETED"
	JobStatusFailed    JobStatus = "FAILED"
	JobStatusRetrying  JobStatus = "RETRYING"
)

// Job encapsulates a unit of background work tracked in Redis or Memory.
type Job struct {
	ID          string          `json:"id"`
	Type        JobType         `json:"type"`
	Status      JobStatus       `json:"status"`
	Payload     json.RawMessage `json:"payload"`
	Attempts    int             `json:"attempts"`
	MaxRetries  int             `json:"max_retries"`
	LastError   string          `json:"last_error,omitempty"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
	CompletedAt *time.Time      `json:"completed_at,omitempty"`
}

// NewJob constructs a Job with initial PENDING status and default max retries.
func NewJob(jobType JobType, payload interface{}, maxRetries int) (*Job, error) {
	if maxRetries <= 0 {
		maxRetries = 3
	}

	rawPayload, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed marshaling job payload: %w", err)
	}

	now := time.Now().UTC()
	jobID := fmt.Sprintf("job_%d_%s", now.UnixNano(), string(jobType))

	return &Job{
		ID:         jobID,
		Type:       jobType,
		Status:     JobStatusPending,
		Payload:    rawPayload,
		Attempts:   0,
		MaxRetries: maxRetries,
		CreatedAt:  now,
		UpdatedAt:  now,
	}, nil
}

// Specific Job Payloads

type LargeUploadPayload struct {
	UploadID string `json:"upload_id"`
	FileID   string `json:"file_id"`
	UserID   string `json:"user_id"`
}

type ReplicationPayload struct {
	FileID                string `json:"file_id"`
	ChunkID               string `json:"chunk_id"`
	TargetReplicationFactor int  `json:"target_replication_factor"`
}

type HealthCheckPayload struct {
	TriggeredBy string `json:"triggered_by"`
}

type CleanupAbandonedPayload struct {
	OlderThanSeconds int64  `json:"older_than_seconds"`
	UserID           string `json:"user_id,omitempty"`
}

type FailedChunkRetryPayload struct {
	FileID  string `json:"file_id"`
	ChunkID string `json:"chunk_id"`
}
