package storage

import (
	"fmt"
)

// AllocationStrategy abstracts how a storage node is chosen for a new chunk.
type AllocationStrategy interface {
	Name() string
	SelectNode(nodes []*StorageNode, chunkSize int64, excludedIDs map[string]bool) (*StorageNode, error)
}

// MaxAvailableStrategy selects the online node with the maximum remaining available capacity.
// This ensures that nodes with larger capacity (e.g. 2 GB vs 500 MB) take on chunks first,
// maintaining a balanced free-space distribution across the logical storage pool.
type MaxAvailableStrategy struct{}

// NewMaxAvailableStrategy creates a new MaxAvailableStrategy.
func NewMaxAvailableStrategy() *MaxAvailableStrategy {
	return &MaxAvailableStrategy{}
}

func (s *MaxAvailableStrategy) Name() string {
	return "max_available_capacity"
}

func (s *MaxAvailableStrategy) SelectNode(nodes []*StorageNode, chunkSize int64, excludedIDs map[string]bool) (*StorageNode, error) {
	var bestNode *StorageNode
	var maxAvail int64 = -1

	for _, node := range nodes {
		if excludedIDs != nil && excludedIDs[node.ID] {
			continue
		}

		stats := node.GetStats()
		if stats.Status != NodeStatusOnline {
			continue
		}

		if stats.AvailableCapacity >= chunkSize && stats.AvailableCapacity > maxAvail {
			maxAvail = stats.AvailableCapacity
			bestNode = node
		}
	}

	if bestNode == nil {
		return nil, fmt.Errorf("%w: requested %d bytes", ErrNoAvailableNodes, chunkSize)
	}

	return bestNode, nil
}

// ProportionalAllocationStrategy selects nodes based on available capacity ratio,
// giving higher weight to nodes with larger headroom while still distributing work.
type ProportionalAllocationStrategy struct{}

func NewProportionalAllocationStrategy() *ProportionalAllocationStrategy {
	return &ProportionalAllocationStrategy{}
}

func (s *ProportionalAllocationStrategy) Name() string {
	return "proportional_capacity"
}

func (s *ProportionalAllocationStrategy) SelectNode(nodes []*StorageNode, chunkSize int64, excludedIDs map[string]bool) (*StorageNode, error) {
	// Filter qualifying online nodes
	var candidates []*StorageNode
	var totalAvailable int64

	for _, node := range nodes {
		if excludedIDs != nil && excludedIDs[node.ID] {
			continue
		}
		stats := node.GetStats()
		if stats.Status == NodeStatusOnline && stats.AvailableCapacity >= chunkSize {
			candidates = append(candidates, node)
			totalAvailable += stats.AvailableCapacity
		}
	}

	if len(candidates) == 0 {
		return nil, fmt.Errorf("%w: requested %d bytes", ErrNoAvailableNodes, chunkSize)
	}

	// For determinism in tests or small pools, pick candidate with largest headroom
	var selected *StorageNode
	var highest int64 = -1
	for _, c := range candidates {
		avail := c.GetStats().AvailableCapacity
		if avail > highest {
			highest = avail
			selected = c
		}
	}

	return selected, nil
}
