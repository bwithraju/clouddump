package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/bwithraju/clouddump/backend/internal/chunks"
	"github.com/bwithraju/clouddump/backend/internal/database"
	"github.com/bwithraju/clouddump/backend/internal/storage"
	"github.com/bwithraju/clouddump/backend/internal/uploads"
)

// WorkerDependencies encapsulates the services and repositories required by background handlers.
type WorkerDependencies struct {
	StorageManager *storage.StorageManager
	UploadManager  *uploads.UploadManager
	Repo           database.Repository
}

// RegisterDefaultHandlers registers all 5 standard background worker tasks.
func RegisterDefaultHandlers(pool *JobWorkerPool, deps WorkerDependencies) {
	pool.RegisterHandler(JobTypeLargeUploadProcessing, makeLargeUploadHandler(deps))
	pool.RegisterHandler(JobTypeReplication, makeReplicationHandler(deps))
	pool.RegisterHandler(JobTypeNodeHealthCheck, makeHealthCheckHandler(deps))
	pool.RegisterHandler(JobTypeCleanupAbandoned, makeCleanupAbandonedHandler(deps))
	pool.RegisterHandler(JobTypeFailedChunkRetry, makeFailedChunkRetryHandler(deps))
}

// 1. Large Upload Processing Handler
func makeLargeUploadHandler(deps WorkerDependencies) JobHandlerFunc {
	return func(ctx context.Context, job *Job) error {
		var payload LargeUploadPayload
		if err := json.Unmarshal(job.Payload, &payload); err != nil {
			return fmt.Errorf("invalid large upload payload: %w", err)
		}

		if payload.FileID == "" {
			return errors.New("missing file_id in large upload job")
		}

		file, err := deps.Repo.Files().GetFile(ctx, payload.FileID)
		if err != nil {
			return fmt.Errorf("failed fetching file record: %w", err)
		}

		// Idempotency: If already completed, nothing to do
		if file.Status == string(chunks.FileStatusCompleted) {
			return nil
		}

		// Check all chunks
		chunkRecords, err := deps.Repo.Chunks().ListChunksByFile(ctx, payload.FileID)
		if err != nil {
			return fmt.Errorf("failed listing chunks: %w", err)
		}

		if len(chunkRecords) < file.TotalChunks {
			return fmt.Errorf("not all chunks stored yet: %d/%d", len(chunkRecords), file.TotalChunks)
		}

		// Verify chunk integrity
		for _, c := range chunkRecords {
			if c.Checksum == "" {
				return fmt.Errorf("chunk %d missing checksum", c.ChunkIndex)
			}
		}

		// Mark file COMPLETED
		if err := deps.Repo.Files().UpdateFileStatus(ctx, payload.FileID, string(chunks.FileStatusCompleted)); err != nil {
			return fmt.Errorf("failed updating file status to COMPLETED: %w", err)
		}

		log.Printf("[Jobs] Large upload processing completed for file %s", payload.FileID)
		return nil
	}
}

