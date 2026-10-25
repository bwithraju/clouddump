package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"time"
)

var (
	ErrNotFound       = errors.New("record not found in database")
	ErrDuplicateKey   = errors.New("duplicate key violation")
	ErrForeignKeyFail = errors.New("foreign key constraint violation")
)

// Repository aggregates all metadata entity operations.
type Repository interface {
	Users() UserRepository
	Nodes() StorageNodeRepository
	Files() FileRepository
	Chunks() ChunkRepository
}

// UserRepository interface.
type UserRepository interface {
	CreateUser(ctx context.Context, u *User) error
	GetUserByID(ctx context.Context, id string) (*User, error)
	GetUserByEmail(ctx context.Context, email string) (*User, error)
}

// StorageNodeRepository interface.
type StorageNodeRepository interface {
	CreateNode(ctx context.Context, node *StorageNodeRecord) error
	GetNode(ctx context.Context, id string) (*StorageNodeRecord, error)
	ListNodesByUser(ctx context.Context, userID string) ([]*StorageNodeRecord, error)
	ListAllNodes(ctx context.Context) ([]*StorageNodeRecord, error)
	UpdateNodeCapacity(ctx context.Context, id string, used, available int64) error
	UpdateNodeStatus(ctx context.Context, id string, status string) error
	DeleteNode(ctx context.Context, id string) error
}

// FileRepository interface.
type FileRepository interface {
	CreateFile(ctx context.Context, file *FileRecord) error
	GetFile(ctx context.Context, id string) (*FileRecord, error)
	ListFilesByUser(ctx context.Context, userID string) ([]*FileRecord, error)
	UpdateFileStatus(ctx context.Context, id string, status string) error
	DeleteFile(ctx context.Context, id string) error
}

// ChunkRepository interface.
type ChunkRepository interface {
	CreateChunk(ctx context.Context, chunk *ChunkRecord) error
	CreateChunksBatch(ctx context.Context, chunks []*ChunkRecord) error
	GetChunk(ctx context.Context, id string) (*ChunkRecord, error)
	ListChunksByFile(ctx context.Context, fileID string) ([]*ChunkRecord, error)
	ListChunksByNode(ctx context.Context, nodeID string) ([]*ChunkRecord, error)
	UpdateChunkStatus(ctx context.Context, id string, status string) error
	DeleteChunksByFile(ctx context.Context, fileID string) error
}

// PostgresRepository implements Repository backed by PostgreSQL.
type PostgresRepository struct {
	db *sql.DB
}

// NewPostgresRepository initializes a PostgresRepository.
func NewPostgresRepository(db *sql.DB) *PostgresRepository {
	return &PostgresRepository{db: db}
}

func (r *PostgresRepository) Users() UserRepository {
	return &postgresUserRepo{db: r.db}
}

func (r *PostgresRepository) Nodes() StorageNodeRepository {
	return &postgresNodeRepo{db: r.db}
}

func (r *PostgresRepository) Files() FileRepository {
	return &postgresFileRepo{db: r.db}
}

func (r *PostgresRepository) Chunks() ChunkRepository {
	return &postgresChunkRepo{db: r.db}
}

// --- Postgres Implementations ---

type postgresUserRepo struct{ db *sql.DB }

func (r *postgresUserRepo) CreateUser(ctx context.Context, u *User) error {
	query := `
		INSERT INTO users (id, email, password_hash, name, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`
	now := time.Now().UTC()
	u.CreatedAt = now
	u.UpdatedAt = now
	_, err := r.db.ExecContext(ctx, query, u.ID, u.Email, u.PasswordHash, u.Name, u.CreatedAt, u.UpdatedAt)
	return err
}

