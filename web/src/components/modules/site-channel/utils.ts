import type {
    SiteChannelAccount,
    SiteChannelCard,
    SiteChannelGroup,
    SiteChannelModel,
    SiteSourceKey,
    SiteModelHistorySummary,
    SiteModelRouteSource,
    SiteModelRouteType,
} from '@/api/endpoints/site-channel';

export type PendingCompletionKeyItem = {
    site_id: number;
    site_name: string;
    account_id: number;
    account_name: string;
    group_key: string;
    group_name: string;
    key_id: number;
    key_name: string;
    token: string;
    token_masked: string;
};

export type PendingCompletionAccount = {
    site_id: number;
    site_name: string;
    account_id: number;
    account_name: string;
    items: PendingCompletionKeyItem[];
};

export type PendingCompletionSite = {
    site_id: number;
    site_name: string;
    platform: SiteChannelCard['platform'];
    base_url: string;
    accounts: PendingCompletionAccount[];
    pending_count: number;
};

export const SITE_GROUP_FILTER_ALL = { kind: 'all' } as const;

export type SiteChannelGroupFilter =
    | typeof SITE_GROUP_FILTER_ALL
    | { kind: 'group'; groupKey: string };

export type SiteModelView = SiteChannelModel & {
    group_key: string;
    group_name: string;
    key_count: number;
    enabled_key_count: number;
    has_keys: boolean;
    has_projected_channel: boolean;
    projected_channel_ids: number[];
};

export type SiteSourceKeyFormItem = {
    id?: number;
    enabled: boolean;
    token: string;
    token_masked?: string;
    is_new?: boolean;
    name: string;
    value_status?: 'ready' | 'masked_pending';
    last_sync_at?: number | null;
};

export function createGroupFilter(groupKey: string): SiteChannelGroupFilter {
    return { kind: 'group', groupKey };
}

export function isSameGroupFilter(
    left: SiteChannelGroupFilter,
    right: SiteChannelGroupFilter,
) {
    if (left.kind !== right.kind) return false;
    if (left.kind === 'group' && right.kind === 'group') {
        return left.groupKey === right.groupKey;
    }
    return true;
}

export function routeTypeLabel(routeType: SiteModelRouteType) {
    switch (routeType) {
        case 'unknown':
            return '未识别端点';
        case 'openai_response':
            return 'OpenAI Response';
        case 'anthropic':
            return 'Anthropic';
        case 'gemini':
            return 'Gemini';
        case 'volcengine':
            return 'Volcengine';
        case 'openai_embedding':
            return 'OpenAI Embedding';
        default:
            return 'OpenAI Chat';
    }
}

export function routeSourceLabel(routeSource: SiteModelRouteSource) {
    switch (routeSource) {
        case 'manual_override':
            return '\u624b\u52a8';
        case 'runtime_learned':
            return '\u8fd0\u884c\u65f6';
        case 'default_assigned':
            return '\u9ed8\u8ba4';
        default:
            return '\u540c\u6b65\u63a8\u65ad';
    }
}

export function platformLabel(platform: SiteChannelCard['platform']) {
    switch (platform) {
        case 'new-api':
            return 'New API';
        case 'anyrouter':
            return 'AnyRouter';
        case 'one-api':
            return 'One API';
        case 'one-hub':
            return 'One Hub';
        case 'done-hub':
            return 'Done Hub';
        case 'sub2api':
            return 'Sub2API';
        case 'openai':
            return 'OpenAI';
        case 'claude':
            return 'Claude';
        case 'gemini':
            return 'Gemini';
        default:
            return platform;
    }
}

export function flattenAccountModels(
    account: SiteChannelAccount,
    activeFilter: SiteChannelGroupFilter,
) {
    const visibleGroups = filterGroups(account.groups, activeFilter);

    return visibleGroups.flatMap((group) =>
        group.models.map((model) => ({
            ...model,
            group_key: group.group_key,
            group_name: group.group_name,
            key_count: group.key_count,
            enabled_key_count: group.enabled_key_count,
            has_keys: group.has_keys,
            has_projected_channel: group.has_projected_channel,
            projected_channel_ids: group.projected_channel_ids,
        })),
    );
}

export function filterGroups(groups: SiteChannelGroup[], activeFilter: SiteChannelGroupFilter) {
    if (activeFilter.kind === 'all') return groups;
    return groups.filter((group) => group.group_key === activeFilter.groupKey);
}

export function groupFilterCount(groups: SiteChannelGroup[], activeFilter: SiteChannelGroupFilter) {
    return filterGroups(groups, activeFilter).reduce((count, group) => count + group.models.length, 0);
}

export function countAccountKeys(account: SiteChannelAccount) {
    return account.groups.reduce(
        (acc, group) => {
            acc.total += group.key_count;
            acc.enabled += group.enabled_key_count;
            return acc;
        },
        { total: 0, enabled: 0 },
    );
}

export function isMaskedTokenValue(value: string) {
    const trimmed = value.trim();
    if (!trimmed) return false;
    return trimmed.includes('*') || trimmed.includes('•');
}

export function buildSiteTokenManagementUrl(baseUrl: string, platform: SiteChannelCard['platform']) {
    const trimmed = baseUrl.trim();
    if (!trimmed) return '';

    const normalizedBase = trimmed.replace(/\/+$/, '');
    if (platform !== 'new-api') return normalizedBase;
    return `${normalizedBase}/console/token`;
}

