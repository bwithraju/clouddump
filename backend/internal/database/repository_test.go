package database

import (
	"context"
	"strings"
	"testing"
)

// Test 1: Schema DDL definitions, tables, constraints, foreign keys, and indexes
func TestSchemaDDL_SyntaxAndStructure(t *testing.T) {
	requiredTables := []string{
		"CREATE TABLE IF NOT EXISTS users",
		"CREATE TABLE IF NOT EXISTS storage_nodes",
		"CREATE TABLE IF NOT EXISTS files",
		"CREATE TABLE IF NOT EXISTS chunks",
	}

	for _, tbl := range requiredTables {
		if !strings.Contains(SchemaDDL, tbl) {
			t.Errorf("SchemaDDL missing table statement: %s", tbl)
		}
	}

	requiredConstraints := []string{
		"REFERENCES users(id)",
		"REFERENCES files(id)",
		"REFERENCES storage_nodes(id)",
		"CONSTRAINT uq_file_chunk_index UNIQUE (file_id, chunk_index)",
		"CHECK (size >= 0)",
		"CHECK (total_capacity >= 0)",
	}

	for _, c := range requiredConstraints {
		if !strings.Contains(SchemaDDL, c) {
			t.Errorf("SchemaDDL missing constraint: %s", c)
		}
	}

	requiredIndexes := []string{
		"idx_storage_nodes_user_id",
		"idx_files_user_id",
		"idx_chunks_file_id",
		"idx_chunks_storage_node_id",
	}

	for _, idx := range requiredIndexes {
		if !strings.Contains(SchemaDDL, idx) {
			t.Errorf("SchemaDDL missing index: %s", idx)
		}
	}
}

// Test 2: Connection string formatting
func TestConfig_ConnectionString(t *testing.T) {
	cfg := Config{
		Host:     "localhost",
		Port:     5432,
		User:     "postgres",
		Password: "secretpassword",
		DBName:   "clouddump",
		SSLMode:  "disable",
	}

	connStr := cfg.ConnectionString()
	expected := "host=localhost port=5432 user=postgres password=secretpassword dbname=clouddump sslmode=disable"
	if connStr != expected {
		t.Errorf("expected connStr %q, got %q", expected, connStr)
	}

	// Default SSL mode fallback
	cfgNoSSL := Config{Host: "db", Port: 5432, User: "u", Password: "p", DBName: "d"}
	if !strings.Contains(cfgNoSSL.ConnectionString(), "sslmode=disable") {
		t.Errorf("expected default sslmode=disable, got %s", cfgNoSSL.ConnectionString())
	}
}

// Test 3: User repository lifecycle
func TestRepository_UserLifecycle(t *testing.T) {
	repo := NewMemoryRepository()
	ctx := context.Background()

	u := &User{
		ID:           "usr_001",
		Email:        "student@college.edu",
		PasswordHash: "$2a$10$hashedpasswordsample",
		Name:         "Alice Chen",
	}

	// Create user
	if err := repo.Users().CreateUser(ctx, u); err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}

	// Duplicate email rejection
	uDup := &User{
		ID:           "usr_002",
		Email:        "student@college.edu",
		PasswordHash: "anotherhash",
		Name:         "Duplicate Alice",
	}
	if err := repo.Users().CreateUser(ctx, uDup); err == nil {
		t.Fatalf("expected duplicate email to be rejected, got nil")
	}

	// Get by ID
	byID, err := repo.Users().GetUserByID(ctx, "usr_001")
	if err != nil {
		t.Fatalf("GetUserByID failed: %v", err)
	}
	if byID.Email != u.Email || byID.Name != u.Name {
		t.Errorf("user data mismatch: %+v", byID)
	}

	// Get by Email
	byEmail, err := repo.Users().GetUserByEmail(ctx, "student@college.edu")
	if err != nil {
		t.Fatalf("GetUserByEmail failed: %v", err)
	}
	if byEmail.ID != "usr_001" {
		t.Errorf("expected user ID usr_001, got %s", byEmail.ID)
	}
}

