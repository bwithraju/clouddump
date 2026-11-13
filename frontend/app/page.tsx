"use client";

import { useState } from "react";
import Link from "next/link";
import {
  UploadCloud,
  Download,
  CheckCircle2,
  AlertTriangle,
  AlertCircle,
  Activity,
  RefreshCw,
  Loader2,
  Trash2,
  Folder,
  FileText,
  FileCode,
  Image as ImageIcon,
  Film,
  Archive,
  MoreVertical,
  Filter,
  List as ListIcon,
  LayoutGrid,
  HardDrive,
  Server,
  Cloud,
  ArrowRight,
  ShieldAlert,
  Sparkles,
} from "lucide-react";
import { useQueryClient } from "@tanstack/react-query";
import { usePoolStats } from "@/hooks/use-nodes";
import { useFiles } from "@/hooks/use-files";
import { useSystemActivity, useUploadSessions } from "@/hooks/use-system";
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button, getButtonClassName } from "@/components/ui/button";
import { Progress } from "@/components/ui/progress";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { formatBytes, formatDate } from "@/lib/utils";
import { downloadFileWithProgress, DownloadState, deleteFile } from "@/lib/api";

export default function DashboardPage() {
  const queryClient = useQueryClient();
  const { data: stats, isLoading: statsLoading, refetch: refetchStats } = usePoolStats();
  const { data: files, isLoading: filesLoading } = useFiles();
  const { data: activity, isLoading: activityLoading, refetch: refetchActivity } = useSystemActivity();
  const { data: uploadSessions } = useUploadSessions();

  // View state: List vs Grid
  const [viewMode, setViewMode] = useState<"list" | "grid">("list");
  const [downloadState, setDownloadState] = useState<DownloadState | null>(null);
  const [deletingId, setDeletingId] = useState<string | null>(null);
  const [selectedFileIds, setSelectedFileIds] = useState<Record<string, boolean>>({});

  const totalCapacity = stats?.total_capacity || activity?.pool_stats?.total_capacity || 0;
  const usedCapacity = stats?.used_capacity || activity?.pool_stats?.used_capacity || 0;
  const freeCapacity = stats?.available_capacity || activity?.pool_stats?.available_capacity || Math.max(0, totalCapacity - usedCapacity);
  const usedPercent = totalCapacity > 0 ? Math.min(100, Math.round((usedCapacity / totalCapacity) * 100)) : 0;
  const nodeCount = stats?.node_count || activity?.pool_stats?.node_count || 0;
  const onlineNodes = stats?.online_nodes || activity?.pool_stats?.online_nodes || 0;
  const offlineNodes = stats?.offline_nodes || activity?.pool_stats?.offline_nodes || 0;

  const recentFiles = files ? [...files].slice(0, 8) : [];
  const activeUploads = uploadSessions
    ? uploadSessions.filter((s) => s.status === "UPLOADING" || s.status === "INITIALIZED")
    : [];

  const recentOps = activity?.recent_operations || [];
  const failedOps = activity?.failed_operations || [];

  // Helper to get matching file type icon & color
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

  const handleDelete = async (fileId: string, filename: string) => {
    if (
      !window.confirm(
        `Are you sure you want to delete "${filename}"? All stored chunks across distributed nodes will be permanently removed.`
      )
    ) {
      return;
    }

    setDeletingId(fileId);
    try {
      await deleteFile(fileId);
      queryClient.invalidateQueries({ queryKey: ["files"] });
      queryClient.invalidateQueries({ queryKey: ["nodes-stats"] });
      queryClient.invalidateQueries({ queryKey: ["nodes"] });
      queryClient.invalidateQueries({ queryKey: ["system/activity"] });
    } catch (err: any) {
      alert(`Failed to delete file: ${err.message || err}`);
    } finally {
      setDeletingId(null);
    }
  };

  const toggleSelectAll = (checked: boolean) => {
    const next: Record<string, boolean> = {};
    if (checked) {
      recentFiles.forEach((f) => {
        next[f.id] = true;
      });
    }
    setSelectedFileIds(next);
  };

  const toggleSelectOne = (id: string) => {
    setSelectedFileIds((prev) => ({ ...prev, [id]: !prev[id] }));
  };

  const isAllSelected = recentFiles.length > 0 && recentFiles.every((f) => selectedFileIds[f.id]);

  return (
    <div className="space-y-7">
      {/* 1. Header with Title & "+ Upload file" Button (Matching BacCloud Reference) */}
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4">
        <div>
          <h1 className="text-2xl font-bold tracking-tight text-slate-900">Dashboard</h1>
          <p className="text-xs text-gray-500 mt-0.5">
            Overview of your distributed cloud storage, active nodes, and recent files.
          </p>
        </div>
        <div className="flex items-center gap-2.5">
          <Button
            variant="outline"
            size="sm"
            onClick={() => {
              refetchStats();
              refetchActivity();
            }}
            className="text-xs flex items-center gap-1.5 text-gray-600 hover:text-slate-900"
            title="Refresh pool telemetry"
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

      {/* 2. Soft Purple BacCloud-Style Notice Banner */}
      <div className="rounded-xl border border-indigo-100 bg-indigo-50/70 p-3.5 px-4 text-xs text-indigo-950 flex flex-col sm:flex-row sm:items-center justify-between gap-2 shadow-xs">
        <div className="flex items-center gap-2.5">
          <div className="flex h-5 w-5 shrink-0 items-center justify-center rounded-full bg-indigo-200 text-indigo-800 font-bold text-[11px]">
            !
          </div>
          <div>
            {offlineNodes > 0 ? (
              <span>
                Attention: <strong className="font-semibold">{offlineNodes} storage {offlineNodes === 1 ? "node is" : "nodes are"} offline</strong>. Replication failover is active.
              </span>
            ) : (
              <span>
                All <strong className="font-semibold">{onlineNodes} storage nodes</strong> are healthy and online. Distributed chunk replication is active.
              </span>
            )}
          </div>
        </div>
        <Link
          href="/nodes"
          className="text-indigo-700 hover:text-indigo-900 font-semibold underline underline-offset-2 shrink-0 text-xs"
        >
          View storage nodes →
        </Link>
      </div>

      {/* 3. Total Storage Hero Utilization (Matching BacCloud Reference) */}
      <div className="space-y-3">
        <div className="flex items-baseline justify-between">
          <div>
            <div className="text-[11px] font-semibold text-gray-400 uppercase tracking-wider">
              Total Storage
            </div>
            <div className="flex items-baseline gap-2 mt-0.5">
              <span className="text-3xl font-extrabold tracking-tight text-slate-900">
                {statsLoading ? "..." : formatBytes(usedCapacity)}
              </span>
              <span className="text-xs text-gray-500 font-normal">
                Used from {formatBytes(totalCapacity)}
              </span>
            </div>
          </div>
          <div className="text-xs text-gray-500 font-medium">
            <span className="text-indigo-600 font-semibold">{usedPercent}%</span> capacity used
          </div>
        </div>

        {/* Multi-segmented Sleek Storage Bar */}
        <div className="h-3 w-full overflow-hidden rounded-full bg-gray-100 flex p-0.5">
          {/* Blue / Indigo Primary Segment */}
          <div
            style={{ width: `${Math.min(100, usedPercent)}%` }}
            className="h-full rounded-full bg-gradient-to-r from-indigo-600 via-indigo-500 to-purple-600 transition-all duration-500"
          />
        </div>

        {/* Legend dots underneath */}
        <div className="flex flex-wrap items-center gap-5 text-xs text-gray-500 pt-0.5">
          <div className="flex items-center gap-1.5">
            <span className="h-2 w-2 rounded-full bg-indigo-600" />
            <span className="font-medium text-slate-700">Primary Chunks</span>
            <span className="text-gray-400 font-mono">({formatBytes(usedCapacity)})</span>
          </div>
          <div className="flex items-center gap-1.5">
            <span className="h-2 w-2 rounded-full bg-purple-500" />
            <span className="font-medium text-slate-700">Online Nodes</span>
            <span className="text-gray-400 font-mono">({onlineNodes} / {nodeCount})</span>
          </div>
          <div className="flex items-center gap-1.5">
            <span className="h-2 w-2 rounded-full bg-amber-400" />
            <span className="font-medium text-slate-700">Free Headroom</span>
            <span className="text-gray-400 font-mono">({formatBytes(freeCapacity)})</span>
          </div>
        </div>
      </div>

      {/* 4. "Quick access" Section (4 Cards Matching BacCloud Reference) */}
      <div className="space-y-3">
        <div className="flex items-center justify-between">
          <h2 className="text-base font-bold text-slate-900">Quick access</h2>
          <Link
            href="/nodes"
            className="text-xs font-medium text-indigo-600 hover:text-indigo-800"
          >
            See all nodes →
          </Link>
        </div>

        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
          {/* Card 1: Local Storage Pool */}
          <div className="rounded-xl border border-gray-200/80 bg-white p-4 shadow-xs hover:border-gray-300 hover:shadow-sm transition-all flex flex-col justify-between group">
            <div className="flex items-center justify-between mb-4">
              <div className="flex h-10 w-10 items-center justify-center rounded-xl bg-indigo-50 text-indigo-600 group-hover:scale-105 transition-transform">
                <Folder className="h-5 w-5 fill-indigo-600/30 text-indigo-600" />
              </div>
              <Link
                href="/nodes"
                className="text-gray-400 hover:text-slate-700 p-1"
                title="Node settings"
              >
                <MoreVertical className="h-4 w-4" />
              </Link>
            </div>
            <div>
              <div className="font-semibold text-sm text-slate-900 truncate">
                {stats?.nodes?.[0]?.name || "Local Node Alpha"}
              </div>
              <div className="text-xs text-gray-400 mt-0.5">
                {stats?.nodes?.[0]
                  ? `${formatBytes(stats.nodes[0].used_capacity)} • ${stats.nodes[0].status}`
                  : `${formatBytes(usedCapacity)} • Active`}
              </div>
            </div>
          </div>

          {/* Card 2: Replicated Secondary Node */}
          <div className="rounded-xl border border-gray-200/80 bg-white p-4 shadow-xs hover:border-gray-300 hover:shadow-sm transition-all flex flex-col justify-between group">
            <div className="flex items-center justify-between mb-4">
              <div className="flex h-10 w-10 items-center justify-center rounded-xl bg-purple-50 text-purple-600 group-hover:scale-105 transition-transform">
                <Folder className="h-5 w-5 fill-purple-600/30 text-purple-600" />
              </div>
              <Link
                href="/nodes"
                className="text-gray-400 hover:text-slate-700 p-1"
                title="Node settings"
              >
                <MoreVertical className="h-4 w-4" />
              </Link>
            </div>
            <div>
              <div className="font-semibold text-sm text-slate-900 truncate">
                {stats?.nodes?.[1]?.name || "Local Node Beta"}
              </div>
              <div className="text-xs text-gray-400 mt-0.5">
                {stats?.nodes?.[1]
                  ? `${formatBytes(stats.nodes[1].used_capacity)} • ${stats.nodes[1].status}`
                  : `Redundancy 2x • Active`}
              </div>
            </div>
          </div>

          {/* Card 3: Google Drive Cloud Pool */}
          <div className="rounded-xl border border-gray-200/80 bg-white p-4 shadow-xs hover:border-gray-300 hover:shadow-sm transition-all flex flex-col justify-between group">
            <div className="flex items-center justify-between mb-4">
              <div className="flex h-10 w-10 items-center justify-center rounded-xl bg-blue-50 text-blue-600 group-hover:scale-105 transition-transform">
                <Cloud className="h-5 w-5 text-blue-600" />
              </div>
              <Link
                href="/nodes"
                className="text-gray-400 hover:text-slate-700 p-1"
                title="Connect Google Drive"
              >
                <MoreVertical className="h-4 w-4" />
              </Link>
            </div>
            <div>
              <div className="font-semibold text-sm text-slate-900 truncate">
                Google Drive Storage
              </div>
              <div className="text-xs text-gray-400 mt-0.5">
                OAuth 2.0 Integration • Connected
              </div>
            </div>
          </div>

          {/* Card 4: Latest File or Upload Shortcut */}
          <div className="rounded-xl border border-gray-200/80 bg-white p-4 shadow-xs hover:border-gray-300 hover:shadow-sm transition-all flex flex-col justify-between group">
            <div className="flex items-center justify-between mb-4">
              <div className="flex h-10 w-10 items-center justify-center rounded-xl bg-amber-50 text-amber-600 group-hover:scale-105 transition-transform">
                <FileText className="h-5 w-5 text-amber-600" />
              </div>
              <Link
                href="/files"
                className="text-gray-400 hover:text-slate-700 p-1"
                title="View files"
              >
                <MoreVertical className="h-4 w-4" />
              </Link>
            </div>
            <div>
              <div className="font-semibold text-sm text-slate-900 truncate">
                {recentFiles[0]?.filename || "Ready for Upload"}
              </div>
              <div className="text-xs text-gray-400 mt-0.5">
                {recentFiles[0]
                  ? `${formatBytes(recentFiles[0].size)} • ${recentFiles[0].total_chunks} chunks`
                  : "Drag & drop files anytime"}
              </div>
            </div>
          </div>
        </div>
      </div>

      {/* 5. Live Upload / Download Progress Trackers */}
      {activeUploads.length > 0 && (
        <div className="rounded-xl border border-indigo-100 bg-white p-4 shadow-xs space-y-3">
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-2">
              <Loader2 className="h-4 w-4 text-indigo-600 animate-spin" />
              <span className="text-xs font-semibold text-slate-900">Active Chunk Upload</span>
            </div>
            <Link href="/upload" className="text-xs text-indigo-600 font-medium hover:underline">
              Upload center →
            </Link>
          </div>
          {activeUploads.map((session) => (
            <div key={session.upload_id} className="space-y-1.5">
              <div className="flex justify-between text-xs text-slate-700">
                <span className="font-medium truncate max-w-md">{session.filename}</span>
                <span className="font-mono text-indigo-600">{session.progress_percent}%</span>
              </div>
              <Progress value={session.progress_percent} className="h-2" />
              <div className="flex justify-between text-[11px] text-gray-400">
                <span>Chunks: {session.completed_count} / {session.total_chunks}</span>
                <span>{formatBytes(session.total_size)}</span>
              </div>
            </div>
          ))}
        </div>
      )}

      {downloadState && (
        <div className="rounded-xl border border-gray-200 bg-white p-4 shadow-xs space-y-2">
          <div className="flex items-center justify-between text-xs">
            <div className="flex items-center gap-2 font-medium text-slate-900">
              {downloadState.status === "downloading" && <Loader2 className="h-4 w-4 text-indigo-600 animate-spin" />}
              {downloadState.status === "completed" && <CheckCircle2 className="h-4 w-4 text-emerald-600" />}
              {downloadState.status === "failed" && <AlertCircle className="h-4 w-4 text-rose-600" />}
              <span>
                {downloadState.status === "downloading" ? "Downloading: " : downloadState.status === "completed" ? "Downloaded: " : "Failed: "}
                {downloadState.filename}
              </span>
            </div>
            <span className="font-mono text-indigo-600 font-medium">
              {downloadState.percent > 0 ? `${downloadState.percent}%` : "Streaming..."}
            </span>
          </div>
          <Progress value={downloadState.percent} className="h-2" />
          <div className="flex justify-between text-[11px] text-gray-400">
            <span>{formatBytes(downloadState.loadedBytes)} of {formatBytes(downloadState.totalBytes)}</span>
            {downloadState.error && <span className="text-rose-600">{downloadState.error}</span>}
          </div>
        </div>
      )}

      {/* 6. "All documents" Section (Matching BacCloud Reference Table) */}
      <div className="space-y-3">
        {/* Section Header Controls */}
        <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-3">
          <h2 className="text-base font-bold text-slate-900">All documents</h2>
          <div className="flex items-center gap-2">
            <Link
              href="/files"
              className="inline-flex items-center gap-1.5 rounded-lg border border-gray-200 bg-white px-3 py-1.5 text-xs font-medium text-slate-700 hover:bg-gray-50 transition-colors shadow-xs"
            >
              <Filter className="h-3.5 w-3.5 text-gray-500" />
              <span>Filter</span>
            </Link>

            <div className="flex rounded-lg border border-gray-200 bg-white p-0.5 shadow-xs text-xs">
              <button
                type="button"
                onClick={() => setViewMode("list")}
                className={`flex items-center gap-1 rounded-md px-2.5 py-1 font-medium transition-colors ${
                  viewMode === "list"
                    ? "bg-gray-100 text-slate-900 font-semibold"
                    : "text-gray-500 hover:text-slate-900"
                }`}
                title="List view"
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
                title="Grid view"
              >
                <LayoutGrid className="h-3.5 w-3.5" />
                <span>Grid</span>
              </button>
            </div>
          </div>
        </div>

        {/* Files Content: Table or Grid */}
        {filesLoading ? (
          <div className="rounded-xl border border-gray-200/80 bg-white p-12 text-center text-xs text-gray-400">
            <Loader2 className="mx-auto h-6 w-6 text-indigo-600 animate-spin mb-2" />
            Loading distributed files...
          </div>
        ) : recentFiles.length === 0 ? (
          <div className="rounded-xl border border-gray-200/80 bg-white p-12 text-center space-y-3">
            <div className="mx-auto flex h-12 w-12 items-center justify-center rounded-2xl bg-gray-50 text-gray-400">
              <Folder className="h-6 w-6" />
            </div>
            <div className="text-sm font-semibold text-slate-900">No documents yet</div>
            <p className="text-xs text-gray-500 max-w-sm mx-auto">
              Upload your first document to distribute chunks across your storage nodes.
            </p>
            <Link
              href="/upload"
              className="inline-flex items-center gap-2 rounded-lg bg-slate-900 px-4 py-2 text-xs font-semibold text-white shadow-xs hover:bg-slate-800 transition-colors"
            >
              <UploadCloud className="h-4 w-4" />
              Upload Now
            </Link>
          </div>
        ) : viewMode === "list" ? (
          <div className="rounded-xl border border-gray-200/80 bg-white shadow-xs overflow-hidden">
            <Table>
              <TableHeader>
                <TableRow className="border-b border-gray-100 bg-gray-50/50">
                  <TableHead className="w-10">
                    <input
                      type="checkbox"
                      checked={isAllSelected}
                      onChange={(e) => toggleSelectAll(e.target.checked)}
                      className="rounded border-gray-300 text-indigo-600 focus:ring-indigo-500 h-3.5 w-3.5"
                    />
                  </TableHead>
                  <TableHead>File name</TableHead>
                  <TableHead>Date uploaded</TableHead>
                  <TableHead>Status & Chunks</TableHead>
                  <TableHead>File size</TableHead>
                  <TableHead className="text-right">Actions</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {recentFiles.map((file) => {
                  const isSelected = !!selectedFileIds[file.id];
                  return (
                    <TableRow key={file.id} className="hover:bg-gray-50/60 transition-colors">
                      <TableCell className="w-10">
                        <input
                          type="checkbox"
                          checked={isSelected}
                          onChange={() => toggleSelectOne(file.id)}
                          className="rounded border-gray-300 text-indigo-600 focus:ring-indigo-500 h-3.5 w-3.5"
                        />
                      </TableCell>
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
                              {file.id.slice(0, 16)}...
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
                  );
                })}
              </TableBody>
            </Table>
          </div>
        ) : (
          /* Grid View Mode */
          <div className="grid grid-cols-1 sm:grid-cols-2 md:grid-cols-3 lg:grid-cols-4 gap-4">
            {recentFiles.map((file) => (
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

      {/* 7. Bottom Grid: Recent System Operations & Diagnostics Log */}
      <div className="grid grid-cols-1 lg:grid-cols-2 gap-6 pt-2">
        {/* Recent Operations */}
        <div className="rounded-xl border border-gray-200/80 bg-white p-5 shadow-xs space-y-3">
          <div className="flex items-center justify-between border-b border-gray-100 pb-3">
            <div>
              <h3 className="text-sm font-semibold text-slate-900">Recent Operations</h3>
              <p className="text-xs text-gray-500">Chunk distribution and system background tasks</p>
            </div>
            <span className="text-xs font-mono text-gray-400">{recentOps.length} logged</span>
          </div>

          {activityLoading ? (
            <div className="py-6 text-center text-xs text-gray-400">Loading operations...</div>
          ) : recentOps.length === 0 ? (
            <div className="py-6 text-center text-xs text-gray-400">No recent operations recorded.</div>
          ) : (
            <div className="divide-y divide-gray-100 max-h-60 overflow-y-auto pr-1 text-xs">
              {recentOps.slice(0, 6).map((op) => (
                <div key={op.id} className="py-2.5 flex items-center justify-between">
                  <div className="min-w-0 pr-3">
                    <div className="font-medium text-slate-800 truncate flex items-center gap-1.5">
                      <span className="font-mono text-[10px] text-indigo-600 font-semibold uppercase">[{op.type}]</span>
                      <span className="truncate">{op.entity}</span>
                    </div>
                    <div className="text-[11px] text-gray-400 truncate">{op.details}</div>
                  </div>
                  <div className="text-right shrink-0">
                    <Badge variant={op.status === "SUCCESS" ? "success" : "warning"} className="text-[10px]">
                      {op.status}
                    </Badge>
                    <div className="text-[10px] text-gray-400 font-mono mt-0.5">{formatDate(op.timestamp)}</div>
                  </div>
                </div>
              ))}
            </div>
          )}
        </div>

        {/* Diagnostics & Node Health Status */}
        <div className="rounded-xl border border-gray-200/80 bg-white p-5 shadow-xs space-y-3">
          <div className="flex items-center justify-between border-b border-gray-100 pb-3">
            <div>
              <h3 className="text-sm font-semibold text-slate-900">Diagnostics & Failover</h3>
              <p className="text-xs text-gray-500">Node health checks and failed chunk retries</p>
            </div>
            <Badge variant={failedOps.length > 0 ? "destructive" : "success"} className="text-[10px]">
              {failedOps.length === 0 ? "All Healthy" : `${failedOps.length} alerts`}
            </Badge>
          </div>

          {failedOps.length === 0 ? (
            <div className="py-8 text-center space-y-2">
              <CheckCircle2 className="mx-auto h-7 w-7 text-emerald-500" />
              <div className="text-xs font-semibold text-slate-900">System Fully Operational</div>
              <p className="text-xs text-gray-500 max-w-xs mx-auto">
                All storage nodes are connected with 0 degraded files and 0 chunk write errors.
              </p>
            </div>
          ) : (
            <div className="divide-y divide-gray-100 max-h-60 overflow-y-auto pr-1 text-xs">
              {failedOps.map((f) => (
                <div key={f.id} className="py-2.5 flex items-start justify-between gap-3">
                  <div className="min-w-0">
                    <div className="font-medium text-rose-700 flex items-center gap-1.5 truncate">
                      <AlertTriangle className="h-3.5 w-3.5 text-amber-500 shrink-0" />
                      <span className="font-mono text-[10px]">[{f.type}]</span>
                      <span className="truncate">{f.entity}</span>
                    </div>
                    <div className="text-[11px] text-gray-500 mt-0.5">{f.reason}</div>
                  </div>
                  <Badge variant={f.severity === "HIGH" ? "destructive" : "warning"} className="text-[10px]">
                    {f.severity}
                  </Badge>
                </div>
              ))}
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
