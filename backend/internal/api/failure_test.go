package api

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"

	"github.com/bwithraju/clouddump/backend/internal/chunks"
	"github.com/bwithraju/clouddump/backend/internal/database"
	"github.com/bwithraju/clouddump/backend/internal/providers"
	"github.com/bwithraju/clouddump/backend/internal/storage"
)

func setupFailureTestEnvironment(t *testing.T, chunkSize int64) (http.Handler, *storage.StorageManager, database.Repository, map[string]string) {
	mgr := storage.NewStorageManager(storage.NewMaxAvailableStrategy())

	tempDir1, _ := os.MkdirTemp("", "fail_node1_*")
	tempDir2, _ := os.MkdirTemp("", "fail_node2_*")
	tempDir3, _ := os.MkdirTemp("", "fail_node3_*")

	p1, _ := providers.NewLocalStorageProvider(tempDir1, 50*1024*1024)
	p2, _ := providers.NewLocalStorageProvider(tempDir2, 100*1024*1024)
	p3, _ := providers.NewLocalStorageProvider(tempDir3, 150*1024*1024)

	n1, _ := storage.NewStorageNode("node1", "Node 1", p1)
	n2, _ := storage.NewStorageNode("node2", "Node 2", p2)
	n3, _ := storage.NewStorageNode("node3", "Node 3", p3)

	_ = mgr.RegisterNode(n1)
	_ = mgr.RegisterNode(n2)
	_ = mgr.RegisterNode(n3)

	dirs := map[string]string{
		"node1": tempDir1,
		"node2": tempDir2,
		"node3": tempDir3,
	}

	distributor, _ := chunks.NewChunkDistributor(mgr, chunkSize)
	reconstructor, _ := chunks.NewFileReconstructor(mgr)
	repo := database.NewMemoryRepository()

	fh := NewFileHandler(mgr, distributor, reconstructor, repo, chunkSize)
	nh := NewNodesHandler(mgr, repo)
	router := NewRouter(RouterConfig{FileHandler: fh, NodesHandler: nh})

	return router, mgr, repo, dirs
}

func uploadMultipartFile(t *testing.T, router http.Handler, filename string, data []byte) string {
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

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("upload failed with code %d: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode upload response: %v", err)
	}
	return resp.ID
}

