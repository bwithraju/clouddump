export const API_BASE_URL =
  process.env.NEXT_PUBLIC_API_URL || "http://localhost:8080/api";

export interface User {
  id: string;
  email: string;
  name: string;
  created_at: string;
}

export interface AuthResponse {
  user: User;
  token?: string;
  expires_at?: string;
}

export interface HealthCheckResponse {
  status: string;
  service: string;
  version: string;
  timestamp: string;
}

export interface FileRecord {
  id: string;
  user_id: string;
  filename: string;
  size: number;
  mime_type: string;
  chunk_size: number;
  total_chunks: number;
  status: "UPLOADING" | "COMPLETED" | "FAILED" | "DEGRADED" | string;
  created_at: string;
  updated_at: string;
}

export interface ChunkReplicaRecord {
  storage_node_id: string;
  provider_object_id: string;
  status: string;
}

export interface ChunkRecord {
  id: string;
  file_id: string;
  chunk_index: number;
  size: number;
  checksum: string;
  storage_node_id: string;
  provider_object_id: string;
  status: "STORED" | "CORRUPTED" | "MISSING" | string;
  replicas?: ChunkReplicaRecord[];
  created_at: string;
}

export interface FileDetailsResponse {
  file: FileRecord;
  chunks: ChunkRecord[];
  distribution: Record<string, number>;
}

export interface NodeStats {
  id: string;
  name: string;
  provider_type: string;
  total_capacity: number;
  used_capacity: number;
  available_capacity: number;
  status: "ONLINE" | "OFFLINE" | "DEGRADED" | string;
  last_health_check: string;
}

export interface PoolStats {
  total_capacity: number;
  used_capacity: number;
  available_capacity: number;
  node_count: number;
  online_nodes: number;
  offline_nodes: number;
  nodes: NodeStats[];
}

export interface SessionSummary {
  upload_id: string;
  file_id: string;
  filename: string;
  total_size: number;
  chunk_size: number;
  total_chunks: number;
  completed_count: number;
  completed_indices: number[];
  failed_count: number;
  failed_chunks: Record<number, string>;
  status: "INITIALIZED" | "UPLOADING" | "COMPLETED" | "FAILED" | "CANCELLED" | string;
  progress_percent: number;
  next_expected: number;
  created_at: string;
  updated_at: string;
}

export interface SystemOperation {
  id: string;
  type: string;
  entity: string;
  status: "SUCCESS" | "RUNNING" | "FAILED" | "WARNING" | string;
  details: string;
  timestamp: string;
}

export interface FailedOperation {
  id: string;
  type: string;
  entity: string;
  reason: string;
  severity: "HIGH" | "MEDIUM" | "LOW" | string;
  timestamp: string;
}

export interface SystemActivityResponse {
  uptime_seconds: number;
  pool_stats: PoolStats;
  total_files: number;
  completed_files: number;
  degraded_files: number;
  failed_files: number;
  active_uploads: number;
  recent_operations: SystemOperation[];
  failed_operations: FailedOperation[];
}

export interface DownloadState {
  fileId: string;
  filename: string;
  status: "idle" | "downloading" | "completed" | "failed";
  loadedBytes: number;
  totalBytes: number;
  percent: number;
  error?: string;
}

// Authentication APIs
export async function register(email: string, password: string, name?: string): Promise<AuthResponse> {
  const res = await fetch(`${API_BASE_URL}/auth/register`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    credentials: "include",
    body: JSON.stringify({ email, password, name }),
  });
  if (!res.ok) {
    const err = await res.text();
    throw new Error(err || `Registration failed: HTTP ${res.status}`);
  }
  return res.json();
}

export async function login(email: string, password: string): Promise<AuthResponse> {
  const res = await fetch(`${API_BASE_URL}/auth/login`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    credentials: "include",
    body: JSON.stringify({ email, password }),
  });
  if (!res.ok) {
    const err = await res.text();
    throw new Error(err || `Login failed: HTTP ${res.status}`);
  }
  return res.json();
}

