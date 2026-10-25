package jobs

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"
)

// MemoryJobQueue provides an in-memory, thread-safe implementation of JobQueue.
type MemoryJobQueue struct {
	jobs    map[string]*Job
	pending chan string
	closed  bool
	mu      sync.RWMutex
}

// NewMemoryJobQueue initializes a new MemoryJobQueue.
func NewMemoryJobQueue() *MemoryJobQueue {
	return &MemoryJobQueue{
		jobs:    make(map[string]*Job),
		pending: make(chan string, 10000),
	}
}

// Enqueue adds a job to the memory queue.
func (q *MemoryJobQueue) Enqueue(ctx context.Context, job *Job) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.closed {
		return ErrQueueClosed
	}
	if job == nil || job.ID == "" {
		return errors.New("cannot enqueue empty job")
	}

	jobCopy := *job
	q.jobs[job.ID] = &jobCopy

	select {
	case q.pending <- job.ID:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	default:
		return errors.New("job queue channel buffer full")
	}
}

// Dequeue waits for a job ID from the channel or times out.
func (q *MemoryJobQueue) Dequeue(ctx context.Context, timeout time.Duration) (*Job, error) {
	if timeout <= 0 {
		timeout = 100 * time.Millisecond
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-timer.C:
		return nil, nil // timed out, no job available
	case jobID, ok := <-q.pending:
		if !ok {
			return nil, ErrQueueClosed
		}
		return q.GetJob(ctx, jobID)
	}
}

// GetJob returns a copy of the stored job by ID.
func (q *MemoryJobQueue) GetJob(ctx context.Context, jobID string) (*Job, error) {
	q.mu.RLock()
	defer q.mu.RUnlock()

	job, ok := q.jobs[jobID]
	if !ok {
		return nil, ErrJobNotFound
	}

	jobCopy := *job
	return &jobCopy, nil
}

// UpdateJob updates the stored state of the job.
func (q *MemoryJobQueue) UpdateJob(ctx context.Context, job *Job) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.closed {
		return ErrQueueClosed
	}

	if _, ok := q.jobs[job.ID]; !ok {
		return ErrJobNotFound
	}

	job.UpdatedAt = time.Now().UTC()
	jobCopy := *job
	q.jobs[job.ID] = &jobCopy
	return nil
}

// ListJobs returns the most recent jobs up to limit sorted by creation time descending.
func (q *MemoryJobQueue) ListJobs(ctx context.Context, limit int) ([]*Job, error) {
	q.mu.RLock()
	defer q.mu.RUnlock()

	list := make([]*Job, 0, len(q.jobs))
	for _, j := range q.jobs {
		jobCopy := *j
		list = append(list, &jobCopy)
	}

	sort.Slice(list, func(i, j int) bool {
		return list[i].CreatedAt.After(list[j].CreatedAt)
	})

	if limit > 0 && len(list) > limit {
		list = list[:limit]
	}

	return list, nil
}

// Close closes the queue and stops dequeue channel.
func (q *MemoryJobQueue) Close() error {
	q.mu.Lock()
	defer q.mu.Unlock()

	if !q.closed {
		q.closed = true
		close(q.pending)
	}
	return nil
}