func TestNodeFailureHandling_EndToEndLifecycle(t *testing.T) {
	chunkSize := int64(100 * 1024) // 100 KB
	router, mgr, repo, dirs := setupFailureTestEnvironment(t, chunkSize)
	defer cleanupAPITestDirs(dirs)

	// Generate a 350 KB test file (4 chunks: 100KB, 100KB, 100KB, 50KB)
	file1Data := make([]byte, 350*1024)
	_, _ = io.ReadFull(rand.Reader, file1Data)

	// Step 1: Upload file 1 across the online nodes
	file1ID := uploadMultipartFile(t, router, "dataset.dat", file1Data)

	// Verify download works before any failure
	reqDl := httptest.NewRequest(http.MethodGet, "/api/files/"+file1ID+"/download", nil)
	recDl := httptest.NewRecorder()
	router.ServeHTTP(recDl, reqDl)
	if recDl.Code != http.StatusOK {
		t.Fatalf("initial download failed with code %d: %s", recDl.Code, recDl.Body.String())
	}
	if !bytes.Equal(recDl.Body.Bytes(), file1Data) {
		t.Fatalf("initial download content mismatch")
	}

	// Check which node has chunk 0 of file 1
	chunks1, err := repo.Chunks().ListChunksByFile(t.Context(), file1ID)
	if err != nil || len(chunks1) == 0 {
		t.Fatalf("failed to get chunks for file 1: %v", err)
	}
	targetFailedNode := chunks1[0].StorageNodeID

	// Step 2: NODE GOES OFFLINE
	// Simulate failure on targetFailedNode
	failPayload, _ := json.Marshal(SimulateFailureRequest{Offline: true})
	reqFail := httptest.NewRequest(http.MethodPost, "/api/nodes/"+targetFailedNode+"/simulate-failure", bytes.NewReader(failPayload))
	recFail := httptest.NewRecorder()
	router.ServeHTTP(recFail, reqFail)
	if recFail.Code != http.StatusOK {
		t.Fatalf("simulate failure failed with code %d: %s", recFail.Code, recFail.Body.String())
	}

	// Verify node is marked OFFLINE in pool stats
	reqStats := httptest.NewRequest(http.MethodGet, "/api/nodes/stats", nil)
	recStats := httptest.NewRecorder()
	router.ServeHTTP(recStats, reqStats)
	var stats storage.PoolStats
	_ = json.NewDecoder(recStats.Body).Decode(&stats)
	if stats.OfflineNodes != 1 {
		t.Errorf("expected 1 offline node, got %d", stats.OfflineNodes)
	}

	// Verify file1 is marked DEGRADED in file metadata
	reqFile1 := httptest.NewRequest(http.MethodGet, "/api/files/"+file1ID+"/status", nil)
	recFile1 := httptest.NewRecorder()
	router.ServeHTTP(recFile1, reqFile1)
	var statusResp map[string]interface{}
	_ = json.NewDecoder(recFile1.Body).Decode(&statusResp)
	if statusResp["status"] != "DEGRADED" {
		t.Errorf("expected file1 status DEGRADED, got %v", statusResp["status"])
	}

	// Step 3: DOWNLOAD WITH MISSING CHUNK / DEGRADED FILE
	// Download must fail with 503 Service Unavailable, never returning corrupted data
	reqDlFail := httptest.NewRequest(http.MethodGet, "/api/files/"+file1ID+"/download", nil)
	recDlFail := httptest.NewRecorder()
	router.ServeHTTP(recDlFail, reqDlFail)
	if recDlFail.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 Service Unavailable on degraded download, got %d", recDlFail.Code)
	}

	// Step 4: UPLOAD DURING FAILURE
	// Upload a second file while targetFailedNode is offline
	file2Data := make([]byte, 150*1024) // 2 chunks: 100KB, 50KB
	_, _ = io.ReadFull(rand.Reader, file2Data)
	file2ID := uploadMultipartFile(t, router, "backup.bin", file2Data)

	// Verify file2 is COMPLETED and did NOT use targetFailedNode
	chunks2, err := repo.Chunks().ListChunksByFile(t.Context(), file2ID)
	if err != nil {
		t.Fatalf("failed to get chunks for file 2: %v", err)
	}
	for _, c := range chunks2 {
		if c.StorageNodeID == targetFailedNode {
			t.Errorf("chunk %d was allocated to offline node %s!", c.ChunkIndex, targetFailedNode)
		}
	}

	// Verify file2 can be downloaded without issues
	reqDl2 := httptest.NewRequest(http.MethodGet, "/api/files/"+file2ID+"/download", nil)
	recDl2 := httptest.NewRecorder()
	router.ServeHTTP(recDl2, reqDl2)
	if recDl2.Code != http.StatusOK {
		t.Fatalf("file2 download failed with code %d: %s", recDl2.Code, recDl2.Body.String())
	}
	if !bytes.Equal(recDl2.Body.Bytes(), file2Data) {
		t.Fatalf("file2 data mismatch")
	}

	// Step 5: NODE RETURNS ONLINE
	// Restore targetFailedNode back to online
	restorePayload, _ := json.Marshal(SimulateFailureRequest{Offline: false})
	reqRestore := httptest.NewRequest(http.MethodPost, "/api/nodes/"+targetFailedNode+"/simulate-failure", bytes.NewReader(restorePayload))
	recRestore := httptest.NewRecorder()
	router.ServeHTTP(recRestore, reqRestore)
	if recRestore.Code != http.StatusOK {
		t.Fatalf("simulate restore failed with code %d: %s", recRestore.Code, recRestore.Body.String())
	}

	// Verify all nodes are ONLINE in pool stats
	recStats2 := httptest.NewRecorder()
	router.ServeHTTP(recStats2, reqStats)
	var stats2 storage.PoolStats
	_ = json.NewDecoder(recStats2.Body).Decode(&stats2)
	if stats2.OfflineNodes != 0 || stats2.OnlineNodes != 3 {
		t.Errorf("expected 0 offline, 3 online nodes after restore; got offline=%d, online=%d", stats2.OfflineNodes, stats2.OnlineNodes)
	}

	// Verify file1 is recovered back to COMPLETED
	recFile1Restored := httptest.NewRecorder()
	router.ServeHTTP(recFile1Restored, reqFile1)
	var statusRestored map[string]interface{}
	_ = json.NewDecoder(recFile1Restored.Body).Decode(&statusRestored)
	if statusRestored["status"] != "COMPLETED" {
		t.Errorf("expected file1 status to recover to COMPLETED, got %v", statusRestored["status"])
	}

	// Step 6: DOWNLOAD AFTER NODE RECOVERY
	// File 1 download must now succeed byte-for-byte
	recDlRecovered := httptest.NewRecorder()
	router.ServeHTTP(recDlRecovered, reqDl)
	if recDlRecovered.Code != http.StatusOK {
		t.Fatalf("download after recovery failed with code %d: %s", recDlRecovered.Code, recDlRecovered.Body.String())
	}
	if !bytes.Equal(recDlRecovered.Body.Bytes(), file1Data) {
		t.Fatalf("downloaded data mismatch after node recovery: expected %d bytes, got %d bytes", len(file1Data), len(recDlRecovered.Body.Bytes()))
	}
	_ = mgr
}