export async function logout(): Promise<void> {
  await fetch(`${API_BASE_URL}/auth/logout`, {
    method: "POST",
    credentials: "include",
  });
}

export async function fetchCurrentUser(): Promise<User | null> {
  const res = await fetch(`${API_BASE_URL}/auth/me`, {
    credentials: "include",
  });
  if (!res.ok) {
    return null;
  }
  const data = await res.json();
  return data.user || null;
}

// Google Drive OAuth & Storage Node APIs
export async function fetchGoogleAuthURL(): Promise<{ auth_url: string; configured: boolean }> {
  const res = await fetch(`${API_BASE_URL}/oauth/google/auth-url`, {
    credentials: "include",
  });
  if (!res.ok) {
    throw new Error(`Failed fetching Google Auth URL: HTTP ${res.status}`);
  }
  return res.json();
}

export async function exchangeGoogleOAuthCode(code: string): Promise<any> {
  const res = await fetch(`${API_BASE_URL}/oauth/google/callback`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    credentials: "include",
    body: JSON.stringify({ code }),
  });
  if (!res.ok) {
    const errText = await res.text();
    throw new Error(`Failed to link Google Drive: ${errText}`);
  }
  return res.json();
}

export async function fetchGoogleOAuthStatus(): Promise<{ connected: boolean; expires_at?: string }> {
  const res = await fetch(`${API_BASE_URL}/oauth/google/status`, {
    credentials: "include",
  });
  if (!res.ok) {
    return { connected: false };
  }
  return res.json();
}

export async function disconnectGoogleDrive(): Promise<void> {
  await fetch(`${API_BASE_URL}/oauth/google/disconnect`, {
    method: "POST",
    credentials: "include",
  });
}

export async function createGoogleDriveNode(name?: string, folderId?: string): Promise<NodeStats> {
  const res = await fetch(`${API_BASE_URL}/nodes/google-drive`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    credentials: "include",
    body: JSON.stringify({ name, folder_id: folderId }),
  });
  if (!res.ok) {
    const errText = await res.text();
    throw new Error(`Failed to create Google Drive storage node: ${errText}`);
  }
  return res.json();
}

// Health Check
export async function fetchHealth(): Promise<HealthCheckResponse> {
  const res = await fetch(`${API_BASE_URL}/health`, {
    credentials: "include",
  });
  if (!res.ok) {
    throw new Error(`Health check failed with HTTP ${res.status}`);
  }
  return res.json();
}

// Storage Pool & Nodes
export async function fetchPoolStats(): Promise<PoolStats> {
  const res = await fetch(`${API_BASE_URL}/nodes/stats`, {
    credentials: "include",
  });
  if (!res.ok) {
    throw new Error(`Failed to fetch storage pool stats: HTTP ${res.status}`);
  }
  return res.json();
}

export async function fetchNodes(): Promise<NodeStats[]> {
  const res = await fetch(`${API_BASE_URL}/nodes`, {
    credentials: "include",
  });
  if (!res.ok) {
    throw new Error(`Failed to fetch storage nodes: HTTP ${res.status}`);
  }
  return res.json();
}

export async function simulateNodeFailure(
  nodeId: string,
  offline: boolean
): Promise<{
  node: NodeStats;
  offline: boolean;
  degraded_files: string[];
  recovered_files: string[];
}> {
  const res = await fetch(`${API_BASE_URL}/nodes/${encodeURIComponent(nodeId)}/simulate-failure`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    credentials: "include",
    body: JSON.stringify({ offline }),
  });
  if (!res.ok) {
    const errText = await res.text();
    throw new Error(`Failed to update node status (${res.status}): ${errText}`);
  }
  return res.json();
}

