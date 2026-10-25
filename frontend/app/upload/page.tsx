"use client";

import * as React from "react";
import Link from "next/link";
import {
  UploadCloud,
  FileText,
  CheckCircle2,
  XCircle,
  AlertCircle,
  ArrowRight,
  Pause,
  Play,
  RotateCcw,
  Layers,
  ShieldCheck,
} from "lucide-react";
import {
  initResumableUpload,
  fetchUploadSession,
  uploadChunk,
  cancelResumableUpload,
  SessionSummary,
  uploadFile,
} from "@/lib/api";
import { useQueryClient } from "@tanstack/react-query";
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Progress } from "@/components/ui/progress";
import { Badge } from "@/components/ui/badge";
import { formatBytes } from "@/lib/utils";

const CHUNK_SIZE = 2 * 1024 * 1024; // 2 MB chunks for responsive resumable uploads

export default function UploadPage() {
  const queryClient = useQueryClient();

  const [dragActive, setDragActive] = React.useState(false);
  const [selectedFile, setSelectedFile] = React.useState<File | null>(null);

  // Resumable upload states
  const [isUploading, setIsUploading] = React.useState(false);
  const [isPaused, setIsPaused] = React.useState(false);
  const [session, setSession] = React.useState<SessionSummary | null>(null);
  const [currentChunkIndex, setCurrentChunkIndex] = React.useState<number | null>(null);
  const [errorMsg, setErrorMsg] = React.useState<string | null>(null);
  const [completedFileId, setCompletedFileId] = React.useState<string | null>(null);

  const pausedRef = React.useRef(false);
  const cancelledRef = React.useRef(false);
  const fileInputRef = React.useRef<HTMLInputElement>(null);

  const handleDrag = (e: React.DragEvent) => {
    e.preventDefault();
    e.stopPropagation();
    if (e.type === "dragenter" || e.type === "dragover") {
      setDragActive(true);
    } else if (e.type === "dragleave") {
      setDragActive(false);
    }
  };

  const handleDrop = (e: React.DragEvent) => {
    e.preventDefault();
    e.stopPropagation();
    setDragActive(false);
    if (e.dataTransfer.files && e.dataTransfer.files[0]) {
      handleSelectFile(e.dataTransfer.files[0]);
    }
  };

  const handleSelectFile = (file: File) => {
    setSelectedFile(file);
    setErrorMsg(null);
    setSession(null);
    setCompletedFileId(null);
    setIsUploading(false);
    setIsPaused(false);
    setCurrentChunkIndex(null);
  };

  // Start or resume upload loop
  const startUploadLoop = async (currentSession: SessionSummary) => {
    if (!selectedFile) return;

    setIsUploading(true);
    setIsPaused(false);
    pausedRef.current = false;
    cancelledRef.current = false;
    setErrorMsg(null);

    let latestSession = currentSession;
    const totalChunks = latestSession.total_chunks;

    // Check which chunks are already completed
    const completedSet = new Set(latestSession.completed_indices || []);

    try {
      for (let i = 0; i < totalChunks; i++) {
        // If user paused, break loop safely
        if (pausedRef.current || cancelledRef.current) {
          break;
        }

        // If chunk is already completed, skip it (Idempotent!)
        if (completedSet.has(i)) {
          continue;
        }

        setCurrentChunkIndex(i);

        // Slice chunk from file
        const start = i * latestSession.chunk_size;
        const end = Math.min(start + latestSession.chunk_size, selectedFile.size);
        const chunkBlob = selectedFile.slice(start, end);

        // Upload chunk
        await uploadChunk(latestSession.upload_id, i, chunkBlob);
        completedSet.add(i);

        // Fetch fresh session progress
        latestSession = await fetchUploadSession(latestSession.upload_id);
        setSession(latestSession);
      }

      if (!pausedRef.current && !cancelledRef.current) {
        // Finished all chunks!
        setIsUploading(false);
        setCompletedFileId(latestSession.file_id);
        queryClient.invalidateQueries({ queryKey: ["files"] });
        queryClient.invalidateQueries({ queryKey: ["nodes-stats"] });
        queryClient.invalidateQueries({ queryKey: ["nodes"] });
      }
    } catch (err: any) {
      if (!cancelledRef.current) {
        setErrorMsg(err.message || "Upload interrupted");
        setIsUploading(false);
      }
    }
  };

  // Initial trigger
  const handleStartUpload = async () => {
    if (!selectedFile) return;

    setErrorMsg(null);
    setCompletedFileId(null);

    try {
      // 1. Initialize or query existing session
      let activeSession = session;
      if (!activeSession || activeSession.status === "COMPLETED" || activeSession.status === "CANCELLED") {
        activeSession = await initResumableUpload(
          selectedFile.name,
          selectedFile.size,
          selectedFile.type || "application/octet-stream",
          CHUNK_SIZE
        );
        setSession(activeSession);
      } else {
        // Refresh session
        activeSession = await fetchUploadSession(activeSession.upload_id);
        setSession(activeSession);
      }

      // 2. Start upload loop
      await startUploadLoop(activeSession);
    } catch (err: any) {
      setErrorMsg(err.message || "Failed to initialize upload session");
    }
  };

  const handlePause = () => {
    pausedRef.current = true;
    setIsPaused(true);
    setIsUploading(false);
  };

  const handleResume = async () => {
    if (!session) return;
    try {
      const refreshed = await fetchUploadSession(session.upload_id);
      setSession(refreshed);
      await startUploadLoop(refreshed);
    } catch (err: any) {
      setErrorMsg(`Resume failed: ${err.message || err}`);
    }
  };

  const handleCancel = async () => {
    cancelledRef.current = true;
    setIsUploading(false);
    setIsPaused(false);

    if (session) {
      try {
        await cancelResumableUpload(session.upload_id);
      } catch {
        // ignore
      }
    }

    setSession(null);
    setCurrentChunkIndex(null);
    setErrorMsg("Upload session was cancelled and partially stored chunks were cleared.");
  };

  const handleReset = () => {
    setSelectedFile(null);
    setSession(null);
    setCompletedFileId(null);
    setErrorMsg(null);
    setIsUploading(false);
    setIsPaused(false);
    setCurrentChunkIndex(null);
    if (fileInputRef.current) {
      fileInputRef.current.value = "";
    }
  };

  const progressPercent = session ? Math.round(session.progress_percent) : 0;
  const completedCount = session?.completed_count || 0;
  const totalChunks = session?.total_chunks || 0;

  return (
    <div className="max-w-3xl mx-auto space-y-6">
      {/* Title */}
      <div className="border-b border-slate-800 pb-4">
        <h1 className="text-2xl font-bold tracking-tight text-white flex items-center gap-2">
          <UploadCloud className="h-6 w-6 text-indigo-400" />
          Resumable File Upload
        </h1>
        <p className="text-sm text-slate-400 mt-1">
          Files are sliced into resilient chunks, tracked by active upload sessions, and can be paused or resumed across network drops without duplicate storage.
        </p>
      </div>

      <Card className="border-slate-800">
        <CardContent className="pt-6 space-y-6">
          {/* Success State */}
          {completedFileId ? (
            <div className="text-center py-8 space-y-4">
              <div className="mx-auto flex h-14 w-14 items-center justify-center rounded-full bg-emerald-950/80 border border-emerald-800 text-emerald-400">
                <CheckCircle2 className="h-8 w-8" />
              </div>
              <div>
                <h3 className="text-lg font-semibold text-white">Upload Completed & Verified!</h3>
                <p className="text-sm text-slate-400 mt-1">
                  File <span className="text-white font-medium">{session?.filename || selectedFile?.name}</span> (
                  {formatBytes(session?.total_size || selectedFile?.size || 0)}) was successfully chunked into{" "}
                  {totalChunks} chunks and distributed across nodes.
                </p>
              </div>

              <div className="pt-4 flex items-center justify-center gap-3">
                <Button variant="outline" onClick={handleReset}>
                  Upload Another File
                </Button>
                <Link href={`/files/${completedFileId}`}>
                  <Button className="flex items-center gap-1.5">
                    View File Chunks <ArrowRight className="h-4 w-4" />
                  </Button>
                </Link>
                <Link href="/files">
                  <Button variant="secondary">Go to File Manager</Button>
                </Link>
              </div>
            </div>
          ) : (
            <>
              {/* Dropzone */}
              {!selectedFile && (
                <div
                  onDragEnter={handleDrag}
                  onDragOver={handleDrag}
                  onDragLeave={handleDrag}
                  onDrop={handleDrop}
                  onClick={() => fileInputRef.current?.click()}
                  className={`relative flex flex-col items-center justify-center rounded-xl border-2 border-dashed p-10 text-center transition-colors cursor-pointer ${
                    dragActive
                      ? "border-indigo-500 bg-indigo-950/20"
                      : "border-slate-800 bg-slate-950/50 hover:border-slate-700 hover:bg-slate-900/50"
                  }`}
                >
                  <input
                    ref={fileInputRef}
                    type="file"
                    className="hidden"
                    onChange={(e) => {
                      if (e.target.files && e.target.files[0]) {
                        handleSelectFile(e.target.files[0]);
                      }
                    }}
                  />

                  <div className="flex h-12 w-12 items-center justify-center rounded-full bg-slate-900 text-indigo-400 border border-slate-800 mb-4">
                    <UploadCloud className="h-6 w-6" />
                  </div>

                  <div className="text-sm font-medium text-white mb-1">
                    Choose a file or drag & drop it here
                  </div>
                  <p className="text-xs text-slate-500 max-w-sm">
                    Resumable session uploads support any file size. Interruptions can be retried without re-uploading stored chunks.
                  </p>

                  <Button variant="secondary" size="sm" className="mt-4 pointer-events-none">
                    Select File
                  </Button>
                </div>
              )}

              {/* Selected File Card */}
              {selectedFile && !isUploading && !isPaused && (
                <div className="flex items-center justify-between rounded-lg border border-slate-800 bg-slate-900/60 p-4">
                  <div className="flex items-center gap-3">
                    <FileText className="h-8 w-8 text-indigo-400" />
                    <div>
                      <div className="text-sm font-semibold text-white">{selectedFile.name}</div>
                      <div className="text-xs text-slate-400">
                        {formatBytes(selectedFile.size)} · {selectedFile.type || "application/octet-stream"}
                      </div>
                    </div>
                  </div>
                  <div className="flex items-center gap-2">
                    <Button variant="ghost" size="sm" onClick={handleReset}>
                      Change
                    </Button>
                    <Button size="sm" onClick={handleStartUpload} className="flex items-center gap-1.5">
                      <UploadCloud className="h-4 w-4" />
                      Start Resumable Upload
                    </Button>
                  </div>
                </div>
              )}

              {/* Live Upload Progress */}
              {(isUploading || isPaused) && (
                <div className="rounded-lg border border-slate-800 bg-slate-900/60 p-5 space-y-4">
                  <div className="flex items-center justify-between text-sm">
                    <div className="flex items-center gap-2">
                      {isPaused ? (
                        <Badge variant="warning" className="flex items-center gap-1">
                          <Pause className="h-3 w-3" /> Paused
                        </Badge>
                      ) : (
                        <div className="flex items-center gap-2">
                          <UploadCloud className="h-4 w-4 text-indigo-400 animate-pulse" />
                          <span className="font-medium text-white">
                            Uploading Chunk {currentChunkIndex !== null ? currentChunkIndex + 1 : 1} of {totalChunks}...
                          </span>
                        </div>
                      )}
                    </div>
                    <span className="font-mono text-indigo-400 font-semibold">{progressPercent}%</span>
                  </div>

                  <Progress value={progressPercent} className="h-2.5" />

                  <div className="flex items-center justify-between text-xs text-slate-400">
                    <span>
                      {completedCount} of {totalChunks} chunks completed (
                      {formatBytes(completedCount * CHUNK_SIZE)} of {formatBytes(selectedFile?.size || 0)})
                    </span>

                    <div className="flex items-center gap-2">
                      {isUploading ? (
                        <Button
                          variant="secondary"
                          size="sm"
                          onClick={handlePause}
                          className="h-7 px-3 text-xs flex items-center gap-1"
                        >
                          <Pause className="h-3 w-3" /> Pause
                        </Button>
                      ) : (
                        <Button
                          size="sm"
                          onClick={handleResume}
                          className="h-7 px-3 text-xs flex items-center gap-1 bg-indigo-600 hover:bg-indigo-500"
                        >
                          <Play className="h-3 w-3" /> Resume
                        </Button>
                      )}

                      <Button
                        variant="destructive"
                        size="sm"
                        onClick={handleCancel}
                        className="h-7 px-3 text-xs flex items-center gap-1"
                      >
                        <XCircle className="h-3 w-3" /> Cancel
                      </Button>
                    </div>
                  </div>

                  {/* Visual Chunk Distribution Map */}
                  {totalChunks > 0 && totalChunks <= 60 && (
                    <div className="pt-2 border-t border-slate-800/80">
                      <div className="text-[11px] font-mono text-slate-400 mb-1.5 flex items-center gap-1">
                        <Layers className="h-3 w-3 text-indigo-400" />
                        Chunk Map ({completedCount}/{totalChunks}):
                      </div>
                      <div className="flex flex-wrap gap-1">
                        {Array.from({ length: totalChunks }).map((_, idx) => {
                          const isCompleted = session?.completed_indices?.includes(idx);
                          const isCurrent = currentChunkIndex === idx;

                          let bg = "bg-slate-800 text-slate-500";
                          if (isCompleted) bg = "bg-emerald-600/80 text-white";
                          else if (isCurrent) bg = "bg-amber-500 text-slate-950 animate-pulse font-bold";

                          return (
                            <span
                              key={idx}
                              title={`Chunk #${idx}: ${isCompleted ? "Completed" : isCurrent ? "Uploading..." : "Pending"}`}
                              className={`h-5 w-6 rounded text-[10px] flex items-center justify-center font-mono ${bg}`}
                            >
                              {idx + 1}
                            </span>
                          );
                        })}
                      </div>
                    </div>
                  )}
                </div>
              )}

              {/* Error Box */}
              {errorMsg && (
                <div className="rounded-lg border border-rose-800/80 bg-rose-950/40 p-3 text-sm text-rose-300 flex items-center justify-between gap-2">
                  <div className="flex items-center gap-2">
                    <AlertCircle className="h-4 w-4 shrink-0 text-rose-400" />
                    <span>{errorMsg}</span>
                  </div>
                  {session && !isUploading && (
                    <Button
                      variant="outline"
                      size="sm"
                      onClick={handleResume}
                      className="h-7 px-2 text-xs flex items-center gap-1 text-slate-200 border-rose-800"
                    >
                      <RotateCcw className="h-3 w-3" /> Retry Resume
                    </Button>
                  )}
                </div>
              )}
            </>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
