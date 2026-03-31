import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { apiClient } from '../client';
import { logger } from '@/lib/logger';
import type { SitePlatform } from './site';

export type SiteModelRouteType =
    | 'unknown'
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

export type SiteModelRouteMetadata = {
    kind?: string;
    version?: number;
    source?: string;
    route_supported: boolean;
    route_type?: SiteModelRouteType;
    enable_groups?: string[];
    supported_endpoint_types?: string[];
    heuristic_endpoint_types?: string[];
    normalized_endpoint_types?: string[];
    unsupported_reason?: string;
};

export type SiteChannelModel = {
    model_name: string;
    route_type: SiteModelRouteType;
    route_source: SiteModelRouteSource;
    manual_override: boolean;
    disabled: boolean;
    projected_channel_id?: number | null;
    route_metadata?: SiteModelRouteMetadata | null;
    history?: SiteModelHistorySummary | null;
};

export type SiteChannelGroup = {
    group_key: string;
    group_name: string;
    key_count: number;
    enabled_key_count: number;
    masked_pending_key_count: number;
    has_keys: boolean;
    has_projected_channel: boolean;
    projected_channel_ids: number[];
    source_keys: SiteSourceKey[];
    projected_keys: SiteProjectedKey[];
    models: SiteChannelModel[];
};

export type SiteSourceKey = {
    id: number;
    enabled: boolean;
    token: string;
    token_masked: string;
    name: string;
    group_key: string;
    group_name: string;
    value_status: 'ready' | 'masked_pending';
    last_sync_at?: number | null;
};

export type SiteProjectedKey = {
    id: number;
    channel_id: number;
    channel_name: string;
    enabled: boolean;
    channel_key: string;
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
    base_url: string;
    platform: SitePlatform;
    enabled: boolean;
    account_count: number;
    accounts: SiteChannelAccount[];
};

type SiteChannelModelServer = Omit<SiteChannelModel, 'route_type' | 'route_metadata' | 'history'> & {
    route_type: string | null;
    route_metadata?: SiteModelRouteMetadata | null;
    history?: {
        success_count?: number | null;
        failure_count?: number | null;
        last_request_at?: number | null;
        recent?: Array<{
            time?: number | null;
            success?: boolean | null;
            route_type?: string | null;
            channel_id?: number | null;
            channel_name?: string | null;
            request_model?: string | null;
            actual_model?: string | null;
            error?: string | null;
        }> | null;
    } | null;
};

type SiteChannelGroupServer = Omit<SiteChannelGroup, 'models' | 'projected_channel_ids' | 'source_keys' | 'projected_keys'> & {
    projected_channel_ids?: number[] | null;
    source_keys?: SiteSourceKey[] | null;
    projected_keys?: SiteProjectedKey[] | null;
    models?: SiteChannelModelServer[] | null;
};

type SiteChannelAccountServer = Omit<SiteChannelAccount, 'groups' | 'route_summaries'> & {
    groups?: SiteChannelGroupServer[] | null;
    route_summaries?: Array<{
        route_type?: string | null;
        count?: number | null;
    }> | null;
};

type SiteChannelCardServer = Omit<SiteChannelCard, 'accounts'> & {
    accounts?: SiteChannelAccountServer[] | null;
};

const SITE_MODEL_ROUTE_TYPES = new Set<SiteModelRouteType>([
    'unknown',
    'openai_chat',
    'openai_response',
    'anthropic',
    'gemini',
    'volcengine',
    'openai_embedding',
]);

function normalizeSiteModelRouteType(value: string | null | undefined): SiteModelRouteType {
    if (value && SITE_MODEL_ROUTE_TYPES.has(value as SiteModelRouteType)) {
        return value as SiteModelRouteType;
    }
    return 'unknown';
}

function normalizeSiteModelRouteMetadata(
    metadata: SiteModelRouteMetadata | null | undefined,
): SiteModelRouteMetadata | null {
    if (!metadata) return null;

    return {
        ...metadata,
        route_type: metadata.route_supported
            ? normalizeSiteModelRouteType(metadata.route_type)
            : 'unknown',
        enable_groups: metadata.enable_groups ?? [],
        supported_endpoint_types: metadata.supported_endpoint_types ?? [],
        heuristic_endpoint_types: metadata.heuristic_endpoint_types ?? [],
        normalized_endpoint_types: metadata.normalized_endpoint_types ?? [],
        unsupported_reason: metadata.unsupported_reason ?? undefined,
    };
}