export async function triggerReconcile(): Promise<{
  offline_nodes: string[];
  online_nodes: string[];
  degraded_files: string[];
  recovered_files: string[];
}> {
  const res = await fetch(`${API_BASE_URL}/nodes/reconcile`, {
    method: "POST",
    credentials: "include",
  });
  if (!res.ok) {
    const errText = await res.text();
    throw new Error(`Failed to reconcile nodes (${res.status}): ${errText}`);
  }
  return res.json();
}

// Files
export async function fetchFiles(): Promise<FileRecord[]> {
  const res = await fetch(`${API_BASE_URL}/files`, {
    credentials: "include",
  });
  if (!res.ok) {
    throw new Error(`Failed to list files: HTTP ${res.status}`);
  }
  return res.json();
}

export async function fetchFileDetails(id: string): Promise<FileDetailsResponse> {
  const res = await fetch(`${API_BASE_URL}/files/${encodeURIComponent(id)}`, {
    credentials: "include",
  });
  if (!res.ok) {
    throw new Error(`Failed to load file details: HTTP ${res.status}`);
  }
  return res.json();
}

export async function deleteFile(id: string): Promise<void> {
  const res = await fetch(`${API_BASE_URL}/files/${encodeURIComponent(id)}`, {
    method: "DELETE",
    credentials: "include",
  });
  if (!res.ok) {
    throw new Error(`Failed to delete file: HTTP ${res.status}`);
  }
}

export function downloadFile(id: string, filename: string): void {
  const downloadUrl = `${API_BASE_URL}/files/${encodeURIComponent(id)}/download`;
  const link = document.createElement("a");
  link.href = downloadUrl;
  link.setAttribute("download", filename);
  document.body.appendChild(link);
  link.click();
  document.body.removeChild(link);
}

export async function downloadFileWithProgress(
  id: string,
  filename: string,
  onProgress?: (loaded: number, total: number, percent: number) => void
): Promise<void> {
  const downloadUrl = `${API_BASE_URL}/files/${encodeURIComponent(id)}/download`;
  const res = await fetch(downloadUrl, { credentials: "include" });
  if (!res.ok) {
    const errText = await res.text();
    throw new Error(`Download failed (${res.status}): ${errText}`);
  }

  const contentLengthHeader = res.headers.get("Content-Length");
  const total = contentLengthHeader ? parseInt(contentLengthHeader, 10) : 0;
  let loaded = 0;

  const reader = res.body?.getReader();
  if (!reader) {
    downloadFile(id, filename);
    return;
  }

  const chunks: BlobPart[] = [];
  while (true) {
    const { done, value } = await reader.read();
    if (done) break;
    if (value) {
      chunks.push(value as unknown as BlobPart);
      loaded += value.length;
      if (onProgress && total > 0) {
        onProgress(loaded, total, Math.round((loaded / total) * 100));
      }
    }
  }

  const blob = new Blob(chunks, { type: res.headers.get("Content-Type") || "application/octet-stream" });
  const blobUrl = URL.createObjectURL(blob);
  const link = document.createElement("a");
  link.href = blobUrl;
  link.setAttribute("download", filename);
  document.body.appendChild(link);
  link.click();
  document.body.removeChild(link);
  URL.revokeObjectURL(blobUrl);
}

// System Telemetry & Monitoring APIs
export async function fetchSystemActivity(): Promise<SystemActivityResponse> {
  const res = await fetch(`${API_BASE_URL}/system/activity`, {
    credentials: "include",
  });
  if (!res.ok) {
    throw new Error(`Failed to fetch system activity: HTTP ${res.status}`);
  }
  return res.json();
}

export async function fetchUploadSessions(): Promise<SessionSummary[]> {
  const res = await fetch(`${API_BASE_URL}/uploads`, {
    credentials: "include",
  });
  if (!res.ok) {
    return [];
  }
  return res.json();
}

