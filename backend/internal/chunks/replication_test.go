package chunks

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/bwithraju/clouddump/backend/internal/providers"
	"github.com/bwithraju/clouddump/backend/internal/storage"
)

func createReplicationTestNode(t *testing.T, id, name string, capacity int64) (*storage.StorageNode, string) {
	tempDir, err := os.MkdirTemp("", "clouddump_rep_test_"+id+"_*")
	if err != nil {
		t.Fatalf("failed to create temp dir for node %s: %v", id, err)
	}

	provider, err := providers.NewLocalStorageProvider(tempDir, capacity)
	if err != nil {
		t.Fatalf("failed to create provider for node %s: %v", id, err)
	}

	node, err := storage.NewStorageNode(id, name, provider)
	if err != nil {
		t.Fatalf("failed to create storage node %s: %v", id, err)
	}

	return node, tempDir
}

// Test: Verify default replication factor is 1, and optional factor is 2
func TestReplication_ConfigurableFactor(t *testing.T) {
	mgr := storage.NewStorageManager(storage.NewMaxAvailableStrategy())
	distributor, err := NewChunkDistributor(mgr, 1024)
	if err != nil {
		t.Fatalf("failed to create distributor: %v", err)
	}

	if distributor.ReplicationFactor() != 1 {
		t.Errorf("expected default replication factor 1, got %d", distributor.ReplicationFactor())
	}

	distributor.SetReplicationFactor(2)
	if distributor.ReplicationFactor() != 2 {
		t.Errorf("expected replication factor 2, got %d", distributor.ReplicationFactor())
	}

	distributor.SetReplicationFactor(0) // Invalid fallback
	if distributor.ReplicationFactor() != 1 {
		t.Errorf("expected replication factor fallback to 1, got %d", distributor.ReplicationFactor())
	}
}