export function collectPendingCompletionSites(cards: SiteChannelCard[]): PendingCompletionSite[] {
    return cards
        .map((card) => {
            const accounts = card.accounts
                .map((account) => {
                    const items = account.groups.flatMap((group) =>
                        group.source_keys
                            .filter((key) => key.value_status === 'masked_pending')
                            .map((key) => ({
                                site_id: card.site_id,
                                site_name: card.site_name,
                                account_id: account.account_id,
                                account_name: account.account_name,
                                group_key: group.group_key,
                                group_name: group.group_name,
                                key_id: key.id,
                                key_name: key.name,
                                token: key.token,
                                token_masked: key.token_masked,
                            })),
                    );

                    if (items.length === 0) return null;

                    return {
                        site_id: card.site_id,
                        site_name: card.site_name,
                        account_id: account.account_id,
                        account_name: account.account_name,
                        items,
                    } satisfies PendingCompletionAccount;
                })
                .filter((account): account is PendingCompletionAccount => account !== null);

            if (accounts.length === 0) return null;

            return {
                site_id: card.site_id,
                site_name: card.site_name,
                platform: card.platform,
                base_url: card.base_url,
                accounts,
                pending_count: accounts.reduce((sum, account) => sum + account.items.length, 0),
            } satisfies PendingCompletionSite;
        })
        .filter((site): site is PendingCompletionSite => site !== null);
}

export function buildSourceKeyFormItems(group: SiteChannelGroup): SiteSourceKeyFormItem[] {
    if (!group.source_keys?.length) return [];

    return group.source_keys.map((key) => ({
        id: key.id,
        enabled: key.enabled,
        token: key.token,
        token_masked: key.token_masked,
        name: key.name ?? '',
        value_status: key.value_status,
        last_sync_at: key.last_sync_at ?? null,
    }));
}

export function buildSourceKeyUpdatePayload(
    groupKey: string,
    originalKeys: SiteSourceKey[],
    nextKeys: SiteSourceKeyFormItem[],
) {
    const originalById = new Map(originalKeys.map((key) => [key.id, key] as const));
    const nextIds = new Set(nextKeys.filter((key) => key.id).map((key) => key.id as number));

    const keys_to_delete = originalKeys
        .filter((key) => !nextIds.has(key.id))
        .map((key) => key.id);

    const keys_to_add = nextKeys
        .filter((key) => !key.id && key.token.trim())
        .map((key) => ({
            enabled: key.enabled,
            token: key.token.trim(),
            name: key.name.trim(),
        }));

    const keys_to_update = nextKeys
        .filter((key) => !!key.id)
        .map((key) => {
            const original = originalById.get(key.id as number);
            if (!original) return null;

            const update: { id: number; enabled?: boolean; token?: string; name?: string } = {
                id: key.id as number,
            };

            if (key.enabled !== original.enabled) update.enabled = key.enabled;
            const trimmedToken = key.token.trim();
            if (trimmedToken !== (original.token ?? '').trim()) update.token = trimmedToken;
            if ((key.name.trim()) !== (original.name ?? '').trim()) update.name = key.name.trim();

            if (update.enabled === undefined && update.token === undefined && update.name === undefined) {
                return null;
            }

            return update;
        })
        .filter((item): item is { id: number; enabled?: boolean; token?: string; name?: string } => item !== null);

    const payload: {
        group_key: string;
        keys_to_add?: Array<{ enabled: boolean; token: string; name?: string }>;
        keys_to_update?: Array<{ id: number; enabled?: boolean; token?: string; name?: string }>;
        keys_to_delete?: number[];
    } = { group_key: groupKey };

    if (keys_to_add.length > 0) payload.keys_to_add = keys_to_add;
    if (keys_to_update.length > 0) payload.keys_to_update = keys_to_update;
    if (keys_to_delete.length > 0) payload.keys_to_delete = keys_to_delete;

    return payload;
}

export function hasSourceKeyChanges(
    originalKeys: SiteSourceKey[],
    nextKeys: SiteSourceKeyFormItem[],
) {
    const payload = buildSourceKeyUpdatePayload('x', originalKeys, nextKeys);
    return Boolean(payload.keys_to_add?.length || payload.keys_to_update?.length || payload.keys_to_delete?.length);
}

export function formatHistoryTime(value?: number | null) {
    if (!value) return '\u4ece\u672a\u8bf7\u6c42';
    const date = new Date(value * 1000);
    if (Number.isNaN(date.getTime())) return '\u4ece\u672a\u8bf7\u6c42';
    return date.toLocaleString();
}

export function summarizeHistory(history?: SiteModelHistorySummary | null) {
    if (!history) {
        return {
            successCount: 0,
            failureCount: 0,
            totalCount: 0,
            successRate: 0,
        };
    }

    const totalCount = history.success_count + history.failure_count;
    return {
        successCount: history.success_count,
        failureCount: history.failure_count,
        totalCount,
        successRate: totalCount === 0 ? 0 : history.success_count / totalCount,
    };
}

export function getErrorMessage(error: unknown, fallback: string) {
    if (error && typeof error === 'object' && 'message' in error) {
        const message = (error as { message?: unknown }).message;
        if (typeof message === 'string' && message.trim()) {
            return message;
        }
    }

    return fallback;
}
