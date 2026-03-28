import type {
    SiteChannelAccount,
    SiteChannelCard,
    SiteChannelGroup,
    SiteChannelModel,
    SiteModelHistorySummary,
    SiteModelRouteSource,
    SiteModelRouteType,
} from '@/api/endpoints/site-channel';

export const SITE_GROUP_FILTER_ALL = { kind: 'all' } as const;
export const SITE_GROUP_FILTER_MISSING_KEYS = { kind: 'missing_keys' } as const;

export type SiteChannelGroupFilter =
    | typeof SITE_GROUP_FILTER_ALL
    | typeof SITE_GROUP_FILTER_MISSING_KEYS
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
    if (activeFilter.kind === 'missing_keys') return groups.filter((group) => !group.has_keys);
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
