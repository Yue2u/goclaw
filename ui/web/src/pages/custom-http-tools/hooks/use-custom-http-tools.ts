import { useCallback } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import i18next from "i18next";
import { useHttp } from "@/hooks/use-ws";
import { queryKeys } from "@/lib/query-keys";
import { toast } from "@/stores/use-toast-store";

export interface CustomHTTPToolConfig {
  url: string;
  method: string;
  headers?: Record<string, string>;
  timeout?: number;
}

export interface CustomHTTPToolData {
  id: string;
  tenant_id: string;
  agent_id?: string | null;
  name: string;
  description: string;
  parameters: Record<string, unknown>;
  config: CustomHTTPToolConfig;
  created_at: string;
}

export interface CustomHTTPToolInput {
  name: string;
  description: string;
  parameters: Record<string, unknown>;
  config: CustomHTTPToolConfig;
  agent_id?: string | null;
}

export function useCustomHTTPTools() {
  const http = useHttp();
  const queryClient = useQueryClient();

  const { data: tools = [], isLoading: loading, isFetching: fetching } = useQuery({
    queryKey: queryKeys.customHTTPTools.all,
    queryFn: async () => {
      const res = await http.get<{ tools: CustomHTTPToolData[] }>("/v1/tools/custom");
      return res.tools ?? [];
    },
    staleTime: 60_000,
  });

  const invalidate = useCallback(
    () => queryClient.invalidateQueries({ queryKey: queryKeys.customHTTPTools.all }),
    [queryClient],
  );

  const createTool = useCallback(
    async (data: CustomHTTPToolInput) => {
      try {
        const res = await http.post<CustomHTTPToolData>("/v1/tools/custom", data);
        await invalidate();
        toast.success(i18next.t("customHTTPTools:toast.created"));
        return res;
      } catch (err) {
        toast.error(i18next.t("customHTTPTools:toast.failedCreate"), err instanceof Error ? err.message : "");
        throw err;
      }
    },
    [http, invalidate],
  );

  const deleteTool = useCallback(
    async (id: string) => {
      try {
        await http.delete(`/v1/tools/custom/${id}`);
        await invalidate();
        toast.success(i18next.t("customHTTPTools:toast.deleted"));
      } catch (err) {
        toast.error(i18next.t("customHTTPTools:toast.failedDelete"), err instanceof Error ? err.message : "");
        throw err;
      }
    },
    [http, invalidate],
  );

  return {
    tools,
    loading,
    fetching,
    refresh: invalidate,
    createTool,
    deleteTool,
  };
}