function normalizeSiteModel(model: SiteChannelModelServer): SiteChannelModel {
    return {
        ...model,
        route_type: normalizeSiteModelRouteType(model.route_type),
        projected_channel_id: model.projected_channel_id ?? null,
        route_metadata: normalizeSiteModelRouteMetadata(model.route_metadata),
        history: model.history
            ? {
                success_count:
                    typeof model.history.success_count === 'number'
                        ? model.history.success_count
                        : 0,
                failure_count:
                    typeof model.history.failure_count === 'number'
                        ? model.history.failure_count
                        : 0,
                last_request_at:
                    typeof model.history.last_request_at === 'number'
                        ? model.history.last_request_at
                        : null,
                recent: (model.history.recent ?? []).map((entry) => ({
                    time: typeof entry.time === 'number' ? entry.time : 0,
                    success: Boolean(entry.success),
                    route_type: normalizeSiteModelRouteType(entry.route_type),
                    channel_id: typeof entry.channel_id === 'number' ? entry.channel_id : 0,
                    channel_name: typeof entry.channel_name === 'string' ? entry.channel_name : '',
                    request_model: typeof entry.request_model === 'string' ? entry.request_model : '',
                    actual_model: typeof entry.actual_model === 'string' ? entry.actual_model : '',
                    error: typeof entry.error === 'string' ? entry.error : undefined,
                })),
            }
            : null,
    };
}

function normalizeSiteChannelAccount(account: SiteChannelAccountServer): SiteChannelAccount {
    return {
        ...account,
        groups: (account.groups ?? []).map((group) => ({
            ...group,
            masked_pending_key_count: typeof group.masked_pending_key_count === 'number' ? group.masked_pending_key_count : 0,
            projected_channel_ids: group.projected_channel_ids ?? [],
            source_keys: (group.source_keys ?? []).map((key) => ({
                ...key,
                token: typeof key.token === 'string' ? key.token : '',
                token_masked: typeof key.token_masked === 'string' ? key.token_masked : '',
                name: typeof key.name === 'string' ? key.name : '',
                group_key: typeof key.group_key === 'string' ? key.group_key : group.group_key,
                group_name: typeof key.group_name === 'string' ? key.group_name : group.group_name,
                value_status: key.value_status === 'masked_pending' ? 'masked_pending' : 'ready',
                last_sync_at: typeof key.last_sync_at === 'number' ? key.last_sync_at : null,
            })),
            projected_keys: (group.projected_keys ?? []).map((key) => ({
                ...key,
                channel_key: typeof key.channel_key === 'string' ? key.channel_key : '',
                channel_key_masked: typeof key.channel_key_masked === 'string' ? key.channel_key_masked : '',
                remark: typeof key.remark === 'string' ? key.remark : '',
                status_code: typeof key.status_code === 'number' ? key.status_code : 0,
                last_use_time_stamp: typeof key.last_use_time_stamp === 'number' ? key.last_use_time_stamp : 0,
                total_cost: typeof key.total_cost === 'number' ? key.total_cost : 0,
            })),
            models: (group.models ?? []).map(normalizeSiteModel),
        })),
        route_summaries: (account.route_summaries ?? []).map((summary) => ({
            route_type: normalizeSiteModelRouteType(summary.route_type),
            count: typeof summary.count === 'number' ? summary.count : 0,
        })),
    };
}

function normalizeSiteChannelCard(card: SiteChannelCardServer): SiteChannelCard {
    return {
        ...card,
        accounts: (card.accounts ?? []).map(normalizeSiteChannelAccount),
    };
}

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

export type SiteChannelModelDisabledMutationInput = {
    siteId: number;
    accountId: number;
    payload: SiteModelDisableUpdateRequest[];
};

export type SiteChannelKeyCreateRequest = {
    group_key: string;
    name?: string;
};

export type SiteSourceKeyAddRequest = {
    enabled: boolean;
    token: string;
    name?: string;
};