// Test 4: Storage node repository lifecycle
func TestRepository_StorageNodeLifecycle(t *testing.T) {
	repo := NewMemoryRepository()
	ctx := context.Background()

	node := &StorageNodeRecord{
		ID:                "node_test_1",
		UserID:            "usr_001",
		Provider:          "local",
		Name:              "Node Alpha (1GB)",
		TotalCapacity:     1000 * 1024 * 1024,
		UsedCapacity:      0,
		AvailableCapacity: 1000 * 1024 * 1024,
		Status:            "ONLINE",
	}

	// Create
	if err := repo.Nodes().CreateNode(ctx, node); err != nil {
		t.Fatalf("CreateNode failed: %v", err)
	}

	// Get
	fetched, err := repo.Nodes().GetNode(ctx, "node_test_1")
	if err != nil {
		t.Fatalf("GetNode failed: %v", err)
	}
	if fetched.TotalCapacity != node.TotalCapacity {
		t.Errorf("expected total capacity %d, got %d", node.TotalCapacity, fetched.TotalCapacity)
	}

	// Update capacity
	if err := repo.Nodes().UpdateNodeCapacity(ctx, "node_test_1", 200*1024*1024, 800*1024*1024); err != nil {
		t.Fatalf("UpdateNodeCapacity failed: %v", err)
	}
	updated, _ := repo.Nodes().GetNode(ctx, "node_test_1")
	if updated.UsedCapacity != 200*1024*1024 || updated.AvailableCapacity != 800*1024*1024 {
		t.Errorf("capacity update mismatch: used=%d, avail=%d", updated.UsedCapacity, updated.AvailableCapacity)
	}

	// Update status
	if err := repo.Nodes().UpdateNodeStatus(ctx, "node_test_1", "OFFLINE"); err != nil {
		t.Fatalf("UpdateNodeStatus failed: %v", err)
	}
	offlineNode, _ := repo.Nodes().GetNode(ctx, "node_test_1")
	if offlineNode.Status != "OFFLINE" {
		t.Errorf("expected status OFFLINE, got %s", offlineNode.Status)
	}

	// List
	nodes, err := repo.Nodes().ListNodesByUser(ctx, "usr_001")
	if err != nil || len(nodes) != 1 {
		t.Errorf("expected 1 node, got %d (err: %v)", len(nodes), err)
	}

	// Delete
	if err := repo.Nodes().DeleteNode(ctx, "node_test_1"); err != nil {
		t.Fatalf("DeleteNode failed: %v", err)
	}
	_, err = repo.Nodes().GetNode(ctx, "node_test_1")
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound after deletion, got %v", err)
	}
}

// Test 5: File & Chunk repositories lifecycle and batch creation
func TestRepository_FileAndChunkLifecycle(t *testing.T) {
	repo := NewMemoryRepository()
	ctx := context.Background()

	file := &FileRecord{
		ID:          "file_rec_001",
		UserID:      "usr_001",
		Filename:    "lecture_video.mp4",
		Size:        500 * 1024 * 1024, // 500 MB
		MimeType:    "video/mp4",
		ChunkSize:   250 * 1024 * 1024, // 250 MB
		TotalChunks: 2,
		Status:      "UPLOADING",
	}

	// Create file
	if err := repo.Files().CreateFile(ctx, file); err != nil {
		t.Fatalf("CreateFile failed: %v", err)
	}

	// Batch chunks creation
	chunks := []*ChunkRecord{
		{
			ID:               "chk_rec_001_0",
			FileID:           file.ID,
			ChunkIndex:       0,
			Size:             250 * 1024 * 1024,
			Checksum:         "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
			StorageNodeID:    "node1",
			ProviderObjectID: "chk_rec_001_0.chunk",
			Status:           "STORED",
		},
		{
			ID:               "chk_rec_001_1",
			FileID:           file.ID,
			ChunkIndex:       1,
			Size:             250 * 1024 * 1024,
			Checksum:         "ca978112ca1bbdcafac231b39a23dc4da786081496140970e0e158c6c447f399",
			StorageNodeID:    "node2",
			ProviderObjectID: "chk_rec_001_1.chunk",
			Status:           "STORED",
		},
	}

	if err := repo.Chunks().CreateChunksBatch(ctx, chunks); err != nil {
		t.Fatalf("CreateChunksBatch failed: %v", err)
	}

	// List chunks by file
	storedChunks, err := repo.Chunks().ListChunksByFile(ctx, file.ID)
	if err != nil {
		t.Fatalf("ListChunksByFile failed: %v", err)
	}
	if len(storedChunks) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(storedChunks))
	}
	if storedChunks[0].ChunkIndex != 0 || storedChunks[1].ChunkIndex != 1 {
		t.Errorf("chunk indexing out of order: %+v", storedChunks)
	}

	// Update chunk status to CORRUPTED
	if err := repo.Chunks().UpdateChunkStatus(ctx, "chk_rec_001_1", "CORRUPTED"); err != nil {
		t.Fatalf("UpdateChunkStatus failed: %v", err)
	}
	corruptChk, _ := repo.Chunks().GetChunk(ctx, "chk_rec_001_1")
	if corruptChk.Status != "CORRUPTED" {
		t.Errorf("expected status CORRUPTED, got %s", corruptChk.Status)
	}

	// Update file status to COMPLETED
	if err := repo.Files().UpdateFileStatus(ctx, file.ID, "COMPLETED"); err != nil {
		t.Fatalf("UpdateFileStatus failed: %v", err)
	}
	completedFile, _ := repo.Files().GetFile(ctx, file.ID)
	if completedFile.Status != "COMPLETED" {
		t.Errorf("expected file status COMPLETED, got %s", completedFile.Status)
	}

	// Delete file & associated chunks
	if err := repo.Chunks().DeleteChunksByFile(ctx, file.ID); err != nil {
		t.Fatalf("DeleteChunksByFile failed: %v", err)
	}
	if err := repo.Files().DeleteFile(ctx, file.ID); err != nil {
		t.Fatalf("DeleteFile failed: %v", err)
	}

	remainingChunks, _ := repo.Chunks().ListChunksByFile(ctx, file.ID)
	if len(remainingChunks) != 0 {
		t.Errorf("expected 0 chunks remaining, got %d", len(remainingChunks))
	}
}