func (r *postgresUserRepo) GetUserByID(ctx context.Context, id string) (*User, error) {
	query := `SELECT id, email, password_hash, name, created_at, updated_at FROM users WHERE id = $1`
	row := r.db.QueryRowContext(ctx, query, id)
	var u User
	if err := row.Scan(&u.ID, &u.Email, &u.PasswordHash, &u.Name, &u.CreatedAt, &u.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &u, nil
}

func (r *postgresUserRepo) GetUserByEmail(ctx context.Context, email string) (*User, error) {
	query := `SELECT id, email, password_hash, name, created_at, updated_at FROM users WHERE email = $1`
	row := r.db.QueryRowContext(ctx, query, email)
	var u User
	if err := row.Scan(&u.ID, &u.Email, &u.PasswordHash, &u.Name, &u.CreatedAt, &u.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &u, nil
}

type postgresNodeRepo struct{ db *sql.DB }

func (r *postgresNodeRepo) CreateNode(ctx context.Context, node *StorageNodeRecord) error {
	query := `
		INSERT INTO storage_nodes (id, user_id, provider, name, total_capacity, used_capacity, available_capacity, status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`
	now := time.Now().UTC()
	node.CreatedAt = now
	node.UpdatedAt = now
	_, err := r.db.ExecContext(ctx, query,
		node.ID, node.UserID, node.Provider, node.Name,
		node.TotalCapacity, node.UsedCapacity, node.AvailableCapacity,
		node.Status, node.CreatedAt, node.UpdatedAt)
	return err
}

func (r *postgresNodeRepo) GetNode(ctx context.Context, id string) (*StorageNodeRecord, error) {
	query := `
		SELECT id, user_id, provider, name, total_capacity, used_capacity, available_capacity, status, created_at, updated_at
		FROM storage_nodes WHERE id = $1
	`
	row := r.db.QueryRowContext(ctx, query, id)
	var n StorageNodeRecord
	if err := row.Scan(&n.ID, &n.UserID, &n.Provider, &n.Name, &n.TotalCapacity, &n.UsedCapacity, &n.AvailableCapacity, &n.Status, &n.CreatedAt, &n.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &n, nil
}

func (r *postgresNodeRepo) ListNodesByUser(ctx context.Context, userID string) ([]*StorageNodeRecord, error) {
	query := `
		SELECT id, user_id, provider, name, total_capacity, used_capacity, available_capacity, status, created_at, updated_at
		FROM storage_nodes WHERE user_id = $1 ORDER BY created_at ASC
	`
	rows, err := r.db.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var nodes []*StorageNodeRecord
	for rows.Next() {
		var n StorageNodeRecord
		if err := rows.Scan(&n.ID, &n.UserID, &n.Provider, &n.Name, &n.TotalCapacity, &n.UsedCapacity, &n.AvailableCapacity, &n.Status, &n.CreatedAt, &n.UpdatedAt); err != nil {
			return nil, err
		}
		nodes = append(nodes, &n)
	}
	return nodes, rows.Err()
}

func (r *postgresNodeRepo) ListAllNodes(ctx context.Context) ([]*StorageNodeRecord, error) {
	query := `
		SELECT id, user_id, provider, name, total_capacity, used_capacity, available_capacity, status, created_at, updated_at
		FROM storage_nodes ORDER BY created_at ASC
	`
	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var nodes []*StorageNodeRecord
	for rows.Next() {
		var n StorageNodeRecord
		if err := rows.Scan(&n.ID, &n.UserID, &n.Provider, &n.Name, &n.TotalCapacity, &n.UsedCapacity, &n.AvailableCapacity, &n.Status, &n.CreatedAt, &n.UpdatedAt); err != nil {
			return nil, err
		}
		nodes = append(nodes, &n)
	}
	return nodes, rows.Err()
}

func (r *postgresNodeRepo) UpdateNodeCapacity(ctx context.Context, id string, used, available int64) error {
	query := `
		UPDATE storage_nodes
		SET used_capacity = $1, available_capacity = $2, updated_at = $3
		WHERE id = $4
	`
	_, err := r.db.ExecContext(ctx, query, used, available, time.Now().UTC(), id)
	return err
}

func (r *postgresNodeRepo) UpdateNodeStatus(ctx context.Context, id string, status string) error {
	query := `UPDATE storage_nodes SET status = $1, updated_at = $2 WHERE id = $3`
	_, err := r.db.ExecContext(ctx, query, status, time.Now().UTC(), id)
	return err
}

func (r *postgresNodeRepo) DeleteNode(ctx context.Context, id string) error {
	query := `DELETE FROM storage_nodes WHERE id = $1`
	_, err := r.db.ExecContext(ctx, query, id)
	return err
}

type postgresFileRepo struct{ db *sql.DB }

func (r *postgresFileRepo) CreateFile(ctx context.Context, file *FileRecord) error {
	query := `
		INSERT INTO files (id, user_id, filename, size, mime_type, chunk_size, total_chunks, status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`
	now := time.Now().UTC()
	file.CreatedAt = now
	file.UpdatedAt = now
	_, err := r.db.ExecContext(ctx, query,
		file.ID, file.UserID, file.Filename, file.Size, file.MimeType,
		file.ChunkSize, file.TotalChunks, file.Status, file.CreatedAt, file.UpdatedAt)
	return err
}

func (r *postgresFileRepo) GetFile(ctx context.Context, id string) (*FileRecord, error) {
	query := `
		SELECT id, user_id, filename, size, mime_type, chunk_size, total_chunks, status, created_at, updated_at
		FROM files WHERE id = $1
	`
	row := r.db.QueryRowContext(ctx, query, id)
	var f FileRecord
	if err := row.Scan(&f.ID, &f.UserID, &f.Filename, &f.Size, &f.MimeType, &f.ChunkSize, &f.TotalChunks, &f.Status, &f.CreatedAt, &f.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &f, nil
}

func (r *postgresFileRepo) ListFilesByUser(ctx context.Context, userID string) ([]*FileRecord, error) {
	query := `
		SELECT id, user_id, filename, size, mime_type, chunk_size, total_chunks, status, created_at, updated_at
		FROM files WHERE user_id = $1 ORDER BY created_at DESC
	`
	rows, err := r.db.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var files []*FileRecord
	for rows.Next() {
		var f FileRecord
		if err := rows.Scan(&f.ID, &f.UserID, &f.Filename, &f.Size, &f.MimeType, &f.ChunkSize, &f.TotalChunks, &f.Status, &f.CreatedAt, &f.UpdatedAt); err != nil {
			return nil, err
		}
		files = append(files, &f)
	}
	return files, rows.Err()
}

func (r *postgresFileRepo) UpdateFileStatus(ctx context.Context, id string, status string) error {
	query := `UPDATE files SET status = $1, updated_at = $2 WHERE id = $3`
	_, err := r.db.ExecContext(ctx, query, status, time.Now().UTC(), id)
	return err
}

func (r *postgresFileRepo) DeleteFile(ctx context.Context, id string) error {
	query := `DELETE FROM files WHERE id = $1`
	_, err := r.db.ExecContext(ctx, query, id)
	return err
}

type postgresChunkRepo struct{ db *sql.DB }

func (r *postgresChunkRepo) CreateChunk(ctx context.Context, chunk *ChunkRecord) error {
	query := `
		INSERT INTO chunks (id, file_id, chunk_index, size, checksum, storage_node_id, provider_object_id, status, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`
	chunk.CreatedAt = time.Now().UTC()
	_, err := r.db.ExecContext(ctx, query,
		chunk.ID, chunk.FileID, chunk.ChunkIndex, chunk.Size, chunk.Checksum,
		chunk.StorageNodeID, chunk.ProviderObjectID, chunk.Status, chunk.CreatedAt)
	return err
}

func (r *postgresChunkRepo) CreateChunksBatch(ctx context.Context, chunks []*ChunkRecord) error {
	if len(chunks) == 0 {
		return nil
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin batch chunks transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO chunks (id, file_id, chunk_index, size, checksum, storage_node_id, provider_object_id, status, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare batch chunk statement: %w", err)
	}
	defer stmt.Close()

	now := time.Now().UTC()
	for _, chk := range chunks {
		chk.CreatedAt = now
		if _, err := stmt.ExecContext(ctx,
			chk.ID, chk.FileID, chk.ChunkIndex, chk.Size, chk.Checksum,
			chk.StorageNodeID, chk.ProviderObjectID, chk.Status, chk.CreatedAt); err != nil {
			return fmt.Errorf("failed inserting chunk %d in batch: %w", chk.ChunkIndex, err)
		}
	}

	return tx.Commit()
}

func (r *postgresChunkRepo) GetChunk(ctx context.Context, id string) (*ChunkRecord, error) {
	query := `
		SELECT id, file_id, chunk_index, size, checksum, storage_node_id, provider_object_id, status, created_at
		FROM chunks WHERE id = $1
	`
	row := r.db.QueryRowContext(ctx, query, id)
	var c ChunkRecord
	if err := row.Scan(&c.ID, &c.FileID, &c.ChunkIndex, &c.Size, &c.Checksum, &c.StorageNodeID, &c.ProviderObjectID, &c.Status, &c.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &c, nil
}

func (r *postgresChunkRepo) ListChunksByFile(ctx context.Context, fileID string) ([]*ChunkRecord, error) {
	query := `
		SELECT id, file_id, chunk_index, size, checksum, storage_node_id, provider_object_id, status, created_at
		FROM chunks WHERE file_id = $1 ORDER BY chunk_index ASC
	`
	rows, err := r.db.QueryContext(ctx, query, fileID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var chunks []*ChunkRecord
	for rows.Next() {
		var c ChunkRecord
		if err := rows.Scan(&c.ID, &c.FileID, &c.ChunkIndex, &c.Size, &c.Checksum, &c.StorageNodeID, &c.ProviderObjectID, &c.Status, &c.CreatedAt); err != nil {
			return nil, err
		}
		chunks = append(chunks, &c)
	}
	return chunks, rows.Err()
}

func (r *postgresChunkRepo) ListChunksByNode(ctx context.Context, nodeID string) ([]*ChunkRecord, error) {
	query := `
		SELECT id, file_id, chunk_index, size, checksum, storage_node_id, provider_object_id, status, created_at
		FROM chunks WHERE storage_node_id = $1 ORDER BY chunk_index ASC
	`
	rows, err := r.db.QueryContext(ctx, query, nodeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var chunks []*ChunkRecord
	for rows.Next() {
		var c ChunkRecord
		if err := rows.Scan(&c.ID, &c.FileID, &c.ChunkIndex, &c.Size, &c.Checksum, &c.StorageNodeID, &c.ProviderObjectID, &c.Status, &c.CreatedAt); err != nil {
			return nil, err
		}
		chunks = append(chunks, &c)
	}
	return chunks, rows.Err()
}

func (r *postgresChunkRepo) UpdateChunkStatus(ctx context.Context, id string, status string) error {
	query := `UPDATE chunks SET status = $1 WHERE id = $2`
	_, err := r.db.ExecContext(ctx, query, status, id)
	return err
}

func (r *postgresChunkRepo) DeleteChunksByFile(ctx context.Context, fileID string) error {
	query := `DELETE FROM chunks WHERE file_id = $1`
	_, err := r.db.ExecContext(ctx, query, fileID)
	return err
}

// --- Memory Repository for Isolation Testing & Fallback ---

// MemoryRepository provides a thread-safe in-memory implementation of Repository.
type MemoryRepository struct {
	users  map[string]*User
	nodes  map[string]*StorageNodeRecord
	files  map[string]*FileRecord
	chunks map[string]*ChunkRecord
	mu     sync.RWMutex
}

// NewMemoryRepository constructs a new MemoryRepository.
func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{
		users:  make(map[string]*User),
		nodes:  make(map[string]*StorageNodeRecord),
		files:  make(map[string]*FileRecord),
		chunks: make(map[string]*ChunkRecord),
	}
}

func (m *MemoryRepository) Users() UserRepository {
	return &memUserRepo{repo: m}
}

func (m *MemoryRepository) Nodes() StorageNodeRepository {
	return &memNodeRepo{repo: m}
}

func (m *MemoryRepository) Files() FileRepository {
	return &memFileRepo{repo: m}
}

func (m *MemoryRepository) Chunks() ChunkRepository {
	return &memChunkRepo{repo: m}
}

type memUserRepo struct{ repo *MemoryRepository }

func (r *memUserRepo) CreateUser(ctx context.Context, u *User) error {
	r.repo.mu.Lock()
	defer r.repo.mu.Unlock()
	for _, existing := range r.repo.users {
		if existing.Email == u.Email {
			return ErrDuplicateKey
		}
	}
	now := time.Now().UTC()
	u.CreatedAt = now
	u.UpdatedAt = now
	r.repo.users[u.ID] = u
	return nil
}

func (r *memUserRepo) GetUserByID(ctx context.Context, id string) (*User, error) {
	r.repo.mu.RLock()
	defer r.repo.mu.RUnlock()
	u, ok := r.repo.users[id]
	if !ok {
		return nil, ErrNotFound
	}
	return u, nil
}

func (r *memUserRepo) GetUserByEmail(ctx context.Context, email string) (*User, error) {
	r.repo.mu.RLock()
	defer r.repo.mu.RUnlock()
	for _, u := range r.repo.users {
		if u.Email == email {
			return u, nil
		}
	}
	return nil, ErrNotFound
}

type memNodeRepo struct{ repo *MemoryRepository }

func (r *memNodeRepo) CreateNode(ctx context.Context, node *StorageNodeRecord) error {
	r.repo.mu.Lock()
	defer r.repo.mu.Unlock()
	if _, ok := r.repo.nodes[node.ID]; ok {
		return ErrDuplicateKey
	}
	now := time.Now().UTC()
	node.CreatedAt = now
	node.UpdatedAt = now
	r.repo.nodes[node.ID] = node
	return nil
}

func (r *memNodeRepo) GetNode(ctx context.Context, id string) (*StorageNodeRecord, error) {
	r.repo.mu.RLock()
	defer r.repo.mu.RUnlock()
	n, ok := r.repo.nodes[id]
	if !ok {
		return nil, ErrNotFound
	}
	return n, nil
}

func (r *memNodeRepo) ListNodesByUser(ctx context.Context, userID string) ([]*StorageNodeRecord, error) {
	r.repo.mu.RLock()
	defer r.repo.mu.RUnlock()
	var list []*StorageNodeRecord
	for _, n := range r.repo.nodes {
		if n.UserID == userID {
			list = append(list, n)
		}
	}
	return list, nil
}

func (r *memNodeRepo) ListAllNodes(ctx context.Context) ([]*StorageNodeRecord, error) {
	r.repo.mu.RLock()
	defer r.repo.mu.RUnlock()
	var list []*StorageNodeRecord
	for _, n := range r.repo.nodes {
		list = append(list, n)
	}
	return list, nil
}

func (r *memNodeRepo) UpdateNodeCapacity(ctx context.Context, id string, used, available int64) error {
	r.repo.mu.Lock()
	defer r.repo.mu.Unlock()
	n, ok := r.repo.nodes[id]
	if !ok {
		return ErrNotFound
	}
	n.UsedCapacity = used
	n.AvailableCapacity = available
	n.UpdatedAt = time.Now().UTC()
	return nil
}

func (r *memNodeRepo) UpdateNodeStatus(ctx context.Context, id string, status string) error {
	r.repo.mu.Lock()
	defer r.repo.mu.Unlock()
	n, ok := r.repo.nodes[id]
	if !ok {
		return ErrNotFound
	}
	n.Status = status
	n.UpdatedAt = time.Now().UTC()
	return nil
}

func (r *memNodeRepo) DeleteNode(ctx context.Context, id string) error {
	r.repo.mu.Lock()
	defer r.repo.mu.Unlock()
	if _, ok := r.repo.nodes[id]; !ok {
		return ErrNotFound
	}
	delete(r.repo.nodes, id)
	return nil
}

type memFileRepo struct{ repo *MemoryRepository }

func (r *memFileRepo) CreateFile(ctx context.Context, file *FileRecord) error {
	r.repo.mu.Lock()
	defer r.repo.mu.Unlock()
	if _, ok := r.repo.files[file.ID]; ok {
		return ErrDuplicateKey
	}
	now := time.Now().UTC()
	file.CreatedAt = now
	file.UpdatedAt = now
	r.repo.files[file.ID] = file
	return nil
}

func (r *memFileRepo) GetFile(ctx context.Context, id string) (*FileRecord, error) {
	r.repo.mu.RLock()
	defer r.repo.mu.RUnlock()
	f, ok := r.repo.files[id]
	if !ok {
		return nil, ErrNotFound
	}
	return f, nil
}

func (r *memFileRepo) ListFilesByUser(ctx context.Context, userID string) ([]*FileRecord, error) {
	r.repo.mu.RLock()
	defer r.repo.mu.RUnlock()
	var list []*FileRecord
	for _, f := range r.repo.files {
		if f.UserID == userID {
			list = append(list, f)
		}
	}
	return list, nil
}

func (r *memFileRepo) UpdateFileStatus(ctx context.Context, id string, status string) error {
	r.repo.mu.Lock()
	defer r.repo.mu.Unlock()
	f, ok := r.repo.files[id]
	if !ok {
		return ErrNotFound
	}
	f.Status = status
	f.UpdatedAt = time.Now().UTC()
	return nil
}

func (r *memFileRepo) DeleteFile(ctx context.Context, id string) error {
	r.repo.mu.Lock()
	defer r.repo.mu.Unlock()
	if _, ok := r.repo.files[id]; !ok {
		return ErrNotFound
	}
	delete(r.repo.files, id)
	return nil
}

type memChunkRepo struct{ repo *MemoryRepository }

func (r *memChunkRepo) CreateChunk(ctx context.Context, chunk *ChunkRecord) error {
	r.repo.mu.Lock()
	defer r.repo.mu.Unlock()
	chunk.CreatedAt = time.Now().UTC()
	r.repo.chunks[chunk.ID] = chunk
	return nil
}

func (r *memChunkRepo) CreateChunksBatch(ctx context.Context, chunks []*ChunkRecord) error {
	r.repo.mu.Lock()
	defer r.repo.mu.Unlock()
	now := time.Now().UTC()
	for _, chk := range chunks {
		chk.CreatedAt = now
		r.repo.chunks[chk.ID] = chk
	}
	return nil
}

func (r *memChunkRepo) GetChunk(ctx context.Context, id string) (*ChunkRecord, error) {
	r.repo.mu.RLock()
	defer r.repo.mu.RUnlock()
	c, ok := r.repo.chunks[id]
	if !ok {
		return nil, ErrNotFound
	}
	return c, nil
}

func (r *memChunkRepo) ListChunksByFile(ctx context.Context, fileID string) ([]*ChunkRecord, error) {
	r.repo.mu.RLock()
	defer r.repo.mu.RUnlock()
	var list []*ChunkRecord
	for _, c := range r.repo.chunks {
		if c.FileID == fileID {
			list = append(list, c)
		}
	}
	return list, nil
}

func (r *memChunkRepo) ListChunksByNode(ctx context.Context, nodeID string) ([]*ChunkRecord, error) {
	r.repo.mu.RLock()
	defer r.repo.mu.RUnlock()
	var list []*ChunkRecord
	for _, c := range r.repo.chunks {
		if c.StorageNodeID == nodeID {
			list = append(list, c)
		}
	}
	return list, nil
}

func (r *memChunkRepo) UpdateChunkStatus(ctx context.Context, id string, status string) error {
	r.repo.mu.Lock()
	defer r.repo.mu.Unlock()
	c, ok := r.repo.chunks[id]
	if !ok {
		return ErrNotFound
	}
	c.Status = status
	return nil
}

func (r *memChunkRepo) DeleteChunksByFile(ctx context.Context, fileID string) error {
	r.repo.mu.Lock()
	defer r.repo.mu.Unlock()
	for id, c := range r.repo.chunks {
		if c.FileID == fileID {
			delete(r.repo.chunks, id)
		}
	}
	return nil
}
