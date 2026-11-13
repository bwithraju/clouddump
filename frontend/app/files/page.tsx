"use client";

import * as React from "react";
import Link from "next/link";
import { useSearchParams } from "next/navigation";
import {
  FolderOpen,
  Download,
  Trash2,
  UploadCloud,
  Search,
  AlertCircle,
  RefreshCw,
  CheckCircle2,
  FileText,
  FileCode,
  Image as ImageIcon,
  Film,
  Archive,
  List as ListIcon,
  LayoutGrid,
  Loader2,
  ArrowRight,
} from "lucide-react";
import { useFiles, useDeleteFile } from "@/hooks/use-files";
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button, getButtonClassName } from "@/components/ui/button";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { formatBytes, formatDate } from "@/lib/utils";
import { downloadFileWithProgress, DownloadState } from "@/lib/api";

function FileManagerContent() {
  const searchParams = useSearchParams();
  const initialQuery = searchParams.get("q") || "";

  const { data: files, isLoading, isError, refetch } = useFiles();
  const deleteMutation = useDeleteFile();
  const [searchQuery, setSearchQuery] = React.useState(initialQuery);
  const [deletingId, setDeletingId] = React.useState<string | null>(null);
  const [successMsg, setSuccessMsg] = React.useState<string | null>(null);
  const [viewMode, setViewMode] = React.useState<"list" | "grid">("list");
  const [downloadState, setDownloadState] = React.useState<DownloadState | null>(null);

  React.useEffect(() => {
    if (initialQuery) {
      setSearchQuery(initialQuery);
    }
  }, [initialQuery]);

  const filteredFiles = React.useMemo(() => {
    if (!files) return [];
    if (!searchQuery.trim()) return files;
    const q = searchQuery.toLowerCase();
    return files.filter((f) => f.filename.toLowerCase().includes(q));
  }, [files, searchQuery]);

  const getFileIcon = (filename: string) => {
    const ext = filename.split(".").pop()?.toLowerCase();
    if (["pdf"].includes(ext || "")) return <FileText className="h-5 w-5 text-rose-500" />;
    if (["png", "jpg", "jpeg", "webp", "svg"].includes(ext || "")) return <ImageIcon className="h-5 w-5 text-emerald-500" />;
    if (["mp4", "mov", "mkv", "webm"].includes(ext || "")) return <Film className="h-5 w-5 text-purple-500" />;
    if (["zip", "tar", "gz", "rar"].includes(ext || "")) return <Archive className="h-5 w-5 text-amber-500" />;
    if (["js", "ts", "tsx", "go", "json", "py", "html"].includes(ext || "")) return <FileCode className="h-5 w-5 text-indigo-500" />;
    return <FileText className="h-5 w-5 text-blue-500" />;
  };

  const handleDownload = async (fileId: string, filename: string) => {
    setDownloadState({
      fileId,
      filename,
      status: "downloading",
      loadedBytes: 0,
      totalBytes: 0,
      percent: 0,
    });

    try {
      await downloadFileWithProgress(fileId, filename, (loaded, total, percent) => {
        setDownloadState((prev) =>
          prev && prev.fileId === fileId
            ? { ...prev, loadedBytes: loaded, totalBytes: total, percent }
            : prev
        );
      });
      setDownloadState((prev) =>
        prev && prev.fileId === fileId ? { ...prev, status: "completed", percent: 100 } : prev
      );
      setTimeout(() => setDownloadState(null), 4000);
    } catch (err: any) {
      setDownloadState((prev) =>
        prev && prev.fileId === fileId
          ? { ...prev, status: "failed", error: err.message || "Download failed" }
          : prev
      );
    }
  };

  const handleDelete = async (id: string, filename: string) => {
    if (!confirm(`Are you sure you want to permanently delete "${filename}" and all its distributed chunks?`)) {
      return;
    }

    try {
      setDeletingId(id);
      await deleteMutation.mutateAsync(id);
      setSuccessMsg(`Deleted "${filename}" successfully.`);
      setTimeout(() => setSuccessMsg(null), 4000);
    } catch (err: any) {
      alert(`Failed to delete file: ${err.message || err}`);
    } finally {
      setDeletingId(null);
    }
  };

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4">
        <div>
          <h1 className="text-2xl font-bold tracking-tight text-slate-900 flex items-center gap-2.5">
            <FolderOpen className="h-6 w-6 text-indigo-600" />
            My files
          </h1>
          <p className="text-xs text-gray-500 mt-0.5">
            Browse, inspect distributed chunks, and manage all files stored across the pool.
          </p>
        </div>
        <div className="flex items-center gap-2.5">
          <Button
            variant="outline"
            size="sm"
            onClick={() => refetch()}
            className="text-xs flex items-center gap-1.5 text-gray-600 hover:text-slate-900"
          >
            <RefreshCw className="h-3.5 w-3.5" />
            Refresh
          </Button>
          <Link
            href="/upload"
            className="inline-flex items-center gap-2 rounded-lg bg-slate-900 px-4 py-2 text-xs font-semibold text-white shadow-xs hover:bg-slate-800 transition-colors"
          >
            <UploadCloud className="h-4 w-4" />
            Upload file
          </Link>
        </div>
      </div>

      {/* Success Notification */}
      {successMsg && (
        <div className="rounded-xl border border-emerald-200 bg-emerald-50/80 p-3.5 text-xs text-emerald-900 flex items-center gap-2 shadow-xs">
          <CheckCircle2 className="h-4 w-4 shrink-0 text-emerald-600" />
          <span>{successMsg}</span>
        </div>
      )}

      {/* Controls Bar: Search & View Switcher */}
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-3">
        <div className="relative flex-1 max-w-md">
          <Search className="absolute left-3.5 top-1/2 -translate-y-1/2 h-4 w-4 text-gray-400" />
          <input
            type="text"
            placeholder="Filter files by name..."
            value={searchQuery}
            onChange={(e) => setSearchQuery(e.target.value)}
            className="w-full rounded-lg border border-gray-200 bg-white pl-9 pr-4 py-2 text-xs text-slate-900 placeholder:text-gray-400 focus:border-indigo-500 focus:outline-none focus:ring-2 focus:ring-indigo-500/15 shadow-xs transition-all"
          />
        </div>

        <div className="flex items-center gap-3">
          <span className="text-xs text-gray-500 font-medium">
            {filteredFiles.length} of {files?.length || 0} files
          </span>

          <div className="flex rounded-lg border border-gray-200 bg-white p-0.5 shadow-xs text-xs">
            <button
              type="button"
              onClick={() => setViewMode("list")}
              className={`flex items-center gap-1 rounded-md px-2.5 py-1 font-medium transition-colors ${
                viewMode === "list"
                  ? "bg-gray-100 text-slate-900 font-semibold"
                  : "text-gray-500 hover:text-slate-900"
              }`}
            >
              <ListIcon className="h-3.5 w-3.5" />
              <span>List</span>
            </button>
            <button
              type="button"
              onClick={() => setViewMode("grid")}
              className={`flex items-center gap-1 rounded-md px-2.5 py-1 font-medium transition-colors ${
                viewMode === "grid"
                  ? "bg-gray-100 text-slate-900 font-semibold"
                  : "text-gray-500 hover:text-slate-900"
              }`}
            >
              <LayoutGrid className="h-3.5 w-3.5" />
              <span>Grid</span>
            </button>
          </div>
        </div>
      </div>

      {/* Files Display */}
      {isLoading ? (
        <div className="rounded-xl border border-gray-200/80 bg-white p-16 text-center text-xs text-gray-400 shadow-xs">
          <Loader2 className="mx-auto h-6 w-6 text-indigo-600 animate-spin mb-2" />
          Loading files from repository...
        </div>
      ) : isError ? (
        <div className="rounded-xl border border-rose-200 bg-rose-50/40 p-12 text-center space-y-2 text-rose-700 shadow-xs">
          <AlertCircle className="mx-auto h-8 w-8 text-rose-500" />
          <div className="text-sm font-semibold">Failed to load files</div>
          <p className="text-xs text-gray-500">Ensure the backend server is active at localhost:8080.</p>
        </div>
      ) : filteredFiles.length === 0 ? (
        <div className="rounded-xl border border-gray-200/80 bg-white p-16 text-center space-y-3 shadow-xs">
          <div className="mx-auto flex h-12 w-12 items-center justify-center rounded-2xl bg-gray-50 text-gray-400">
            <FolderOpen className="h-6 w-6" />
          </div>
          <div className="text-sm font-semibold text-slate-900">
            {searchQuery ? "No matching files" : "No files stored in pool yet"}
          </div>
          <p className="text-xs text-gray-500 max-w-sm mx-auto">
            {searchQuery
              ? "Try searching for a different file name."
              : "Upload a file to distribute chunks across your storage nodes."}
          </p>
          {!searchQuery && (
            <Link
              href="/upload"
              className="inline-flex items-center gap-2 rounded-lg bg-slate-900 px-4 py-2 text-xs font-semibold text-white shadow-xs hover:bg-slate-800 transition-colors"
            >
              <UploadCloud className="h-4 w-4" />
              Upload Now
            </Link>
          )}
        </div>
      ) : viewMode === "list" ? (
        <div className="rounded-xl border border-gray-200/80 bg-white shadow-xs overflow-hidden">
          <Table>
            <TableHeader>
              <TableRow className="border-b border-gray-100 bg-gray-50/50">
                <TableHead>File name</TableHead>
                <TableHead>Date uploaded</TableHead>
                <TableHead>Status & Chunks</TableHead>
                <TableHead>File size</TableHead>
                <TableHead className="text-right">Actions</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {filteredFiles.map((file) => (
                <TableRow key={file.id} className="hover:bg-gray-50/60 transition-colors">
                  <TableCell>
                    <div className="flex items-center gap-3">
                      <div className="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg bg-gray-50 border border-gray-100">
                        {getFileIcon(file.filename)}
                      </div>
                      <div className="min-w-0">
                        <Link
                          href={`/files/${file.id}`}
                          className="font-medium text-slate-900 hover:text-indigo-600 hover:underline truncate block text-sm"
                        >
                          {file.filename}
                        </Link>
                        <span className="text-[11px] text-gray-400 font-mono">
                          {file.id}
                        </span>
                      </div>
                    </div>
                  </TableCell>
                  <TableCell className="text-xs text-gray-500">
                    {formatDate(file.created_at)}
                  </TableCell>
                  <TableCell>
                    <div className="flex items-center gap-2">
                      <Badge
                        variant={
                          file.status === "COMPLETED"
                            ? "success"
                            : file.status === "UPLOADING"
                            ? "warning"
                            : file.status === "DEGRADED"
                            ? "warning"
                            : "destructive"
                        }
                      >
                        {file.status}
                      </Badge>
                      <span className="text-xs text-gray-400 font-mono">
                        {file.total_chunks} {file.total_chunks === 1 ? "chunk" : "chunks"}
                      </span>
                    </div>
                  </TableCell>
                  <TableCell className="text-xs font-medium text-slate-700">
                    {formatBytes(file.size)}
                  </TableCell>
                  <TableCell className="text-right">
                    <div className="flex items-center justify-end gap-1">
                      <Button
                        variant="ghost"
                        size="icon"
                        onClick={() => handleDownload(file.id, file.filename)}
                        disabled={downloadState?.status === "downloading" && downloadState.fileId === file.id}
                        title="Download file"
                        className="h-8 w-8 text-gray-500 hover:text-slate-900"
                      >
                        {downloadState?.status === "downloading" && downloadState.fileId === file.id ? (
                          <Loader2 className="h-4 w-4 animate-spin text-indigo-600" />
                        ) : (
                          <Download className="h-4 w-4" />
                        )}
                      </Button>
                      <Link
                        href={`/files/${file.id}`}
                        className={getButtonClassName("ghost", "sm", "h-8 px-2 text-xs text-gray-600 hover:text-slate-900")}
                      >
                        Details
                      </Link>
                      <Button
                        variant="ghost"
                        size="icon"
                        disabled={deletingId === file.id}
                        onClick={() => handleDelete(file.id, file.filename)}
                        className="h-8 w-8 text-gray-400 hover:text-rose-600 hover:bg-rose-50"
                        title="Delete file"
                      >
                        {deletingId === file.id ? (
                          <Loader2 className="h-4 w-4 animate-spin text-rose-600" />
                        ) : (
                          <Trash2 className="h-4 w-4" />
                        )}
                      </Button>
                    </div>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      ) : (
        /* Grid View Mode */
        <div className="grid grid-cols-1 sm:grid-cols-2 md:grid-cols-3 lg:grid-cols-4 gap-4">
          {filteredFiles.map((file) => (
            <div
              key={file.id}
              className="rounded-xl border border-gray-200/80 bg-white p-4 shadow-xs hover:border-gray-300 hover:shadow-sm transition-all flex flex-col justify-between group space-y-3"
            >
              <div className="flex items-center justify-between">
                <div className="flex h-10 w-10 items-center justify-center rounded-xl bg-gray-50 border border-gray-100">
                  {getFileIcon(file.filename)}
                </div>
                <Badge
                  variant={
                    file.status === "COMPLETED"
                      ? "success"
                      : file.status === "UPLOADING"
                      ? "warning"
                      : "destructive"
                  }
                >
                  {file.status}
                </Badge>
              </div>
              <div>
                <Link
                  href={`/files/${file.id}`}
                  className="font-semibold text-sm text-slate-900 hover:text-indigo-600 truncate block"
                >
                  {file.filename}
                </Link>
                <div className="flex items-center justify-between text-xs text-gray-400 mt-1">
                  <span>{formatBytes(file.size)}</span>
                  <span>{file.total_chunks} chunks</span>
                </div>
              </div>
              <div className="flex items-center justify-between pt-2 border-t border-gray-100 text-xs">
                <span className="text-[11px] text-gray-400">{formatDate(file.created_at)}</span>
                <div className="flex items-center gap-1">
                  <Button
                    variant="ghost"
                    size="icon"
                    onClick={() => handleDownload(file.id, file.filename)}
                    className="h-7 w-7 text-gray-500 hover:text-slate-900"
                  >
                    <Download className="h-3.5 w-3.5" />
                  </Button>
                  <Button
                    variant="ghost"
                    size="icon"
                    onClick={() => handleDelete(file.id, file.filename)}
                    className="h-7 w-7 text-gray-400 hover:text-rose-600 hover:bg-rose-50"
                  >
                    <Trash2 className="h-3.5 w-3.5" />
                  </Button>
                </div>
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

export default function FileManagerPage() {
  return (
    <React.Suspense fallback={<div className="py-20 text-center text-xs text-gray-400">Loading file manager...</div>}>
      <FileManagerContent />
    </React.Suspense>
  );
}
