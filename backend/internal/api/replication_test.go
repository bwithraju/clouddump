package api

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/bwithraju/clouddump/backend/internal/storage"
)

func uploadMultipartFileWithReplication(t *testing.T, router http.Handler, filename string, data []byte, replicationFactor int) string {
	body := new(bytes.Buffer)
	writer := multipart.NewWriter(body)

	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		t.Fatalf("failed to create multipart form file: %v", err)
	}
	if _, err := part.Write(data); err != nil {
		t.Fatalf("failed to write multipart data: %v", err)
	}
	_ = writer.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/files/upload", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("X-Filename", filename)
	req.Header.Set("X-File-Size", strconv.Itoa(len(data)))
	if replicationFactor > 0 {
		req.Header.Set("X-Replication-Factor", strconv.Itoa(replicationFactor))
	}

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("upload failed with code %d: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		ID                string `json:"id"`
		ReplicationFactor int    `json:"replication_factor"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode upload response: %v", err)
	}

	if replicationFactor > 0 && resp.ReplicationFactor != replicationFactor {
		t.Errorf("expected replication factor %d, got %d", replicationFactor, resp.ReplicationFactor)
	}

	return resp.ID
}

// Test: Full API lifecycle of replicated chunk upload and download from replica
func TestReplicationAPI_FailoverAndReconstruction(t *testing.T) {
	chunkSize := int64(100 * 1024) // 100 KB
	router, mgr, repo, dirs := setupFailureTestEnvironment(t, chunkSize)
	defer cleanupAPITestDirs(dirs)

	// 250 KB file (3 chunks: 100KB, 100KB, 50KB)
	fileData := make([]byte, 250*1024)
	_, _ = io.ReadFull(rand.Reader, fileData)
	h := sha256.Sum256(fileData)
	expectedSHA256 := hex.EncodeToString(h[:])

	// 1. Upload file with factor 2
	fileID := uploadMultipartFileWithReplication(t, router, "replicated_archive.tar", fileData, 2)

	// 2. Verify metadata tracks primary and replicas on distinct nodes
	reqDetails := httptest.NewRequest(http.MethodGet, "/api/files/"+fileID, nil)
	recDetails := httptest.NewRecorder()
	router.ServeHTTP(recDetails, reqDetails)
	if recDetails.Code != http.StatusOK {
		t.Fatalf("get file details failed: %d", recDetails.Code)
	}

	var detailsResp struct {
		File   map[string]interface{}
		Chunks []struct {
			ID               string `json:"id"`
			ChunkIndex       int    `json:"chunk_index"`
			StorageNodeID    string `json:"storage_node_id"`
			ProviderObjectID string `json:"provider_object_id"`
			Replicas         []struct {
				StorageNodeID    string `json:"storage_node_id"`
				ProviderObjectID string `json:"provider_object_id"`
			} `json:"replicas"`
		} `json:"chunks"`
		Distribution map[string]int `json:"distribution"`
	}
	if err := json.NewDecoder(recDetails.Body).Decode(&detailsResp); err != nil {
		t.Fatalf("failed decoding details: %v", err)
	}

	if len(detailsResp.Chunks) != 3 {
		t.Fatalf("expected 3 chunks, got %d", len(detailsResp.Chunks))
	}

	chunk0 := detailsResp.Chunks[0]
	if len(chunk0.Replicas) != 1 {
		t.Fatalf("expected 1 replica for chunk 0, got %d", len(chunk0.Replicas))
	}
	primaryNode := chunk0.StorageNodeID
	replicaNode := chunk0.Replicas[0].StorageNodeID

	if primaryNode == replicaNode {
		t.Fatalf("primary node (%s) must not match replica node (%s)", primaryNode, replicaNode)
	}

	// 3. Take primary node OFFLINE
	failPayload, _ := json.Marshal(SimulateFailureRequest{Offline: true})
	reqFail := httptest.NewRequest(http.MethodPost, "/api/nodes/"+primaryNode+"/simulate-failure", bytes.NewReader(failPayload))
	recFail := httptest.NewRecorder()
	router.ServeHTTP(recFail, reqFail)
	if recFail.Code != http.StatusOK {
		t.Fatalf("failed to take primary node offline: %d", recFail.Code)
	}

	// Verify primary node is offline in pool stats
	reqStats := httptest.NewRequest(http.MethodGet, "/api/nodes/stats", nil)
	recStats := httptest.NewRecorder()
	router.ServeHTTP(recStats, reqStats)
	var stats storage.PoolStats
	_ = json.NewDecoder(recStats.Body).Decode(&stats)
	if stats.OfflineNodes != 1 {
		t.Errorf("expected 1 offline node, got %d", stats.OfflineNodes)
	}

	// 4. Download file while primary node is offline
	// Because chunk 0 has its replica on an online node, the file download MUST succeed!
	reqDl := httptest.NewRequest(http.MethodGet, "/api/files/"+fileID+"/download", nil)
	recDl := httptest.NewRecorder()
	router.ServeHTTP(recDl, reqDl)

	if recDl.Code != http.StatusOK {
		t.Fatalf("expected 200 OK downloading file from replica, got %d: %s", recDl.Code, recDl.Body.String())
	}

	// 5. Verify byte-for-byte reconstruction and SHA-256 match
	downloadedBytes := recDl.Body.Bytes()
	if !bytes.Equal(downloadedBytes, fileData) {
		t.Fatalf("downloaded data mismatch: expected %d bytes, got %d bytes", len(fileData), len(downloadedBytes))
	}

	dlHash := sha256.Sum256(downloadedBytes)
	dlSHA256 := hex.EncodeToString(dlHash[:])
	if dlSHA256 != expectedSHA256 {
		t.Fatalf("checksum mismatch: expected %s, got %s", expectedSHA256, dlSHA256)
	}

	// 6. Now take replica node OFFLINE as well (both copies offline)
	reqFailRep := httptest.NewRequest(http.MethodPost, "/api/nodes/"+replicaNode+"/simulate-failure", bytes.NewReader(failPayload))
	recFailRep := httptest.NewRecorder()
	router.ServeHTTP(recFailRep, reqFailRep)
	if recFailRep.Code != http.StatusOK {
		t.Fatalf("failed to take replica node offline: %d", recFailRep.Code)
	}

	// Download must now fail with 503 Service Unavailable (all copies of chunk 0 are offline)
	recDlBothOffline := httptest.NewRecorder()
	router.ServeHTTP(recDlBothOffline, reqDl)
	if recDlBothOffline.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 when all chunk copies are offline, got %d", recDlBothOffline.Code)
	}

	// 7. Restore replica node back ONLINE
	restorePayload, _ := json.Marshal(SimulateFailureRequest{Offline: false})
	reqRestore := httptest.NewRequest(http.MethodPost, "/api/nodes/"+replicaNode+"/simulate-failure", bytes.NewReader(restorePayload))
	recRestore := httptest.NewRecorder()
	router.ServeHTTP(recRestore, reqRestore)
	if recRestore.Code != http.StatusOK {
		t.Fatalf("failed to restore replica node: %d", recRestore.Code)
	}

	// Download should succeed again from restored replica
	recDlRestored := httptest.NewRecorder()
	router.ServeHTTP(recDlRestored, reqDl)
	if recDlRestored.Code != http.StatusOK {
		t.Fatalf("expected download to succeed after replica restored, got %d", recDlRestored.Code)
	}
	if !bytes.Equal(recDlRestored.Body.Bytes(), fileData) {
		t.Fatalf("downloaded data mismatch after replica restore")
	}

	_ = mgr
	_ = repo
}