// Resumable Upload APIs
export async function initResumableUpload(
  filename: string,
  size: number,
  mimeType: string = "application/octet-stream",
  chunkSize: number = 2 * 1024 * 1024
): Promise<SessionSummary> {
  const res = await fetch(`${API_BASE_URL}/uploads`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    credentials: "include",
    body: JSON.stringify({
      filename,
      size,
      mime_type: mimeType,
      chunk_size: chunkSize,
    }),
  });
  if (!res.ok) {
    const errText = await res.text();
    throw new Error(`Failed to init upload session (${res.status}): ${errText}`);
  }
  return res.json();
}

export async function fetchUploadSession(uploadId: string): Promise<SessionSummary> {
  const res = await fetch(`${API_BASE_URL}/uploads/${encodeURIComponent(uploadId)}`, {
    credentials: "include",
  });
  if (!res.ok) {
    throw new Error(`Failed to fetch upload session: HTTP ${res.status}`);
  }
  return res.json();
}

export async function uploadChunk(
  uploadId: string,
  chunkIndex: number,
  chunkBlob: Blob
): Promise<any> {
  const res = await fetch(
    `${API_BASE_URL}/uploads/${encodeURIComponent(uploadId)}/chunks/${chunkIndex}`,
    {
      method: "PUT",
      headers: {
        "Content-Type": "application/octet-stream",
        "Content-Length": chunkBlob.size.toString(),
      },
      credentials: "include",
      body: chunkBlob,
    }
  );
  if (!res.ok) {
    const errText = await res.text();
    throw new Error(`Failed to upload chunk ${chunkIndex} (${res.status}): ${errText}`);
  }
  return res.json();
}

export async function cancelResumableUpload(uploadId: string): Promise<void> {
  const res = await fetch(`${API_BASE_URL}/uploads/${encodeURIComponent(uploadId)}/cancel`, {
    method: "POST",
    credentials: "include",
  });
  if (!res.ok) {
    throw new Error(`Failed to cancel upload session: HTTP ${res.status}`);
  }
}

// Direct raw streaming upload with optional replication factor
export function uploadFile(
  file: File,
  replicationFactor: number = 1,
  onProgress?: (percent: number, loaded: number, total: number) => void,
  onAbortRef?: { abort?: () => void }
): Promise<FileRecord> {
  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest();

    if (onAbortRef) {
      onAbortRef.abort = () => xhr.abort();
    }

    xhr.open("POST", `${API_BASE_URL}/files/upload`, true);
    xhr.withCredentials = true;
    xhr.setRequestHeader("X-Filename", file.name);
    xhr.setRequestHeader("X-File-Size", file.size.toString());
    xhr.setRequestHeader("Content-Type", file.type || "application/octet-stream");
    if (replicationFactor > 1) {
      xhr.setRequestHeader("X-Replication-Factor", replicationFactor.toString());
    }

    xhr.upload.onprogress = (event) => {
      if (event.lengthComputable && onProgress) {
        const percent = Math.round((event.loaded / event.total) * 100);
        onProgress(percent, event.loaded, event.total);
      }
    };

    xhr.onload = () => {
      if (xhr.status >= 200 && xhr.status < 300) {
        try {
          const data = JSON.parse(xhr.responseText);
          resolve(data);
        } catch {
          resolve({
            id: "",
            user_id: "default_user",
            filename: file.name,
            size: file.size,
            mime_type: file.type || "application/octet-stream",
            chunk_size: 4 * 1024 * 1024,
            total_chunks: 1,
            status: "COMPLETED",
            created_at: new Date().toISOString(),
            updated_at: new Date().toISOString(),
          });
        }
      } else {
        reject(new Error(`Upload failed with status ${xhr.status}: ${xhr.responseText}`));
      }
    };

    xhr.onerror = () => reject(new Error("Network error during file upload"));
    xhr.onabort = () => reject(new Error("Upload cancelled by user"));

    xhr.send(file);
  });
}
