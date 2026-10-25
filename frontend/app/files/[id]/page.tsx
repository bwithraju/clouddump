"use client";

import * as React from "react";
import Link from "next/link";
import { useParams, useRouter } from "next/navigation";
import { ArrowLeft, Download, Trash2, FileText, CheckCircle2, ShieldCheck, HardDrive, Layers, AlertCircle } from "lucide-react";
import { useFileDetails, useDeleteFile } from "@/hooks/use-files";
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { formatBytes, formatDate } from "@/lib/utils";
import { downloadFile } from "@/lib/api";

export default function FileDetailsPage() {
  const params = useParams();
  const router = useRouter();
  const fileId = Array.isArray(params.id) ? params.id[0] : params.id;

  const { data, isLoading, isError } = useFileDetails(fileId as string);
  const deleteMutation = useDeleteFile();
  const [isDeleting, setIsDeleting] = React.useState(false);

  const file = data?.file;
  const chunks = data?.chunks || [];
  const distribution = data?.distribution || {};

  const handleDelete = async () => {
    if (!file) return;
    if (!confirm(`Are you sure you want to delete "${file.filename}" and its ${chunks.length} chunks?`)) {
      return;
    }

    try {
      setIsDeleting(true);
      await deleteMutation.mutateAsync(file.id);
      router.push("/files");
    } catch (err: any) {
      alert(`Failed to delete file: ${err.message || err}`);
      setIsDeleting(false);
    }
  };

  if (isLoading) {
    return (
      <div className="py-20 text-center text-sm text-slate-400">
        Loading file details and chunk distribution...
      </div>
    );
  }

  if (isError || !file) {
    return (
      <div className="py-20 text-center space-y-4">
        <AlertCircle className="mx-auto h-10 w-10 text-rose-500" />
        <h2 className="text-lg font-semibold text-white">File Not Found</h2>
        <p className="text-sm text-slate-400">The requested file could not be found or has been deleted.</p>
        <Link href="/files">
          <Button variant="outline" size="sm">Back to File Manager</Button>
        </Link>
      </div>
    );
  }

  return (
    <div className="space-y-6">
      {/* Top Breadcrumb & Actions */}
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4 pb-2 border-b border-slate-800">
        <div className="flex items-center gap-3">
          <Link href="/files">
            <Button variant="ghost" size="sm" className="h-9 px-2 text-slate-400 hover:text-white">
              <ArrowLeft className="h-4 w-4" />
            </Button>
          </Link>
          <div>
            <div className="flex items-center gap-2">
              <h1 className="text-xl font-bold tracking-tight text-white">{file.filename}</h1>
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
            <div className="text-xs text-slate-500 font-mono mt-0.5">{file.id}</div>
          </div>
        </div>

        <div className="flex items-center gap-2">
          <Button
            onClick={() => downloadFile(file.id, file.filename)}
            className="flex items-center gap-2 bg-emerald-600 hover:bg-emerald-500"
          >
            <Download className="h-4 w-4" />
            Download File
          </Button>

          <Button
            variant="destructive"
            onClick={handleDelete}
            disabled={isDeleting}
            className="flex items-center gap-2"
          >
            <Trash2 className="h-4 w-4" />
            {isDeleting ? "Deleting..." : "Delete"}
          </Button>
        </div>
      </div>

      {/* File Overview Cards */}
      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
        <Card>
          <CardHeader className="pb-2">
            <CardDescription className="text-xs uppercase tracking-wider font-mono">File Size</CardDescription>
            <CardTitle className="text-lg font-bold text-white">{formatBytes(file.size)}</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="text-xs text-slate-400">{file.size.toLocaleString()} bytes</div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="pb-2">
            <CardDescription className="text-xs uppercase tracking-wider font-mono">MIME Type</CardDescription>
            <CardTitle className="text-lg font-bold text-slate-300 font-mono text-sm truncate">
              {file.mime_type || "application/octet-stream"}
            </CardTitle>
          </CardHeader>
          <CardContent>
            <div className="text-xs text-slate-400">Content type</div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="pb-2">
            <CardDescription className="text-xs uppercase tracking-wider font-mono">Chunk Configuration</CardDescription>
            <CardTitle className="text-lg font-bold text-indigo-400">
              {file.total_chunks} {file.total_chunks === 1 ? "Chunk" : "Chunks"}
            </CardTitle>
          </CardHeader>
          <CardContent>
            <div className="text-xs text-slate-400">Target size: {formatBytes(file.chunk_size)}</div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="pb-2">
            <CardDescription className="text-xs uppercase tracking-wider font-mono">Created Date</CardDescription>
            <CardTitle className="text-base font-bold text-slate-300">
              {formatDate(file.created_at)}
            </CardTitle>
          </CardHeader>
          <CardContent>
            <div className="text-xs text-slate-400">User: {file.user_id}</div>
          </CardContent>
        </Card>
      </div>

      {/* Node Chunk Distribution Summary */}
      <Card>
        <CardHeader className="pb-3">
          <CardTitle className="text-base font-semibold flex items-center gap-2">
            <HardDrive className="h-4 w-4 text-indigo-400" />
            Storage Node Distribution
          </CardTitle>
          <CardDescription className="text-xs">
            How chunks of this file are allocated across independent physical nodes
          </CardDescription>
        </CardHeader>
        <CardContent>
          <div className="grid grid-cols-2 sm:grid-cols-4 gap-3">
            {Object.keys(distribution).length === 0 ? (
              <div className="text-xs text-slate-500">No node distribution data available</div>
            ) : (
              Object.entries(distribution).map(([nodeId, count]) => {
                const pct = Math.round((count / file.total_chunks) * 100);
                return (
                  <div key={nodeId} className="rounded-lg border border-slate-800 bg-slate-950/60 p-3 space-y-1">
                    <div className="flex items-center justify-between text-xs">
                      <span className="font-mono text-indigo-300 font-semibold">{nodeId}</span>
                      <span className="text-slate-400">{pct}%</span>
                    </div>
                    <div className="text-sm font-bold text-white">
                      {count} {count === 1 ? "chunk" : "chunks"}
                    </div>
                  </div>
                );
              })
            )}
          </div>
        </CardContent>
      </Card>

      {/* Chunk Breakdown Table */}
      <Card>
        <CardHeader>
          <CardTitle className="text-base font-semibold flex items-center gap-2">
            <Layers className="h-4 w-4 text-indigo-400" />
            Chunk Breakdown & Cryptographic Checksums
          </CardTitle>
          <CardDescription className="text-xs">
            Every chunk carries an independent SHA-256 hash verified upon streaming reconstruction
          </CardDescription>
        </CardHeader>
        <CardContent>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className="w-16">Index</TableHead>
                <TableHead>Chunk ID</TableHead>
                <TableHead>Storage Node</TableHead>
                <TableHead>Size</TableHead>
                <TableHead>SHA-256 Checksum</TableHead>
                <TableHead>Status</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {chunks.map((chunk) => (
                <TableRow key={chunk.id}>
                  <TableCell className="font-mono text-xs text-slate-400">
                    #{chunk.chunk_index}
                  </TableCell>
                  <TableCell className="font-mono text-xs text-slate-300">
                    {chunk.id}
                  </TableCell>
                  <TableCell>
                    <Badge variant="outline" className="font-mono text-xs text-indigo-300 border-indigo-900/60">
                      {chunk.storage_node_id}
                    </Badge>
                  </TableCell>
                  <TableCell className="font-mono text-xs">{formatBytes(chunk.size)}</TableCell>
                  <TableCell className="font-mono text-xs text-slate-400 truncate max-w-xs" title={chunk.checksum}>
                    <span className="flex items-center gap-1.5">
                      <ShieldCheck className="h-3.5 w-3.5 text-emerald-400 shrink-0" />
                      {chunk.checksum}
                    </span>
                  </TableCell>
                  <TableCell>
                    <Badge
                      variant={
                        chunk.status === "STORED"
                          ? "success"
                          : chunk.status === "CORRUPTED"
                          ? "destructive"
                          : "warning"
                      }
                    >
                      {chunk.status}
                    </Badge>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </CardContent>
      </Card>
    </div>
  );
}
