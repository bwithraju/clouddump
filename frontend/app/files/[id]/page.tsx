"use client";

import * as React from "react";
import Link from "next/link";
import { useParams, useRouter } from "next/navigation";
import {
  ArrowLeft,
  Download,
  Trash2,
  FileText,
  CheckCircle2,
  ShieldCheck,
  HardDrive,
  Layers,
  AlertCircle,
  Copy,
} from "lucide-react";
import { useFileDetails, useDeleteFile } from "@/hooks/use-files";
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button, getButtonClassName } from "@/components/ui/button";
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
  const [copiedHash, setCopiedHash] = React.useState<string | null>(null);

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

  const copyToClipboard = (text: string) => {
    navigator.clipboard.writeText(text);
    setCopiedHash(text);
    setTimeout(() => setCopiedHash(null), 2000);
  };

  if (isLoading) {
    return (
      <div className="py-24 text-center text-xs text-gray-400">
        Loading file metadata and chunk distribution...
      </div>
    );
  }

  if (isError || !file) {
    return (
      <div className="rounded-xl border border-gray-200/80 bg-white p-16 text-center space-y-4 shadow-xs">
        <AlertCircle className="mx-auto h-10 w-10 text-rose-500" />
        <h2 className="text-base font-semibold text-slate-900">File Not Found</h2>
        <p className="text-xs text-gray-500 max-w-sm mx-auto">
          The requested file could not be found or has been permanently removed from the storage pool.
        </p>
        <Link
          href="/files"
          className="inline-flex items-center gap-2 rounded-lg border border-gray-200 bg-white px-4 py-2 text-xs font-semibold text-slate-700 shadow-xs hover:bg-gray-50 transition-colors"
        >
          Back to File Manager
        </Link>
      </div>
    );
  }

  return (
    <div className="space-y-6">
      {/* Top Breadcrumb & Actions Bar */}
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4">
        <div className="flex items-center gap-3">
          <Link
            href="/files"
            className="flex h-9 w-9 items-center justify-center rounded-lg border border-gray-200 bg-white text-gray-500 hover:bg-gray-50 hover:text-slate-900 transition-colors shadow-xs"
            title="Back to all files"
          >
            <ArrowLeft className="h-4 w-4" />
          </Link>
          <div>
            <div className="flex items-center gap-2.5">
              <h1 className="text-xl font-bold tracking-tight text-slate-900">{file.filename}</h1>
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
            </div>
            <div className="text-[11px] text-gray-400 font-mono mt-0.5">{file.id}</div>
          </div>
        </div>

        <div className="flex items-center gap-2">
          <Button
            onClick={() => downloadFile(file.id, file.filename)}
            disabled={file.status !== "COMPLETED"}
            className="flex items-center gap-2 bg-slate-900 hover:bg-slate-800 text-white shadow-xs"
            title={file.status === "DEGRADED" ? "File is degraded: node offline" : "Download File"}
          >
            <Download className="h-4 w-4" />
            Download File
          </Button>

          <Button
            variant="destructive"
            onClick={handleDelete}
            disabled={isDeleting}
            className="flex items-center gap-2 shadow-xs"
          >
            <Trash2 className="h-4 w-4" />
            {isDeleting ? "Deleting..." : "Delete"}
          </Button>
        </div>
      </div>

      {/* Degraded Alert Banner */}
      {file.status === "DEGRADED" && (
        <div className="rounded-xl border border-amber-200 bg-amber-50/80 p-4 flex items-start gap-3 text-amber-900 shadow-xs">
          <AlertCircle className="h-5 w-5 text-amber-600 shrink-0 mt-0.5" />
          <div className="space-y-1 text-xs">
            <div className="font-semibold">File in Degraded State</div>
            <p className="text-amber-800/90">
              One or more storage nodes containing required chunks are currently offline. Download is temporarily restricted until underlying nodes are restored to prevent partial or corrupted file streams.
            </p>
          </div>
        </div>
      )}

      {/* File Overview Stat Cards */}
      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
        <div className="rounded-xl border border-gray-200/80 bg-white p-4 shadow-xs space-y-1">
          <div className="text-[11px] font-semibold text-gray-400 uppercase tracking-wider">File Size</div>
          <div className="text-lg font-bold text-slate-900">{formatBytes(file.size)}</div>
          <div className="text-xs text-gray-400">{file.size.toLocaleString()} bytes</div>
        </div>

        <div className="rounded-xl border border-gray-200/80 bg-white p-4 shadow-xs space-y-1">
          <div className="text-[11px] font-semibold text-gray-400 uppercase tracking-wider">MIME Type</div>
          <div className="text-sm font-semibold text-slate-800 font-mono truncate">
            {file.mime_type || "application/octet-stream"}
          </div>
          <div className="text-xs text-gray-400">Content type</div>
        </div>

        <div className="rounded-xl border border-gray-200/80 bg-white p-4 shadow-xs space-y-1">
          <div className="text-[11px] font-semibold text-gray-400 uppercase tracking-wider">Chunk Count</div>
          <div className="text-lg font-bold text-indigo-600">
            {file.total_chunks} {file.total_chunks === 1 ? "Chunk" : "Chunks"}
          </div>
          <div className="text-xs text-gray-400">Target: {formatBytes(file.chunk_size)}</div>
        </div>

        <div className="rounded-xl border border-gray-200/80 bg-white p-4 shadow-xs space-y-1">
          <div className="text-[11px] font-semibold text-gray-400 uppercase tracking-wider">Created Date</div>
          <div className="text-sm font-semibold text-slate-800">
            {formatDate(file.created_at)}
          </div>
          <div className="text-xs text-gray-400">Owner: {file.user_id}</div>
        </div>
      </div>

      {/* Node Chunk Distribution Summary */}
      <div className="rounded-xl border border-gray-200/80 bg-white p-5 shadow-xs space-y-3">
        <div>
          <h3 className="text-sm font-semibold text-slate-900 flex items-center gap-2">
            <HardDrive className="h-4 w-4 text-indigo-600" />
            Storage Node Distribution
          </h3>
          <p className="text-xs text-gray-500">
            How chunks of this file are allocated across independent heterogeneous nodes
          </p>
        </div>

        <div className="grid grid-cols-2 sm:grid-cols-4 gap-3">
          {Object.keys(distribution).length === 0 ? (
            <div className="text-xs text-gray-400">No node distribution data available</div>
          ) : (
            Object.entries(distribution).map(([nodeId, count]) => {
              const pct = Math.round((count / file.total_chunks) * 100);
              return (
                <div key={nodeId} className="rounded-lg border border-gray-200 bg-gray-50/50 p-3 space-y-1">
                  <div className="flex items-center justify-between text-xs">
                    <span className="font-mono text-indigo-600 font-semibold">{nodeId}</span>
                    <span className="text-gray-400">{pct}%</span>
                  </div>
                  <div className="text-sm font-bold text-slate-900">
                    {count} {count === 1 ? "chunk" : "chunks"}
                  </div>
                </div>
              );
            })
          )}
        </div>
      </div>

      {/* Chunk Breakdown Table */}
      <div className="rounded-xl border border-gray-200/80 bg-white shadow-xs overflow-hidden">
        <div className="p-5 border-b border-gray-100">
          <h3 className="text-sm font-semibold text-slate-900 flex items-center gap-2">
            <Layers className="h-4 w-4 text-indigo-600" />
            Chunk Breakdown & Cryptographic Checksums
          </h3>
          <p className="text-xs text-gray-500">
            Every chunk carries an independent SHA-256 hash verified upon streaming reconstruction
          </p>
        </div>

        <Table>
          <TableHeader>
            <TableRow className="border-b border-gray-100 bg-gray-50/50">
              <TableHead className="w-16">Index</TableHead>
              <TableHead>Chunk ID</TableHead>
              <TableHead>Primary Node</TableHead>
              <TableHead>Replicas</TableHead>
              <TableHead>Size</TableHead>
              <TableHead>SHA-256 Checksum</TableHead>
              <TableHead>Status</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {chunks.map((chunk) => (
              <TableRow key={chunk.id} className="hover:bg-gray-50/60 transition-colors">
                <TableCell className="font-mono text-xs text-gray-400">
                  #{chunk.chunk_index}
                </TableCell>
                <TableCell className="font-mono text-xs text-slate-800">
                  {chunk.id}
                </TableCell>
                <TableCell>
                  <Badge variant="outline" className="font-mono text-xs text-indigo-700 border-indigo-200 bg-indigo-50/50">
                    {chunk.storage_node_id}
                  </Badge>
                </TableCell>
                <TableCell>
                  {chunk.replicas && chunk.replicas.length > 0 ? (
                    <div className="flex flex-wrap gap-1">
                      {chunk.replicas.map((rep, idx) => (
                        <Badge key={idx} variant="outline" className="font-mono text-xs text-emerald-700 border-emerald-200 bg-emerald-50/50">
                          {rep.storage_node_id}
                        </Badge>
                      ))}
                    </div>
                  ) : (
                    <span className="text-xs text-gray-400 font-mono">None</span>
                  )}
                </TableCell>
                <TableCell className="font-mono text-xs font-medium text-slate-700">{formatBytes(chunk.size)}</TableCell>
                <TableCell className="font-mono text-xs text-gray-500 truncate max-w-xs">
                  <button
                    type="button"
                    onClick={() => copyToClipboard(chunk.checksum)}
                    className="flex items-center gap-1.5 hover:text-slate-900 text-left transition-colors"
                    title="Click to copy hash"
                  >
                    <ShieldCheck className="h-3.5 w-3.5 text-emerald-600 shrink-0" />
                    <span className="truncate">{chunk.checksum}</span>
                    <Copy className="h-3 w-3 text-gray-400 shrink-0 ml-1" />
                  </button>
                  {copiedHash === chunk.checksum && (
                    <span className="text-[10px] text-emerald-600 font-sans block">Copied!</span>
                  )}
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
      </div>
    </div>
  );
}
