import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { apiClient, API_BASE_URL } from "../client";
import { logger } from "@/lib/logger";
import { useAuthStore } from "./user";

export enum SitePlatform {
  NewAPI = "new-api",
  AnyRouter = "anyrouter",
  OneAPI = "one-api",
  OneHub = "one-hub",
  DoneHub = "done-hub",
  Sub2API = "sub2api",
  OpenAI = "openai",
  Claude = "claude",
  Gemini = "gemini",
}

export enum SiteOutboundFormatMode {
  Auto = "auto",
  OpenAIOnly = "openai_only",
}

export type SiteOutboundFormatModeValue = SiteOutboundFormatMode | "";

export enum SiteCredentialType {
  UsernamePassword = "username_password",
  AccessToken = "access_token",
  APIKey = "api_key",
}

export type CustomHeader = {
  header_key: string;
  header_value: string;
};

export type SiteToken = {
  id: number;
  site_account_id: number;
  name: string;
  token: string;
  group_key: string;
  group_name: string;
  enabled: boolean;
  source: string;
  is_default: boolean;
  last_sync_at?: string | null;
};

export type SiteUserGroup = {
  id: number;
  site_account_id: number;
  group_key: string;
  name: string;
  raw_payload?: string | null;
};

export type SiteModel = {
  id: number;
  site_account_id: number;
  model_name: string;
  source: string;
};

export type SiteChannelBinding = {
  id: number;
  site_id: number;
  site_account_id: number;
  site_user_group_id?: number | null;
  group_key: string;
  channel_id: number;
};

export type SiteAccount = {
  id: number;
  site_id: number;
  name: string;
  credential_type: SiteCredentialType;
  username: string;
  password: string;
  access_token: string;
  api_key: string;
  platform_user_id?: number | null;
  account_proxy?: string | null;
  enabled: boolean;
  auto_sync: boolean;
  auto_checkin: boolean;
  random_checkin: boolean;
  checkin_interval_hours: number;
  checkin_random_window_minutes: number;
  next_auto_checkin_at?: string | null;
  last_sync_at?: string | null;
  last_checkin_at?: string | null;
  last_sync_status: string;
  last_checkin_status: string;
  last_sync_message: string;
  last_checkin_message: string;
  balance: number;
  balance_used: number;
  tokens: SiteToken[];
  user_groups: SiteUserGroup[];
  models: SiteModel[];
  channel_bindings: SiteChannelBinding[];
};

export type Site = {
  id: number;
  name: string;
  platform: SitePlatform;
  base_url: string;
  enabled: boolean;
  proxy: boolean;
  site_proxy?: string | null;
  use_system_proxy: boolean;
  external_checkin_url?: string | null;
  is_pinned: boolean;
  sort_order: number;
  global_weight: number;
  outbound_format_mode: SiteOutboundFormatModeValue;
  custom_header: CustomHeader[];
  accounts: SiteAccount[];
};

type SiteServer = Omit<Site, "accounts" | "custom_header"> & {
  accounts: Array<
    Omit<
      SiteAccount,
      "tokens" | "user_groups" | "models" | "channel_bindings"
    > & {
      tokens: SiteToken[] | null;
      user_groups: SiteUserGroup[] | null;
      models: SiteModel[] | null;
      channel_bindings: SiteChannelBinding[] | null;
    }
  > | null;
  custom_header: CustomHeader[] | null;
};

export type SiteSyncResult = {
  account_id: number;
  site_id: number;
  channel_count: number;
  group_count: number;
  token_count: number;
  model_count: number;
  managed_channels: number[];
  models: string[];
  message: string;
};

export type SiteCheckinResult = {
  account_id: number;
  site_id: number;
  status: string;
  message: string;
  reward?: string;
};

export type AllAPIHubImportResult = {
  created_sites: number;
  reused_sites: number;
  created_accounts: number;
  updated_accounts: number;
  skipped_accounts: number;
  scheduled_sync_accounts: number;
  warnings: string[];
};

