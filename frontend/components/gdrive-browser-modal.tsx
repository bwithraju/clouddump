"use client";

import * as React from "react";
import { createPortal } from "react-dom";
import Link from "next/link";
import {
  X,
  Search,
  Cloud,
  FileText,
  File,
  Image as ImageIcon,
  Film,
  Music,
  CheckCircle2,
  AlertCircle,
  Loader2,
  Download,
  ArrowRight,
  RefreshCw,
} from "lucide-react";
import { fetchGoogleDriveFiles, importGoogleDriveFile, DriveFileItem } from "@/lib/api";
import { formatBytes, formatDate } from "@/lib/utils";
import { Button, getButtonClassName } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { useQueryClient } from "@tanstack/react-query";

interface GDriveBrowserModalProps {
  isOpen: boolean;
  onClose: () => void;
}

export function GDriveBrowserModal({ isOpen, onClose }: GDriveBrowserModalProps) {
  const queryClient = useQueryClient();
  const [files, setFiles] = React.useState<DriveFileItem[]>([]);
  const [isLoading, setIsLoading] = React.useState(false);
  const [searchQuery, setSearchQuery] = React.useState("");
  const [errorMsg, setErrorMsg] = React.useState<string | null>(null);
  const [importingId, setImportingId] = React.useState<string | null>(null);
  const [importedFiles, setImportedFiles] = React.useState<Record<string, string>>({}); // driveId -> clouddumpFileId
  const [mounted, setMounted] = React.useState(false);

  React.useEffect(() => {
    setMounted(true);
  }, []);

  const loadFiles = React.useCallback(async (query: string = "") => {
    setIsLoading(true);
    setErrorMsg(null);
    try {
      const res = await fetchGoogleDriveFiles(query.trim() || undefined);
      setFiles(res.files || []);
    } catch (err: any) {
      setErrorMsg(err.message || "Failed to load files from Google Drive.");
    } finally {
      setIsLoading(false);
    }
  }, []);

  React.useEffect(() => {
    if (isOpen) {
      loadFiles(searchQuery);
    }
  }, [isOpen, loadFiles]);

  // Handle escape key
  React.useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === "Escape" && isOpen) {
        onClose();
      }
    };
    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, [isOpen, onClose]);

  if (!isOpen || !mounted) return null;

  const handleSearchSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    loadFiles(searchQuery);
  };

  const handleImport = async (file: DriveFileItem) => {
    setImportingId(file.id);
    setErrorMsg(null);
    try {
      const res = await importGoogleDriveFile(file.id, file.name);
      setImportedFiles((prev) => ({ ...prev, [file.id]: res.id }));
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["files"] }),
        queryClient.invalidateQueries({ queryKey: ["nodes-stats"] }),
        queryClient.invalidateQueries({ queryKey: ["nodes"] }),
        queryClient.invalidateQueries({ queryKey: ["system/activity"] }),
      ]);
    } catch (err: any) {
      setErrorMsg(`Import failed for "${file.name}": ${err.message || err}`);
    } finally {
      setImportingId(null);
    }
  };

  const getFileIcon = (mimeType: string) => {
    if (mimeType.startsWith("image/")) return <ImageIcon className="h-4 w-4 text-emerald-400" />;
    if (mimeType.startsWith("video/")) return <Film className="h-4 w-4 text-indigo-400" />;
    if (mimeType.startsWith("audio/")) return <Music className="h-4 w-4 text-purple-400" />;
    if (mimeType.includes("pdf") || mimeType.includes("document") || mimeType.includes("text"))
      return <FileText className="h-4 w-4 text-amber-400" />;
    return <File className="h-4 w-4 text-slate-400" />;
  };

  return createPortal(
    <div
      className="fixed inset-0 z-50 overflow-y-auto bg-slate-950/80 backdrop-blur-sm animate-in fade-in duration-200"
      onClick={(e) => {
        if (e.target === e.currentTarget) onClose();
      }}
    >
      <div className="flex min-h-full items-center justify-center p-4">
        <div
          className="relative w-full max-w-3xl rounded-xl border border-slate-800 bg-slate-900 shadow-2xl flex flex-col max-h-[85vh] overflow-hidden"
          role="dialog"
          aria-modal="true"
          aria-labelledby="gdrive-browser-title"
        >
        {/* Header */}
        <div className="flex items-center justify-between border-b border-slate-800 p-5 bg-slate-950/60">
          <div className="flex items-center gap-2.5">
            <div className="p-2 rounded-lg bg-indigo-950/80 text-indigo-400 border border-indigo-900/50">
              <Cloud className="h-5 w-5" />
            </div>
            <div>
              <h2 id="gdrive-browser-title" className="text-lg font-bold text-white flex items-center gap-2">
                Browse Google Drive
                <Badge variant="outline" className="text-[10px] text-indigo-300 border-indigo-800">
                  Drive API v3
                </Badge>
              </h2>
              <p className="text-xs text-slate-400">
                Browse and import files from your connected Google Drive into the distributed storage pool.
              </p>
            </div>
          </div>
          <button
            type="button"
            onClick={onClose}
            className="rounded-md p-1.5 text-slate-400 hover:text-white hover:bg-slate-800 transition-colors"
            aria-label="Close"
          >
            <X className="h-5 w-5" />
          </button>
        </div>

        {/* Search Bar & Controls */}
        <div className="p-4 border-b border-slate-800 bg-slate-900/80 flex items-center gap-2">
          <form onSubmit={handleSearchSubmit} className="flex-1 relative flex items-center">
            <Search className="absolute left-3 top-2.5 h-4 w-4 text-slate-500" />
            <input
              type="text"
              placeholder="Search files by name..."
              value={searchQuery}
              onChange={(e) => setSearchQuery(e.target.value)}
              className="w-full rounded-lg border border-slate-800 bg-slate-950 pl-9 pr-3 py-1.5 text-xs text-white placeholder-slate-500 focus:border-indigo-500 focus:outline-none focus:ring-1 focus:ring-indigo-500"
            />
          </form>
          <Button
            type="button"
            size="sm"
            onClick={() => loadFiles(searchQuery)}
            disabled={isLoading}
            variant="outline"
            className="h-8 px-3 text-xs flex items-center gap-1.5 border-slate-800"
          >
            <RefreshCw className={`h-3.5 w-3.5 ${isLoading ? "animate-spin" : ""}`} />
            Refresh
          </Button>
        </div>

        {/* Error Alert */}
        {errorMsg && (
          <div className="m-4 rounded-lg border border-rose-800/80 bg-rose-950/40 p-3 text-xs text-rose-300 flex items-start gap-2">
            <AlertCircle className="h-4 w-4 text-rose-400 shrink-0 mt-0.5" />
            <span>{errorMsg}</span>
          </div>
        )}

        {/* Files List */}
        <div className="flex-1 overflow-y-auto divide-y divide-slate-800/60 p-2">
          {isLoading ? (
            <div className="py-16 text-center space-y-3">
              <Loader2 className="mx-auto h-8 w-8 text-indigo-400 animate-spin" />
              <p className="text-xs text-slate-400">Loading files from your Google Drive...</p>
            </div>
          ) : files.length === 0 ? (
            <div className="py-16 text-center space-y-2">
              <Cloud className="mx-auto h-10 w-10 text-slate-600" />
              <p className="text-sm font-medium text-slate-300">No files found</p>
              <p className="text-xs text-slate-500 max-w-xs mx-auto">
                {searchQuery ? "Try searching for a different keyword." : "Your Google Drive has no root files or all items are folders."}
              </p>
            </div>
          ) : (
            files.map((file) => {
              const isImporting = importingId === file.id;
              const importedId = importedFiles[file.id];

              return (
                <div
                  key={file.id}
                  className="p-3 flex items-center justify-between gap-3 hover:bg-slate-800/40 rounded-lg transition-colors"
                >
                  <div className="flex items-center gap-3 min-w-0">
                    <div className="p-2 rounded bg-slate-950 border border-slate-800 shrink-0">
                      {getFileIcon(file.mime_type)}
                    </div>
                    <div className="min-w-0">
                      <div className="text-xs font-semibold text-white truncate max-w-md" title={file.name}>
                        {file.name}
                      </div>
                      <div className="text-[11px] text-slate-400 flex items-center gap-2 mt-0.5">
                        <span>{formatBytes(file.size)}</span>
                        <span>·</span>
                        <span>{formatDate(file.modified_time)}</span>
                      </div>
                    </div>
                  </div>

                  <div className="flex items-center gap-2 shrink-0">
                    {importedId ? (
                      <div className="flex items-center gap-2">
                        <Badge variant="success" className="text-[10px] flex items-center gap-1">
                          <CheckCircle2 className="h-3 w-3" /> Imported
                        </Badge>
                        <Link
                          href={`/files/${importedId}`}
                          className={getButtonClassName("ghost", "sm", "h-7 px-2 text-[11px] text-indigo-300 flex items-center gap-1")}
                        >
                          View Chunks <ArrowRight className="h-3 w-3" />
                        </Link>
                      </div>
                    ) : (
                      <Button
                        size="sm"
                        disabled={isImporting}
                        onClick={() => handleImport(file)}
                        className="h-7 px-3 text-xs bg-indigo-600 hover:bg-indigo-500 flex items-center gap-1.5"
                      >
                        {isImporting ? (
                          <>
                            <Loader2 className="h-3 w-3 animate-spin" />
                            <span>Chunking...</span>
                          </>
                        ) : (
                          <>
                            <Download className="h-3 w-3" />
                            <span>Import to Pool</span>
                          </>
                        )}
                      </Button>
                    )}
                  </div>
                </div>
              );
            })
          )}
        </div>

        {/* Footer */}
        <div className="p-3.5 border-t border-slate-800 bg-slate-950/60 flex items-center justify-between text-xs text-slate-400">
          <span>{files.length} file{files.length === 1 ? "" : "s"} found</span>
          <Button variant="ghost" size="sm" onClick={onClose} className="h-7 px-3 text-xs">
            Close
          </Button>
        </div>
      </div>
    </div>
  </div>,
  document.body
  );
}
