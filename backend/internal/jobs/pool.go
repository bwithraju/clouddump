package jobs

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"
)

// JobHandlerFunc represents the execution logic for a specific JobType.
type JobHandlerFunc func(ctx context.Context, job *Job) error

// JobWorkerPool manages background workers consuming from a JobQueue.
type JobWorkerPool struct {
	queue       JobQueue
	handlers    map[JobType]JobHandlerFunc
	workerCount int
	stopChan    chan struct{}
	wg          sync.WaitGroup
	running     bool
	mu          sync.RWMutex
}

// NewJobWorkerPool constructs a JobWorkerPool.
func NewJobWorkerPool(queue JobQueue, workerCount int) *JobWorkerPool {
	if workerCount <= 0 {
		workerCount = 4
	}
	return &JobWorkerPool{
		queue:       queue,
		handlers:    make(map[JobType]JobHandlerFunc),
		workerCount: workerCount,
		stopChan:    make(chan struct{}),
	}
}

// RegisterHandler binds an execution function to a JobType.
func (p *JobWorkerPool) RegisterHandler(jobType JobType, fn JobHandlerFunc) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.handlers[jobType] = fn
}

// Start launches worker goroutines to process jobs from the queue.
func (p *JobWorkerPool) Start(ctx context.Context) {
	p.mu.Lock()
	if p.running {
		p.mu.Unlock()
		return
	}
	p.running = true
	p.stopChan = make(chan struct{})
	p.mu.Unlock()

	for i := 0; i < p.workerCount; i++ {
		p.wg.Add(1)
		go p.workerLoop(ctx, i+1)
	}
}

// Stop gracefully signals workers to stop and waits for in-flight jobs to complete.
func (p *JobWorkerPool) Stop() {
	p.mu.Lock()
	if !p.running {
		p.mu.Unlock()
		return
	}
	p.running = false
	close(p.stopChan)
	p.mu.Unlock()

	p.wg.Wait()
}

func (p *JobWorkerPool) workerLoop(ctx context.Context, workerID int) {
	defer p.wg.Done()

	for {
		select {
		case <-p.stopChan:
			return
		case <-ctx.Done():
			return
		default:
		}

		// Dequeue next job with 1s timeout
		job, err := p.queue.Dequeue(ctx, 1*time.Second)
		if err != nil || job == nil {
			continue
		}

		p.executeJob(ctx, job, workerID)
	}
}

func (p *JobWorkerPool) executeJob(ctx context.Context, job *Job, workerID int) {
	p.mu.RLock()
	handler, exists := p.handlers[job.Type]
	p.mu.RUnlock()

	if !exists {
		job.Status = JobStatusFailed
		job.LastError = fmt.Sprintf("no registered handler for job type %s", job.Type)
		_ = p.queue.UpdateJob(ctx, job)
		return
	}

	// Mark status RUNNING
	job.Status = JobStatusRunning
	job.UpdatedAt = time.Now().UTC()
	_ = p.queue.UpdateJob(ctx, job)

	// Execute handler
	err := handler(ctx, job)
	if err == nil {
		// Completed successfully
		now := time.Now().UTC()
		job.Status = JobStatusCompleted
		job.CompletedAt = &now
		job.LastError = ""
		_ = p.queue.UpdateJob(ctx, job)
		return
	}

	// Execution failed -> handle retry or terminal failure
	job.Attempts++
	job.LastError = err.Error()

	if job.Attempts < job.MaxRetries {
		job.Status = JobStatusRetrying
		_ = p.queue.UpdateJob(ctx, job)

		// Re-enqueue after exponential backoff
		backoff := time.Duration(100*(1<<job.Attempts)) * time.Millisecond
		go func(j *Job, delay time.Duration) {
			select {
			case <-time.After(delay):
			case <-p.stopChan:
				return
			}
			j.Status = JobStatusPending
			_ = p.queue.Enqueue(context.Background(), j)
		}(job, backoff)
	} else {
		// Retries exhausted
		job.Status = JobStatusFailed
		_ = p.queue.UpdateJob(ctx, job)
		log.Printf("[Worker %d] Job %s (%s) permanently failed after %d attempts: %v",
			workerID, job.ID, job.Type, job.Attempts, err)
	}
}