export function useSiteList() {
  return useQuery({
    queryKey: ["sites", "list"],
    queryFn: async () => apiClient.get<SiteServer[]>("/api/v1/site/list"),
    select: (data) =>
      data.map((site) => ({
        ...site,
        custom_header: site.custom_header ?? [],
        use_system_proxy: site.use_system_proxy ?? false,
        external_checkin_url: site.external_checkin_url ?? null,
        is_pinned: site.is_pinned ?? false,
        sort_order: typeof site.sort_order === "number" ? site.sort_order : 0,
        global_weight:
          typeof site.global_weight === "number" && site.global_weight > 0
            ? site.global_weight
            : 1,
        outbound_format_mode: site.outbound_format_mode ?? "",
        accounts: (site.accounts ?? []).map((account) => ({
          ...account,
          platform_user_id: account.platform_user_id ?? null,
          account_proxy: account.account_proxy ?? null,
          random_checkin: account.random_checkin ?? false,
          checkin_interval_hours:
            typeof account.checkin_interval_hours === "number" &&
            account.checkin_interval_hours > 0
              ? account.checkin_interval_hours
              : 24,
          checkin_random_window_minutes:
            typeof account.checkin_random_window_minutes === "number" &&
            account.checkin_random_window_minutes >= 0
              ? account.checkin_random_window_minutes
              : 120,
          balance: typeof account.balance === "number" ? account.balance : 0,
          balance_used:
            typeof account.balance_used === "number" ? account.balance_used : 0,
          tokens: account.tokens ?? [],
          user_groups: account.user_groups ?? [],
          models: account.models ?? [],
          channel_bindings: account.channel_bindings ?? [],
        })),
      })) as Site[],
    refetchInterval: 30000,
    refetchOnMount: "always",
  });
}

function invalidateSiteQueries(queryClient: ReturnType<typeof useQueryClient>) {
  queryClient.invalidateQueries({ queryKey: ["sites", "list"] });
  queryClient.invalidateQueries({ queryKey: ["site-channel", "list"] });
  queryClient.invalidateQueries({ queryKey: ["channels", "list"] });
  queryClient.invalidateQueries({ queryKey: ["models", "channel"] });
}

function getAuthHeader() {
  const token = useAuthStore.getState().token;
  if (!token) throw new Error("Not authenticated");
  return `Bearer ${token}`;
}

function extractResponseMessage(payload: unknown, fallback: string) {
  if (
    payload &&
    typeof payload === "object" &&
    "message" in payload &&
    typeof payload.message === "string"
  ) {
    return payload.message;
  }
  return fallback;
}

function extractResponseData<T>(payload: unknown): T | undefined {
  if (payload && typeof payload === "object" && "data" in payload) {
    return (payload as { data?: T }).data;
  }
  return undefined;
}

export function useCreateSite() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (data: Omit<Site, "id" | "accounts">) =>
      apiClient.post<Site>("/api/v1/site/create", data),
    onSuccess: () => invalidateSiteQueries(queryClient),
    onError: (error) => logger.error("站点创建失败:", error),
  });
}

export function useUpdateSite() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (
      data: Partial<Omit<Site, "accounts">> & { id: number },
    ) => apiClient.post<Site>("/api/v1/site/update", data),
    onSuccess: () => invalidateSiteQueries(queryClient),
    onError: (error) => logger.error("站点更新失败:", error),
  });
}

export function useEnableSite() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (data: { id: number; enabled: boolean }) =>
      apiClient.post<null>("/api/v1/site/enable", data),
    onSuccess: () => invalidateSiteQueries(queryClient),
    onError: (error) => logger.error("站点状态更新失败:", error),
  });
}

export function useDeleteSite() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (id: number) =>
      apiClient.delete<null>(`/api/v1/site/delete/${id}`),
    onSuccess: () => invalidateSiteQueries(queryClient),
    onError: (error) => logger.error("站点删除失败:", error),
  });
}

export function useCreateSiteAccount() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (
      data: Omit<
        SiteAccount,
        | "id"
        | "tokens"
        | "user_groups"
        | "models"
        | "channel_bindings"
        | "last_sync_at"
        | "last_checkin_at"
        | "last_sync_status"
        | "last_checkin_status"
        | "last_sync_message"
        | "last_checkin_message"
        | "balance"
        | "balance_used"
      >,
    ) => apiClient.post<SiteAccount>("/api/v1/site/account/create", data),
    onSuccess: () => invalidateSiteQueries(queryClient),
    onError: (error) => logger.error("站点账号创建失败:", error),
  });
}

