"use client";

import * as React from "react";
import Link from "next/link";
import { Files, Download, Trash2, Eye, UploadCloud, Search, AlertCircle, RefreshCw, CheckCircle2 } from "lucide-react";
import { useFiles, useDeleteFile } from "@/hooks/use-files";
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { formatBytes, formatDate } from "@/lib/utils";
import { downloadFile } from "@/lib/api";

export default function FileManagerPage() {
  const { data: files, isLoading, isError, refetch } = useFiles();
  const deleteMutation = useDeleteFile();
  const [searchQuery, setSearchQuery] = React.useState("");
  const [deletingId, setDeletingId] = React.useState<string | null>(null);
  const [successMsg, setSuccessMsg] = React.useState<string | null>(null);

  const filteredFiles = React.useMemo(() => {
    if (!files) return [];
    if (!searchQuery.trim()) return files;
    const q = searchQuery.toLowerCase();
    return files.filter((f) => f.filename.toLowerCase().includes(q));
  }, [files, searchQuery]);

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
      <div className="flex flex-col md:flex-row md:items-center justify-between gap-4 pb-2 border-b border-slate-800">
        <div>
          <h1 className="text-2xl font-bold tracking-tight text-white flex items-center gap-2">
            <Files className="h-6 w-6 text-indigo-400" />
            File Manager
          </h1>
          <p className="text-sm text-slate-400 mt-1">
            Browse, inspect chunks, download, and manage files distributed across your storage nodes.
          </p>
        </div>
        <div className="flex items-center gap-3">
          <Button variant="outline" size="sm" onClick={() => refetch()} className="flex items-center gap-1.5">
            <RefreshCw className="h-3.5 w-3.5" />
            Refresh
          </Button>
          <Link href="/upload">
            <Button size="sm" className="flex items-center gap-2">
              <UploadCloud className="h-4 w-4" />
              Upload File
            </Button>
          </Link>
        </div>
      </div>

      {/* Success Notification */}
      {successMsg && (
        <div className="rounded-lg border border-emerald-800/80 bg-emerald-950/40 p-3 text-sm text-emerald-300 flex items-center gap-2">
          <CheckCircle2 className="h-4 w-4 shrink-0 text-emerald-400" />
          <span>{successMsg}</span>
        </div>
      )}

      {/* Search Bar */}
      <div className="flex items-center gap-3">
        <div className="relative flex-1 max-w-md">
          <Search className="absolute left-3 top-2.5 h-4 w-4 text-slate-500" />
          <input
            type="text"
            placeholder="Search files by name..."
            value={searchQuery}
            onChange={(e) => setSearchQuery(e.target.value)}
            className="w-full rounded-lg border border-slate-800 bg-slate-900/80 pl-9 pr-4 py-2 text-sm text-white placeholder-slate-500 focus:border-indigo-500 focus:outline-none focus:ring-1 focus:ring-indigo-500"
          />
        </div>
        <div className="text-xs text-slate-500">
          Showing {filteredFiles.length} of {files?.length || 0} files
        </div>
      </div>

      {/* Files Table Card */}
      <Card>
        <CardContent className="p-0">
          {isLoading ? (
            <div className="py-16 text-center text-sm text-slate-400">Loading files from repository...</div>
          ) : isError ? (
            <div className="py-16 text-center space-y-2 text-rose-400">
              <AlertCircle className="mx-auto h-8 w-8 text-rose-500" />
              <div className="text-sm font-semibold">Failed to load files</div>
              <p className="text-xs text-slate-500">Check that the Go backend server is running.</p>
            </div>
          ) : filteredFiles.length === 0 ? (
            <div className="py-16 text-center space-y-3">
              <Files className="mx-auto h-12 w-12 text-slate-600" />
              <div className="text-base font-medium text-slate-300">
                {searchQuery ? "No matching files found" : "No files stored in pool yet"}
              </div>
              <p className="text-xs text-slate-500 max-w-sm mx-auto">
                {searchQuery
                  ? "Try searching with a different term."
                  : "Upload a file to experience streaming chunk distribution."}
              </p>
              {!searchQuery && (
                <Link href="/upload">
                  <Button size="sm" className="mt-2">Upload First File</Button>
                </Link>
              )}
            </div>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Filename</TableHead>
                  <TableHead>Size</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead>Chunks</TableHead>
                  <TableHead>Created Date</TableHead>
                  <TableHead className="text-right">Actions</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {filteredFiles.map((file) => (
                  <TableRow key={file.id}>
                    <TableCell className="font-medium text-white">
                      <div className="flex items-center gap-2">
                        <span className="p-1.5 rounded bg-indigo-950/80 text-indigo-400 border border-indigo-900/50">
                          <Files className="h-3.5 w-3.5" />
                        </span>
                        <div>
                          <Link
                            href={`/files/${file.id}`}
                            className="hover:underline hover:text-indigo-300 transition-colors"
                          >
                            {file.filename}
                          </Link>
                          <div className="text-[11px] text-slate-500 font-mono">{file.id}</div>
                        </div>
                      </div>
                    </TableCell>
                    <TableCell className="font-mono text-sm">{formatBytes(file.size)}</TableCell>
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
                    <TableCell className="text-slate-400 font-mono text-xs">
                      {file.total_chunks} {file.total_chunks === 1 ? "chunk" : "chunks"}
                    </TableCell>
                    <TableCell className="text-slate-400 text-xs">
                      {formatDate(file.created_at)}
                    </TableCell>
                    <TableCell className="text-right">
                      <div className="flex items-center justify-end gap-1.5">
                        {/* Details */}
                        <Link href={`/files/${file.id}`}>
                          <Button variant="ghost" size="sm" className="h-8 px-2 text-xs flex items-center gap-1">
                            <Eye className="h-3.5 w-3.5 text-indigo-400" />
                            Details
                          </Button>
                        </Link>

                        {/* Download */}
                        <Button
                          variant="ghost"
                          size="sm"
                          className="h-8 px-2 text-xs flex items-center gap-1 text-slate-300 hover:text-emerald-400"
                          onClick={() => downloadFile(file.id, file.filename)}
                          title="Download reconstructed file"
                        >
                          <Download className="h-3.5 w-3.5" />
                          Download
                        </Button>

                        {/* Delete */}
                        <Button
                          variant="ghost"
                          size="sm"
                          disabled={deletingId === file.id}
                          className="h-8 px-2 text-xs text-rose-400 hover:bg-rose-950/40 hover:text-rose-300"
                          onClick={() => handleDelete(file.id, file.filename)}
                          title="Delete file and chunks"
                        >
                          <Trash2 className="h-3.5 w-3.5" />
                          {deletingId === file.id ? "Deleting..." : "Delete"}
                        </Button>
                      </div>
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