// Test: 1. upload file, 2. verify replica, 3. take primary node offline, 4. download file, 5. verify successful reconstruction from replica
func TestReplication_EndToEndLifecycle(t *testing.T) {
	ctx := context.Background()
	mgr := storage.NewStorageManager(storage.NewMaxAvailableStrategy())

	// 3 nodes with equal capacity
	n1, dir1 := createReplicationTestNode(t, "node1", "Node A", 20*1024*1024)
	defer os.RemoveAll(dir1)
	n2, dir2 := createReplicationTestNode(t, "node2", "Node B", 20*1024*1024)
	defer os.RemoveAll(dir2)
	n3, dir3 := createReplicationTestNode(t, "node3", "Node C", 20*1024*1024)
	defer os.RemoveAll(dir3)

	_ = mgr.RegisterNode(n1)
	_ = mgr.RegisterNode(n2)
	_ = mgr.RegisterNode(n3)

	chunkSize := int64(100 * 1024) // 100 KB
	distributor, _ := NewChunkDistributor(mgr, chunkSize)
	reconstructor, _ := NewFileReconstructor(mgr)

	// File with 3 chunks (100KB, 100KB, 50KB = 250KB)
	fileSize := int64(250 * 1024)
	originalData := make([]byte, fileSize)
	_, _ = io.ReadFull(rand.Reader, originalData)

	originalHasher := sha256.New()
	originalHasher.Write(originalData)
	expectedFileSHA256 := hex.EncodeToString(originalHasher.Sum(nil))

	// STEP 1: Upload file with replication factor = 2
	result, err := distributor.DistributeWithReplication(ctx, "rep_file_1", bytes.NewReader(originalData), fileSize, 2, nil)
	if err != nil {
		t.Fatalf("failed to distribute file with replication: %v", err)
	}

	if result.ReplicationFactor != 2 {
		t.Errorf("expected replication factor 2 in result, got %d", result.ReplicationFactor)
	}
	if len(result.Chunks) != 3 {
		t.Fatalf("expected 3 chunks, got %d", len(result.Chunks))
	}

	// STEP 2: Verify replicas - distinct nodes, physical existence, capacity accounting
	for i, chunk := range result.Chunks {
		if len(chunk.Replicas) != 1 {
			t.Fatalf("chunk %d expected 1 replica, got %d", i, len(chunk.Replicas))
		}

		primaryNodeID := chunk.StorageNodeID
		replicaNodeID := chunk.Replicas[0].StorageNodeID

		// Requirement: Avoid placing both copies on the same node
		if primaryNodeID == replicaNodeID {
			t.Fatalf("chunk %d placed primary and replica on SAME node %s!", i, primaryNodeID)
		}

		// Verify physical files exist on both node directories
		var primaryDir, replicaDir string
		switch primaryNodeID {
		case "node1":
			primaryDir = dir1
		case "node2":
			primaryDir = dir2
		case "node3":
			primaryDir = dir3
		}
		switch replicaNodeID {
		case "node1":
			replicaDir = dir1
		case "node2":
			replicaDir = dir2
		case "node3":
			replicaDir = dir3
		}

		primaryPath := filepath.Join(primaryDir, chunk.ProviderObjectID)
		replicaPath := filepath.Join(replicaDir, chunk.Replicas[0].ProviderObjectID)

		if _, err := os.Stat(primaryPath); os.IsNotExist(err) {
			t.Fatalf("primary chunk %d missing on disk at %s", i, primaryPath)
		}
		if _, err := os.Stat(replicaPath); os.IsNotExist(err) {
			t.Fatalf("replica chunk %d missing on disk at %s", i, replicaPath)
		}

		// Verify mapping tracks primary and replicas
		chunkMapping := result.Mapping[i]
		if chunkMapping[primaryNodeID] == "" {
			t.Errorf("mapping missing primary node %s for chunk %d", primaryNodeID, i)
		}
		if chunkMapping[replicaNodeID] == "" {
			t.Errorf("mapping missing replica node %s for chunk %d", replicaNodeID, i)
		}
	}

	// Capacity accounting check: total used across nodes must equal 2 * fileSize (500 KB)
	poolStats := mgr.GetPoolStats(ctx)
	if poolStats.UsedCapacity != fileSize*2 {
		t.Errorf("expected total pool used capacity %d (2x), got %d", fileSize*2, poolStats.UsedCapacity)
	}

	// STEP 3: Take primary node of chunk 0 offline
	primaryNodeChunk0 := result.Chunks[0].StorageNodeID
	replicaNodeChunk0 := result.Chunks[0].Replicas[0].StorageNodeID

	t.Logf("Taking primary node %s offline for chunk 0 (replica is on %s)", primaryNodeChunk0, replicaNodeChunk0)
	_ = mgr.SetNodeSimulatedOffline(primaryNodeChunk0, true)

	// Verify node is offline
	nodeStats, _ := mgr.GetNode(primaryNodeChunk0)
	if nodeStats.IsOnline() {
		t.Fatalf("expected node %s to be offline", primaryNodeChunk0)
	}

	// STEP 4: Download / Reconstruct file
	destBuffer := new(bytes.Buffer)
	written, err := reconstructor.ReconstructTo(ctx, result.Chunks, destBuffer)
	if err != nil {
		t.Fatalf("reconstruction failed while primary node is offline: %v", err)
	}

	if written != fileSize {
		t.Errorf("expected %d bytes reconstructed, got %d", fileSize, written)
	}

	// STEP 5: Verify successful reconstruction from replica byte-for-byte and SHA-256 match
	reconstructedData := destBuffer.Bytes()
	if !bytes.Equal(reconstructedData, originalData) {
		t.Fatalf("reconstructed data does not match original byte-for-byte!")
	}

	recHasher := sha256.New()
	recHasher.Write(reconstructedData)
	reconstructedSHA256 := hex.EncodeToString(recHasher.Sum(nil))

	if reconstructedSHA256 != expectedFileSHA256 {
		t.Fatalf("checksum mismatch: expected %s, got %s", expectedFileSHA256, reconstructedSHA256)
	}

	// Restore primary node and verify normal reconstruction
	_ = mgr.SetNodeSimulatedOffline(primaryNodeChunk0, false)
	destBuffer2 := new(bytes.Buffer)
	_, err = reconstructor.ReconstructTo(ctx, result.Chunks, destBuffer2)
	if err != nil {
		t.Fatalf("reconstruction failed after primary restored: %v", err)
	}
	if !bytes.Equal(destBuffer2.Bytes(), originalData) {
		t.Fatalf("reconstructed data mismatch after primary restored")
	}
}

// Test: Insufficient distinct nodes for requested replication factor
func TestReplication_InsufficientDistinctNodes(t *testing.T) {
	ctx := context.Background()
	mgr := storage.NewStorageManager(storage.NewMaxAvailableStrategy())

	// Only 1 node registered
	n1, dir1 := createReplicationTestNode(t, "node1", "Solo Node", 20*1024*1024)
	defer os.RemoveAll(dir1)
	_ = mgr.RegisterNode(n1)

	distributor, _ := NewChunkDistributor(mgr, 1024)

	// Attempt factor 2 with only 1 online node
	_, err := distributor.DistributeWithReplication(ctx, "file_insufficient", bytes.NewReader([]byte("test")), 4, 2, nil)
	if err == nil {
		t.Fatalf("expected error for factor 2 on single node, got nil")
	}
}
