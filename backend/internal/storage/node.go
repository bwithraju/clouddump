package storage

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/bwithraju/clouddump/backend/internal/providers"
)

var (
	ErrNodeNotFound     = errors.New("storage node not found")
	ErrNodeUnavailable  = errors.New("storage node is offline or unavailable")
	ErrNodeFull         = errors.New("storage node has insufficient available capacity")
	ErrDuplicateNodeID  = errors.New("storage node with this ID already registered")
	ErrNoAvailableNodes = errors.New("no online storage nodes available with sufficient capacity")
)

// NodeStatus defines the operational state of a storage node.
type NodeStatus string

const (
	NodeStatusOnline   NodeStatus = "ONLINE"
	NodeStatusOffline  NodeStatus = "OFFLINE"
	NodeStatusDegraded NodeStatus = "DEGRADED"
)

// NodeStats snapshot of node metrics.
type NodeStats struct {
	ID                string     `json:"id"`
	Name              string     `json:"name"`
	ProviderType      string     `json:"provider_type"`
	TotalCapacity     int64      `json:"total_capacity"`
	UsedCapacity      int64      `json:"used_capacity"`
	AvailableCapacity int64      `json:"available_capacity"`
	Status            NodeStatus `json:"status"`
	LastHealthCheck   time.Time  `json:"last_health_check"`
}

// StorageNode encapsulates a StorageProvider with node-level metadata, capacity tracking, and status.
type StorageNode struct {
	ID                string
	Name              string
	Provider          providers.StorageProvider
	TotalCapacity     int64
	UsedCapacity      int64
	AvailableCapacity int64
	Status            NodeStatus
	LastHealthCheck   time.Time
	mu                sync.RWMutex
}

// NewStorageNode constructs a new StorageNode wrapping a provider.
func NewStorageNode(id, name string, provider providers.StorageProvider) (*StorageNode, error) {
	if id == "" {
		return nil, errors.New("node ID cannot be empty")
	}
	if name == "" {
		return nil, errors.New("node name cannot be empty")
	}
	if provider == nil {
		return nil, errors.New("provider cannot be nil")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	total, err := provider.GetTotalCapacity(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get provider total capacity: %w", err)
	}

	avail, err := provider.GetAvailableCapacity(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get provider available capacity: %w", err)
	}

	used := total - avail
	if used < 0 {
		used = 0
	}

	return &StorageNode{
		ID:                id,
		Name:              name,
		Provider:          provider,
		TotalCapacity:     total,
		UsedCapacity:      used,
		AvailableCapacity: avail,
		Status:            NodeStatusOnline,
		LastHealthCheck:   time.Now().UTC(),
	}, nil
}

// RefreshCapacity queries the underlying provider to sync capacity numbers.
func (n *StorageNode) RefreshCapacity(ctx context.Context) error {
	n.mu.Lock()
	defer n.mu.Unlock()

	total, err := n.Provider.GetTotalCapacity(ctx)
	if err != nil {
		return err
	}
	avail, err := n.Provider.GetAvailableCapacity(ctx)
	if err != nil {
		return err
	}

	n.TotalCapacity = total
	n.AvailableCapacity = avail
	n.UsedCapacity = total - avail
	if n.UsedCapacity < 0 {
		n.UsedCapacity = 0
	}
	return nil
}

// CheckHealth probes the provider and updates status and timestamp.
func (n *StorageNode) CheckHealth(ctx context.Context) (bool, error) {
	n.mu.Lock()
	defer n.mu.Unlock()

	n.LastHealthCheck = time.Now().UTC()
	healthy, err := n.Provider.HealthCheck(ctx)
	if err != nil || !healthy {
		n.Status = NodeStatusOffline
		return false, err
	}

	n.Status = NodeStatusOnline
	return true, nil
}

// ReserveCapacity attempts to reserve bytes on the node for an upcoming chunk write.
func (n *StorageNode) ReserveCapacity(bytes int64) bool {
	n.mu.Lock()
	defer n.mu.Unlock()

	if n.Status != NodeStatusOnline {
		return false
	}
	if n.AvailableCapacity < bytes {
		return false
	}

	n.AvailableCapacity -= bytes
	n.UsedCapacity += bytes
	return true
}

// ReleaseCapacity reverts a capacity reservation if write aborted.
func (n *StorageNode) ReleaseCapacity(bytes int64) {
	n.mu.Lock()
	defer n.mu.Unlock()

	n.AvailableCapacity += bytes
	if n.UsedCapacity >= bytes {
		n.UsedCapacity -= bytes
	} else {
		n.UsedCapacity = 0
	}
}

// IsOnline returns whether the node is currently in Online status.
func (n *StorageNode) IsOnline() bool {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.Status == NodeStatusOnline
}

// GetStats returns a thread-safe copy of node metrics.
func (n *StorageNode) GetStats() NodeStats {
	n.mu.RLock()
	defer n.mu.RUnlock()

	return NodeStats{
		ID:                n.ID,
		Name:              n.Name,
		ProviderType:      n.Provider.ProviderType(),
		TotalCapacity:     n.TotalCapacity,
		UsedCapacity:      n.UsedCapacity,
		AvailableCapacity: n.AvailableCapacity,
		Status:            n.Status,
		LastHealthCheck:   n.LastHealthCheck,
	}
}
