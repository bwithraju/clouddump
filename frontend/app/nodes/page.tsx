"use client";

import * as React from "react";
import Link from "next/link";
import {
  Server,
  HardDrive,
  CheckCircle2,
  AlertTriangle,
  RefreshCw,
  PowerOff,
  Power,
  ShieldAlert,
  Cloud,
  PlusCircle,
  FolderOpen,
  AlertCircle,
  Radio,
} from "lucide-react";
import { useQueryClient } from "@tanstack/react-query";
import { usePoolStats, useNodes } from "@/hooks/use-nodes";
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Progress } from "@/components/ui/progress";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { formatBytes, formatDate } from "@/lib/utils";
import { GDriveBrowserModal } from "@/components/gdrive-browser-modal";
import {
  simulateNodeFailure,
  triggerReconcile,
  fetchGoogleOAuthStatus,
  fetchGoogleAuthURL,
  createGoogleDriveNode,
  disconnectGoogleDrive,
} from "@/lib/api";

export default function StorageNodesPage() {
  const queryClient = useQueryClient();
  const { data: stats, isLoading: statsLoading, refetch: refetchStats } = usePoolStats();
  const { data: nodes, isLoading: nodesLoading, refetch: refetchNodes } = useNodes();
  const [togglingNodeId, setTogglingNodeId] = React.useState<string | null>(null);
  const [reconciling, setReconciling] = React.useState(false);
  const [gdriveConnected, setGdriveConnected] = React.useState(false);
  const [connecting, setConnecting] = React.useState(false);
  const [creatingNode, setCreatingNode] = React.useState(false);
  const [browserModalOpen, setBrowserModalOpen] = React.useState(false);
  const [oauthBanner, setOauthBanner] = React.useState<{ type: "success" | "error"; message: string } | null>(null);

  React.useEffect(() => {
    fetchGoogleOAuthStatus()
      .then((status) => {
        setGdriveConnected(status.connected);
      })
      .catch(() => {});

    if (typeof window !== "undefined") {
      const params = new URLSearchParams(window.location.search);
      if (params.get("gdrive") === "connected") {
        setOauthBanner({
          type: "success",
          message: "Google Drive account was successfully linked via OAuth 2.0! You can now browse files or add Google Drive storage nodes.",
        });
        setGdriveConnected(true);
        queryClient.invalidateQueries({ queryKey: ["nodes"] });
        window.history.replaceState({}, "", window.location.pathname);
      } else if (params.get("gdrive_error")) {
        setOauthBanner({
          type: "error",
          message: `Google OAuth Authorization Failed: ${params.get("gdrive_error")}`,
        });
        window.history.replaceState({}, "", window.location.pathname);
      }
    }
  }, [queryClient]);

  const handleConnectGDrive = async () => {
    try {
      setConnecting(true);
      const res = await fetchGoogleAuthURL();
      if (!res.configured) {
        alert("Google OAuth is not configured on the backend. Please set GOOGLE_CLIENT_ID and GOOGLE_CLIENT_SECRET environment variables.");
        return;
      }
      window.location.href = res.auth_url;
    } catch (err: any) {
      alert(`OAuth Error: ${err.message || err}`);
    } finally {
      setConnecting(false);
    }
  };

  const handleCreateDriveNode = async () => {
    try {
      setCreatingNode(true);
      await createGoogleDriveNode("Google Drive Primary");
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["nodes"] }),
        queryClient.invalidateQueries({ queryKey: ["poolStats"] }),
      ]);
    } catch (err: any) {
      alert(`Failed to create Google Drive node: ${err.message || err}`);
    } finally {
      setCreatingNode(false);
    }
  };

  const handleDisconnectGDrive = async () => {
    try {
      await disconnectGoogleDrive();
      setGdriveConnected(false);
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["nodes"] }),
        queryClient.invalidateQueries({ queryKey: ["poolStats"] }),
      ]);
    } catch (err: any) {
      alert(`Failed to disconnect: ${err.message || err}`);
    }
  };

  const handleRefresh = async () => {
    await Promise.all([refetchStats(), refetchNodes()]);
  };

  const handleToggleFailure = async (nodeId: string, currentStatus: string) => {
    try {
      setTogglingNodeId(nodeId);
      const makeOffline = currentStatus === "ONLINE";
      await simulateNodeFailure(nodeId, makeOffline);
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["nodes"] }),
        queryClient.invalidateQueries({ queryKey: ["poolStats"] }),
        queryClient.invalidateQueries({ queryKey: ["files"] }),
      ]);
    } catch (err: any) {
      alert(`Failure simulation error: ${err.message || err}`);
    } finally {
      setTogglingNodeId(null);
    }
  };

  const handleReconcile = async () => {
    try {
      setReconciling(true);
      await triggerReconcile();
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["nodes"] }),
        queryClient.invalidateQueries({ queryKey: ["poolStats"] }),
        queryClient.invalidateQueries({ queryKey: ["files"] }),
      ]);
    } catch (err: any) {
      alert(`Reconciliation error: ${err.message || err}`);
    } finally {
      setReconciling(false);
    }
  };

  const totalCap = stats?.total_capacity || 0;
  const usedCap = stats?.used_capacity || 0;
  const freeCap = stats?.available_capacity || Math.max(0, totalCap - usedCap);
  const usedPct = totalCap > 0 ? Math.min(100, Math.round((usedCap / totalCap) * 100)) : 0;
  const offlineNodes = stats?.offline_nodes || 0;

  return (
    <div className="space-y-7">
      {/* Page Header */}
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4">
        <div>
          <h1 className="text-2xl font-bold tracking-tight text-slate-900 flex items-center gap-2.5">
            <Server className="h-6 w-6 text-indigo-600" />
            Nodes &amp; Storage
          </h1>
          <p className="text-xs text-gray-500 mt-0.5">
            Monitor node health, capacity allocation, simulate outages, and manage Google Drive cloud providers.
          </p>
        </div>
        <div className="flex items-center gap-2">
          <Button
            variant="outline"
            size="sm"
            onClick={handleReconcile}
            disabled={reconciling}
            className="text-xs flex items-center gap-1.5 text-indigo-700 border-indigo-200 bg-indigo-50/50 hover:bg-indigo-50"
          >
            <ShieldAlert className="h-3.5 w-3.5 text-indigo-600" />
            {reconciling ? "Probing Health..." : "Probe Health & Reconcile"}
          </Button>
          <Button
            variant="outline"
            size="sm"
            onClick={handleRefresh}
            className="text-xs flex items-center gap-1.5 text-gray-600 hover:text-slate-900"
          >
            <RefreshCw className="h-3.5 w-3.5" />
            Refresh
          </Button>
        </div>
      </div>

      {/* Offline Alert Banner */}
      {offlineNodes > 0 && (
        <div className="rounded-xl border border-amber-200 bg-amber-50/80 p-4 flex items-start gap-3 text-amber-900 shadow-xs text-xs">
          <AlertTriangle className="h-5 w-5 text-amber-600 shrink-0 mt-0.5" />
          <div className="space-y-1">
            <div className="font-semibold text-sm">
              {offlineNodes} storage {offlineNodes === 1 ? "node is" : "nodes are"} currently OFFLINE
            </div>
            <p className="text-amber-800/90">
              Affected chunks are temporarily unreachable. Files with missing primary chunks are marked <span className="font-mono font-semibold">DEGRADED</span> to prevent corrupt file streams. New uploads automatically divert to online nodes.
            </p>
          </div>
        </div>
      )}

      {/* OAuth Callback Notification Banner */}
      {oauthBanner && (
        <div
          className={`rounded-xl p-4 text-xs flex items-center justify-between gap-3 shadow-xs ${
            oauthBanner.type === "success"
              ? "border border-emerald-200 bg-emerald-50 text-emerald-900"
              : "border border-rose-200 bg-rose-50 text-rose-900"
          }`}
        >
          <div className="flex items-center gap-2.5">
            {oauthBanner.type === "success" ? (
              <CheckCircle2 className="h-5 w-5 text-emerald-600 shrink-0" />
            ) : (
              <AlertCircle className="h-5 w-5 text-rose-600 shrink-0" />
            )}
            <span>{oauthBanner.message}</span>
          </div>
          <Button
            variant="ghost"
            size="sm"
            onClick={() => setOauthBanner(null)}
            className="h-6 px-2 text-xs text-gray-500 hover:text-slate-900"
          >
            Dismiss
          </Button>
        </div>
      )}

      {/* Aggregate Pool Overview Cards */}
      <div className="grid grid-cols-2 md:grid-cols-5 gap-4">
        <div className="rounded-xl border border-gray-200/80 bg-white p-4 shadow-xs space-y-1">
          <div className="text-[11px] font-semibold text-gray-400 uppercase tracking-wider">Total Capacity</div>
          <div className="text-xl font-bold text-slate-900">{formatBytes(totalCap)}</div>
          <div className="text-xs text-gray-400">All registered targets</div>
        </div>

        <div className="rounded-xl border border-gray-200/80 bg-white p-4 shadow-xs space-y-1">
          <div className="text-[11px] font-semibold text-gray-400 uppercase tracking-wider">Used Capacity</div>
          <div className="text-xl font-bold text-indigo-600">{formatBytes(usedCap)}</div>
          <div className="text-xs text-gray-400">{usedPct}% of pool reserved</div>
        </div>

        <div className="rounded-xl border border-gray-200/80 bg-white p-4 shadow-xs space-y-1">
          <div className="text-[11px] font-semibold text-gray-400 uppercase tracking-wider">Available Capacity</div>
          <div className="text-xl font-bold text-emerald-600">{formatBytes(freeCap)}</div>
          <div className="text-xs text-gray-400">Ready for incoming chunks</div>
        </div>

        <div className="rounded-xl border border-gray-200/80 bg-white p-4 shadow-xs space-y-1">
          <div className="text-[11px] font-semibold text-gray-400 uppercase tracking-wider">Online Nodes</div>
          <div className="text-xl font-bold text-emerald-600 flex items-center gap-1.5">
            <CheckCircle2 className="h-5 w-5 text-emerald-500" />
            {stats?.online_nodes || 0} / {stats?.node_count || 0}
          </div>
          <div className="text-xs text-gray-400">Operational targets</div>
        </div>

        <div className={`rounded-xl border p-4 shadow-xs space-y-1 ${
          offlineNodes > 0
            ? "border-amber-200 bg-amber-50/50"
            : "border-gray-200/80 bg-white"
        }`}>
          <div className="text-[11px] font-semibold text-gray-400 uppercase tracking-wider">Offline Nodes</div>
          <div className={`text-xl font-bold flex items-center gap-1.5 ${
            offlineNodes > 0 ? "text-amber-600" : "text-gray-400"
          }`}>
            <AlertTriangle className={`h-5 w-5 ${offlineNodes > 0 ? "text-amber-500" : "text-gray-300"}`} />
            {offlineNodes}
          </div>
          <div className="text-xs text-gray-400">
            {offlineNodes > 0 ? "Failover active" : "Zero failed nodes"}
          </div>
        </div>
      </div>

      {/* Cloud Storage Provider Integration (Google Drive) */}
      <div className="rounded-xl border border-gray-200/80 bg-white p-5 shadow-xs space-y-3">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-2.5">
            <div className="flex h-9 w-9 items-center justify-center rounded-xl bg-blue-50 text-blue-600">
              <Cloud className="h-5 w-5" />
            </div>
            <div>
              <h3 className="text-sm font-semibold text-slate-900">Google Drive Storage Provider</h3>
              <p className="text-xs text-gray-500">
                Connect Google Drive via OAuth 2.0 to browse remote files or add Google Drive as a heterogeneous storage node.
              </p>
            </div>
          </div>
          <Badge variant={gdriveConnected ? "success" : "outline"}>
            {gdriveConnected ? "OAuth Linked (Active)" : "Not Connected"}
          </Badge>
        </div>

        <div className="flex flex-wrap items-center gap-2.5 pt-1">
          {!gdriveConnected ? (
            <Button
              size="sm"
              onClick={handleConnectGDrive}
              disabled={connecting}
              className="bg-slate-900 hover:bg-slate-800 text-white text-xs shadow-xs"
            >
              {connecting ? "Opening OAuth..." : "Connect Google Drive via OAuth"}
            </Button>
          ) : (
            <>
              <Button
                size="sm"
                onClick={() => setBrowserModalOpen(true)}
                className="bg-indigo-600 hover:bg-indigo-700 text-white text-xs flex items-center gap-1.5 shadow-xs"
              >
                <FolderOpen className="h-3.5 w-3.5" />
                Browse &amp; Import Files from Drive
              </Button>
              <Button
                size="sm"
                variant="outline"
                onClick={handleCreateDriveNode}
                disabled={creatingNode}
                className="text-xs flex items-center gap-1.5 shadow-xs"
              >
                <PlusCircle className="h-3.5 w-3.5 text-indigo-600" />
                {creatingNode ? "Adding Node..." : "Add Drive Node to Pool"}
              </Button>
              <Button
                variant="outline"
                size="sm"
                onClick={handleDisconnectGDrive}
                className="text-xs text-rose-600 hover:bg-rose-50 hover:text-rose-700 border-gray-200"
              >
                Disconnect Google Drive
              </Button>
            </>
          )}
        </div>
      </div>

      <GDriveBrowserModal
        isOpen={browserModalOpen}
        onClose={() => setBrowserModalOpen(false)}
      />

      {/* Node Cards Grid */}
      <div className="space-y-3">
        <h2 className="text-base font-bold text-slate-900">Node Allocation Status</h2>
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
          {(nodes || []).map((node) => {
            const usagePercent =
              node.total_capacity > 0
                ? Math.min(100, Math.round((node.used_capacity / node.total_capacity) * 100))
                : 0;
            const isOnline = node.status === "ONLINE";
            const isToggling = togglingNodeId === node.id;

            return (
              <div
                key={node.id}
                className="rounded-xl border border-gray-200/80 bg-white p-4 shadow-xs space-y-3 flex flex-col justify-between"
              >
                <div className="space-y-2">
                  <div className="flex items-center justify-between">
                    <span className="font-semibold text-sm text-slate-900 truncate pr-2">{node.name}</span>
                    <Badge variant={isOnline ? "success" : node.status === "DEGRADED" ? "warning" : "destructive"}>
                      {node.status}
                    </Badge>
                  </div>

                  <div className="text-[11px] text-gray-400 font-mono">
                    Provider: {node.provider_type || "local"} · {node.id}
                  </div>

                  <Progress value={usagePercent} className="h-2" />

                  <div className="flex justify-between text-xs text-gray-500">
                    <span>{formatBytes(node.used_capacity)} used</span>
                    <span className="font-semibold text-slate-700">{usagePercent}%</span>
                  </div>

                  <div className="flex justify-between text-[11px] text-gray-400 pt-0.5 border-t border-gray-100">
                    <span>Total: {formatBytes(node.total_capacity)}</span>
                    <span className="text-emerald-600 font-medium">Free: {formatBytes(node.available_capacity)}</span>
                  </div>
                </div>

                <div className="pt-2">
                  <Button
                    variant={isOnline ? "destructive" : "outline"}
                    size="sm"
                    disabled={isToggling}
                    onClick={() => handleToggleFailure(node.id, node.status)}
                    className="w-full h-8 text-xs flex items-center justify-center gap-1.5"
                  >
                    {isOnline ? (
                      <>
                        <PowerOff className="h-3.5 w-3.5" />
                        Simulate Offline
                      </>
                    ) : (
                      <>
                        <Power className="h-3.5 w-3.5 text-emerald-600" />
                        Restore Online
                      </>
                    )}
                  </Button>
                </div>
              </div>
            );
          })}
        </div>
      </div>

      {/* Node Fault Simulation Table */}
      <div className="space-y-3">
        <h2 className="text-base font-bold text-slate-900">Node Diagnostics Table</h2>
        <div className="rounded-xl border border-gray-200/80 bg-white shadow-xs overflow-hidden">
          {nodesLoading ? (
            <div className="p-8 text-center text-xs text-gray-400">Loading nodes...</div>
          ) : !nodes || nodes.length === 0 ? (
            <div className="p-8 text-center text-xs text-gray-400">No storage nodes registered.</div>
          ) : (
            <Table>
              <TableHeader>
                <TableRow className="border-b border-gray-100 bg-gray-50/50">
                  <TableHead>Node ID</TableHead>
                  <TableHead>Name</TableHead>
                  <TableHead>Provider</TableHead>
                  <TableHead>Total Capacity</TableHead>
                  <TableHead>Used</TableHead>
                  <TableHead>Available</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead>Last Heartbeat</TableHead>
                  <TableHead className="text-right">Failure Simulation</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {nodes.map((node) => {
                  const isOnline = node.status === "ONLINE";
                  const isToggling = togglingNodeId === node.id;

                  return (
                    <TableRow key={node.id} className="hover:bg-gray-50/60 transition-colors">
                      <TableCell className="font-mono text-xs font-semibold text-indigo-600">
                        {node.id}
                      </TableCell>
                      <TableCell className="font-medium text-slate-900 text-sm">{node.name}</TableCell>
                      <TableCell className="font-mono text-xs text-gray-500">{node.provider_type || "local"}</TableCell>
                      <TableCell className="font-mono text-xs text-slate-700">{formatBytes(node.total_capacity)}</TableCell>
                      <TableCell className="font-mono text-xs text-indigo-600">{formatBytes(node.used_capacity)}</TableCell>
                      <TableCell className="font-mono text-xs text-emerald-600">{formatBytes(node.available_capacity)}</TableCell>
                      <TableCell>
                        <Badge variant={isOnline ? "success" : node.status === "DEGRADED" ? "warning" : "destructive"}>
                          {node.status}
                        </Badge>
                      </TableCell>
                      <TableCell className="text-xs text-gray-400">
                        {node.last_health_check ? formatDate(node.last_health_check) : "Live"}
                      </TableCell>
                      <TableCell className="text-right">
                        <Button
                          variant={isOnline ? "destructive" : "outline"}
                          size="sm"
                          disabled={isToggling}
                          onClick={() => handleToggleFailure(node.id, node.status)}
                          className="h-7 px-2.5 text-xs inline-flex items-center gap-1.5"
                        >
                          {isOnline ? (
                            <>
                              <PowerOff className="h-3 w-3" />
                              Simulate Offline
                            </>
                          ) : (
                            <>
                              <Power className="h-3 w-3 text-emerald-600" />
                              Restore Online
                            </>
                          )}
                        </Button>
                      </TableCell>
                    </TableRow>
                  );
                })}
              </TableBody>
            </Table>
          )}
        </div>
      </div>
    </div>
  );
}
