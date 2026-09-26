package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"
)

// PoolStats provides aggregated metrics across all registered storage nodes.
type PoolStats struct {
	TotalCapacity     int64       `json:"total_capacity"`
	UsedCapacity      int64       `json:"used_capacity"`
	AvailableCapacity int64       `json:"available_capacity"`
	NodeCount         int         `json:"node_count"`
	OnlineNodes       int         `json:"online_nodes"`
	OfflineNodes      int         `json:"offline_nodes"`
	Nodes             []NodeStats `json:"nodes"`
}

// StorageManager manages the logical storage pool, nodes, and capacity-aware chunk operations.
type StorageManager struct {
	nodes    map[string]*StorageNode
	strategy AllocationStrategy
	mu       sync.RWMutex
}

// NewStorageManager instantiates a StorageManager with the specified allocation strategy.
func NewStorageManager(strategy AllocationStrategy) *StorageManager {
	if strategy == nil {
		strategy = NewMaxAvailableStrategy()
	}
	return &StorageManager{
		nodes:    make(map[string]*StorageNode),
		strategy: strategy,
	}
}

// SetAllocationStrategy dynamically updates the chunk placement algorithm.
func (m *StorageManager) SetAllocationStrategy(strategy AllocationStrategy) {
	if strategy == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.strategy = strategy
}

// RegisterNode adds a new storage node to the logical pool.
func (m *StorageManager) RegisterNode(node *StorageNode) error {
	if node == nil {
		return errors.New("cannot register nil node")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.nodes[node.ID]; exists {
		return fmt.Errorf("%w: %s", ErrDuplicateNodeID, node.ID)
	}

	m.nodes[node.ID] = node
	return nil
}

// UnregisterNode removes a storage node by ID from the pool.
func (m *StorageManager) UnregisterNode(nodeID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.nodes[nodeID]; !exists {
		return fmt.Errorf("%w: %s", ErrNodeNotFound, nodeID)
	}

	delete(m.nodes, nodeID)
	return nil
}

// GetNode retrieves a registered node by ID.
func (m *StorageManager) GetNode(nodeID string) (*StorageNode, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	node, exists := m.nodes[nodeID]
	if !exists {
		return nil, fmt.Errorf("%w: %s", ErrNodeNotFound, nodeID)
	}
	return node, nil
}

// ListNodes returns a slice of all registered storage nodes.
func (m *StorageManager) ListNodes() []*StorageNode {
	m.mu.RLock()
	defer m.mu.RUnlock()

	nodes := make([]*StorageNode, 0, len(m.nodes))
	for _, n := range m.nodes {
		nodes = append(nodes, n)
	}
	return nodes
}

// GetPoolStats computes the total, used, and available capacity across all online nodes.
func (m *StorageManager) GetPoolStats(ctx context.Context) PoolStats {
	m.mu.RLock()
	defer m.mu.RUnlock()

	stats := PoolStats{
		Nodes: make([]NodeStats, 0, len(m.nodes)),
	}

	for _, n := range m.nodes {
		nodeStats := n.GetStats()
		stats.Nodes = append(stats.Nodes, nodeStats)
		stats.NodeCount++

		stats.TotalCapacity += nodeStats.TotalCapacity
		stats.UsedCapacity += nodeStats.UsedCapacity
		stats.AvailableCapacity += nodeStats.AvailableCapacity

		if nodeStats.Status == NodeStatusOnline {
			stats.OnlineNodes++
		} else {
			stats.OfflineNodes++
		}
	}

	return stats
}

// SelectNodeForChunk selects a suitable storage node based on current capacity and allocation strategy.
func (m *StorageManager) SelectNodeForChunk(chunkSize int64, excludedNodeIDs ...string) (*StorageNode, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	excluded := make(map[string]bool, len(excludedNodeIDs))
	for _, id := range excludedNodeIDs {
		excluded[id] = true
	}

	nodes := make([]*StorageNode, 0, len(m.nodes))
	for _, n := range m.nodes {
		nodes = append(nodes, n)
	}

	return m.strategy.SelectNode(nodes, chunkSize, excluded)
}

// StoreChunk stores a chunk on a specific node with capacity reservation and release safeguards.
func (m *StorageManager) StoreChunk(ctx context.Context, nodeID string, chunkID string, r io.Reader, size int64) (string, int64, error) {
	node, err := m.GetNode(nodeID)
	if err != nil {
		return "", 0, err
	}

	if !node.IsOnline() {
		return "", 0, fmt.Errorf("%w: node %s", ErrNodeUnavailable, nodeID)
	}

	// Store on provider
	objectID, written, err := node.Provider.StoreChunk(ctx, chunkID, r, size)
	if err != nil {
		return "", 0, fmt.Errorf("failed to store chunk on node %s: %w", nodeID, err)
	}

	// Refresh node metrics to reflect newly written chunk
	_ = node.RefreshCapacity(ctx)

	return objectID, written, nil
}

// GetChunk opens a streaming read handle to a chunk from the designated node.
func (m *StorageManager) GetChunk(ctx context.Context, nodeID string, objectID string) (io.ReadCloser, error) {
	node, err := m.GetNode(nodeID)
	if err != nil {
		return nil, err
	}

	if !node.IsOnline() {
		return nil, fmt.Errorf("%w: node %s", ErrNodeUnavailable, nodeID)
	}

	rc, err := node.Provider.GetChunk(ctx, objectID)
	if err != nil {
		return nil, fmt.Errorf("failed to get chunk from node %s: %w", nodeID, err)
	}

	return rc, nil
}

// DeleteChunk permanently removes a chunk from the designated node.
func (m *StorageManager) DeleteChunk(ctx context.Context, nodeID string, objectID string) error {
	node, err := m.GetNode(nodeID)
	if err != nil {
		return err
	}

	if err := node.Provider.DeleteChunk(ctx, objectID); err != nil {
		return fmt.Errorf("failed to delete chunk from node %s: %w", nodeID, err)
	}

	_ = node.RefreshCapacity(ctx)
	return nil
}

// RunHealthChecks evaluates all nodes and updates their statuses concurrently.
func (m *StorageManager) RunHealthChecks(ctx context.Context) map[string]bool {
	m.mu.RLock()
	nodes := make([]*StorageNode, 0, len(m.nodes))
	for _, n := range m.nodes {
		nodes = append(nodes, n)
	}
	m.mu.RUnlock()

	results := make(map[string]bool)
	var wg sync.WaitGroup
	var mu sync.Mutex

	for _, n := range nodes {
		wg.Add(1)
		go func(target *StorageNode) {
			defer wg.Done()
			checkCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
			defer cancel()

			healthy, _ := target.CheckHealth(checkCtx)
			mu.Lock()
			results[target.ID] = healthy
			mu.Unlock()
		}(n)
	}

	wg.Wait()
	return results
}
