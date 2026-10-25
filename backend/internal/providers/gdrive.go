package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// DefaultDriveAPIBaseURL is the standard v3 REST endpoint.
	DefaultDriveAPIBaseURL = "https://www.googleapis.com/drive/v3"
	// DefaultDriveUploadBaseURL is the resumable upload endpoint.
	DefaultDriveUploadBaseURL = "https://www.googleapis.com/upload/drive/v3/files"
	// DefaultGoogleDriveCapacity is fallback capacity if account quota is unlimited or unreadable (15 GB).
	DefaultGoogleDriveCapacity = int64(15 * 1024 * 1024 * 1024)
)

var (
	// ErrGoogleDriveRateLimit is returned when rate limits are exhausted.
	ErrGoogleDriveRateLimit = errors.New("google drive api rate limit exceeded")
	// ErrGoogleDriveQuotaExceeded is returned when Google storage quota is exceeded.
	ErrGoogleDriveQuotaExceeded = errors.New("google drive storage quota exceeded")
)

// HTTPClient abstracts HTTP calls for testability and custom OAuth transports.
type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

// GoogleDriveConfig holds parameters for constructing a GoogleDriveProvider.
type GoogleDriveConfig struct {
	Client         HTTPClient
	FolderID       string // optional parent folder in Drive
	TotalCapacity  int64  // fallback capacity
	APIBaseURL     string
	UploadBaseURL  string
	MaxRetries     int
	BaseBackoff    time.Duration
}

// GoogleDriveProvider implements StorageProvider for Google Drive.
type GoogleDriveProvider struct {
	client           HTTPClient
	folderID         string
	totalCapacity    int64
	apiBaseURL       string
	uploadBaseURL    string
	maxRetries       int
	baseBackoff      time.Duration
	simulatedOffline bool
	cachedQuotaTotal int64
	cachedQuotaAvail int64
	quotaCachedAt    time.Time
	mu               sync.RWMutex
}

// NewGoogleDriveProvider initializes a new Google Drive StorageProvider.
func NewGoogleDriveProvider(cfg GoogleDriveConfig) (*GoogleDriveProvider, error) {
	client := cfg.Client
	if client == nil {
		client = http.DefaultClient
	}

	apiBase := cfg.APIBaseURL
	if apiBase == "" {
		apiBase = DefaultDriveAPIBaseURL
	}
	apiBase = strings.TrimRight(apiBase, "/")

	uploadBase := cfg.UploadBaseURL
	if uploadBase == "" {
		uploadBase = DefaultDriveUploadBaseURL
	}
	uploadBase = strings.TrimRight(uploadBase, "/")

	totCap := cfg.TotalCapacity
	if totCap <= 0 {
		totCap = DefaultGoogleDriveCapacity
	}

	maxRetries := cfg.MaxRetries
	if maxRetries <= 0 {
		maxRetries = 3
	}

	baseBackoff := cfg.BaseBackoff
	if baseBackoff <= 0 {
		baseBackoff = 100 * time.Millisecond
	}

	return &GoogleDriveProvider{
		client:        client,
		folderID:      cfg.FolderID,
		totalCapacity: totCap,
		apiBaseURL:    apiBase,
		uploadBaseURL: uploadBase,
		maxRetries:    maxRetries,
		baseBackoff:   baseBackoff,
	}, nil
}

// ProviderType returns "google_drive".
func (p *GoogleDriveProvider) ProviderType() string {
	return "google_drive"
}

// SetSimulatedOffline toggles simulated outage state for failure handling tests.
func (p *GoogleDriveProvider) SetSimulatedOffline(offline bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.simulatedOffline = offline
}

// isOffline returns true if simulated offline.
func (p *GoogleDriveProvider) isOffline() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.simulatedOffline
}

func (p *GoogleDriveProvider) validateChunkID(chunkID string) error {
	if chunkID == "" || len(chunkID) > 128 || !validChunkIDRegex.MatchString(chunkID) {
		return fmt.Errorf("%w: chunk ID must be alphanumeric and between 1-128 characters", ErrInvalidChunkID)
	}
	return nil
}