// 2. Replication Handler (Idempotent, no duplicate chunk creation)
func makeReplicationHandler(deps WorkerDependencies) JobHandlerFunc {
	return func(ctx context.Context, job *Job) error {
		var payload ReplicationPayload
		if err := json.Unmarshal(job.Payload, &payload); err != nil {
			return fmt.Errorf("invalid replication payload: %w", err)
		}

		if payload.ChunkID == "" {
			return errors.New("missing chunk_id in replication job")
		}

		targetFactor := payload.TargetReplicationFactor
		if targetFactor <= 1 {
			targetFactor = 2
		}

		chk, err := deps.Repo.Chunks().GetChunk(ctx, payload.ChunkID)
		if err != nil {
			return fmt.Errorf("failed fetching chunk record: %w", err)
		}

		// Idempotency: Check if chunk already has the target number of copies
		currentCopies := 1 + len(chk.Replicas)
		if currentCopies >= targetFactor {
			log.Printf("[Jobs] Chunk %s already has %d copies (target %d). Skipping duplicate creation.",
				chk.ID, currentCopies, targetFactor)
			return nil
		}

		// Identify existing nodes where this chunk already resides (to avoid placing copies on the same node)
		existingNodes := make(map[string]bool)
		existingNodes[chk.StorageNodeID] = true
		for _, r := range chk.Replicas {
			existingNodes[r.StorageNodeID] = true
		}

		excludedIDs := make([]string, 0, len(existingNodes))
		for id := range existingNodes {
			excludedIDs = append(excludedIDs, id)
		}

		// Select an alternative online node with sufficient capacity
		replicaNode, err := deps.StorageManager.SelectNodeForChunk(chk.Size, excludedIDs...)
		if err != nil {
			return fmt.Errorf("failed selecting replica node: %w", err)
		}

		// Ensure replica node is not in existingNodes (Strictly distinct nodes)
		if existingNodes[replicaNode.ID] {
			return fmt.Errorf("selected replica node %s already holds a copy of chunk %s", replicaNode.ID, chk.ID)
		}

		// Stream chunk data from source node
		stream, err := deps.StorageManager.GetChunk(ctx, chk.StorageNodeID, chk.ProviderObjectID)
		if err != nil {
			// If primary node is unavailable, try existing replicas
			var replicaErr error
			for _, rep := range chk.Replicas {
				if rNode, gErr := deps.StorageManager.GetNode(rep.StorageNodeID); gErr == nil && rNode.IsOnline() {
					stream, replicaErr = deps.StorageManager.GetChunk(ctx, rep.StorageNodeID, rep.ProviderObjectID)
					if replicaErr == nil {
						break
					}
				}
			}
			if stream == nil {
				return fmt.Errorf("failed retrieving source chunk for replication: %w", err)
			}
		}
		defer stream.Close()

		storedObjectID, _, err := deps.StorageManager.StoreChunk(ctx, replicaNode.ID, chk.ID, stream, chk.Size)
		if err != nil {
			return fmt.Errorf("failed storing chunk copy on replica node %s: %w", replicaNode.ID, err)
		}

		// Record replica metadata
		newReplica := database.ChunkReplicaRecord{
			StorageNodeID:    replicaNode.ID,
			ProviderObjectID: storedObjectID,
			Status:           string(chunks.ChunkStatusStored),
		}

		chk.Replicas = append(chk.Replicas, newReplica)
		if err := deps.Repo.Chunks().CreateChunk(ctx, chk); err != nil {
			return fmt.Errorf("failed updating chunk replica metadata: %w", err)
		}

		log.Printf("[Jobs] Successfully replicated chunk %s to node %s (object: %s)",
			chk.ID, replicaNode.ID, storedObjectID)
		return nil
	}
}

// 3. Node Health Check Handler
func makeHealthCheckHandler(deps WorkerDependencies) JobHandlerFunc {
	return func(ctx context.Context, job *Job) error {
		// Run health checks on all registered storage nodes
		results := deps.StorageManager.RunHealthChecks(ctx)

		// Reconcile affected files and chunks
		reconcileResult, err := deps.StorageManager.ReconcileNodeFailures(ctx, deps.Repo)
		if err != nil {
			return fmt.Errorf("health check reconciliation failed: %w", err)
		}

		log.Printf("[Jobs] Health check finished. Probed: %d nodes, Degraded files: %d, Recovered files: %d",
			len(results), len(reconcileResult.DegradedFiles), len(reconcileResult.RecoveredFiles))
		return nil
	}
}

