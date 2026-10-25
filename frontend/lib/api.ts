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
  status: "UPLOADING" | "COMPLETED" | "FAILED" | string;
  created_at: string;
  updated_at: string;
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

// Streaming upload with real-time byte progress & cancellation
export function uploadFile(
  file: File,
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
