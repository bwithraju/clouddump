"use client";

import Link from "next/link";
import { Server, HardDrive, CheckCircle2, AlertTriangle, RefreshCw, Activity, Layers } from "lucide-react";
import { usePoolStats, useNodes } from "@/hooks/use-nodes";
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Progress } from "@/components/ui/progress";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { formatBytes, formatDate } from "@/lib/utils";

export default function StorageNodesPage() {
  const { data: stats, isLoading: statsLoading, refetch: refetchStats } = usePoolStats();
  const { data: nodes, isLoading: nodesLoading, refetch: refetchNodes } = useNodes();

  const handleRefresh = () => {
    refetchStats();
    refetchNodes();
  };

  const totalCap = stats?.total_capacity || 0;
  const usedCap = stats?.used_capacity || 0;
  const freeCap = stats?.available_capacity || (totalCap - usedCap);
  const usedPct = totalCap > 0 ? Math.round((usedCap / totalCap) * 100) : 0;

  return (
    <div className="space-y-8">
      {/* Page Header */}
      <div className="flex flex-col md:flex-row md:items-center justify-between gap-4 pb-2 border-b border-slate-800">
        <div>
          <h1 className="text-2xl font-bold tracking-tight text-white flex items-center gap-2">
            <Server className="h-6 w-6 text-indigo-400" />
            Storage Nodes
          </h1>
          <p className="text-sm text-slate-400 mt-1">
            Monitor heterogeneous storage nodes powering the logical distributed storage pool.
          </p>
        </div>
        <Button variant="outline" size="sm" onClick={handleRefresh} className="flex items-center gap-1.5 self-start">
          <RefreshCw className="h-3.5 w-3.5" />
          Refresh Nodes
        </Button>
      </div>

      {/* Aggregate Pool Overview */}
      <div className="grid grid-cols-1 md:grid-cols-4 gap-4">
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
            <CardDescription className="text-xs uppercase tracking-wider font-mono">Healthy Nodes</CardDescription>
            <CardTitle className="text-xl font-bold text-emerald-400 flex items-center gap-1.5">
              <CheckCircle2 className="h-5 w-5 text-emerald-500" />
              {stats?.online_nodes || 0} / {stats?.node_count || 0}
            </CardTitle>
          </CardHeader>
          <CardContent>
            <div className="text-xs text-slate-400">100% operational</div>
          </CardContent>
        </Card>
      </div>

      {/* Node Cards Grid */}
      <div>
        <h2 className="text-lg font-semibold text-white mb-4">Node Capacity & Allocation Status</h2>
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-4">
          {(nodes || []).map((node) => {
            const usagePercent =
              node.total_capacity > 0
                ? Math.round((node.used_capacity / node.total_capacity) * 100)
                : 0;

            return (
              <Card key={node.id} className="border-slate-800">
                <CardHeader className="pb-3">
                  <div className="flex items-center justify-between">
                    <span className="text-xs font-mono text-indigo-400 uppercase tracking-wider">
                      {node.id}
                    </span>
                    <Badge variant={node.status === "ONLINE" ? "success" : "destructive"}>
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
            Complete telemetry from the StorageManager allocation registry
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
                </TableRow>
              </TableHeader>
              <TableBody>
                {(nodes || []).map((node) => (
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
                      <Badge variant={node.status === "ONLINE" ? "success" : "destructive"}>
                        {node.status}
                      </Badge>
                    </TableCell>
                    <TableCell className="text-xs text-slate-400">
                      {node.last_health_check ? formatDate(node.last_health_check) : "Live"}
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