// 4. Cleanup Abandoned Uploads Handler
func makeCleanupAbandonedHandler(deps WorkerDependencies) JobHandlerFunc {
	return func(ctx context.Context, job *Job) error {
		var payload CleanupAbandonedPayload
		_ = json.Unmarshal(job.Payload, &payload)

		threshold := 1 * time.Hour
		if payload.OlderThanSeconds > 0 {
			threshold = time.Duration(payload.OlderThanSeconds) * time.Second
		}

		if deps.UploadManager == nil {
			return nil
		}

		// List all sessions
		sessions := deps.UploadManager.ListSessions(payload.UserID)
		now := time.Now().UTC()
		cleanedCount := 0

		for _, s := range sessions {
			summary := s.Summary()
			// If session is abandoned (not completed, inactive for > threshold)
			if summary.Status != uploads.UploadStatusCompleted && now.Sub(summary.UpdatedAt) > threshold {
				log.Printf("[Jobs] Cleaning up abandoned upload session: %s (file: %s)", s.UploadID, s.FileID)
				_ = deps.UploadManager.CancelSession(ctx, s.UploadID)
				cleanedCount++
			}
		}

		log.Printf("[Jobs] Abandoned upload cleanup completed. Cleaned %d sessions.", cleanedCount)
		return nil
	}
}

// 5. Failed Chunk Retry Handler
func makeFailedChunkRetryHandler(deps WorkerDependencies) JobHandlerFunc {
	return func(ctx context.Context, job *Job) error {
		var payload FailedChunkRetryPayload
		if err := json.Unmarshal(job.Payload, &payload); err != nil {
			return fmt.Errorf("invalid failed chunk retry payload: %w", err)
		}

		if payload.ChunkID == "" {
			return errors.New("chunk_id required for retry")
		}

		chk, err := deps.Repo.Chunks().GetChunk(ctx, payload.ChunkID)
		if err != nil {
			return fmt.Errorf("failed retrieving chunk: %w", err)
		}

		// Idempotency: If primary node is healthy and online, no retry needed
		if primaryNode, err := deps.StorageManager.GetNode(chk.StorageNodeID); err == nil && primaryNode.IsOnline() {
			return nil
		}

		// Check if an online replica exists to recover from
		var sourceReplica *database.ChunkReplicaRecord
		for _, rep := range chk.Replicas {
			if rNode, err := deps.StorageManager.GetNode(rep.StorageNodeID); err == nil && rNode.IsOnline() {
				sourceReplica = &rep
				break
			}
		}

		if sourceReplica == nil {
			return fmt.Errorf("cannot retry chunk %s: all primary and replica nodes are currently offline", chk.ID)
		}

		// Select a new online node with sufficient capacity
		newNode, err := deps.StorageManager.SelectNodeForChunk(chk.Size, chk.StorageNodeID, sourceReplica.StorageNodeID)
		if err != nil {
			return fmt.Errorf("failed selecting new node for chunk retry: %w", err)
		}

		// Read from healthy replica
		stream, err := deps.StorageManager.GetChunk(ctx, sourceReplica.StorageNodeID, sourceReplica.ProviderObjectID)
		if err != nil {
			return fmt.Errorf("failed streaming chunk from replica: %w", err)
		}
		defer stream.Close()

		// Write to new node
		newObjID, _, err := deps.StorageManager.StoreChunk(ctx, newNode.ID, chk.ID, stream, chk.Size)
		if err != nil {
			return fmt.Errorf("failed writing recovered chunk to node %s: %w", newNode.ID, err)
		}

		// Update chunk primary location to the new node
		chk.StorageNodeID = newNode.ID
		chk.ProviderObjectID = newObjID
		chk.Status = string(chunks.ChunkStatusStored)
		if err := deps.Repo.Chunks().CreateChunk(ctx, chk); err != nil {
			return fmt.Errorf("failed updating chunk metadata: %w", err)
		}

		// Check if file can be restored from DEGRADED to COMPLETED
		if chk.FileID != "" {
			chunksList, _ := deps.Repo.Chunks().ListChunksByFile(ctx, chk.FileID)
			allHealthy := true
			for _, c := range chunksList {
				if node, err := deps.StorageManager.GetNode(c.StorageNodeID); err != nil || !node.IsOnline() {
					allHealthy = false
					break
				}
			}
			if allHealthy {
				_ = deps.Repo.Files().UpdateFileStatus(ctx, chk.FileID, string(chunks.FileStatusCompleted))
			}
		}

		log.Printf("[Jobs] Successfully recovered and retried failed chunk %s on node %s", chk.ID, newNode.ID)
		return nil
	}
}
