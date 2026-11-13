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
  LogIn,
  Trash2,
  Download,
  Loader2,
} from "lucide-react";
import {
  initResumableUpload,
  fetchUploadSession,
  uploadChunk,
  cancelResumableUpload,
  SessionSummary,
  deleteFile,
  downloadFile,
} from "@/lib/api";
import { useQueryClient } from "@tanstack/react-query";
import { useAuth } from "@/hooks/use-auth";
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from "@/components/ui/card";
import { Button, getButtonClassName } from "@/components/ui/button";
import { AuthModal } from "@/components/auth-modal";
import { Progress } from "@/components/ui/progress";
import { Badge } from "@/components/ui/badge";
import { formatBytes } from "@/lib/utils";

const CHUNK_SIZE = 2 * 1024 * 1024; // 2 MB chunks

export default function UploadPage() {
  const queryClient = useQueryClient();
  const { isAuthenticated, isLoading: authLoading } = useAuth();
  const [authModalOpen, setAuthModalOpen] = React.useState(false);

  const [dragActive, setDragActive] = React.useState(false);
  const [selectedFile, setSelectedFile] = React.useState<File | null>(null);

  // Resumable upload states
  const [isUploading, setIsUploading] = React.useState(false);
  const [isPaused, setIsPaused] = React.useState(false);
  const [session, setSession] = React.useState<SessionSummary | null>(null);
  const [currentChunkIndex, setCurrentChunkIndex] = React.useState<number | null>(null);
  const [errorMsg, setErrorMsg] = React.useState<string | null>(null);
  const [completedFileId, setCompletedFileId] = React.useState<string | null>(null);
  const [isDeleting, setIsDeleting] = React.useState(false);
  const [actionFeedback, setActionFeedback] = React.useState<{ type: "success" | "error"; message: string } | null>(null);

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
    setActionFeedback(null);
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
    const completedSet = new Set(latestSession.completed_indices || []);

    try {
      for (let i = 0; i < totalChunks; i++) {
        if (pausedRef.current || cancelledRef.current) {
          break;
        }

        if (completedSet.has(i)) {
          continue;
        }

        setCurrentChunkIndex(i);

        const start = i * latestSession.chunk_size;
        const end = Math.min(start + latestSession.chunk_size, selectedFile.size);
        const chunkBlob = selectedFile.slice(start, end);

        await uploadChunk(latestSession.upload_id, i, chunkBlob);
        completedSet.add(i);

        latestSession = await fetchUploadSession(latestSession.upload_id);
        setSession(latestSession);
      }

      if (!pausedRef.current && !cancelledRef.current) {
        setIsUploading(false);
        setCompletedFileId(latestSession.file_id);
        queryClient.invalidateQueries({ queryKey: ["files"] });
        queryClient.invalidateQueries({ queryKey: ["nodes-stats"] });
        queryClient.invalidateQueries({ queryKey: ["nodes"] });
        queryClient.invalidateQueries({ queryKey: ["system/activity"] });
      }
    } catch (err: any) {
      if (!cancelledRef.current) {
        setErrorMsg(err.message || "Upload interrupted");
        setIsUploading(false);
      }
    }
  };

  const handleStartUpload = async () => {
    if (!selectedFile) return;

    setErrorMsg(null);
    setActionFeedback(null);
    setCompletedFileId(null);

    if (!isAuthenticated) {
      setErrorMsg("Authentication required: Please sign in or create an account to start an upload session.");
      setAuthModalOpen(true);
      return;
    }

    try {
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
        activeSession = await fetchUploadSession(activeSession.upload_id);
        setSession(activeSession);
      }

      await startUploadLoop(activeSession);
    } catch (err: any) {
      const msg = err.message || "Failed to initialize upload session";
      if (msg.includes("401") || msg.toLowerCase().includes("unauthorized") || msg.toLowerCase().includes("authentication required")) {
        setErrorMsg("Authentication required: Your session may have expired. Please sign in.");
        setAuthModalOpen(true);
      } else {
        setErrorMsg(msg);
      }
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
    setActionFeedback(null);
    setIsUploading(false);
    setIsPaused(false);
    setCurrentChunkIndex(null);
    if (fileInputRef.current) {
      fileInputRef.current.value = "";
    }
  };

  const handleDeleteCompleted = async () => {
    if (!completedFileId) return;
    const filename = session?.filename || selectedFile?.name || "this file";
    if (
      !window.confirm(
        `Are you sure you want to delete "${filename}"? All chunks across storage nodes will be permanently removed.`
      )
    ) {
      return;
    }

    setIsDeleting(true);
    setActionFeedback(null);

    try {
      await deleteFile(completedFileId);
      queryClient.invalidateQueries({ queryKey: ["files"] });
      queryClient.invalidateQueries({ queryKey: ["nodes-stats"] });
      queryClient.invalidateQueries({ queryKey: ["nodes"] });
      queryClient.invalidateQueries({ queryKey: ["system/activity"] });

      setActionFeedback({
        type: "success",
        message: `File "${filename}" was successfully deleted and its chunks were purged.`,
      });
      setCompletedFileId(null);
      setSelectedFile(null);
      setSession(null);
      setCurrentChunkIndex(null);
      if (fileInputRef.current) {
        fileInputRef.current.value = "";
      }
    } catch (err: any) {
      setActionFeedback({
        type: "error",
        message: err.message || "Failed to delete file from storage pool.",
      });
    } finally {
      setIsDeleting(false);
    }
  };

  const handleDownloadCompleted = () => {
    if (!completedFileId) return;
    const filename = session?.filename || selectedFile?.name || "download";
    downloadFile(completedFileId, filename);
  };

  const progressPercent = session ? Math.round(session.progress_percent) : 0;
  const completedCount = session?.completed_count || 0;
  const totalChunks = session?.total_chunks || 0;

  return (
    <div className="max-w-3xl mx-auto space-y-6">
      {/* Title */}
      <div>
        <h1 className="text-2xl font-bold tracking-tight text-slate-900 flex items-center gap-2.5">
          <UploadCloud className="h-6 w-6 text-indigo-600" />
          Upload file
        </h1>
        <p className="text-xs text-gray-500 mt-0.5">
          Files are sliced into resilient chunks and streamed directly into heterogeneous storage nodes with resumable failover support.
        </p>
      </div>

      {/* Authentication Banner when unauthenticated */}
      {!isAuthenticated && !authLoading && (
        <div className="rounded-xl border border-indigo-100 bg-indigo-50/70 p-4 flex flex-col sm:flex-row sm:items-center justify-between gap-3 text-xs shadow-xs">
          <div className="flex items-center gap-2.5 text-indigo-950">
            <ShieldCheck className="h-5 w-5 text-indigo-600 shrink-0" />
            <div>
              <span className="font-semibold block sm:inline">Authentication Required: </span>
              <span>Sign in to initialize resumable upload sessions and distribute chunks across storage nodes.</span>
            </div>
          </div>
          <Button
            size="sm"
            onClick={() => setAuthModalOpen(true)}
            className="h-8 px-3 text-xs bg-slate-900 hover:bg-slate-800 text-white shrink-0 self-start sm:self-auto flex items-center gap-1.5 shadow-xs"
          >
            <LogIn className="h-3.5 w-3.5" />
            Log In / Register
          </Button>
        </div>
      )}

      {/* Feedback Banner */}
      {actionFeedback && !completedFileId && (
        <div
          className={`rounded-xl p-3.5 text-xs flex items-center justify-between gap-2 shadow-xs ${
            actionFeedback.type === "success"
              ? "border border-emerald-200 bg-emerald-50 text-emerald-900"
              : "border border-rose-200 bg-rose-50 text-rose-900"
          }`}
        >
          <div className="flex items-center gap-2">
            {actionFeedback.type === "success" ? (
              <CheckCircle2 className="h-4 w-4 shrink-0 text-emerald-600" />
            ) : (
              <AlertCircle className="h-4 w-4 shrink-0 text-rose-600" />
            )}
            <span>{actionFeedback.message}</span>
          </div>
          <Button
            variant="ghost"
            size="sm"
            onClick={() => setActionFeedback(null)}
            className="h-6 px-1.5 text-xs text-gray-500 hover:text-slate-900"
          >
            Dismiss
          </Button>
        </div>
      )}

      <div className="rounded-xl border border-gray-200/80 bg-white p-6 shadow-xs space-y-6">
        {/* Success State */}
        {completedFileId ? (
          <div className="text-center py-8 space-y-4">
            <div className="mx-auto flex h-14 w-14 items-center justify-center rounded-2xl bg-emerald-50 border border-emerald-200 text-emerald-600">
              <CheckCircle2 className="h-8 w-8" />
            </div>
            <div>
              <h3 className="text-lg font-bold text-slate-900">Upload Completed & Verified!</h3>
              <p className="text-xs text-gray-500 mt-1 max-w-md mx-auto">
                File <span className="text-slate-900 font-semibold">{session?.filename || selectedFile?.name}</span> (
                {formatBytes(session?.total_size || selectedFile?.size || 0)}) was successfully chunked into{" "}
                {totalChunks} chunks and distributed across storage nodes.
              </p>
            </div>

            {actionFeedback && (
              <div
                className={`mx-auto max-w-md rounded-xl p-3 text-xs flex items-center justify-center gap-2 shadow-xs ${
                  actionFeedback.type === "success"
                    ? "border border-emerald-200 bg-emerald-50 text-emerald-900"
                    : "border border-rose-200 bg-rose-50 text-rose-900"
                }`}
              >
                <CheckCircle2 className="h-4 w-4 shrink-0 text-emerald-600" />
                <span>{actionFeedback.message}</span>
              </div>
            )}

            <div className="pt-4 flex flex-wrap items-center justify-center gap-2.5">
              <Button variant="outline" onClick={handleReset} className="flex items-center gap-1.5 text-xs">
                <RotateCcw className="h-3.5 w-3.5" />
                Upload Another File
              </Button>
              <Button
                variant="outline"
                onClick={handleDownloadCompleted}
                className="flex items-center gap-1.5 text-xs"
              >
                <Download className="h-3.5 w-3.5 text-indigo-600" />
                Download
              </Button>
              <Link
                href={`/files/${completedFileId}`}
                className="inline-flex items-center gap-1.5 rounded-lg bg-slate-900 px-4 py-2 text-xs font-semibold text-white shadow-xs hover:bg-slate-800 transition-colors"
              >
                View File Chunks <ArrowRight className="h-4 w-4" />
              </Link>
              <Link
                href="/files"
                className="inline-flex items-center gap-1.5 rounded-lg border border-gray-200 bg-white px-4 py-2 text-xs font-semibold text-slate-700 shadow-xs hover:bg-gray-50 transition-colors"
              >
                Go to File Manager
              </Link>
              <Button
                variant="destructive"
                onClick={handleDeleteCompleted}
                disabled={isDeleting}
                className="flex items-center gap-1.5 text-xs shadow-xs"
                title="Delete file and permanently remove all chunks"
              >
                {isDeleting ? (
                  <Loader2 className="h-3.5 w-3.5 animate-spin" />
                ) : (
                  <Trash2 className="h-3.5 w-3.5" />
                )}
                {isDeleting ? "Deleting..." : "Delete File"}
              </Button>
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
                className={`relative flex flex-col items-center justify-center rounded-2xl border-2 border-dashed p-12 text-center transition-all cursor-pointer ${
                  dragActive
                    ? "border-indigo-500 bg-indigo-50/50 scale-[0.99]"
                    : "border-gray-200 bg-gray-50/50 hover:border-indigo-400 hover:bg-indigo-50/20"
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

                <div className="flex h-12 w-12 items-center justify-center rounded-2xl bg-white text-indigo-600 border border-gray-200 shadow-xs mb-3">
                  <UploadCloud className="h-6 w-6" />
                </div>

                <div className="text-sm font-semibold text-slate-900 mb-1">
                  Choose a file or drag & drop it here
                </div>
                <p className="text-xs text-gray-500 max-w-sm">
                  Resumable session uploads slice files into resilient 2 MB chunks. Interruptions can be retried without re-uploading stored chunks.
                </p>

                <div className="mt-4 inline-flex items-center justify-center rounded-lg bg-white text-slate-800 border border-gray-200 shadow-xs h-8 px-3 text-xs font-medium pointer-events-none">
                  Select File
                </div>
              </div>
            )}

            {/* Selected File Card */}
            {selectedFile && !isUploading && !isPaused && (
              <div className="flex items-center justify-between rounded-xl border border-gray-200 bg-gray-50/60 p-4">
                <div className="flex items-center gap-3">
                  <div className="flex h-10 w-10 items-center justify-center rounded-xl bg-white border border-gray-200 shadow-xs text-indigo-600">
                    <FileText className="h-5 w-5" />
                  </div>
                  <div>
                    <div className="text-sm font-semibold text-slate-900">{selectedFile.name}</div>
                    <div className="text-xs text-gray-500">
                      {formatBytes(selectedFile.size)} · {selectedFile.type || "application/octet-stream"}
                    </div>
                  </div>
                </div>
                <div className="flex items-center gap-2">
                  <Button variant="ghost" size="sm" onClick={handleReset} className="text-xs">
                    Change
                  </Button>
                  <Button size="sm" onClick={handleStartUpload} className="flex items-center gap-1.5 text-xs bg-slate-900 text-white hover:bg-slate-800">
                    <UploadCloud className="h-4 w-4" />
                    Start Resumable Upload
                  </Button>
                </div>
              </div>
            )}

            {/* Live Upload Progress */}
            {(isUploading || isPaused) && (
              <div className="rounded-xl border border-gray-200 bg-gray-50/50 p-5 space-y-4">
                <div className="flex items-center justify-between text-sm">
                  <div className="flex items-center gap-2">
                    {isPaused ? (
                      <Badge variant="warning" className="flex items-center gap-1">
                        <Pause className="h-3 w-3" /> Paused
                      </Badge>
                    ) : (
                      <div className="flex items-center gap-2">
                        <UploadCloud className="h-4 w-4 text-indigo-600 animate-pulse" />
                        <span className="font-semibold text-slate-900 text-xs">
                          Uploading Chunk {currentChunkIndex !== null ? currentChunkIndex + 1 : 1} of {totalChunks}...
                        </span>
                      </div>
                    )}
                  </div>
                  <span className="font-mono text-indigo-600 font-bold text-xs">{progressPercent}%</span>
                </div>

                <Progress value={progressPercent} className="h-2.5" />

                <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-3 text-xs text-gray-500">
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
                        className="h-7 px-3 text-xs flex items-center gap-1 bg-indigo-600 hover:bg-indigo-700 text-white"
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
                  <div className="pt-2 border-t border-gray-200/80">
                    <div className="text-[11px] font-mono text-gray-500 mb-1.5 flex items-center gap-1">
                      <Layers className="h-3 w-3 text-indigo-600" />
                      Chunk Map ({completedCount}/{totalChunks}):
                    </div>
                    <div className="flex flex-wrap gap-1">
                      {Array.from({ length: totalChunks }).map((_, idx) => {
                        const isCompleted = session?.completed_indices?.includes(idx);
                        const isCurrent = currentChunkIndex === idx;

                        let bg = "bg-gray-200 text-gray-600";
                        if (isCompleted) bg = "bg-emerald-600 text-white";
                        else if (isCurrent) bg = "bg-amber-400 text-slate-900 font-bold animate-pulse";

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
              <div className="rounded-xl border border-rose-200 bg-rose-50 p-3.5 text-xs text-rose-900 flex items-center justify-between gap-2 shadow-xs">
                <div className="flex items-center gap-2">
                  <AlertCircle className="h-4 w-4 shrink-0 text-rose-600" />
                  <span>{errorMsg}</span>
                </div>
                {session && !isUploading && (
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={handleResume}
                    className="h-7 px-2 text-xs flex items-center gap-1 border-rose-300 bg-white text-rose-900 hover:bg-rose-50"
                  >
                    <RotateCcw className="h-3 w-3" /> Retry Resume
                  </Button>
                )}
              </div>
            )}
          </>
        )}
      </div>

      <AuthModal
        isOpen={authModalOpen}
        onClose={() => setAuthModalOpen(false)}
        defaultMode="login"
      />
    </div>
  );
}
