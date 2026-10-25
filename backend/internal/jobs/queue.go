package jobs

import (
	"context"
	"errors"
	"time"
)

var (
	ErrJobNotFound = errors.New("job not found in queue")
	ErrQueueClosed = errors.New("job queue is closed")
)

// JobQueue defines the contract for persistent or in-memory job queues.
type JobQueue interface {
	// Enqueue appends a job to the queue and persists its metadata.
	Enqueue(ctx context.Context, job *Job) error

	// Dequeue blocks up to timeout waiting for a pending job to be leased.
	Dequeue(ctx context.Context, timeout time.Duration) (*Job, error)

	// GetJob retrieves a job snapshot by its unique ID.
	GetJob(ctx context.Context, jobID string) (*Job, error)

	// UpdateJob writes modified job status, attempts, or errors back to the store.
	UpdateJob(ctx context.Context, job *Job) error

	// ListJobs returns the most recent jobs up to limit.
	ListJobs(ctx context.Context, limit int) ([]*Job, error)

	// Close terminates queue connections and workers gracefully.
	Close() error
}
