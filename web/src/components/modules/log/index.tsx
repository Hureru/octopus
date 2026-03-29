'use client';

import { useCallback, useMemo } from 'react';
import { useLogs } from '@/api/endpoints/log';
import { LogCard, type LogSiteActionTarget } from './Item';
import { Loader2 } from 'lucide-react';
import { useTranslations } from 'next-intl';
import { VirtualizedGrid } from '@/components/common/VirtualizedGrid';
import { useChannelList } from '@/api/endpoints/channel';
import { useSiteChannelList } from '@/api/endpoints/site-channel';

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
        const next = new Map<number, NonNullable<typeof channelsData>[number]['raw']>();
        for (const channel of channelsData ?? []) {
            next.set(channel.raw.id, channel.raw);
        }
        return next;
    }, [channelsData]);

    const siteActionTargets = useMemo(() => {
        const next = new Map<number, LogSiteActionTarget>();

        for (const log of logs) {
            const channelId = resolveLogChannelId(log);
            if (!channelId) continue;

            const channel = managedChannelMap.get(channelId);
            if (!channel?.managed_source) continue;

            const source = channel.managed_source;
            const baseGroupKey = getBaseGroupKey(source.group_key);
            const modelName = resolveLogModelName(log);
            const card = siteChannelsData?.find((item) => item.site_id === source.site_id) ?? null;
            const account = card?.accounts.find((item) => item.account_id === source.site_account_id) ?? null;

            let matchedGroup = account?.groups.find(
                (group) =>
                    group.group_key === baseGroupKey &&
                    group.models.some((model) => model.model_name === modelName),
            ) ?? null;

            let matchedModel = matchedGroup?.models.find((model) => model.model_name === modelName) ?? null;

            if (!matchedGroup && account && modelName) {
                const candidates = account.groups.flatMap((group) =>
                    group.models
                        .filter((model) => model.model_name === modelName)
                        .map((model) => ({ group, model })),
                );

                if (candidates.length === 1) {
                    matchedGroup = candidates[0].group;
                    matchedModel = candidates[0].model;
                }
            }

            next.set(log.id, {
                siteId: source.site_id,
                siteName: card?.site_name ?? `站点 #${source.site_id}`,
                accountId: source.site_account_id,
                accountName: account?.account_name ?? `账号 #${source.site_account_id}`,
                groupKey: matchedGroup?.group_key ?? baseGroupKey,
                groupName: matchedGroup?.group_name ?? baseGroupKey,
                modelName: matchedModel?.model_name ?? modelName,
                modelDisabled: matchedModel?.disabled ?? false,
                canDisableModel: !!matchedGroup && !!matchedModel,
                channelId,
                channelName: channel.name,
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
            renderItem={(log) => <LogCard log={log} siteTarget={siteActionTargets.get(log.id) ?? null} />}
            footer={footer}
            onReachEnd={handleReachEnd}
            reachEndEnabled={canLoadMore}
            reachEndOffset={2}
        />
    );
}
