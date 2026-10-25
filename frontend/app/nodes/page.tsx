"use client";

import * as React from "react";
import Link from "next/link";
import { Server, HardDrive, CheckCircle2, AlertTriangle, RefreshCw, PowerOff, Power, ShieldAlert, Cloud, PlusCircle } from "lucide-react";
import { useQueryClient } from "@tanstack/react-query";
import { usePoolStats, useNodes } from "@/hooks/use-nodes";
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Progress } from "@/components/ui/progress";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { formatBytes, formatDate } from "@/lib/utils";
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

  React.useEffect(() => {
    fetchGoogleOAuthStatus().then((status) => {
      setGdriveConnected(status.connected);
    }).catch(() => {});
  }, []);

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
  const freeCap = stats?.available_capacity || (totalCap - usedCap);
  const usedPct = totalCap > 0 ? Math.round((usedCap / totalCap) * 100) : 0;
  const offlineNodes = stats?.offline_nodes || 0;

  return (
    <div className="space-y-8">
      {/* Page Header */}
      <div className="flex flex-col md:flex-row md:items-center justify-between gap-4 pb-2 border-b border-slate-800">
        <div>
          <h1 className="text-2xl font-bold tracking-tight text-white flex items-center gap-2">
            <Server className="h-6 w-6 text-indigo-400" />
            Storage Nodes & Fault Management
          </h1>
          <p className="text-sm text-slate-400 mt-1">
            Monitor node health, capacity distribution, and simulate outage scenarios for fault tolerance.
          </p>
        </div>
        <div className="flex items-center gap-2 self-start">
          <Button
            variant="outline"
            size="sm"
            onClick={handleReconcile}
            disabled={reconciling}
            className="flex items-center gap-1.5 text-xs text-indigo-300 border-indigo-900/60 hover:bg-indigo-950/40"
          >
            <ShieldAlert className="h-3.5 w-3.5" />
            {reconciling ? "Probing Health..." : "Probe Health & Reconcile"}
          </Button>
          <Button variant="outline" size="sm" onClick={handleRefresh} className="flex items-center gap-1.5">
            <RefreshCw className="h-3.5 w-3.5" />
            Refresh
          </Button>
        </div>
      </div>

      {/* Offline Alert Banner */}
      {offlineNodes > 0 && (
        <div className="rounded-lg border border-amber-900/70 bg-amber-950/30 p-4 flex items-start gap-3 text-amber-200">
          <AlertTriangle className="h-5 w-5 text-amber-400 shrink-0 mt-0.5" />
          <div className="space-y-1">
            <div className="text-sm font-semibold">
              {offlineNodes} storage {offlineNodes === 1 ? "node is" : "nodes are"} currently OFFLINE
            </div>
            <p className="text-xs text-amber-300/80">
              Affected file chunks are currently unreachable. Files requiring chunks from offline nodes are marked <span className="font-mono font-semibold">DEGRADED</span> to prevent data corruption. New uploads will automatically bypass offline nodes.
            </p>
          </div>
        </div>
      )}

      {/* Aggregate Pool Overview */}
      <div className="grid grid-cols-1 md:grid-cols-5 gap-4">
        <Card>
          <CardHeader className="pb-2">
            <CardDescription className="text-xs uppercase tracking-wider font-mono">Total Capacity</CardDescription>
            <CardTitle className="text-xl font-bold text-white">{formatBytes(totalCap)}</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="text-xs text-slate-400">All registered targets</div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="pb-2">
            <CardDescription className="text-xs uppercase tracking-wider font-mono">Used Capacity</CardDescription>
            <CardTitle className="text-xl font-bold text-indigo-400">{formatBytes(usedCap)}</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="text-xs text-slate-400">{usedPct}% of pool reserved</div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="pb-2">
            <CardDescription className="text-xs uppercase tracking-wider font-mono">Available Capacity</CardDescription>
            <CardTitle className="text-xl font-bold text-emerald-400">{formatBytes(freeCap)}</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="text-xs text-slate-400">Ready for incoming chunks</div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="pb-2">
            <CardDescription className="text-xs uppercase tracking-wider font-mono">Online Nodes</CardDescription>
            <CardTitle className="text-xl font-bold text-emerald-400 flex items-center gap-1.5">
              <CheckCircle2 className="h-5 w-5 text-emerald-500" />
              {stats?.online_nodes || 0} / {stats?.node_count || 0}
            </CardTitle>
          </CardHeader>
          <CardContent>
            <div className="text-xs text-slate-400">Operational targets</div>
          </CardContent>
        </Card>

        <Card className={offlineNodes > 0 ? "border-amber-900/60 bg-amber-950/20" : ""}>
          <CardHeader className="pb-2">
            <CardDescription className="text-xs uppercase tracking-wider font-mono">Offline Nodes</CardDescription>
            <CardTitle className={`text-xl font-bold flex items-center gap-1.5 ${offlineNodes > 0 ? "text-amber-400" : "text-slate-400"}`}>
              <AlertTriangle className={`h-5 w-5 ${offlineNodes > 0 ? "text-amber-500" : "text-slate-500"}`} />
              {offlineNodes}
            </CardTitle>
          </CardHeader>
          <CardContent>
            <div className="text-xs text-slate-400">
              {offlineNodes > 0 ? "Outage simulated/active" : "Zero failed nodes"}
            </div>
          </CardContent>
        </Card>
      </div>

      {/* Cloud Storage Provider Integration */}
      <Card className="border-indigo-900/50 bg-slate-900/40">
        <CardHeader className="pb-3">
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-2">
              <Cloud className="h-5 w-5 text-indigo-400" />
              <CardTitle className="text-base font-semibold">Google Drive Storage Provider</CardTitle>
            </div>
            <Badge variant={gdriveConnected ? "success" : "outline"}>
              {gdriveConnected ? "OAuth Linked" : "Not Connected"}
            </Badge>
          </div>
          <CardDescription className="text-xs">
            Connect Google Drive via OAuth 2.0 as an independent StorageProvider in your logical storage pool.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <div className="flex flex-wrap items-center gap-3">
            {!gdriveConnected ? (
              <Button
                size="sm"
                onClick={handleConnectGDrive}
                disabled={connecting}
                className="bg-indigo-600 hover:bg-indigo-500 text-xs"
              >
                {connecting ? "Opening OAuth..." : "Connect Google Drive via OAuth"}
              </Button>
            ) : (
              <>
                <Button
                  size="sm"
                  onClick={handleCreateDriveNode}
                  disabled={creatingNode}
                  className="bg-emerald-600 hover:bg-emerald-500 text-xs flex items-center gap-1.5"
                >
                  <PlusCircle className="h-3.5 w-3.5" />
                  {creatingNode ? "Adding Node..." : "Add Google Drive Node to Pool"}
                </Button>
                <Button
                  variant="outline"
                  size="sm"
                  onClick={handleDisconnectGDrive}
                  className="text-xs border-rose-900/60 text-rose-300 hover:bg-rose-950/30"
                >
                  Disconnect Google Drive
                </Button>
              </>
            )}
          </div>
        </CardContent>
      </Card>

      {/* Node Cards Grid */}
      <div>
        <h2 className="text-lg font-semibold text-white mb-4">Node Allocation Status & Fault Simulation</h2>
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-4">
          {(nodes || []).map((node) => {
            const usagePercent =
              node.total_capacity > 0
                ? Math.round((node.used_capacity / node.total_capacity) * 100)
                : 0;
            const isOnline = node.status === "ONLINE";
            const isToggling = togglingNodeId === node.id;

            return (
              <Card key={node.id} className={isOnline ? "border-slate-800" : "border-rose-900/60 bg-rose-950/10"}>
                <CardHeader className="pb-3">
                  <div className="flex items-center justify-between">
                    <span className="text-xs font-mono text-indigo-400 uppercase tracking-wider">
                      {node.id}
                    </span>
                    <Badge variant={isOnline ? "success" : node.status === "DEGRADED" ? "warning" : "destructive"}>
                      {node.status}
                    </Badge>
                  </div>
                  <CardTitle className="text-base font-semibold mt-1">{node.name}</CardTitle>
                  <CardDescription className="text-xs">
                    Provider: <span className="font-mono text-slate-300">{node.provider_type || "local"}</span>
                  </CardDescription>
                </CardHeader>
                <CardContent className="space-y-3">
                  <div>
                    <div className="flex justify-between text-xs mb-1">
                      <span className="text-slate-400">Used</span>
                      <span className="text-white font-mono font-medium">{usagePercent}%</span>
                    </div>
                    <Progress value={usagePercent} className="h-2" />
                  </div>

                  <div className="rounded-lg bg-slate-950/70 border border-slate-800/80 p-2.5 text-xs space-y-1 font-mono">
                    <div className="flex justify-between">
                      <span className="text-slate-500">Total:</span>
                      <span className="text-slate-300">{formatBytes(node.total_capacity)}</span>
                    </div>
                    <div className="flex justify-between">
                      <span className="text-slate-500">Used:</span>
                      <span className="text-indigo-400">{formatBytes(node.used_capacity)}</span>
                    </div>
                    <div className="flex justify-between">
                      <span className="text-slate-500">Free:</span>
                      <span className="text-emerald-400">{formatBytes(node.available_capacity)}</span>
                    </div>
                  </div>

                  {/* Fault Simulation Toggle */}
                  <Button
                    variant={isOnline ? "destructive" : "outline"}
                    size="sm"
                    disabled={isToggling}
                    onClick={() => handleToggleFailure(node.id, node.status)}
                    className="w-full text-xs flex items-center justify-center gap-1.5 mt-2"
                  >
                    {isOnline ? (
                      <>
                        <PowerOff className="h-3.5 w-3.5" />
                        {isToggling ? "Taking Offline..." : "Simulate Outage"}
                      </>
                    ) : (
                      <>
                        <Power className="h-3.5 w-3.5 text-emerald-400" />
                        {isToggling ? "Restoring..." : "Restore Online"}
                      </>
                    )}
                  </Button>
                </CardContent>
              </Card>
            );
          })}
        </div>
      </div>

      {/* Detailed Node Table */}
      <Card>
        <CardHeader>
          <CardTitle className="text-base font-semibold">Registered Nodes Detail</CardTitle>
          <CardDescription className="text-xs">
            Complete telemetry from the StorageManager allocation registry with live simulation controls
          </CardDescription>
        </CardHeader>
        <CardContent>
          {nodesLoading ? (
            <div className="py-8 text-center text-sm text-slate-400">Loading node metrics...</div>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Node ID</TableHead>
                  <TableHead>Name</TableHead>
                  <TableHead>Provider</TableHead>
                  <TableHead>Total Capacity</TableHead>
                  <TableHead>Used</TableHead>
                  <TableHead>Available</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead>Last Check</TableHead>
                  <TableHead className="text-right">Failure Simulation</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {(nodes || []).map((node) => {
                  const isOnline = node.status === "ONLINE";
                  const isToggling = togglingNodeId === node.id;

                  return (
                    <TableRow key={node.id}>
                      <TableCell className="font-mono text-xs font-semibold text-indigo-300">
                        {node.id}
                      </TableCell>
                      <TableCell className="font-medium text-white">{node.name}</TableCell>
                      <TableCell className="font-mono text-xs text-slate-400">{node.provider_type || "local"}</TableCell>
                      <TableCell className="font-mono text-sm">{formatBytes(node.total_capacity)}</TableCell>
                      <TableCell className="font-mono text-sm text-indigo-400">{formatBytes(node.used_capacity)}</TableCell>
                      <TableCell className="font-mono text-sm text-emerald-400">{formatBytes(node.available_capacity)}</TableCell>
                      <TableCell>
                        <Badge variant={isOnline ? "success" : node.status === "DEGRADED" ? "warning" : "destructive"}>
                          {node.status}
                        </Badge>
                      </TableCell>
                      <TableCell className="text-xs text-slate-400">
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
                              <Power className="h-3 w-3 text-emerald-400" />
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
        </CardContent>
      </Card>
    </div>
  );
}
