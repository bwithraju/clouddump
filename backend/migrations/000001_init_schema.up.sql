-- CloudDump PostgreSQL Initial Schema Migration
-- Up Migration: users, storage_nodes, files, chunks, chunk_replicas, oauth_tokens

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

CREATE TABLE IF NOT EXISTS chunk_replicas (
    id VARCHAR(128) PRIMARY KEY,
    chunk_id VARCHAR(128) NOT NULL REFERENCES chunks(id) ON DELETE CASCADE,
    storage_node_id VARCHAR(64) NOT NULL REFERENCES storage_nodes(id) ON DELETE RESTRICT,
    provider_object_id VARCHAR(255) NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'STORED',
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_chunk_replicas_chunk_id ON chunk_replicas(chunk_id);
CREATE INDEX IF NOT EXISTS idx_chunk_replicas_storage_node_id ON chunk_replicas(storage_node_id);

CREATE TABLE IF NOT EXISTS oauth_tokens (
    id VARCHAR(64) PRIMARY KEY,
    user_id VARCHAR(64) NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    provider VARCHAR(64) NOT NULL,
    access_token TEXT NOT NULL,
    refresh_token TEXT NOT NULL,
    token_type VARCHAR(64) NOT NULL DEFAULT 'Bearer',
    expiry TIMESTAMP WITH TIME ZONE NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT uq_user_provider UNIQUE (user_id, provider)
);

CREATE INDEX IF NOT EXISTS idx_oauth_tokens_user_id ON oauth_tokens(user_id);
