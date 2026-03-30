import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { apiClient } from '../client';
import { logger } from '@/lib/logger';
import type { SitePlatform } from './site';

export type SiteModelRouteType =
    | 'openai_chat'
    | 'openai_response'
    | 'anthropic'
    | 'gemini'
    | 'volcengine'
    | 'openai_embedding';

export type SiteModelRouteSource =
    | 'sync_inferred'
    | 'manual_override'
    | 'runtime_learned'
    | 'default_assigned';

export type SiteRouteSummary = {
    route_type: SiteModelRouteType;
    count: number;
};

export type SiteModelHistoryEntry = {
    time: number;
    success: boolean;
    route_type: SiteModelRouteType;
    channel_id: number;
    channel_name: string;
    request_model: string;
    actual_model: string;
    error?: string;
};

export type SiteModelHistorySummary = {
    success_count: number;
    failure_count: number;
    last_request_at?: number | null;
    recent: SiteModelHistoryEntry[];
};

export type SiteChannelModel = {
    model_name: string;
    route_type: SiteModelRouteType;
    route_source: SiteModelRouteSource;
    manual_override: boolean;
    disabled: boolean;
    projected_channel_id?: number | null;
    history?: SiteModelHistorySummary | null;
};

export type SiteChannelGroup = {
    group_key: string;
    group_name: string;
    key_count: number;
    enabled_key_count: number;
    has_keys: boolean;
    has_projected_channel: boolean;
    projected_channel_ids: number[];
    projected_keys: SiteProjectedKey[];
    models: SiteChannelModel[];
};

export type SiteProjectedKey = {
    id: number;
    channel_id: number;
    channel_name: string;
    enabled: boolean;
    channel_key_masked: string;
    remark: string;
    status_code: number;
    last_use_time_stamp: number;
    total_cost: number;
};

export type SiteChannelAccount = {
    site_id: number;
    account_id: number;
    account_name: string;
    enabled: boolean;
    auto_sync: boolean;
    group_count: number;
    model_count: number;
    groups: SiteChannelGroup[];
    route_summaries: SiteRouteSummary[];
};

export type SiteChannelCard = {
    site_id: number;
    site_name: string;
    platform: SitePlatform;
    enabled: boolean;
    account_count: number;
    accounts: SiteChannelAccount[];
};

export type SiteModelRouteUpdateRequest = {
    group_key: string;
    model_name: string;
    route_type: SiteModelRouteType;
    route_raw_payload?: string;
};

export type SiteModelDisableUpdateRequest = {
    group_key: string;
    model_name: string;
    disabled: boolean;
};

export type SiteChannelKeyCreateRequest = {
    group_key: string;
    name?: string;
};

export type SiteProjectedKeyAddRequest = {
    enabled: boolean;
    channel_key: string;
    remark?: string;
};

export type SiteProjectedKeyUpdateItem = {
    id: number;
    enabled?: boolean;
    channel_key?: string;
    remark?: string;
};

export type SiteProjectedKeyUpdateRequest = {
    group_key: string;
    keys_to_add?: SiteProjectedKeyAddRequest[];
    keys_to_update?: SiteProjectedKeyUpdateItem[];
    keys_to_delete?: number[];
};

function getAccountPath(siteId: number, accountId: number, suffix: string) {
    return '/api/v1/site-channel/' + siteId + '/account/' + accountId + suffix;
}

function replaceSiteChannelAccount(
    cards: SiteChannelCard[] | undefined,
    siteId: number,
    account: SiteChannelAccount,
) {
    if (!cards) return cards;

    return cards.map((card) => {
        if (card.site_id !== siteId) return card;

        return {
            ...card,
            accounts: card.accounts.map((item) =>
                item.account_id === account.account_id ? account : item,
            ),
        };
    });
}

function invalidateSiteChannelQueries(queryClient: ReturnType<typeof useQueryClient>) {
    queryClient.invalidateQueries({ queryKey: ['site-channel', 'list'] });
    queryClient.invalidateQueries({ queryKey: ['sites', 'list'] });
    queryClient.invalidateQueries({ queryKey: ['channels', 'list'] });
    queryClient.invalidateQueries({ queryKey: ['models', 'channel'] });
}

export function useSiteChannelList() {
    return useQuery({
        queryKey: ['site-channel', 'list'],
        queryFn: async () => apiClient.get<SiteChannelCard[]>('/api/v1/site-channel/list'),
        refetchInterval: 30000,
        refetchOnMount: 'always',
    });
}

export function useUpdateSiteChannelModelRoutes(siteId: number, accountId: number) {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: async (payload: SiteModelRouteUpdateRequest[]) =>
            apiClient.put<SiteChannelAccount>(getAccountPath(siteId, accountId, '/model-routes'), payload),
        onSuccess: (account) => {
            queryClient.setQueryData<SiteChannelCard[]>(['site-channel', 'list'], (cards) =>
                replaceSiteChannelAccount(cards, siteId, account),
            );
            invalidateSiteChannelQueries(queryClient);
        },
        onError: (error) => {
            logger.error('site channel model route update failed:', error);
        },
    });
}

export function useCreateSiteChannelKey(siteId: number, accountId: number) {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: async (payload: SiteChannelKeyCreateRequest) =>
            apiClient.post<SiteChannelAccount>(getAccountPath(siteId, accountId, '/keys'), payload),
        onSuccess: (account) => {
            queryClient.setQueryData<SiteChannelCard[]>(['site-channel', 'list'], (cards) =>
                replaceSiteChannelAccount(cards, siteId, account),
            );
            invalidateSiteChannelQueries(queryClient);
        },
        onError: (error) => {
            logger.error('site channel key create failed:', error);
        },
    });
}

export function useUpdateSiteChannelModelDisabled(siteId: number, accountId: number) {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: async (payload: SiteModelDisableUpdateRequest[]) =>
            apiClient.put<SiteChannelAccount>(getAccountPath(siteId, accountId, '/model-disabled'), payload),
        onSuccess: (account) => {
            queryClient.setQueryData<SiteChannelCard[]>(['site-channel', 'list'], (cards) =>
                replaceSiteChannelAccount(cards, siteId, account),
            );
            invalidateSiteChannelQueries(queryClient);
        },
        onError: (error) => {
            logger.error('site channel model disabled update failed:', error);
        },
    });
}

export function useUpdateSiteProjectedKeys(siteId: number, accountId: number) {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: async (payload: SiteProjectedKeyUpdateRequest) =>
            apiClient.put<SiteChannelAccount>(getAccountPath(siteId, accountId, '/projected-keys'), payload),
        onSuccess: (account) => {
            queryClient.setQueryData<SiteChannelCard[]>(['site-channel', 'list'], (cards) =>
                replaceSiteChannelAccount(cards, siteId, account),
            );
            invalidateSiteChannelQueries(queryClient);
        },
        onError: (error) => {
            logger.error('site projected key update failed:', error);
        },
    });
}

export function useResetSiteChannelModelRoutes(siteId: number, accountId: number) {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: async () =>
            apiClient.post<SiteChannelAccount>(getAccountPath(siteId, accountId, '/model-routes/reset'), {}),
        onSuccess: (account) => {
            queryClient.setQueryData<SiteChannelCard[]>(['site-channel', 'list'], (cards) =>
                replaceSiteChannelAccount(cards, siteId, account),
            );
            invalidateSiteChannelQueries(queryClient);
        },
        onError: (error) => {
            logger.error('site channel model route reset failed:', error);
        },
    });
}