export type SiteSourceKeyUpdateItem = {
    id: number;
    enabled?: boolean;
    token?: string;
    name?: string;
};

export type SiteSourceKeyUpdateRequest = {
    group_key: string;
    keys_to_add?: SiteSourceKeyAddRequest[];
    keys_to_update?: SiteSourceKeyUpdateItem[];
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
}

function invalidateSiteChannelAndRelated(queryClient: ReturnType<typeof useQueryClient>) {
    queryClient.invalidateQueries({ queryKey: ['site-channel', 'list'] });
    queryClient.invalidateQueries({ queryKey: ['channels', 'list'] });
    queryClient.invalidateQueries({ queryKey: ['models', 'channel'] });
}

export function useSiteChannelList() {
    return useQuery({
        queryKey: ['site-channel', 'list'],
        queryFn: async () => apiClient.get<SiteChannelCardServer[]>('/api/v1/site-channel/list'),
        select: (cards) => cards.map(normalizeSiteChannelCard),
        refetchInterval: 30000,
    });
}

export function useUpdateSiteChannelModelRoutes(siteId: number, accountId: number) {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: async (payload: SiteModelRouteUpdateRequest[]) =>
            apiClient.put<SiteChannelAccountServer>(getAccountPath(siteId, accountId, '/model-routes'), payload),
        onSuccess: (account) => {
            const normalizedAccount = normalizeSiteChannelAccount(account);
            queryClient.setQueryData<SiteChannelCard[]>(['site-channel', 'list'], (cards) =>
                replaceSiteChannelAccount(cards, siteId, normalizedAccount),
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
            apiClient.post<SiteChannelAccountServer>(getAccountPath(siteId, accountId, '/keys'), payload),
        onSuccess: (account) => {
            const normalizedAccount = normalizeSiteChannelAccount(account);
            queryClient.setQueryData<SiteChannelCard[]>(['site-channel', 'list'], (cards) =>
                replaceSiteChannelAccount(cards, siteId, normalizedAccount),
            );
            invalidateSiteChannelQueries(queryClient);
        },
        onError: (error) => {
            logger.error('site channel key create failed:', error);
        },
    });
}

export function useUpdateSiteChannelModelDisabled() {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: async ({ siteId, accountId, payload }: SiteChannelModelDisabledMutationInput) =>
            apiClient.put<SiteChannelAccountServer>(getAccountPath(siteId, accountId, '/model-disabled'), payload),
        onSuccess: (account, variables) => {
            const normalizedAccount = normalizeSiteChannelAccount(account);
            queryClient.setQueryData<SiteChannelCard[]>(['site-channel', 'list'], (cards) =>
                replaceSiteChannelAccount(cards, variables.siteId, normalizedAccount),
            );
            invalidateSiteChannelQueries(queryClient);
        },
        onError: (error) => {
            logger.error('site channel model disabled update failed:', error);
        },
    });
}

export function useUpdateSiteSourceKeys(siteId: number, accountId: number) {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: async (payload: SiteSourceKeyUpdateRequest) =>
            apiClient.put<SiteChannelAccountServer>(getAccountPath(siteId, accountId, '/source-keys'), payload),
        onSuccess: (account) => {
            const normalizedAccount = normalizeSiteChannelAccount(account);
            queryClient.setQueryData<SiteChannelCard[]>(['site-channel', 'list'], (cards) =>
                replaceSiteChannelAccount(cards, siteId, normalizedAccount),
            );
            invalidateSiteChannelAndRelated(queryClient);
        },
        onError: (error) => {
            logger.error('site source key update failed:', error);
        },
    });
}

export function useResetSiteChannelModelRoutes(siteId: number, accountId: number) {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: async () =>
            apiClient.post<SiteChannelAccountServer>(getAccountPath(siteId, accountId, '/model-routes/reset'), {}),
        onSuccess: (account) => {
            const normalizedAccount = normalizeSiteChannelAccount(account);
            queryClient.setQueryData<SiteChannelCard[]>(['site-channel', 'list'], (cards) =>
                replaceSiteChannelAccount(cards, siteId, normalizedAccount),
            );
            invalidateSiteChannelQueries(queryClient);
        },
        onError: (error) => {
            logger.error('site channel model route reset failed:', error);
        },
    });
}
