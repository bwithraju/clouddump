"use client";

import { useQuery } from "@tanstack/react-query";
import { fetchHealth, HealthCheckResponse } from "@/lib/api";

export function useHealth() {
  return useQuery<HealthCheckResponse>({
    queryKey: ["health"],
    queryFn: fetchHealth,
    retry: 2,
    refetchInterval: 10000,
  });
}