export function useUpdateSiteAccount() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (
      data: Partial<
        Omit<
          SiteAccount,
          "tokens" | "user_groups" | "models" | "channel_bindings"
        >
      > & { id: number },
    ) => apiClient.post<SiteAccount>("/api/v1/site/account/update", data),
    onSuccess: () => invalidateSiteQueries(queryClient),
    onError: (error) => logger.error("站点账号更新失败:", error),
  });
}

export function useEnableSiteAccount() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (data: { id: number; enabled: boolean }) =>
      apiClient.post<null>("/api/v1/site/account/enable", data),
    onSuccess: () => invalidateSiteQueries(queryClient),
    onError: (error) => logger.error("站点账号状态更新失败:", error),
  });
}

export function useDeleteSiteAccount() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (id: number) =>
      apiClient.delete<null>(`/api/v1/site/account/delete/${id}`),
    onSuccess: () => invalidateSiteQueries(queryClient),
    onError: (error) => logger.error("站点账号删除失败:", error),
  });
}

export function useSyncSiteAccount() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (id: number) =>
      apiClient.post<SiteSyncResult>(`/api/v1/site/account/sync/${id}`, {}),
    onSuccess: () => invalidateSiteQueries(queryClient),
    onError: (error) => logger.error("站点账号同步失败:", error),
  });
}

export function useCheckinSiteAccount() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (id: number) =>
      apiClient.post<SiteCheckinResult>(
        `/api/v1/site/account/checkin/${id}`,
        {},
      ),
    onSuccess: () => invalidateSiteQueries(queryClient),
    onError: (error) => logger.error("站点账号签到失败:", error),
  });
}

export function useSyncAllSites() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async () => apiClient.post<null>("/api/v1/site/sync-all", {}),
    onSuccess: () => invalidateSiteQueries(queryClient),
    onError: (error) => logger.error("站点批量同步失败:", error),
  });
}

export function useCheckinAllSites() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async () =>
      apiClient.post<null>("/api/v1/site/checkin-all", {}),
    onSuccess: () => invalidateSiteQueries(queryClient),
    onError: (error) => logger.error("站点批量签到失败:", error),
  });
}

export function useImportAllAPIHub() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (payload: { file?: File | null; text?: string }) => {
      const hasFile = !!payload.file;
      const hasText = !!payload.text?.trim();
      if (!hasFile && !hasText) {
        throw new Error("请选择 JSON 文件或粘贴导出内容");
      }

      const headers: HeadersInit = {
        Authorization: getAuthHeader(),
      };
      let body: BodyInit;

      if (payload.file) {
        const form = new FormData();
        form.append("file", payload.file);
        body = form;
      } else {
        headers["Content-Type"] = "application/json";
        body = payload.text!.trim();
      }

      const response = await fetch(
        `${API_BASE_URL}/api/v1/site/import/all-api-hub`,
        {
          method: "POST",
          headers,
          body,
        },
      );
      const contentType = response.headers.get("content-type") || "";
      const data = contentType.includes("application/json")
        ? await response.json()
        : await response.text();

      if (!response.ok) {
        throw new Error(
          extractResponseMessage(
            data,
            typeof data === "string" ? data : response.statusText,
          ),
        );
      }

      const result =
        extractResponseData<AllAPIHubImportResult>(data) ??
        (data as AllAPIHubImportResult);
      return {
        ...result,
        warnings: result.warnings ?? [],
      };
    },
    onSuccess: () => invalidateSiteQueries(queryClient),
    onError: (error) => logger.error("导入 All API Hub 账号失败:", error),
  });
}

export function useDetectSitePlatform() {
  return useMutation({
    mutationFn: async (url: string) =>
      apiClient.post<{ platform: string }>("/api/v1/site/detect", { url }),
    onError: (error) => logger.error("平台检测失败:", error),
  });
}

export function useSiteBatchAction() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (data: { ids: number[]; action: string }) =>
      apiClient.post<{
        success_ids: number[];
        failed_items: Array<{ id: number; message: string }>;
      }>("/api/v1/site/batch", data),
    onSuccess: () => invalidateSiteQueries(queryClient),
    onError: (error) => logger.error("批量操作失败:", error),
  });
}

export function useSiteAvailableModels(siteId: number | null) {
  return useQuery({
    queryKey: ["sites", "available-models", siteId],
    queryFn: async () =>
      apiClient.get<{ site_id: number; models: string[] }>(
        `/api/v1/site/${siteId}/available-models`,
      ),
    enabled: siteId != null && siteId > 0,
  });
}
