package database

import (
	"time"
)

// User represents a CloudDump account owner.
type User struct {
	ID           string    `json:"id" db:"id"`
	Email        string    `json:"email" db:"email"`
	PasswordHash string    `json:"-" db:"password_hash"`
	Name         string    `json:"name" db:"name"`
	CreatedAt    time.Time `json:"created_at" db:"created_at"`
	UpdatedAt    time.Time `json:"updated_at" db:"updated_at"`
}

// StorageNodeRecord represents a storage node persisted in PostgreSQL.
type StorageNodeRecord struct {
	ID                string    `json:"id" db:"id"`
	UserID            string    `json:"user_id" db:"user_id"`
	Provider          string    `json:"provider" db:"provider"`
	Name              string    `json:"name" db:"name"`
	TotalCapacity     int64     `json:"total_capacity" db:"total_capacity"`
	UsedCapacity      int64     `json:"used_capacity" db:"used_capacity"`
	AvailableCapacity int64     `json:"available_capacity" db:"available_capacity"`
	Status            string    `json:"status" db:"status"`
	CreatedAt         time.Time `json:"created_at" db:"created_at"`
	UpdatedAt         time.Time `json:"updated_at" db:"updated_at"`
}

// FileRecord represents a file metadata entry persisted in PostgreSQL.
type FileRecord struct {
	ID          string    `json:"id" db:"id"`
	UserID      string    `json:"user_id" db:"user_id"`
	Filename    string    `json:"filename" db:"filename"`
	Size        int64     `json:"size" db:"size"`
	MimeType    string    `json:"mime_type" db:"mime_type"`
	ChunkSize   int64     `json:"chunk_size" db:"chunk_size"`
	TotalChunks int       `json:"total_chunks" db:"total_chunks"`
	Status      string    `json:"status" db:"status"`
	CreatedAt   time.Time `json:"created_at" db:"created_at"`
	UpdatedAt   time.Time `json:"updated_at" db:"updated_at"`
}

// ChunkReplicaRecord represents replica metadata for a chunk on a secondary node.
type ChunkReplicaRecord struct {
	StorageNodeID    string `json:"storage_node_id"`
	ProviderObjectID string `json:"provider_object_id"`
	Status           string `json:"status"`
}

// ChunkRecord represents a chunk metadata entry persisted in PostgreSQL.
type ChunkRecord struct {
	ID               string               `json:"id" db:"id"`
	FileID           string               `json:"file_id" db:"file_id"`
	ChunkIndex       int                  `json:"chunk_index" db:"chunk_index"`
	Size             int64                `json:"size" db:"size"`
	Checksum         string               `json:"checksum" db:"checksum"`
	StorageNodeID    string               `json:"storage_node_id" db:"storage_node_id"`
	ProviderObjectID string               `json:"provider_object_id" db:"provider_object_id"`
	Status           string               `json:"status" db:"status"`
	Replicas         []ChunkReplicaRecord `json:"replicas,omitempty"`
	CreatedAt        time.Time            `json:"created_at" db:"created_at"`
}
