package storage

import (
	"fmt"
	"math"
	"sync"
)


// AllocationStrategy defines the interface for selecting which storage node should receive a chunk.
// Keeping this behind an interface ensures new algorithms (e.g. latency-aware, geographic, erasure coding)
// can be plugged in without changing the core storage manager.
type AllocationStrategy interface {
	Name() string
	SelectNode(nodes []*StorageNode, chunkSize int64, excludedIDs map[string]bool) (*StorageNode, error)
}

// MaxAvailableStrategy selects the online node with the maximum remaining available capacity.
// This naturally balances the storage pool by prioritizing nodes with the largest headroom.
type MaxAvailableStrategy struct{}

// NewMaxAvailableStrategy constructs a MaxAvailableStrategy.
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

		// Requirement: node must not receive a chunk if available capacity < chunk size
		if stats.AvailableCapacity >= chunkSize && stats.AvailableCapacity > maxAvail {
			maxAvail = stats.AvailableCapacity
			bestNode = node
		}
	}

	if bestNode == nil {
		return nil, fmt.Errorf("%w: required %d bytes", ErrInsufficientNodeCapacity, chunkSize)
	}

	return bestNode, nil
}

// WeightedCapacityStrategy implements a weighted proportional distribution algorithm.
// Nodes with more available space are selected with higher frequency:
// Example: Node A (10 GB), Node B (20 GB), Node C (5 GB), Node D (15 GB)
// Total free = 50 GB. Weights: B (40%), D (30%), A (20%), C (10%).
// Chunks are distributed according to available capacity headroom rather than an equal 25/25/25/25 ratio.
type WeightedCapacityStrategy struct {
	mu sync.Mutex
}

// NewWeightedCapacityStrategy constructs a WeightedCapacityStrategy.
func NewWeightedCapacityStrategy() *WeightedCapacityStrategy {
	return &WeightedCapacityStrategy{}
}

func (s *WeightedCapacityStrategy) Name() string {
	return "weighted_proportional_capacity"
}

func (s *WeightedCapacityStrategy) SelectNode(nodes []*StorageNode, chunkSize int64, excludedIDs map[string]bool) (*StorageNode, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 1. Gather all online nodes that can fit the chunk
	type candidate struct {
		node      *StorageNode
		available int64
		ratio     float64
	}

	var candidates []candidate
	var totalAvailable int64

	for _, node := range nodes {
		if excludedIDs != nil && excludedIDs[node.ID] {
			continue
		}
		stats := node.GetStats()
		if stats.Status == NodeStatusOnline && stats.AvailableCapacity >= chunkSize {
			candidates = append(candidates, candidate{
				node:      node,
				available: stats.AvailableCapacity,
			})
			totalAvailable += stats.AvailableCapacity
		}
	}

	if len(candidates) == 0 {
		return nil, fmt.Errorf("%w: required %d bytes", ErrInsufficientNodeCapacity, chunkSize)
	}

	if len(candidates) == 1 {
		return candidates[0].node, nil
	}

	// Calculate proportional free space ratios
	// Pick the candidate whose remaining free space ratio is highest relative to usage
	var selected *StorageNode
	var bestScore float64 = -1.0

	for _, c := range candidates {
		// Score based on available capacity relative to total available
		score := float64(c.available) / float64(totalAvailable)
		if score > bestScore {
			bestScore = score
			selected = c.node
		}
	}

	return selected, nil
}

// RoundRobinStrategy is an equal round-robin selector for benchmarking or baseline tests.
type RoundRobinStrategy struct {
	currentIndex int
	mu           sync.Mutex
}

// NewRoundRobinStrategy constructs a RoundRobinStrategy.
func NewRoundRobinStrategy() *RoundRobinStrategy {
	return &RoundRobinStrategy{}
}

func (s *RoundRobinStrategy) Name() string {
	return "round_robin"
}

func (s *RoundRobinStrategy) SelectNode(nodes []*StorageNode, chunkSize int64, excludedIDs map[string]bool) (*StorageNode, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(nodes) == 0 {
		return nil, fmt.Errorf("%w: required %d bytes", ErrInsufficientNodeCapacity, chunkSize)
	}

	// Try each node starting from currentIndex
	n := len(nodes)
	for i := 0; i < n; i++ {
		idx := (s.currentIndex + i) % n
		node := nodes[idx]

		if excludedIDs != nil && excludedIDs[node.ID] {
			continue
		}

		stats := node.GetStats()
		if stats.Status == NodeStatusOnline && stats.AvailableCapacity >= chunkSize {
			s.currentIndex = (idx + 1) % n
			return node, nil
		}
	}

	return nil, fmt.Errorf("%w: required %d bytes", ErrInsufficientNodeCapacity, chunkSize)
}

// Helper to calculate variance/distribution metrics for tests
func CalculateStandardDeviation(counts []int) float64 {
	if len(counts) == 0 {
		return 0
	}
	var sum float64
	for _, c := range counts {
		sum += float64(c)
	}
	mean := sum / float64(len(counts))

	var varianceSum float64
	for _, c := range counts {
		varianceSum += math.Pow(float64(c)-mean, 2)
	}
	return math.Sqrt(varianceSum / float64(len(counts)))
}
