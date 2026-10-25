"use client";

import { useQuery } from "@tanstack/react-query";
import { fetchSystemActivity, fetchUploadSessions, SystemActivityResponse, SessionSummary } from "@/lib/api";

export function useSystemActivity() {
  return useQuery<SystemActivityResponse, Error>({
    queryKey: ["system-activity"],
    queryFn: fetchSystemActivity,
    refetchInterval: 5000,
  });
}

export function useUploadSessions() {
  return useQuery<SessionSummary[], Error>({
    queryKey: ["upload-sessions"],
    queryFn: fetchUploadSessions,
    refetchInterval: 3000,
  });
}
