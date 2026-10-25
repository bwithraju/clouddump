"use client";

import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { fetchFiles, fetchFileDetails, deleteFile, FileRecord, FileDetailsResponse } from "@/lib/api";

export function useFiles() {
  return useQuery<FileRecord[], Error>({
    queryKey: ["files"],
    queryFn: fetchFiles,
    refetchInterval: 5000,
  });
}

export function useFileDetails(id: string) {
  return useQuery<FileDetailsResponse, Error>({
    queryKey: ["file", id],
    queryFn: () => fetchFileDetails(id),
    enabled: Boolean(id),
    refetchInterval: 5000,
  });
}

export function useDeleteFile() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (id: string) => deleteFile(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["files"] });
      queryClient.invalidateQueries({ queryKey: ["nodes-stats"] });
      queryClient.invalidateQueries({ queryKey: ["nodes"] });
    },
  });
}
