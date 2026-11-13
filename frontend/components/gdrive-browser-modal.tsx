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
  const [importedFiles, setImportedFiles] = React.useState<Record<string, string>>({});
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
    if (mimeType.startsWith("image/")) return <ImageIcon className="h-4 w-4 text-emerald-500" />;
    if (mimeType.startsWith("video/")) return <Film className="h-4 w-4 text-indigo-500" />;
    if (mimeType.startsWith("audio/")) return <Music className="h-4 w-4 text-purple-500" />;
    if (mimeType.includes("pdf") || mimeType.includes("document") || mimeType.includes("text"))
      return <FileText className="h-4 w-4 text-amber-500" />;
    return <File className="h-4 w-4 text-gray-400" />;
  };

  return createPortal(
    <div
      className="fixed inset-0 z-50 overflow-y-auto bg-slate-950/40 backdrop-blur-xs animate-in fade-in duration-150"
      onClick={(e) => {
        if (e.target === e.currentTarget) onClose();
      }}
    >
      <div className="flex min-h-full items-center justify-center p-4">
        <div
          className="relative w-full max-w-3xl rounded-2xl border border-gray-200 bg-white shadow-2xl flex flex-col max-h-[85vh] overflow-hidden text-slate-900"
          role="dialog"
          aria-modal="true"
          aria-labelledby="gdrive-browser-title"
        >
          {/* Header */}
          <div className="flex items-center justify-between border-b border-gray-100 p-5 bg-gray-50/60">
            <div className="flex items-center gap-2.5">
              <div className="p-2 rounded-xl bg-blue-50 text-blue-600 border border-blue-100">
                <Cloud className="h-5 w-5" />
              </div>
              <div>
                <h2 id="gdrive-browser-title" className="text-base font-bold text-slate-900 flex items-center gap-2">
                  Browse Google Drive
                  <Badge variant="outline" className="text-[10px] text-indigo-700 border-indigo-200 bg-indigo-50">
                    Drive API v3
                  </Badge>
                </h2>
                <p className="text-xs text-gray-500">
                  Browse and import remote files from your connected Google Drive into the distributed storage pool.
                </p>
              </div>
            </div>
            <button
              type="button"
              onClick={onClose}
              className="rounded-lg p-1.5 text-gray-400 hover:text-slate-700 hover:bg-gray-100 transition-colors"
              aria-label="Close"
            >
              <X className="h-5 w-5" />
            </button>
          </div>

          {/* Search Bar & Controls */}
          <div className="p-4 border-b border-gray-100 bg-white flex items-center gap-2">
            <form onSubmit={handleSearchSubmit} className="flex-1 relative flex items-center">
              <Search className="absolute left-3 top-2.5 h-4 w-4 text-gray-400" />
              <input
                type="text"
                placeholder="Search files in Google Drive by name..."
                value={searchQuery}
                onChange={(e) => setSearchQuery(e.target.value)}
                className="w-full rounded-lg border border-gray-200 bg-gray-50/70 pl-9 pr-3 py-1.5 text-xs text-slate-900 placeholder:text-gray-400 focus:bg-white focus:border-indigo-500 focus:outline-none focus:ring-2 focus:ring-indigo-500/15 transition-all"
              />
            </form>
            <Button
              type="button"
              size="sm"
              onClick={() => loadFiles(searchQuery)}
              disabled={isLoading}
              variant="outline"
              className="h-8 px-3 text-xs flex items-center gap-1.5 border-gray-200 text-slate-700"
            >
              {isLoading ? (
                <Loader2 className="h-3.5 w-3.5 animate-spin" />
              ) : (
                <RefreshCw className="h-3.5 w-3.5" />
              )}
              Refresh
            </Button>
          </div>

          {/* Alerts */}
          {errorMsg && (
            <div className="mx-4 mt-3 rounded-xl border border-rose-200 bg-rose-50 p-3 text-xs text-rose-800 flex items-start gap-2 shadow-xs">
              <AlertCircle className="h-4 w-4 text-rose-600 shrink-0 mt-0.5" />
              <span>{errorMsg}</span>
            </div>
          )}

          {/* File List */}
          <div className="flex-1 overflow-y-auto divide-y divide-gray-100 p-2">
            {isLoading ? (
              <div className="py-16 text-center text-xs text-gray-400 space-y-2">
                <Loader2 className="mx-auto h-6 w-6 text-indigo-600 animate-spin" />
                <div>Fetching files from Google Drive...</div>
              </div>
            ) : files.length === 0 ? (
              <div className="py-16 text-center space-y-2 text-xs text-gray-400">
                <Cloud className="mx-auto h-8 w-8 text-gray-300" />
                <div className="font-medium text-slate-700">No files found</div>
                <p className="text-gray-400">
                  {searchQuery ? "Try another search term" : "Your Google Drive root is empty or has no accessible files."}
                </p>
              </div>
            ) : (
              files.map((file) => {
                const isImporting = importingId === file.id;
                const importedId = importedFiles[file.id];

                return (
                  <div
                    key={file.id}
                    className="flex items-center justify-between p-3 rounded-lg hover:bg-gray-50/80 transition-colors text-xs"
                  >
                    <div className="flex items-center gap-3 min-w-0 pr-4">
                      <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg bg-gray-50 border border-gray-100">
                        {getFileIcon(file.mime_type)}
                      </div>
                      <div className="min-w-0">
                        <div className="font-semibold text-slate-900 truncate text-xs">{file.name}</div>
                        <div className="text-[11px] text-gray-400 flex items-center gap-2 mt-0.5">
                          <span>{formatBytes(file.size)}</span>
                          <span>•</span>
                          <span>{formatDate(file.modified_time)}</span>
                          {file.is_folder && (
                            <Badge variant="secondary" className="text-[9px] py-0 px-1">
                              Folder
                            </Badge>
                          )}
                        </div>
                      </div>
                    </div>

                    <div className="flex items-center gap-2 shrink-0">
                      {importedId ? (
                        <div className="flex items-center gap-1.5">
                          <span className="flex items-center gap-1 text-[11px] text-emerald-600 font-medium">
                            <CheckCircle2 className="h-3.5 w-3.5" />
                            Imported
                          </span>
                          <Link
                            href={`/files/${importedId}`}
                            onClick={onClose}
                            className="inline-flex items-center gap-1 rounded-md bg-indigo-50 border border-indigo-200 px-2 py-1 text-[11px] font-medium text-indigo-700 hover:bg-indigo-100"
                          >
                            View Chunks <ArrowRight className="h-3 w-3" />
                          </Link>
                        </div>
                      ) : (
                        <Button
                          size="sm"
                          disabled={isImporting || file.is_folder}
                          onClick={() => handleImport(file)}
                          className="h-7 px-3 text-xs bg-slate-900 hover:bg-slate-800 text-white shadow-xs"
                        >
                          {isImporting ? (
                            <>
                              <Loader2 className="h-3 w-3 animate-spin mr-1" />
                              Importing...
                            </>
                          ) : (
                            <>
                              <Download className="h-3 w-3 mr-1" />
                              Import to Pool
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
          <div className="p-3.5 border-t border-gray-100 bg-gray-50/60 flex items-center justify-between text-xs text-gray-500">
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
