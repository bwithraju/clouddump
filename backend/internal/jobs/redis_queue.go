package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	RedisQueuePendingKey = "clouddump:jobs:pending"
	RedisJobMetaPrefix   = "clouddump:jobs:meta:"
	RedisJobIndexKey     = "clouddump:jobs:index"
)

// RedisJobQueue implements JobQueue backed by a Redis instance or cluster.
type RedisJobQueue struct {
	client *redis.Client
}

// NewRedisJobQueue initializes a Redis-backed JobQueue.
func NewRedisJobQueue(addr, password string, db int) (*RedisJobQueue, error) {
	if addr == "" {
		addr = "localhost:6379"
	}

	rdb := redis.NewClient(&redis.Options{
		Addr:         addr,
		Password:     password,
		DB:           db,
		DialTimeout:  3 * time.Second,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := rdb.Ping(ctx).Err(); err != nil {
		_ = rdb.Close()
		return nil, fmt.Errorf("failed connecting to redis at %s: %w", addr, err)
	}

	return &RedisJobQueue{client: rdb}, nil
}

// Enqueue persists job metadata and pushes its ID into the pending work list.
func (q *RedisJobQueue) Enqueue(ctx context.Context, job *Job) error {
	if job == nil || job.ID == "" {
		return errors.New("cannot enqueue empty or uninitialized job")
	}

	data, err := json.Marshal(job)
	if err != nil {
		return fmt.Errorf("failed marshaling job: %w", err)
	}

	pipe := q.client.TxPipeline()
	pipe.Set(ctx, RedisJobMetaPrefix+job.ID, data, 7*24*time.Hour)
	pipe.ZAdd(ctx, RedisJobIndexKey, redis.Z{
		Score:  float64(job.CreatedAt.UnixNano()),
		Member: job.ID,
	})
	pipe.LPush(ctx, RedisQueuePendingKey, job.ID)

	_, err = pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed enqueuing job in redis: %w", err)
	}
	return nil
}

// Dequeue blocks up to timeout waiting for an available job from the pending list.
func (q *RedisJobQueue) Dequeue(ctx context.Context, timeout time.Duration) (*Job, error) {
	if timeout <= 0 {
		timeout = 1 * time.Second
	}

	results, err := q.client.BRPop(ctx, timeout, RedisQueuePendingKey).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, nil // timed out with no jobs
		}
		return nil, err
	}

	if len(results) < 2 {
		return nil, nil
	}
	jobID := results[1]
	return q.GetJob(ctx, jobID)
}

// GetJob retrieves job metadata by ID.
func (q *RedisJobQueue) GetJob(ctx context.Context, jobID string) (*Job, error) {
	data, err := q.client.Get(ctx, RedisJobMetaPrefix+jobID).Bytes()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, ErrJobNotFound
		}
		return nil, err
	}

	var job Job
	if err := json.Unmarshal(data, &job); err != nil {
		return nil, fmt.Errorf("failed unmarshaling job from redis: %w", err)
	}
	return &job, nil
}

// UpdateJob updates the stored metadata for a job.
func (q *RedisJobQueue) UpdateJob(ctx context.Context, job *Job) error {
	if job == nil || job.ID == "" {
		return errors.New("invalid job")
	}

	job.UpdatedAt = time.Now().UTC()
	data, err := json.Marshal(job)
	if err != nil {
		return fmt.Errorf("failed marshaling updated job: %w", err)
	}

	return q.client.Set(ctx, RedisJobMetaPrefix+job.ID, data, 7*24*time.Hour).Err()
}

// ListJobs retrieves up to limit recent jobs sorted by creation timestamp descending.
func (q *RedisJobQueue) ListJobs(ctx context.Context, limit int) ([]*Job, error) {
	if limit <= 0 {
		limit = 50
	}

	jobIDs, err := q.client.ZRevRange(ctx, RedisJobIndexKey, 0, int64(limit-1)).Result()
	if err != nil {
		return nil, err
	}

	jobs := make([]*Job, 0, len(jobIDs))
	for _, id := range jobIDs {
		job, err := q.GetJob(ctx, id)
		if err == nil && job != nil {
			jobs = append(jobs, job)
		}
	}
	return jobs, nil
}

// Close closes the Redis client connection.
func (q *RedisJobQueue) Close() error {
	return q.client.Close()
}
