package database

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "github.com/lib/pq"
)

// Config holds PostgreSQL connection parameters.
type Config struct {
	Host     string
	Port     int
	User     string
	Password string
	DBName   string
	SSLMode  string
}

// ConnectionString formats the lib/pq PostgreSQL connection string.
func (c *Config) ConnectionString() string {
	ssl := c.SSLMode
	if ssl == "" {
		ssl = "disable"
	}
	return fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		c.Host, c.Port, c.User, c.Password, c.DBName, ssl)
}

// Connect initializes and returns a pooled PostgreSQL database connection.
func Connect(cfg Config) (*sql.DB, error) {
	connStr := cfg.ConnectionString()
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to open postgres connection: %w", err)
	}

	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to ping postgres: %w", err)
	}

	return db, nil
}

// SchemaDDL defines the core schema creation script for PostgreSQL.
const SchemaDDL = `
CREATE TABLE IF NOT EXISTS users (
    id VARCHAR(64) PRIMARY KEY,
    email VARCHAR(255) UNIQUE NOT NULL,
    password_hash VARCHAR(255) NOT NULL,
    name VARCHAR(255) NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS storage_nodes (
    id VARCHAR(64) PRIMARY KEY,
    user_id VARCHAR(64) REFERENCES users(id) ON DELETE CASCADE,
    provider VARCHAR(64) NOT NULL,
    name VARCHAR(255) NOT NULL,
    total_capacity BIGINT NOT NULL CHECK (total_capacity >= 0),
    used_capacity BIGINT NOT NULL DEFAULT 0 CHECK (used_capacity >= 0),
    available_capacity BIGINT NOT NULL CHECK (available_capacity >= 0),
    status VARCHAR(32) NOT NULL DEFAULT 'ONLINE',
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_storage_nodes_user_id ON storage_nodes(user_id);
CREATE INDEX IF NOT EXISTS idx_storage_nodes_status ON storage_nodes(status);

CREATE TABLE IF NOT EXISTS files (
    id VARCHAR(64) PRIMARY KEY,
    user_id VARCHAR(64) REFERENCES users(id) ON DELETE CASCADE,
    filename VARCHAR(255) NOT NULL,
    size BIGINT NOT NULL CHECK (size >= 0),
    mime_type VARCHAR(128) NOT NULL DEFAULT 'application/octet-stream',
    chunk_size BIGINT NOT NULL CHECK (chunk_size > 0),
    total_chunks INT NOT NULL CHECK (total_chunks >= 0),
    status VARCHAR(32) NOT NULL DEFAULT 'UPLOADING',
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_files_user_id ON files(user_id);
CREATE INDEX IF NOT EXISTS idx_files_status ON files(status);
CREATE INDEX IF NOT EXISTS idx_files_created_at ON files(created_at DESC);

CREATE TABLE IF NOT EXISTS chunks (
    id VARCHAR(128) PRIMARY KEY,
    file_id VARCHAR(64) NOT NULL REFERENCES files(id) ON DELETE CASCADE,
    chunk_index INT NOT NULL CHECK (chunk_index >= 0),
    size BIGINT NOT NULL CHECK (size >= 0),
    checksum VARCHAR(64) NOT NULL,
    storage_node_id VARCHAR(64) NOT NULL REFERENCES storage_nodes(id) ON DELETE RESTRICT,
    provider_object_id VARCHAR(255) NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'PENDING',
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT uq_file_chunk_index UNIQUE (file_id, chunk_index)
);

CREATE INDEX IF NOT EXISTS idx_chunks_file_id ON chunks(file_id);
CREATE INDEX IF NOT EXISTS idx_chunks_storage_node_id ON chunks(storage_node_id);
CREATE INDEX IF NOT EXISTS idx_chunks_status ON chunks(status);
`

// Migrate runs the initial database schema DDL inside a transaction.
func Migrate(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin migration transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	if _, err := tx.ExecContext(ctx, SchemaDDL); err != nil {
		return fmt.Errorf("failed to execute migration DDL: %w", err)
	}

	return tx.Commit()
}
