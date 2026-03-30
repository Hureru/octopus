'use client';

import { useCallback, useMemo } from 'react';
import { useLogs } from '@/api/endpoints/log';
import { LogCard, type LogSiteActionTarget, type LogSiteActionTargets } from './Item';
import { Loader2 } from 'lucide-react';
import { useTranslations } from 'next-intl';
import { VirtualizedGrid } from '@/components/common/VirtualizedGrid';
import { useChannelList } from '@/api/endpoints/channel';
import { useSiteChannelList } from '@/api/endpoints/site-channel';

type ManagedChannelLookup = {
    name: string;
    managed_source?: {
        site_id: number;
        site_account_id: number;
        group_key: string;
    } | null;
};

function getBaseGroupKey(groupKey: string) {
    return groupKey.split('::', 1)[0] || groupKey;
}

function resolveLogChannelId(log: { channel: number; attempts?: Array<{ channel_id: number }> }) {
    if (log.channel) return log.channel;
    if (!log.attempts?.length) return 0;

    for (let index = log.attempts.length - 1; index >= 0; index -= 1) {
        const channelId = log.attempts[index]?.channel_id ?? 0;
        if (channelId) return channelId;
    }

    return 0;
}

function resolveLogModelName(log: { actual_model_name: string; request_model_name: string }) {
    return log.actual_model_name.trim() || log.request_model_name.trim();
}

function resolveLogSiteActionTarget(
    channelId: number,
    modelName: string,
    managedChannelMap: ReadonlyMap<number, ManagedChannelLookup>,
    siteChannelsData: ReturnType<typeof useSiteChannelList>['data'],
): LogSiteActionTarget | null {
    const normalizedModelName = modelName.trim();
    if (!channelId || !normalizedModelName) return null;

    const channel = managedChannelMap.get(channelId);
    if (!channel?.managed_source) return null;

    const source = channel.managed_source;
    const baseGroupKey = getBaseGroupKey(source.group_key);
    const card = siteChannelsData?.find((item) => item.site_id === source.site_id) ?? null;
    const account = card?.accounts.find((item) => item.account_id === source.site_account_id) ?? null;

    let matchedGroup = account?.groups.find(
        (group) =>
            group.group_key === baseGroupKey &&
            group.models.some((model) => model.model_name === normalizedModelName),
    ) ?? null;

    let matchedModel = matchedGroup?.models.find((model) => model.model_name === normalizedModelName) ?? null;

    if (!matchedGroup && account) {
        const candidates = account.groups.flatMap((group) =>
            group.models
                .filter((model) => model.model_name === normalizedModelName)
                .map((model) => ({ group, model })),
        );

        if (candidates.length === 1) {
            matchedGroup = candidates[0].group;
            matchedModel = candidates[0].model;
        }
    }

    if (!matchedGroup || !matchedModel) return null;

    return {
        siteId: source.site_id,
        siteName: card?.site_name ?? `站点 #${source.site_id}`,
        accountId: source.site_account_id,
        accountName: account?.account_name ?? `账号 #${source.site_account_id}`,
        groupKey: matchedGroup.group_key,
        groupName: matchedGroup.group_name,
        modelName: matchedModel.model_name,
        modelDisabled: matchedModel.disabled,
        canDisableModel: true,
        channelId,
        channelName: channel.name,
    };
}

/**
 * 日志页面组件
 * - 初始加载 pageSize 条历史日志
 * - SSE 实时推送新日志
 * - 滚动自动加载更多
 */
export function Log() {
    const t = useTranslations('log');
    const { logs, hasMore, isLoading, isLoadingMore, loadMore } = useLogs({ pageSize: 10 });
    const { data: channelsData } = useChannelList();
    const { data: siteChannelsData } = useSiteChannelList();

    const managedChannelMap = useMemo(() => {
        const next = new Map<number, ManagedChannelLookup>();
        for (const channel of channelsData ?? []) {
            next.set(channel.raw.id, {
                name: channel.raw.name,
                managed_source: channel.raw.managed_source,
            });
        }
        return next;
    }, [channelsData]);

    const siteActionTargets = useMemo(() => {
        const next = new Map<number, LogSiteActionTargets>();

        for (const log of logs) {
            const fallbackModelName = resolveLogModelName(log);
            const attemptTargets = (log.attempts ?? []).map((attempt) =>
                resolveLogSiteActionTarget(
                    attempt.channel_id,
                    attempt.model_name?.trim() || fallbackModelName,
                    managedChannelMap,
                    siteChannelsData,
                ),
            );

            const legacyErrorTarget = log.error
                ? resolveLogSiteActionTarget(
                    resolveLogChannelId(log),
                    fallbackModelName,
                    managedChannelMap,
                    siteChannelsData,
                )
                : null;

            if (!attemptTargets.some(Boolean) && !legacyErrorTarget) continue;

            next.set(log.id, {
                attemptTargets,
                legacyErrorTarget,
            });
        }

        return next;
    }, [logs, managedChannelMap, siteChannelsData]);

    const canLoadMore = hasMore && !isLoading && !isLoadingMore && logs.length > 0;
    const handleReachEnd = useCallback(() => {
        if (!canLoadMore) return;
        void loadMore();
    }, [canLoadMore, loadMore]);

    const footer = useMemo(() => {
        if (hasMore && (isLoading || isLoadingMore)) {
            return (
                <div className="flex justify-center py-4">
                    <Loader2 className="h-6 w-6 animate-spin text-muted-foreground" />
                </div>
            );
        }
        if (!hasMore && logs.length > 0) {
            return (
                <div className="flex justify-center py-4">
                    <span className="text-sm text-muted-foreground">{t('list.noMore')}</span>
                </div>
            );
        }
        return null;
    }, [hasMore, isLoading, isLoadingMore, logs.length, t]);

    return (
        <VirtualizedGrid
            items={logs}
            layout="list"
            columns={{ default: 1 }}
            estimateItemHeight={80}
            overscan={8}
            getItemKey={(log) => `log-${log.id}`}
            renderItem={(log) => <LogCard log={log} siteTargets={siteActionTargets.get(log.id) ?? null} />}
            footer={footer}
            onReachEnd={handleReachEnd}
            reachEndEnabled={canLoadMore}
            reachEndOffset={2}
        />
    );
}
