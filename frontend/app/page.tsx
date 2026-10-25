"use client";

import Link from "next/link";
import { HardDrive, Server, FileText, UploadCloud, ArrowRight, Download, Trash2, CheckCircle2, AlertTriangle, Clock } from "lucide-react";
import { usePoolStats } from "@/hooks/use-nodes";
import { useFiles, useDeleteFile } from "@/hooks/use-files";
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Progress } from "@/components/ui/progress";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { formatBytes, formatDate } from "@/lib/utils";
import { downloadFile } from "@/lib/api";

export default function DashboardPage() {
  const { data: stats, isLoading: statsLoading } = usePoolStats();
  const { data: files, isLoading: filesLoading } = useFiles();
  const deleteMutation = useDeleteFile();

  const totalCapacity = stats?.total_capacity || 0;
  const usedCapacity = stats?.used_capacity || 0;
  const freeCapacity = stats?.available_capacity || (totalCapacity - usedCapacity);
  const usedPercent = totalCapacity > 0 ? Math.round((usedCapacity / totalCapacity) * 100) : 0;
  const nodeCount = stats?.node_count || 0;
  const onlineNodes = stats?.online_nodes || 0;

  const recentFiles = files ? [...files].slice(0, 5) : [];

  return (
    <div className="space-y-8">
      {/* Top Banner / Hero */}
      <div className="flex flex-col md:flex-row md:items-center justify-between gap-4 pb-2 border-b border-slate-800/80">
        <div>
          <h1 className="text-2xl font-bold tracking-tight text-white">Storage Pool Dashboard</h1>
          <p className="text-sm text-slate-400 mt-1">
            Real-time status of your distributed storage nodes and active logical pool.
          </p>
        </div>
        <div className="flex items-center gap-3">
          <Link href="/upload">
            <Button className="flex items-center gap-2">
              <UploadCloud className="h-4 w-4" />
              Upload File
            </Button>
          </Link>
          <Link href="/files">
            <Button variant="outline" className="flex items-center gap-2">
              <FileText className="h-4 w-4" />
              Manage Files
            </Button>
          </Link>
        </div>
      </div>

      {/* Metric Cards Grid */}
      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-5 gap-4">
        {/* Total Storage */}
        <Card>
          <CardHeader className="pb-2">
            <CardDescription className="text-xs uppercase tracking-wider font-mono">Total Pool</CardDescription>
            <CardTitle className="text-xl font-bold text-white">
              {statsLoading ? "..." : formatBytes(totalCapacity)}
            </CardTitle>
          </CardHeader>
          <CardContent>
            <div className="text-xs text-slate-400">Aggregated capacity</div>
          </CardContent>
        </Card>

        {/* Used Storage */}
        <Card>
          <CardHeader className="pb-2">
            <CardDescription className="text-xs uppercase tracking-wider font-mono">Used Storage</CardDescription>
            <CardTitle className="text-xl font-bold text-indigo-400">
              {statsLoading ? "..." : formatBytes(usedCapacity)}
            </CardTitle>
          </CardHeader>
          <CardContent className="space-y-1.5">
            <Progress value={usedPercent} className="h-1.5" />
            <div className="text-xs text-slate-400 flex justify-between">
              <span>{usedPercent}% allocated</span>
            </div>
          </CardContent>
        </Card>

        {/* Free Storage */}
        <Card>
          <CardHeader className="pb-2">
            <CardDescription className="text-xs uppercase tracking-wider font-mono">Free Storage</CardDescription>
            <CardTitle className="text-xl font-bold text-emerald-400">
              {statsLoading ? "..." : formatBytes(freeCapacity)}
            </CardTitle>
          </CardHeader>
          <CardContent>
            <div className="text-xs text-slate-400">Available across nodes</div>
          </CardContent>
        </Card>

        {/* Total Nodes */}
        <Card>
          <CardHeader className="pb-2">
            <CardDescription className="text-xs uppercase tracking-wider font-mono">Number of Nodes</CardDescription>
            <CardTitle className="text-xl font-bold text-white">
              {statsLoading ? "..." : nodeCount}
            </CardTitle>
          </CardHeader>
          <CardContent>
            <div className="text-xs text-slate-400">Heterogeneous storage targets</div>
          </CardContent>
        </Card>

        {/* Online Nodes */}
        <Card>
          <CardHeader className="pb-2">
            <CardDescription className="text-xs uppercase tracking-wider font-mono">Online Nodes</CardDescription>
            <CardTitle className="text-xl font-bold text-emerald-400 flex items-center gap-1.5">
              <CheckCircle2 className="h-5 w-5 text-emerald-500" />
              {statsLoading ? "..." : `${onlineNodes} / ${nodeCount}`}
            </CardTitle>
          </CardHeader>
          <CardContent>
            <div className="text-xs text-slate-400">Active and healthy</div>
          </CardContent>
        </Card>
      </div>

      {/* Nodes Distribution Quick Glance */}
      {stats?.nodes && stats.nodes.length > 0 && (
        <Card>
          <CardHeader className="flex flex-row items-center justify-between pb-3">
            <div>
              <CardTitle className="text-base font-semibold">Active Storage Nodes</CardTitle>
              <CardDescription className="text-xs">Capacity distribution and health</CardDescription>
            </div>
            <Link href="/nodes">
              <Button variant="ghost" size="sm" className="text-xs flex items-center gap-1 text-indigo-400">
                View all nodes <ArrowRight className="h-3 w-3" />
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
                      <span className="font-medium text-sm text-slate-200">{node.name}</span>
                      <Badge variant={node.status === "ONLINE" ? "success" : "destructive"}>
                        {node.status}
                      </Badge>
                    </div>
                    <Progress value={nodePercent} className="h-1.5" />
                    <div className="flex justify-between text-xs text-slate-400">
                      <span>{formatBytes(node.used_capacity)} / {formatBytes(node.total_capacity)}</span>
                      <span>{nodePercent}%</span>
                    </div>
                  </div>
                );
              })}
            </div>
          </CardContent>
        </Card>
      )}

      {/* Recent Files Table */}
      <Card>
        <CardHeader className="flex flex-row items-center justify-between pb-3">
          <div>
            <CardTitle className="text-base font-semibold">Recent Files</CardTitle>
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
                      <Link href={`/files/${file.id}`} className="hover:underline hover:text-indigo-300">
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
                        onClick={() => downloadFile(file.id, file.filename)}
                        title="Download file"
                      >
                        <Download className="h-3.5 w-3.5 text-slate-300" />
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
