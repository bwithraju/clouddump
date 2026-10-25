"use client";

import * as React from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { UploadCloud, FileText, CheckCircle2, XCircle, AlertCircle, ArrowRight, HardDrive } from "lucide-react";
import { uploadFile, FileRecord } from "@/lib/api";
import { useQueryClient } from "@tanstack/react-query";
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Progress } from "@/components/ui/progress";
import { formatBytes } from "@/lib/utils";

export default function UploadPage() {
  const router = useRouter();
  const queryClient = useQueryClient();

  const [dragActive, setDragActive] = React.useState(false);
  const [selectedFile, setSelectedFile] = React.useState<File | null>(null);
  const [isUploading, setIsUploading] = React.useState(false);
  const [progress, setProgress] = React.useState(0);
  const [loadedBytes, setLoadedBytes] = React.useState(0);
  const [errorMsg, setErrorMsg] = React.useState<string | null>(null);
  const [uploadedResult, setUploadedResult] = React.useState<FileRecord | null>(null);

  const abortRef = React.useRef<{ abort?: () => void }>({});
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
    setUploadedResult(null);
    setProgress(0);
    setLoadedBytes(0);
  };

  const handleStartUpload = async () => {
    if (!selectedFile) return;

    setIsUploading(true);
    setErrorMsg(null);
    setProgress(0);
    setLoadedBytes(0);

    try {
      const result = await uploadFile(
        selectedFile,
        (percent, loaded) => {
          setProgress(percent);
          setLoadedBytes(loaded);
        },
        abortRef.current
      );

      setUploadedResult(result);
      queryClient.invalidateQueries({ queryKey: ["files"] });
      queryClient.invalidateQueries({ queryKey: ["nodes-stats"] });
      queryClient.invalidateQueries({ queryKey: ["nodes"] });
    } catch (err: any) {
      setErrorMsg(err.message || "An error occurred during upload");
    } finally {
      setIsUploading(false);
    }
  };

  const handleCancel = () => {
    if (abortRef.current.abort) {
      abortRef.current.abort();
    }
    setIsUploading(false);
    setProgress(0);
    setLoadedBytes(0);
    setErrorMsg("Upload cancelled by user.");
  };

  const handleReset = () => {
    setSelectedFile(null);
    setUploadedResult(null);
    setErrorMsg(null);
    setProgress(0);
    setLoadedBytes(0);
    if (fileInputRef.current) {
      fileInputRef.current.value = "";
    }
  };

  return (
    <div className="max-w-3xl mx-auto space-y-6">
      {/* Page Title */}
      <div className="border-b border-slate-800 pb-4">
        <h1 className="text-2xl font-bold tracking-tight text-white flex items-center gap-2">
          <UploadCloud className="h-6 w-6 text-indigo-400" />
          Upload to Storage Pool
        </h1>
        <p className="text-sm text-slate-400 mt-1">
          Files are automatically streamed, split into cryptographic chunks, and allocated across storage nodes by available capacity.
        </p>
      </div>

      {/* Main Upload Card */}
      <Card className="border-slate-800">
        <CardContent className="pt-6 space-y-6">
          {/* Success State */}
          {uploadedResult ? (
            <div className="text-center py-8 space-y-4">
              <div className="mx-auto flex h-14 w-14 items-center justify-center rounded-full bg-emerald-950/80 border border-emerald-800 text-emerald-400">
                <CheckCircle2 className="h-8 w-8" />
              </div>
              <div>
                <h3 className="text-lg font-semibold text-white">Upload Complete!</h3>
                <p className="text-sm text-slate-400 mt-1">
                  File <span className="text-white font-medium">{uploadedResult.filename}</span> ({formatBytes(uploadedResult.size)}) was successfully chunked and distributed.
                </p>
              </div>

              <div className="pt-4 flex items-center justify-center gap-3">
                <Button variant="outline" onClick={handleReset}>
                  Upload Another File
                </Button>
                {uploadedResult.id && (
                  <Link href={`/files/${uploadedResult.id}`}>
                    <Button className="flex items-center gap-1.5">
                      View File Chunks <ArrowRight className="h-4 w-4" />
                    </Button>
                  </Link>
                )}
                <Link href="/files">
                  <Button variant="secondary">Go to File Manager</Button>
                </Link>
              </div>
            </div>
          ) : (
            <>
              {/* Drag & Drop Zone */}
              <div
                onDragEnter={handleDrag}
                onDragOver={handleDrag}
                onDragLeave={handleDrag}
                onDrop={handleDrop}
                onClick={() => !isUploading && fileInputRef.current?.click()}
                className={`relative flex flex-col items-center justify-center rounded-xl border-2 border-dashed p-10 text-center transition-colors cursor-pointer ${
                  dragActive
                    ? "border-indigo-500 bg-indigo-950/20"
                    : "border-slate-800 bg-slate-950/50 hover:border-slate-700 hover:bg-slate-900/50"
                } ${isUploading ? "pointer-events-none opacity-60" : ""}`}
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
                  disabled={isUploading}
                />

                <div className="flex h-12 w-12 items-center justify-center rounded-full bg-slate-900 text-indigo-400 border border-slate-800 mb-4">
                  <UploadCloud className="h-6 w-6" />
                </div>

                <div className="text-sm font-medium text-white mb-1">
                  {selectedFile ? selectedFile.name : "Choose a file or drag & drop it here"}
                </div>
                <p className="text-xs text-slate-500 max-w-sm">
                  Supports streaming upload of any file type or size. Large files will span multiple local storage nodes.
                </p>

                {!selectedFile && (
                  <Button variant="secondary" size="sm" className="mt-4 pointer-events-none">
                    Select File
                  </Button>
                )}
              </div>

              {/* Selected File Details */}
              {selectedFile && !isUploading && (
                <div className="flex items-center justify-between rounded-lg border border-slate-800 bg-slate-900/60 p-4">
                  <div className="flex items-center gap-3">
                    <FileText className="h-8 w-8 text-indigo-400" />
                    <div>
                      <div className="text-sm font-semibold text-white">{selectedFile.name}</div>
                      <div className="text-xs text-slate-400">
                        {formatBytes(selectedFile.size)} · {selectedFile.type || "binary stream"}
                      </div>
                    </div>
                  </div>
                  <div className="flex items-center gap-2">
                    <Button variant="ghost" size="sm" onClick={handleReset}>
                      Change
                    </Button>
                    <Button size="sm" onClick={handleStartUpload} className="flex items-center gap-1.5">
                      <UploadCloud className="h-4 w-4" />
                      Start Upload
                    </Button>
                  </div>
                </div>
              )}

              {/* Upload Progress Display */}
              {isUploading && (
                <div className="rounded-lg border border-slate-800 bg-slate-900/60 p-5 space-y-3">
                  <div className="flex items-center justify-between text-sm">
                    <div className="flex items-center gap-2">
                      <UploadCloud className="h-4 w-4 text-indigo-400 animate-bounce" />
                      <span className="font-medium text-white">Streaming & Distributing...</span>
                    </div>
                    <span className="font-mono text-indigo-400 font-semibold">{progress}%</span>
                  </div>

                  <Progress value={progress} className="h-2.5" />

                  <div className="flex items-center justify-between text-xs text-slate-400">
                    <span>
                      {formatBytes(loadedBytes)} of {selectedFile ? formatBytes(selectedFile.size) : "unknown"}
                    </span>
                    <Button
                      variant="destructive"
                      size="sm"
                      onClick={handleCancel}
                      className="h-7 px-3 text-xs flex items-center gap-1"
                    >
                      <XCircle className="h-3.5 w-3.5" />
                      Cancel Upload
                    </Button>
                  </div>
                </div>
              )}

              {/* Error Message */}
              {errorMsg && (
                <div className="rounded-lg border border-rose-800/80 bg-rose-950/40 p-3 text-sm text-rose-300 flex items-center gap-2">
                  <AlertCircle className="h-4 w-4 shrink-0 text-rose-400" />
                  <span>{errorMsg}</span>
                </div>
              )}
            </>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
