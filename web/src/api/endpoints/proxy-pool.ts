import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useTranslations } from 'next-intl';
import { apiClient } from '../client';
import { logger } from '@/lib/logger';

export type ProxyMode = 'direct' | 'system' | 'pool' | 'inherit';

export type ProxyConfiguration = {
    id: number;
    name: string;
    url: string;
    enabled: boolean;
    remark: string;
    reference_count: number;
    created_at: string;
    updated_at: string;
};

export type ProxyConfigurationReferenceType = 'site' | 'site_account' | 'channel' | 'managed_channel';

export type ProxyConfigurationReference = {
    type: ProxyConfigurationReferenceType;
    site_id?: number;
    site_name?: string;
    site_archived?: boolean;
    site_account_id?: number;
    site_account_name?: string;
    channel_id?: number;
    channel_name?: string;
    managed?: boolean;
};

export type ProxyTestRequest = {
    proxy_config_id?: number | null;
    proxy_url?: string;
    url?: string;
};

export type ProxyTestResult = {
    success: boolean;
    status_code: number;
    duration_ms: number;
    message: string;
};

function invalidateProxyPool(queryClient: ReturnType<typeof useQueryClient>) {
    queryClient.invalidateQueries({ queryKey: ['proxy-pool'] });
    queryClient.invalidateQueries({ queryKey: ['sites', 'list'] });
    queryClient.invalidateQueries({ queryKey: ['channels', 'list'] });
}

export function useProxyConfigurationList() {
    return useQuery({
        queryKey: ['proxy-pool', 'list'],
        queryFn: async () => apiClient.get<ProxyConfiguration[]>('/api/v1/proxy-pool/list'),
        select: (data) => data ?? [],
        refetchInterval: 30000,
    });
}

export function useProxyConfigurationReferences(id: number | null, enabled = true) {
    return useQuery({
        queryKey: ['proxy-pool', 'references', id],
        queryFn: async () => apiClient.get<ProxyConfigurationReference[]>(`/api/v1/proxy-pool/references/${id}`),
        select: (data) => data ?? [],
        enabled: enabled && typeof id === 'number' && id > 0,
    });
}

export function useCreateProxyConfiguration() {
    const queryClient = useQueryClient();
    const t = useTranslations('proxyPool');
    return useMutation({
        mutationFn: async (data: Omit<ProxyConfiguration, 'id' | 'reference_count' | 'created_at' | 'updated_at'>) =>
            apiClient.post<ProxyConfiguration>('/api/v1/proxy-pool/create', data),
        onSuccess: () => invalidateProxyPool(queryClient),
        onError: (error) => logger.error(t('createFailed'), error),
    });
}

export function useUpdateProxyConfiguration() {
    const queryClient = useQueryClient();
    const t = useTranslations('proxyPool');
    return useMutation({
        mutationFn: async (data: Partial<Pick<ProxyConfiguration, 'name' | 'url' | 'enabled' | 'remark'>> & { id: number }) =>
            apiClient.post<ProxyConfiguration>('/api/v1/proxy-pool/update', data),
        onSuccess: () => invalidateProxyPool(queryClient),
        onError: (error) => logger.error(t('updateFailed'), error),
    });
}

export function useDeleteProxyConfiguration() {
    const queryClient = useQueryClient();
    const t = useTranslations('proxyPool');
    return useMutation({
        mutationFn: async (id: number) => apiClient.delete<null>(`/api/v1/proxy-pool/delete/${id}`),
        onSuccess: () => invalidateProxyPool(queryClient),
        onError: (error) => logger.error(t('deleteFailed'), error),
    });
}

export function useTestProxyConfiguration() {
    const t = useTranslations('proxyPool');
    return useMutation({
        mutationFn: async (data: ProxyTestRequest) => apiClient.post<ProxyTestResult>('/api/v1/proxy-pool/test', data),
        onError: (error) => logger.error(t('testFailed'), error),
    });
}
