export const API_BASE_URL =
  process.env.NEXT_PUBLIC_API_URL || "http://localhost:8080/api";

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

// Health Check
export async function fetchHealth(): Promise<HealthCheckResponse> {
  const res = await fetch(`${API_BASE_URL}/health`);
  if (!res.ok) {
    throw new Error(`Health check failed with HTTP ${res.status}`);
  }
  return res.json();
}

// Storage Pool & Nodes
export async function fetchPoolStats(): Promise<PoolStats> {
  const res = await fetch(`${API_BASE_URL}/nodes/stats`);
  if (!res.ok) {
    throw new Error(`Failed to fetch storage pool stats: HTTP ${res.status}`);
  }
  return res.json();
}

export async function fetchNodes(): Promise<NodeStats[]> {
  const res = await fetch(`${API_BASE_URL}/nodes`);
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
  });
  if (!res.ok) {
    const errText = await res.text();
    throw new Error(`Failed to reconcile nodes (${res.status}): ${errText}`);
  }
  return res.json();
}

// Files
export async function fetchFiles(): Promise<FileRecord[]> {
  const res = await fetch(`${API_BASE_URL}/files`);
  if (!res.ok) {
    throw new Error(`Failed to list files: HTTP ${res.status}`);
  }
  return res.json();
}

export async function fetchFileDetails(id: string): Promise<FileDetailsResponse> {
  const res = await fetch(`${API_BASE_URL}/files/${encodeURIComponent(id)}`);
  if (!res.ok) {
    throw new Error(`Failed to load file details: HTTP ${res.status}`);
  }
  return res.json();
}

export async function deleteFile(id: string): Promise<void> {
  const res = await fetch(`${API_BASE_URL}/files/${encodeURIComponent(id)}`, {
    method: "DELETE",
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
  const res = await fetch(`${API_BASE_URL}/uploads/${encodeURIComponent(uploadId)}`);
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