// StoreChunk uploads a chunk to Google Drive as a Drive object.
// Uses Drive v3 resumable upload to stream directly without buffering in RAM.
func (p *GoogleDriveProvider) StoreChunk(ctx context.Context, chunkID string, r io.Reader, size int64) (string, int64, error) {
	select {
	case <-ctx.Done():
		return "", 0, ctx.Err()
	default:
	}

	if p.isOffline() {
		return "", 0, ErrNodeOffline
	}

	if err := p.validateChunkID(chunkID); err != nil {
		return "", 0, err
	}

	// 1. Initiate Resumable Upload Session
	initMeta := map[string]interface{}{
		"name":        chunkID,
		"description": "CloudDump chunk " + chunkID,
	}
	if p.folderID != "" {
		initMeta["parents"] = []string{p.folderID}
	}
	metaBytes, err := json.Marshal(initMeta)
	if err != nil {
		return "", 0, fmt.Errorf("failed marshaling chunk metadata: %w", err)
	}

	initURL := fmt.Sprintf("%s?uploadType=resumable", p.uploadBaseURL)
	var uploadLocation string

	err = p.executeWithRetry(ctx, "initiate-upload", func() (*http.Response, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, initURL, bytes.NewReader(metaBytes))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json; charset=UTF-8")
		req.Header.Set("X-Upload-Content-Type", "application/octet-stream")
		if size > 0 {
			req.Header.Set("X-Upload-Content-Length", strconv.FormatInt(size, 10))
		}
		return p.client.Do(req)
	}, func(resp *http.Response) error {
		if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusCreated {
			uploadLocation = resp.Header.Get("Location")
			if uploadLocation == "" {
				return errors.New("missing Location header in resumable upload initiation response")
			}
			return nil
		}
		return p.handleHTTPError(resp)
	})

	if err != nil {
		return "", 0, fmt.Errorf("failed initiating drive chunk upload: %w", err)
	}

	// 2. Stream chunk payload directly to uploadLocation
	var objectID string
	var bytesUploaded int64

	// If reader is a Seekable or body with exact length
	err = p.executeWithRetry(ctx, "upload-chunk-stream", func() (*http.Response, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPut, uploadLocation, r)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/octet-stream")
		if size > 0 {
			req.Header.Set("Content-Length", strconv.FormatInt(size, 10))
		}
		return p.client.Do(req)
	}, func(resp *http.Response) error {
		defer resp.Body.Close()
		if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusCreated {
			var fileResp struct {
				ID   string `json:"id"`
				Name string `json:"name"`
				Size string `json:"size"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&fileResp); err != nil {
				return fmt.Errorf("failed decoding drive response: %w", err)
			}
			objectID = fileResp.ID
			if objectID == "" {
				return errors.New("empty object ID returned by Google Drive")
			}
			bytesUploaded = size
			return nil
		}
		return p.handleHTTPError(resp)
	})

	if err != nil {
		return "", 0, fmt.Errorf("failed uploading chunk to drive: %w", err)
	}

	p.invalidateQuotaCache()
	return objectID, bytesUploaded, nil
}

// GetChunk downloads a chunk identified by its Drive file ID (objectID).
// Returns a streaming io.ReadCloser (caller is responsible for closing).
func (p *GoogleDriveProvider) GetChunk(ctx context.Context, objectID string) (io.ReadCloser, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	if p.isOffline() {
		return nil, ErrNodeOffline
	}

	if strings.TrimSpace(objectID) == "" {
		return nil, ErrChunkNotFound
	}

	downloadURL := fmt.Sprintf("%s/files/%s?alt=media", p.apiBaseURL, objectID)
	var stream io.ReadCloser

	err := p.executeWithRetry(ctx, "get-chunk", func() (*http.Response, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
		if err != nil {
			return nil, err
		}
		return p.client.Do(req)
	}, func(resp *http.Response) error {
		if resp.StatusCode == http.StatusOK {
			stream = resp.Body
			return nil
		}
		defer resp.Body.Close()
		if resp.StatusCode == http.StatusNotFound {
			return ErrChunkNotFound
		}
		return p.handleHTTPError(resp)
	})

	if err != nil {
		return nil, err
	}

	return stream, nil
}

// DeleteChunk removes a chunk file from Google Drive by its file ID.
func (p *GoogleDriveProvider) DeleteChunk(ctx context.Context, objectID string) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	if p.isOffline() {
		return ErrNodeOffline
	}

	if strings.TrimSpace(objectID) == "" {
		return nil
	}

	deleteURL := fmt.Sprintf("%s/files/%s", p.apiBaseURL, objectID)

	err := p.executeWithRetry(ctx, "delete-chunk", func() (*http.Response, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodDelete, deleteURL, nil)
		if err != nil {
			return nil, err
		}
		return p.client.Do(req)
	}, func(resp *http.Response) error {
		defer resp.Body.Close()
		if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusNoContent || resp.StatusCode == http.StatusNotFound {
			return nil
		}
		return p.handleHTTPError(resp)
	})

	if err != nil {
		return fmt.Errorf("failed deleting chunk from drive: %w", err)
	}

	p.invalidateQuotaCache()
	return nil
}

// GetTotalCapacity queries Drive storage quota and returns the total capacity in bytes.
func (p *GoogleDriveProvider) GetTotalCapacity(ctx context.Context) (int64, error) {
	total, _, err := p.getQuota(ctx)
	return total, err
}

// GetAvailableCapacity queries Drive storage quota and returns the remaining free capacity in bytes.
func (p *GoogleDriveProvider) GetAvailableCapacity(ctx context.Context) (int64, error) {
	_, avail, err := p.getQuota(ctx)
	return avail, err
}

// HealthCheck verifies connectivity and quota status with Google Drive.
func (p *GoogleDriveProvider) HealthCheck(ctx context.Context) (bool, error) {
	if p.isOffline() {
		return false, ErrNodeOffline
	}

	_, _, err := p.getQuota(ctx)
	if err != nil {
		return false, err
	}
	return true, nil
}

// getQuota queries the about endpoint for storageQuota, using a short TTL cache.
func (p *GoogleDriveProvider) getQuota(ctx context.Context) (int64, int64, error) {
	p.mu.RLock()
	if !p.quotaCachedAt.IsZero() && time.Since(p.quotaCachedAt) < 15*time.Second {
		tot := p.cachedQuotaTotal
		avl := p.cachedQuotaAvail
		p.mu.RUnlock()
		return tot, avl, nil
	}
	p.mu.RUnlock()

	aboutURL := fmt.Sprintf("%s/about?fields=storageQuota", p.apiBaseURL)
	var total, avail int64

	err := p.executeWithRetry(ctx, "get-quota", func() (*http.Response, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, aboutURL, nil)
		if err != nil {
			return nil, err
		}
		return p.client.Do(req)
	}, func(resp *http.Response) error {
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return p.handleHTTPError(resp)
		}

		var aboutResp struct {
			StorageQuota struct {
				Limit string `json:"limit"`
				Usage string `json:"usage"`
			} `json:"storageQuota"`
		}

		if err := json.NewDecoder(resp.Body).Decode(&aboutResp); err != nil {
			return fmt.Errorf("failed parsing about quota response: %w", err)
		}

		var limitBytes int64
		if aboutResp.StorageQuota.Limit != "" {
			limitBytes, _ = strconv.ParseInt(aboutResp.StorageQuota.Limit, 10, 64)
		}
		var usageBytes int64
		if aboutResp.StorageQuota.Usage != "" {
			usageBytes, _ = strconv.ParseInt(aboutResp.StorageQuota.Usage, 10, 64)
		}

		if limitBytes > 0 {
			total = limitBytes
			avail = limitBytes - usageBytes
			if avail < 0 {
				avail = 0
			}
		} else {
			// Unlimited or unconfigured quota fallback
			total = p.totalCapacity
			avail = p.totalCapacity - usageBytes
			if avail < 0 {
				avail = 0
			}
		}
		return nil
	})

	if err != nil {
		return 0, 0, err
	}

	p.mu.Lock()
	p.cachedQuotaTotal = total
	p.cachedQuotaAvail = avail
	p.quotaCachedAt = time.Now().UTC()
	p.mu.Unlock()

	return total, avail, nil
}

func (p *GoogleDriveProvider) invalidateQuotaCache() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.quotaCachedAt = time.Time{}
}

// executeWithRetry executes an HTTP request with exponential backoff for rate limits and server errors.
func (p *GoogleDriveProvider) executeWithRetry(
	ctx context.Context,
	op string,
	doRequest func() (*http.Response, error),
	handleResponse func(resp *http.Response) error,
) error {
	var lastErr error

	for attempt := 0; attempt <= p.maxRetries; attempt++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		resp, err := doRequest()
		if err != nil {
			lastErr = err
			if attempt < p.maxRetries {
				p.backoffSleep(ctx, attempt)
				continue
			}
			return fmt.Errorf("google drive request error (%s): %w", op, err)
		}

		// Handle rate limit (429) or transient server errors (500, 502, 503, 504)
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			_ = resp.Body.Close()
			lastErr = fmt.Errorf("google drive %s returned status %d", op, resp.StatusCode)
			if attempt < p.maxRetries {
				p.backoffSleep(ctx, attempt)
				continue
			}
			return ErrGoogleDriveRateLimit
		}

		// Check for 403 rate limit / userRateLimitExceeded
		if resp.StatusCode == http.StatusForbidden {
			body, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			bodyStr := string(body)

			if strings.Contains(bodyStr, "rateLimitExceeded") || strings.Contains(bodyStr, "userRateLimitExceeded") {
				lastErr = ErrGoogleDriveRateLimit
				if attempt < p.maxRetries {
					p.backoffSleep(ctx, attempt)
					continue
				}
				return ErrGoogleDriveRateLimit
			}

			if strings.Contains(bodyStr, "storageQuotaExceeded") || strings.Contains(bodyStr, "quotaExceeded") {
				return ErrInsufficientCapacity
			}

			return fmt.Errorf("google drive 403 Forbidden: %s", bodyStr)
		}

		// Let handleResponse process status code and body
		err = handleResponse(resp)
		if err != nil {
			return err
		}
		return nil
	}

	return lastErr
}

func (p *GoogleDriveProvider) backoffSleep(ctx context.Context, attempt int) {
	// Exponential backoff: base * 2^attempt + jitter
	multiplier := 1 << attempt
	sleepMs := p.baseBackoff.Milliseconds() * int64(multiplier)
	jitter := rand.Int63n(50)
	duration := time.Duration(sleepMs+jitter) * time.Millisecond

	select {
	case <-time.After(duration):
	case <-ctx.Done():
	}
}

func (p *GoogleDriveProvider) handleHTTPError(resp *http.Response) error {
	body, _ := io.ReadAll(resp.Body)
	bodyStr := strings.TrimSpace(string(body))

	switch resp.StatusCode {
	case http.StatusNotFound:
		return ErrChunkNotFound
	case http.StatusForbidden:
		if strings.Contains(bodyStr, "storageQuotaExceeded") || strings.Contains(bodyStr, "quotaExceeded") {
			return ErrInsufficientCapacity
		}
		return fmt.Errorf("google drive forbidden (403): %s", bodyStr)
	case http.StatusUnauthorized:
		return fmt.Errorf("google drive unauthorized (401): invalid or expired token")
	default:
		return fmt.Errorf("google drive api error (status %d): %s", resp.StatusCode, bodyStr)
	}
}
