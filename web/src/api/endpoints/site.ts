import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { apiClient } from '../client';
import { logger } from '@/lib/logger';

export enum SitePlatform {
    NewAPI = 'new-api',
    OneAPI = 'one-api',
    OneHub = 'one-hub',
    DoneHub = 'done-hub',
    Sub2API = 'sub2api',
    OpenAI = 'openai',
    Claude = 'claude',
    Gemini = 'gemini',
}

export enum SiteCredentialType {
    UsernamePassword = 'username_password',
    AccessToken = 'access_token',
    APIKey = 'api_key',
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
    enabled: boolean;
    auto_sync: boolean;
    auto_checkin: boolean;
    last_sync_at?: string | null;
    last_checkin_at?: string | null;
    last_sync_status: string;
    last_checkin_status: string;
    last_sync_message: string;
    last_checkin_message: string;
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
    custom_header: CustomHeader[];
    accounts: SiteAccount[];
};

type SiteServer = Omit<Site, 'accounts' | 'custom_header'> & {
    accounts: Array<Omit<SiteAccount, 'tokens' | 'user_groups' | 'models' | 'channel_bindings'> & {
        tokens: SiteToken[] | null;
        user_groups: SiteUserGroup[] | null;
        models: SiteModel[] | null;
        channel_bindings: SiteChannelBinding[] | null;
    }> | null;
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

export function useSiteList() {
    return useQuery({
        queryKey: ['sites', 'list'],
        queryFn: async () => apiClient.get<SiteServer[]>('/api/v1/site/list'),
        select: (data) => data.map((site) => ({
            ...site,
            custom_header: site.custom_header ?? [],
            accounts: (site.accounts ?? []).map((account) => ({
                ...account,
                tokens: account.tokens ?? [],
                user_groups: account.user_groups ?? [],
                models: account.models ?? [],
                channel_bindings: account.channel_bindings ?? [],
            })),
        })) as Site[],
        refetchInterval: 30000,
        refetchOnMount: 'always',
    });
}

function invalidateSiteQueries(queryClient: ReturnType<typeof useQueryClient>) {
    queryClient.invalidateQueries({ queryKey: ['sites', 'list'] });
    queryClient.invalidateQueries({ queryKey: ['channels', 'list'] });
    queryClient.invalidateQueries({ queryKey: ['models', 'channel'] });
}

export function useCreateSite() {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: async (data: Omit<Site, 'id' | 'accounts'>) => apiClient.post<Site>('/api/v1/site/create', data),
        onSuccess: () => invalidateSiteQueries(queryClient),
        onError: (error) => logger.error('站点创建失败:', error),
    });
}

export function useUpdateSite() {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: async (data: Partial<Omit<Site, 'accounts'>> & { id: number }) => apiClient.post<Site>('/api/v1/site/update', data),
        onSuccess: () => invalidateSiteQueries(queryClient),
        onError: (error) => logger.error('站点更新失败:', error),
    });
}

export function useEnableSite() {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: async (data: { id: number; enabled: boolean }) => apiClient.post<null>('/api/v1/site/enable', data),
        onSuccess: () => invalidateSiteQueries(queryClient),
        onError: (error) => logger.error('站点状态更新失败:', error),
    });
}

export function useDeleteSite() {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: async (id: number) => apiClient.delete<null>(`/api/v1/site/delete/${id}`),
        onSuccess: () => invalidateSiteQueries(queryClient),
        onError: (error) => logger.error('站点删除失败:', error),
    });
}

export function useCreateSiteAccount() {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: async (data: Omit<SiteAccount, 'id' | 'tokens' | 'user_groups' | 'models' | 'channel_bindings' | 'last_sync_at' | 'last_checkin_at' | 'last_sync_status' | 'last_checkin_status' | 'last_sync_message' | 'last_checkin_message'>) => apiClient.post<SiteAccount>('/api/v1/site/account/create', data),
        onSuccess: () => invalidateSiteQueries(queryClient),
        onError: (error) => logger.error('站点账号创建失败:', error),
    });
}

export function useUpdateSiteAccount() {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: async (data: Partial<Omit<SiteAccount, 'tokens' | 'user_groups' | 'models' | 'channel_bindings'>> & { id: number }) => apiClient.post<SiteAccount>('/api/v1/site/account/update', data),
        onSuccess: () => invalidateSiteQueries(queryClient),
        onError: (error) => logger.error('站点账号更新失败:', error),
    });
}

export function useEnableSiteAccount() {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: async (data: { id: number; enabled: boolean }) => apiClient.post<null>('/api/v1/site/account/enable', data),
        onSuccess: () => invalidateSiteQueries(queryClient),
        onError: (error) => logger.error('站点账号状态更新失败:', error),
    });
}

export function useDeleteSiteAccount() {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: async (id: number) => apiClient.delete<null>(`/api/v1/site/account/delete/${id}`),
        onSuccess: () => invalidateSiteQueries(queryClient),
        onError: (error) => logger.error('站点账号删除失败:', error),
    });
}

export function useSyncSiteAccount() {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: async (id: number) => apiClient.post<SiteSyncResult>(`/api/v1/site/account/sync/${id}`, {}),
        onSuccess: () => invalidateSiteQueries(queryClient),
        onError: (error) => logger.error('站点账号同步失败:', error),
    });
}

export function useCheckinSiteAccount() {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: async (id: number) => apiClient.post<SiteCheckinResult>(`/api/v1/site/account/checkin/${id}`, {}),
        onSuccess: () => invalidateSiteQueries(queryClient),
        onError: (error) => logger.error('站点账号签到失败:', error),
    });
}

export function useSyncAllSites() {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: async () => apiClient.post<null>('/api/v1/site/sync-all', {}),
        onSuccess: () => invalidateSiteQueries(queryClient),
        onError: (error) => logger.error('站点批量同步失败:', error),
    });
}

export function useCheckinAllSites() {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: async () => apiClient.post<null>('/api/v1/site/checkin-all', {}),
        onSuccess: () => invalidateSiteQueries(queryClient),
        onError: (error) => logger.error('站点批量签到失败:', error),
    });
}
