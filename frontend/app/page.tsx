"use client";

import { useState } from "react";
import Link from "next/link";
import {
  HardDrive,
  Server,
  FileText,
  UploadCloud,
  ArrowRight,
  Download,
  CheckCircle2,
  AlertTriangle,
  Clock,
  AlertCircle,
  Activity,
  Layers,
  RefreshCw,
  Loader2,
} from "lucide-react";
import { usePoolStats } from "@/hooks/use-nodes";
import { useFiles } from "@/hooks/use-files";
import { useSystemActivity, useUploadSessions } from "@/hooks/use-system";
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Progress } from "@/components/ui/progress";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { formatBytes, formatDate } from "@/lib/utils";
import { downloadFileWithProgress, DownloadState } from "@/lib/api";

export default function DashboardPage() {
  const { data: stats, isLoading: statsLoading } = usePoolStats();
  const { data: files, isLoading: filesLoading } = useFiles();
  const { data: activity, isLoading: activityLoading, refetch: refetchActivity } = useSystemActivity();
  const { data: uploadSessions } = useUploadSessions();

  // Live download progress state
  const [downloadState, setDownloadState] = useState<DownloadState | null>(null);

  const totalCapacity = stats?.total_capacity || activity?.pool_stats?.total_capacity || 0;
  const usedCapacity = stats?.used_capacity || activity?.pool_stats?.used_capacity || 0;
  const freeCapacity = stats?.available_capacity || activity?.pool_stats?.available_capacity || (totalCapacity - usedCapacity);
  const usedPercent = totalCapacity > 0 ? Math.round((usedCapacity / totalCapacity) * 100) : 0;
  const nodeCount = stats?.node_count || activity?.pool_stats?.node_count || 0;
  const onlineNodes = stats?.online_nodes || activity?.pool_stats?.online_nodes || 0;
  const offlineNodes = stats?.offline_nodes || activity?.pool_stats?.offline_nodes || 0;

  const recentFiles = files ? [...files].slice(0, 5) : [];
  const activeUploads = uploadSessions
    ? uploadSessions.filter((s) => s.status === "UPLOADING" || s.status === "INITIALIZED")
    : [];

  const recentOps = activity?.recent_operations || [];
  const failedOps = activity?.failed_operations || [];

  // Handler for tracked download
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

  const formatUptime = (seconds: number) => {
    if (!seconds || seconds <= 0) return "Just started";
    const hrs = Math.floor(seconds / 3600);
    const mins = Math.floor((seconds % 3600) / 60);
    const secs = seconds % 60;
    if (hrs > 0) return `${hrs}h ${mins}m ${secs}s`;
    if (mins > 0) return `${mins}m ${secs}s`;
    return `${secs}s`;
  };

  return (
    <div className="space-y-8">
      {/* Top Banner / System Header */}
      <div className="flex flex-col md:flex-row md:items-center justify-between gap-4 pb-2 border-b border-slate-800/80">
        <div>
          <div className="flex items-center gap-2.5">
            <h1 className="text-2xl font-bold tracking-tight text-white">System Dashboard</h1>
            <Badge variant="outline" className="border-emerald-800/80 bg-emerald-950/40 text-emerald-400 font-mono text-xs flex items-center gap-1.5 py-0.5">
              <span className="h-1.5 w-1.5 rounded-full bg-emerald-400 animate-pulse" />
              Live Telemetry
            </Badge>
          </div>
          <p className="text-sm text-slate-400 mt-1 flex items-center gap-2">
            <span>Uptime: {activity?.uptime_seconds ? formatUptime(activity.uptime_seconds) : "Connected"}</span>
            <span>•</span>
            <span>Heterogeneous storage pool and node health monitoring</span>
          </p>
        </div>
        <div className="flex items-center gap-3">
          <Button
            variant="ghost"
            size="sm"
            onClick={() => refetchActivity()}
            className="text-xs text-slate-400 hover:text-white flex items-center gap-1.5"
            title="Refresh telemetry"
          >
            <RefreshCw className="h-3.5 w-3.5" />
            Refresh
          </Button>
          <Link href="/upload">
            <Button className="flex items-center gap-2">
              <UploadCloud className="h-4 w-4" />
              Upload File
            </Button>
          </Link>
          <Link href="/files">
            <Button variant="outline" className="flex items-center gap-2">
              <FileText className="h-4 w-4" />
              Files
            </Button>
          </Link>
        </div>
      </div>

      {/* Critical System Warnings / Failed Operations Alert */}
      {failedOps.length > 0 && (
        <div className="rounded-lg border border-red-900/70 bg-red-950/30 p-4 space-y-2 text-red-200">
          <div className="flex items-center gap-2 text-sm font-semibold text-red-300">
            <AlertTriangle className="h-4 w-4 text-red-400 shrink-0" />
            <span>Attention: {failedOps.length} degraded or failed {failedOps.length === 1 ? "operation" : "operations"} detected</span>
          </div>
          <div className="grid grid-cols-1 md:grid-cols-2 gap-2 text-xs">
            {failedOps.slice(0, 4).map((f) => (
              <div key={f.id} className="rounded border border-red-900/40 bg-red-950/40 p-2.5 flex items-start gap-2">
                <AlertCircle className="h-3.5 w-3.5 text-red-400 shrink-0 mt-0.5" />
                <div className="min-w-0">
                  <div className="font-medium text-red-200 truncate">{f.entity} ({f.type})</div>
                  <div className="text-red-300/80 text-[11px] truncate">{f.reason}</div>
                </div>
              </div>
            ))}
          </div>
        </div>
      )}

      {/* Storage Utilization & System Metrics Grid */}
      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
        {/* Storage Pool Utilization */}
        <Card className="border-slate-800 bg-slate-900/60">
          <CardHeader className="pb-2">
            <CardDescription className="text-xs uppercase tracking-wider font-mono flex items-center justify-between">
              <span>Storage Utilization</span>
              <HardDrive className="h-3.5 w-3.5 text-indigo-400" />
            </CardDescription>
            <CardTitle className="text-2xl font-bold text-white flex items-baseline gap-2">
              <span>{usedPercent}%</span>
              <span className="text-xs font-normal text-slate-400">
                ({statsLoading ? "..." : formatBytes(usedCapacity)} used)
              </span>
            </CardTitle>
          </CardHeader>
          <CardContent className="space-y-2">
            <Progress value={usedPercent} className="h-2" />
            <div className="text-xs text-slate-400 flex justify-between">
              <span>Total: {formatBytes(totalCapacity)}</span>
              <span className="text-emerald-400 font-medium">Free: {formatBytes(freeCapacity)}</span>
            </div>
          </CardContent>
        </Card>

        {/* Node Health Status */}
        <Card className="border-slate-800 bg-slate-900/60">
          <CardHeader className="pb-2">
            <CardDescription className="text-xs uppercase tracking-wider font-mono flex items-center justify-between">
              <span>Node Health</span>
              <Server className="h-3.5 w-3.5 text-emerald-400" />
            </CardDescription>
            <CardTitle className="text-2xl font-bold text-white flex items-center gap-2">
              <span className={offlineNodes > 0 ? "text-amber-400" : "text-emerald-400"}>
                {statsLoading ? "..." : `${onlineNodes} / ${nodeCount}`}
              </span>
              <span className="text-xs font-normal text-slate-400">online</span>
            </CardTitle>
          </CardHeader>
          <CardContent className="space-y-2">
            <div className="flex items-center justify-between text-xs">
              <span className="text-slate-400">Operational status</span>
              <Badge variant={offlineNodes === 0 ? "success" : "warning"} className="text-[10px]">
                {offlineNodes === 0 ? "All Healthy" : `${offlineNodes} Offline`}
              </Badge>
            </div>
            <div className="text-[11px] text-slate-500">
              <Link href="/nodes" className="text-indigo-400 hover:underline">
                View & simulate node failures →
              </Link>
            </div>
          </CardContent>
        </Card>

        {/* Files Overview */}
        <Card className="border-slate-800 bg-slate-900/60">
          <CardHeader className="pb-2">
            <CardDescription className="text-xs uppercase tracking-wider font-mono flex items-center justify-between">
              <span>Managed Files</span>
              <FileText className="h-3.5 w-3.5 text-blue-400" />
            </CardDescription>
            <CardTitle className="text-2xl font-bold text-white">
              {filesLoading ? "..." : files?.length || 0}
            </CardTitle>
          </CardHeader>
          <CardContent className="space-y-1.5">
            <div className="flex justify-between text-xs text-slate-400">
              <span>Completed: {activity?.completed_files ?? (files?.filter((f) => f.status === "COMPLETED").length || 0)}</span>
              {activity && activity.degraded_files > 0 && (
                <span className="text-amber-400 font-semibold">{activity.degraded_files} degraded</span>
              )}
            </div>
            <div className="text-[11px] text-slate-500">
              Distributed across logical chunks
            </div>
          </CardContent>
        </Card>

        {/* Active Operations Counter */}
        <Card className="border-slate-800 bg-slate-900/60">
          <CardHeader className="pb-2">
            <CardDescription className="text-xs uppercase tracking-wider font-mono flex items-center justify-between">
              <span>Active Operations</span>
              <Activity className="h-3.5 w-3.5 text-purple-400" />
            </CardDescription>
            <CardTitle className="text-2xl font-bold text-white flex items-center gap-2">
              <span>{activeUploads.length + (downloadState?.status === "downloading" ? 1 : 0)}</span>
              <span className="text-xs font-normal text-slate-400">running</span>
            </CardTitle>
          </CardHeader>
          <CardContent className="space-y-1.5">
            <div className="text-xs text-slate-400">
              <span>{activeUploads.length} uploads in progress</span>
            </div>
            <div className="text-[11px] text-slate-500">
              {downloadState?.status === "downloading" ? "1 active download stream" : "Idle background workers"}
            </div>
          </CardContent>
        </Card>
      </div>

      {/* Live Upload Progress Section (if any session active) */}
      {activeUploads.length > 0 && (
        <Card className="border-indigo-900/50 bg-indigo-950/20">
          <CardHeader className="pb-3 flex flex-row items-center justify-between">
            <div className="flex items-center gap-2">
              <Loader2 className="h-4 w-4 text-indigo-400 animate-spin" />
              <div>
                <CardTitle className="text-sm font-semibold text-indigo-200">Active Upload Progress</CardTitle>
                <CardDescription className="text-xs text-indigo-300/70">Real-time chunk streaming & distribution</CardDescription>
              </div>
            </div>
            <Link href="/upload">
              <Button variant="ghost" size="sm" className="text-xs text-indigo-300 hover:text-white">
                Upload Center →
              </Button>
            </Link>
          </CardHeader>
          <CardContent className="space-y-3">
            {activeUploads.map((session) => (
              <div key={session.upload_id} className="rounded-lg border border-indigo-900/40 bg-slate-950/60 p-3 space-y-2">
                <div className="flex items-center justify-between text-xs">
                  <div className="font-medium text-slate-200 flex items-center gap-2">
                    <FileText className="h-3.5 w-3.5 text-indigo-400" />
                    <span>{session.filename}</span>
                    <span className="text-slate-400">({formatBytes(session.total_size)})</span>
                  </div>
                  <div className="flex items-center gap-2">
                    <span className="text-indigo-300 font-mono font-medium">{session.progress_percent}%</span>
                    <Badge variant="warning" className="text-[10px]">{session.status}</Badge>
                  </div>
                </div>
                <Progress value={session.progress_percent} className="h-2" />
                <div className="flex justify-between text-[11px] text-slate-400">
                  <span>Chunks: {session.completed_count} / {session.total_chunks} completed</span>
                  <span>Target: {formatBytes(session.chunk_size)} / chunk</span>
                </div>
              </div>
            ))}
          </CardContent>
        </Card>
      )}

      {/* Live Download Progress Notification Card */}
      {downloadState && (
        <Card className={`border ${downloadState.status === "failed" ? "border-red-900 bg-red-950/20" : "border-emerald-900/50 bg-emerald-950/20"}`}>
          <CardHeader className="pb-2">
            <div className="flex items-center justify-between text-xs">
              <div className="flex items-center gap-2 font-medium">
                {downloadState.status === "downloading" && <Loader2 className="h-3.5 w-3.5 text-emerald-400 animate-spin" />}
                {downloadState.status === "completed" && <CheckCircle2 className="h-3.5 w-3.5 text-emerald-400" />}
                {downloadState.status === "failed" && <AlertCircle className="h-3.5 w-3.5 text-red-400" />}
                <span className="text-slate-200">
                  {downloadState.status === "downloading" ? "Streaming Download:" : downloadState.status === "completed" ? "Download Completed:" : "Download Error:"} {downloadState.filename}
                </span>
              </div>
              <span className="font-mono text-emerald-400 font-medium">
                {downloadState.percent > 0 ? `${downloadState.percent}%` : "Streaming..."}
              </span>
            </div>
          </CardHeader>
          <CardContent className="space-y-1.5 pt-0">
            <Progress value={downloadState.percent} className="h-1.5" />
            <div className="flex justify-between text-[11px] text-slate-400">
              <span>{formatBytes(downloadState.loadedBytes)} {downloadState.totalBytes > 0 ? `/ ${formatBytes(downloadState.totalBytes)}` : "retrieved"}</span>
              {downloadState.error && <span className="text-red-400">{downloadState.error}</span>}
            </div>
          </CardContent>
        </Card>
      )}

      {/* Nodes Distribution Quick Glance */}
      {stats?.nodes && stats.nodes.length > 0 && (
        <Card className="border-slate-800">
          <CardHeader className="flex flex-row items-center justify-between pb-3">
            <div>
              <CardTitle className="text-base font-semibold text-white">Storage Node Health & Allocation</CardTitle>
              <CardDescription className="text-xs">Capacity distribution and live node states</CardDescription>
            </div>
            <Link href="/nodes">
              <Button variant="ghost" size="sm" className="text-xs flex items-center gap-1 text-indigo-400">
                Manage Nodes <ArrowRight className="h-3 w-3" />
              </Button>
            </Link>
          </CardHeader>
          <CardContent>
            <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-3">
              {stats.nodes.map((node) => {
                const nodePercent = node.total_capacity > 0 ? Math.round((node.used_capacity / node.total_capacity) * 100) : 0;
                return (
                  <div key={node.id} className="rounded-lg border border-slate-800 bg-slate-950/60 p-3.5 space-y-2">
                    <div className="flex items-center justify-between">
                      <span className="font-medium text-sm text-slate-200 truncate pr-2">{node.name}</span>
                      <Badge variant={node.status === "ONLINE" ? "success" : node.status === "DEGRADED" ? "warning" : "destructive"}>
                        {node.status}
                      </Badge>
                    </div>
                    <Progress value={nodePercent} className="h-1.5" />
                    <div className="flex justify-between text-xs text-slate-400">
                      <span>{formatBytes(node.used_capacity)} / {formatBytes(node.total_capacity)}</span>
                      <span>{nodePercent}%</span>
                    </div>
                    <div className="text-[11px] text-slate-500 flex justify-between">
                      <span>{node.provider_type}</span>
                      <span>Avail: {formatBytes(node.available_capacity)}</span>
                    </div>
                  </div>
                );
              })}
            </div>
          </CardContent>
        </Card>
      )}

      {/* Recent Operations & Failed Operations Split Grid */}
      <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
        {/* Recent Operations Log */}
        <Card className="border-slate-800">
          <CardHeader className="pb-3 flex flex-row items-center justify-between">
            <div>
              <CardTitle className="text-base font-semibold text-white">Recent Operations</CardTitle>
              <CardDescription className="text-xs">Audit log of system tasks and chunk transfers</CardDescription>
            </div>
            <Badge variant="outline" className="text-[11px] font-mono text-slate-400">
              {recentOps.length} logged
            </Badge>
          </CardHeader>
          <CardContent>
            {activityLoading ? (
              <div className="py-8 text-center text-xs text-slate-400">Loading operations...</div>
            ) : recentOps.length === 0 ? (
              <div className="py-8 text-center text-xs text-slate-500">No recent operations recorded yet.</div>
            ) : (
              <div className="divide-y divide-slate-800/60 max-h-72 overflow-y-auto pr-1">
                {recentOps.slice(0, 8).map((op) => (
                  <div key={op.id} className="py-2.5 flex items-center justify-between text-xs">
                    <div className="space-y-0.5 min-w-0 pr-3">
                      <div className="font-medium text-slate-200 truncate flex items-center gap-1.5">
                        <span className="font-mono text-[11px] text-indigo-400 uppercase">[{op.type}]</span>
                        <span className="truncate">{op.entity}</span>
                      </div>
                      <div className="text-[11px] text-slate-400 truncate">{op.details}</div>
                    </div>
                    <div className="text-right shrink-0 space-y-1">
                      <Badge
                        variant={op.status === "SUCCESS" ? "success" : op.status === "RUNNING" ? "warning" : "destructive"}
                        className="text-[10px]"
                      >
                        {op.status}
                      </Badge>
                      <div className="text-[10px] text-slate-500 font-mono">{formatDate(op.timestamp)}</div>
                    </div>
                  </div>
                ))}
              </div>
            )}
          </CardContent>
        </Card>

        {/* Failed / Degraded Operations Diagnostics */}
        <Card className="border-slate-800">
          <CardHeader className="pb-3 flex flex-row items-center justify-between">
            <div>
              <CardTitle className="text-base font-semibold text-white">Diagnostics & Alerts</CardTitle>
              <CardDescription className="text-xs">Failed chunk retries, offline targets, and degraded files</CardDescription>
            </div>
            <Badge variant={failedOps.length > 0 ? "destructive" : "success"} className="text-[11px]">
              {failedOps.length} issues
            </Badge>
          </CardHeader>
          <CardContent>
            {activityLoading ? (
              <div className="py-8 text-center text-xs text-slate-400">Loading diagnostics...</div>
            ) : failedOps.length === 0 ? (
              <div className="py-10 text-center space-y-2">
                <CheckCircle2 className="mx-auto h-8 w-8 text-emerald-500" />
                <div className="text-sm font-medium text-slate-300">All Systems Operational</div>
                <p className="text-xs text-slate-500">
                  No failed chunk uploads, offline nodes, or degraded files detected.
                </p>
              </div>
            ) : (
              <div className="divide-y divide-slate-800/60 max-h-72 overflow-y-auto pr-1">
                {failedOps.map((f) => (
                  <div key={f.id} className="py-2.5 flex items-start justify-between text-xs gap-3">
                    <div className="space-y-0.5 min-w-0">
                      <div className="font-medium text-red-300 flex items-center gap-1.5 truncate">
                        <AlertTriangle className="h-3.5 w-3.5 text-amber-400 shrink-0" />
                        <span className="font-mono text-[11px] text-red-400">[{f.type}]</span>
                        <span className="truncate">{f.entity}</span>
                      </div>
                      <div className="text-[11px] text-slate-400">{f.reason}</div>
                    </div>
                    <div className="text-right shrink-0 space-y-1">
                      <Badge variant={f.severity === "HIGH" ? "destructive" : "warning"} className="text-[10px]">
                        {f.severity}
                      </Badge>
                      <div className="text-[10px] text-slate-500 font-mono">{formatDate(f.timestamp)}</div>
                    </div>
                  </div>
                ))}
              </div>
            )}
          </CardContent>
        </Card>
      </div>

      {/* Recent Files Table with Live Download Tracker */}
      <Card className="border-slate-800">
        <CardHeader className="flex flex-row items-center justify-between pb-3">
          <div>
            <CardTitle className="text-base font-semibold text-white">Recent Files</CardTitle>
            <CardDescription className="text-xs">Latest uploaded files across the pool</CardDescription>
          </div>
          <Link href="/files">
            <Button variant="ghost" size="sm" className="text-xs flex items-center gap-1 text-indigo-400">
              View all files <ArrowRight className="h-3 w-3" />
            </Button>
          </Link>
        </CardHeader>
        <CardContent>
          {filesLoading ? (
            <div className="py-8 text-center text-sm text-slate-400">Loading files...</div>
          ) : recentFiles.length === 0 ? (
            <div className="py-12 text-center space-y-3">
              <FileText className="mx-auto h-10 w-10 text-slate-600" />
              <div className="text-sm font-medium text-slate-300">No files uploaded yet</div>
              <p className="text-xs text-slate-500 max-w-sm mx-auto">
                Upload your first file to distribute chunks across the storage node pool.
              </p>
              <Link href="/upload">
                <Button size="sm" className="mt-2">Upload Now</Button>
              </Link>
            </div>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Filename</TableHead>
                  <TableHead>Size</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead>Chunks</TableHead>
                  <TableHead>Created</TableHead>
                  <TableHead className="text-right">Actions</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {recentFiles.map((file) => (
                  <TableRow key={file.id}>
                    <TableCell className="font-medium text-white flex items-center gap-2">
                      <FileText className="h-4 w-4 text-indigo-400 shrink-0" />
                      <Link href={`/files/${file.id}`} className="hover:underline hover:text-indigo-300 truncate max-w-xs">
                        {file.filename}
                      </Link>
                    </TableCell>
                    <TableCell>{formatBytes(file.size)}</TableCell>
                    <TableCell>
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
                    </TableCell>
                    <TableCell className="text-slate-400">{file.total_chunks}</TableCell>
                    <TableCell className="text-slate-400">{formatDate(file.created_at)}</TableCell>
                    <TableCell className="text-right space-x-2">
                      <Button
                        variant="ghost"
                        size="sm"
                        disabled={downloadState?.status === "downloading" && downloadState.fileId === file.id}
                        onClick={() => handleDownload(file.id, file.filename)}
                        title="Download file with progress tracking"
                      >
                        {downloadState?.status === "downloading" && downloadState.fileId === file.id ? (
                          <Loader2 className="h-3.5 w-3.5 text-indigo-400 animate-spin" />
                        ) : (
                          <Download className="h-3.5 w-3.5 text-slate-300" />
                        )}
                      </Button>
                      <Link href={`/files/${file.id}`}>
                        <Button variant="ghost" size="sm">Details</Button>
                      </Link>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
