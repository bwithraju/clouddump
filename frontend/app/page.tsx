"use client";

import React from "react";
import { Server, Database, HardDrive, CheckCircle2, ShieldCheck, Activity } from "lucide-react";
import { useHealth } from "@/hooks/use-health";

export default function Home() {
  const { data: health, isLoading, isError, error } = useHealth();

  return (
    <div className="min-h-screen flex flex-col justify-between p-6 sm:p-10 max-w-6xl mx-auto">
      {/* Header */}
      <header className="flex flex-col sm:flex-row sm:items-center justify-between gap-4 pb-6 border-b border-slate-800">
        <div>
          <div className="flex items-center gap-2">
            <HardDrive className="w-8 h-8 text-indigo-400" />
            <h1 className="text-2xl font-bold tracking-tight text-white">
              CloudDump
            </h1>
            <span className="px-2 py-0.5 text-xs font-medium rounded bg-indigo-950 text-indigo-300 border border-indigo-800">
              v1.0.0-dev
            </span>
          </div>
          <p className="text-sm text-slate-400 mt-1">
            Distributed Cloud Storage Engine · Logical Storage Pool
          </p>
        </div>

        {/* Backend Connectivity Status */}
        <div className="flex items-center gap-3 bg-slate-900 border border-slate-800 px-4 py-2 rounded-lg text-sm">
          <Activity className="w-4 h-4 text-slate-400 animate-pulse" />
          <span className="text-slate-400">Backend API:</span>
          {isLoading && (
            <span className="text-amber-400 font-medium">Checking...</span>
          )}
          {isError && (
            <span className="text-rose-400 font-medium">
              Offline (Waiting for Go Server)
            </span>
          )}
          {health && (
            <span className="text-emerald-400 font-medium flex items-center gap-1">
              <CheckCircle2 className="w-4 h-4" />
              Online ({health.service})
            </span>
          )}
        </div>
      </header>

      {/* Main Content */}
      <main className="py-10 space-y-8">
        <section className="bg-slate-900/60 border border-slate-800/80 rounded-xl p-6 sm:p-8 backdrop-blur">
          <h2 className="text-lg font-semibold text-white mb-2">
            System Overview & Pool Concept
          </h2>
          <p className="text-sm text-slate-300 leading-relaxed max-w-3xl">
            CloudDump combines heterogeneous independent storage nodes into one logical pool.
            Files larger than any single node are streamed, split into configurable cryptographic
            chunks (SHA-256), and distributed across nodes according to real-time available capacity.
          </p>

          <div className="grid grid-cols-1 md:grid-cols-4 gap-4 mt-6">
            <div className="bg-slate-950/70 border border-slate-800 p-4 rounded-lg">
              <div className="text-xs text-slate-500 uppercase font-mono tracking-wider">Node 1 (Simulated)</div>
              <div className="text-lg font-semibold text-slate-200 mt-1">500 MB</div>
              <div className="text-xs text-emerald-400 mt-1">Status: Registered</div>
            </div>
            <div className="bg-slate-950/70 border border-slate-800 p-4 rounded-lg">
              <div className="text-xs text-slate-500 uppercase font-mono tracking-wider">Node 2 (Simulated)</div>
              <div className="text-lg font-semibold text-slate-200 mt-1">1.0 GB</div>
              <div className="text-xs text-emerald-400 mt-1">Status: Registered</div>
            </div>
            <div className="bg-slate-950/70 border border-slate-800 p-4 rounded-lg">
              <div className="text-xs text-slate-500 uppercase font-mono tracking-wider">Node 3 (Simulated)</div>
              <div className="text-lg font-semibold text-slate-200 mt-1">2.0 GB</div>
              <div className="text-xs text-emerald-400 mt-1">Status: Registered</div>
            </div>
            <div className="bg-slate-950/70 border border-slate-800 p-4 rounded-lg">
              <div className="text-xs text-slate-500 uppercase font-mono tracking-wider">Node 4 (Simulated)</div>
              <div className="text-lg font-semibold text-slate-200 mt-1">700 MB</div>
              <div className="text-xs text-emerald-400 mt-1">Status: Registered</div>
            </div>
          </div>
        </section>

        {/* Architecture Stack */}
        <section className="grid grid-cols-1 md:grid-cols-3 gap-6">
          <div className="bg-slate-900/40 border border-slate-800 p-5 rounded-lg space-y-3">
            <div className="flex items-center gap-2 text-indigo-400 font-semibold text-sm">
              <Server className="w-5 h-5" />
              <span>Backend Core</span>
            </div>
            <p className="text-xs text-slate-400">
              Go 1.24+ REST API with streaming I/O, goroutine worker concurrency, and SHA-256 integrity verification.
            </p>
          </div>

          <div className="bg-slate-900/40 border border-slate-800 p-5 rounded-lg space-y-3">
            <div className="flex items-center gap-2 text-sky-400 font-semibold text-sm">
              <Database className="w-5 h-5" />
              <span>Metadata & Queue</span>
            </div>
            <p className="text-xs text-slate-400">
              PostgreSQL for files/chunks/nodes relational schema, with Redis for resumable upload workers.
            </p>
          </div>

          <div className="bg-slate-900/40 border border-slate-800 p-5 rounded-lg space-y-3">
            <div className="flex items-center gap-2 text-violet-400 font-semibold text-sm">
              <ShieldCheck className="w-5 h-5" />
              <span>Storage Abstraction</span>
            </div>
            <p className="text-xs text-slate-400">
              Modular StorageProvider interface supporting LocalStorageProvider with planned Google Drive integration.
            </p>
          </div>
        </section>
      </main>

      {/* Footer */}
      <footer className="pt-6 border-t border-slate-900 flex flex-col sm:flex-row items-center justify-between text-xs text-slate-500 gap-2">
        <div>CloudDump · Production-Style Distributed Cloud Storage</div>
        <div>Phase 1 Initialized · Next.js + Go</div>
      </footer>
    </div>
  );
}
