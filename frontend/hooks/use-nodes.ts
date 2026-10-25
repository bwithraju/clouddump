"use client";

import { useQuery } from "@tanstack/react-query";
import { fetchPoolStats, fetchNodes, PoolStats, NodeStats } from "@/lib/api";

export function usePoolStats() {
  return useQuery<PoolStats, Error>({
    queryKey: ["nodes-stats"],
    queryFn: fetchPoolStats,
    refetchInterval: 5000,
  });
}

export function useNodes() {
  return useQuery<NodeStats[], Error>({
    queryKey: ["nodes"],
    queryFn: fetchNodes,
    refetchInterval: 5000,
  });
}
