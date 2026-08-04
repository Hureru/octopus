'use client';

import { useCallback, useEffect, useId, useMemo, useRef, useState } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { useLogs, useLogSiteActionTargets, type LogKeywordMode, type LogKeywordScope } from '@/api/endpoints/log';
import type { UsageAnalyticsFilters, UsageBreakdownItem } from '@/api/endpoints/log-analytics';
import { LogCard, type LogSiteActionTargets } from './Item';
import { Loader2 } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { useTranslations } from 'next-intl';
import dayjs from 'dayjs';
import { VirtualizedGrid } from '@/components/common/VirtualizedGrid';
import { useSearchStore } from '@/components/modules/toolbar';
import { useToolbarViewOptionsStore } from '@/components/modules/toolbar/view-options-store';
import { useLogUIStore } from './ui-store';
import { useLogAnalyticsStore } from './analytics-store';
import { UsageAnalytics } from './Analytics';
import { LogControls, type LogViewTabIds } from './Controls';

type LogFilters = {
    keyword: string;
    keywordMode: LogKeywordMode;
    keywordScope: LogKeywordScope;
    channelIds: number[];
    siteIds: number[];
    siteAccountIds: number[];
    apiKeyIds: number[];
    requestModels: string[];
    actualModels: string[];
    canonicalModels: string[];
    startTime?: number;
    endTime?: number;
};

const LOG_PAGE_SIZE = 10;

function useDebouncedValue<T>(value: T, delay = 200) {
    const [debounced, setDebounced] = useState(value);
    useEffect(() => {
        const handle = setTimeout(() => setDebounced(value), delay);
        return () => clearTimeout(handle);
    }, [value, delay]);
    return debounced;
}

function filtersActive(filters: LogFilters) {
    return (
        !!filters.keyword.trim() ||
        filters.channelIds.length > 0 ||
        filters.siteIds.length > 0 ||
        filters.siteAccountIds.length > 0 ||
        filters.apiKeyIds.length > 0 ||
        filters.requestModels.length > 0 ||
        filters.actualModels.length > 0 ||
        filters.canonicalModels.length > 0 ||
        !!filters.startTime ||
        !!filters.endTime
    );
}

/**
 * 日志页面组件
 * - 初始加载 pageSize 条历史日志
 * - SSE 实时推送新日志（无过滤时）
 * - 过滤模式使用 cursor 分页，滚动加载更多
 */
function LogDetailList() {
    const t = useTranslations('log');
    const pageKey = 'log' as const;
    const searchTerm = useSearchStore((s) => s.getSearchTerm(pageKey));
    const refreshRequestId = useLogUIStore((s) => s.refreshRequestId);
    const setRefreshing = useLogUIStore((s) => s.setRefreshing);
    const lastHandledRefreshRequestIdRef = useRef(refreshRequestId);
    const logDateRange = useToolbarViewOptionsStore((s) => s.logDateRange);
    const logChannelIds = useToolbarViewOptionsStore((s) => s.logChannelIds);
    const logKeywordMode = useToolbarViewOptionsStore((s) => s.logKeywordMode);
    const logKeywordScope = useToolbarViewOptionsStore((s) => s.logKeywordScope);
    const siteIds = useLogAnalyticsStore((s) => s.siteIds);
    const siteAccountIds = useLogAnalyticsStore((s) => s.siteAccountIds);
    const apiKeyIds = useLogAnalyticsStore((s) => s.apiKeyIds);
    const requestModels = useLogAnalyticsStore((s) => s.requestModels);
    const actualModels = useLogAnalyticsStore((s) => s.actualModels);
    const canonicalModels = useLogAnalyticsStore((s) => s.canonicalModels);
    const filters = useMemo<LogFilters>(() => ({
        keyword: searchTerm,
        keywordMode: logKeywordMode,
        keywordScope: logKeywordScope,
        channelIds: logChannelIds,
        siteIds,
        siteAccountIds,
        apiKeyIds,
        requestModels,
        actualModels,
        canonicalModels,
        startTime: logDateRange.start,
        endTime: logDateRange.end,
    }), [
        actualModels,
        apiKeyIds,
        canonicalModels,
        logDateRange.end,
        logDateRange.start,
        logChannelIds,
        logKeywordMode,
        logKeywordScope,
        requestModels,
        searchTerm,
        siteAccountIds,
        siteIds,
    ]);
    const debouncedFilters = useDebouncedValue(filters, 200);
    const filterMode = filtersActive(debouncedFilters);
    const logFilters = useMemo(() => ({
        keyword: debouncedFilters.keyword.trim() || undefined,
        keyword_mode: debouncedFilters.keyword.trim() ? debouncedFilters.keywordMode : undefined,
        keyword_scope: debouncedFilters.keyword.trim() ? debouncedFilters.keywordScope : undefined,
        channel_ids: debouncedFilters.channelIds.length > 0 ? debouncedFilters.channelIds : undefined,
        site_ids: debouncedFilters.siteIds.length > 0 ? debouncedFilters.siteIds : undefined,
        site_account_ids: debouncedFilters.siteAccountIds.length > 0 ? debouncedFilters.siteAccountIds : undefined,
        api_key_ids: debouncedFilters.apiKeyIds.length > 0 ? debouncedFilters.apiKeyIds : undefined,
        request_models: debouncedFilters.requestModels.length > 0 ? debouncedFilters.requestModels : undefined,
        actual_models: debouncedFilters.actualModels.length > 0 ? debouncedFilters.actualModels : undefined,
        canonical_models: debouncedFilters.canonicalModels.length > 0 ? debouncedFilters.canonicalModels : undefined,
        start_time: debouncedFilters.startTime,
        end_time: debouncedFilters.endTime,
    }), [debouncedFilters]);
    const liveLogsQuery = useLogs({ pageSize: LOG_PAGE_SIZE, filters: logFilters, mode: filterMode ? 'paged' : 'stream' });
    const logs = liveLogsQuery.logs;
    const hasMore = liveLogsQuery.hasMore;
    const isLoading = liveLogsQuery.isLoading;
    const isLoadingMore = liveLogsQuery.isLoadingMore;
    const loadMore = liveLogsQuery.loadMore;
    const warning = liveLogsQuery.warning;
    const logsError = liveLogsQuery.error;

    const logIDs = useMemo(() => logs.map((log) => log.id), [logs]);
    const siteActionTargetsQuery = useLogSiteActionTargets(logIDs, logs.length > 0);
    const siteActionTargets = useMemo(() => {
        const next = new Map<number, LogSiteActionTargets>();
        const data = siteActionTargetsQuery.data ?? {};
        for (const [id, targets] of Object.entries(data)) {
            next.set(Number(id), targets);
        }
        return next;
    }, [siteActionTargetsQuery.data]);

    const canLoadMore = hasMore && !isLoading && !isLoadingMore && logs.length > 0;
    const handleReachEnd = useCallback(() => {
        if (!canLoadMore) return;
        void loadMore();
    }, [canLoadMore, loadMore]);

    const refreshIdRef = useRef(0);
    const handleRefresh = useCallback(async () => {
        refreshIdRef.current += 1;
        const myId = refreshIdRef.current;
        setRefreshing(true);
        const startedAt = Date.now();
        try {
            await liveLogsQuery.refetch();
        } finally {
            const elapsed = Date.now() - startedAt;
            const remaining = Math.max(0, 500 - elapsed);
            setTimeout(() => {
                if (refreshIdRef.current === myId) setRefreshing(false);
            }, remaining);
        }
    }, [liveLogsQuery, setRefreshing]);

    useEffect(() => {
        if (refreshRequestId === lastHandledRefreshRequestIdRef.current) return;
        lastHandledRefreshRequestIdRef.current = refreshRequestId;
        void handleRefresh();
    }, [handleRefresh, refreshRequestId]);

    const footer = useMemo(() => {
        if (hasMore) {
            return (
                <div className="flex justify-center py-4">
                    <Loader2 className="h-6 w-6 animate-spin text-muted-foreground" />
                </div>
            );
        }
        if (logs.length > 0) {
            return (
                <div className="flex justify-center py-4">
                    <span className="text-sm text-muted-foreground">{t('list.noMore')}</span>
                </div>
            );
        }
        return null;
    }, [hasMore, logs.length, t]);

    return (
        <div className="flex h-full min-h-0 flex-col gap-3">
            {warning ? (
                <div className="rounded-lg border border-amber-500/40 bg-amber-500/10 px-3 py-2 text-xs text-amber-700 dark:text-amber-300">
                    {warning}
                </div>
            ) : null}
            {logsError ? (
                <div
                    role="alert"
                    className="flex flex-wrap items-center justify-between gap-2 rounded-lg border border-destructive/30 bg-destructive/10 px-3 py-2 text-xs text-destructive"
                >
                    <span>{t('list.loadFailed', { message: logsError.message })}</span>
                    <Button type="button" variant="outline" size="sm" onClick={() => void liveLogsQuery.refetch()}>
                        {t('list.retry')}
                    </Button>
                </div>
            ) : null}
            <div className="relative min-h-0 flex-1">
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
            </div>
        </div>
    );
}

export function Log() {
    const queryClient = useQueryClient();
    const tabsId = useId();
    const view = useLogAnalyticsStore((state) => state.view);
    const scope = useLogAnalyticsStore((state) => state.scope);
    const dimension = useLogAnalyticsStore((state) => state.dimension);
    const timezone = useLogAnalyticsStore((state) => state.timezone);
    const siteIds = useLogAnalyticsStore((state) => state.siteIds);
    const siteAccountIds = useLogAnalyticsStore((state) => state.siteAccountIds);
    const apiKeyIds = useLogAnalyticsStore((state) => state.apiKeyIds);
    const requestModels = useLogAnalyticsStore((state) => state.requestModels);
    const actualModels = useLogAnalyticsStore((state) => state.actualModels);
    const canonicalModels = useLogAnalyticsStore((state) => state.canonicalModels);
    const setView = useLogAnalyticsStore((state) => state.setView);
    const setSiteIds = useLogAnalyticsStore((state) => state.setSiteIds);
    const setSiteAccountIds = useLogAnalyticsStore((state) => state.setSiteAccountIds);
    const setAPIKeyIds = useLogAnalyticsStore((state) => state.setAPIKeyIds);
    const setRequestModels = useLogAnalyticsStore((state) => state.setRequestModels);
    const setActualModels = useLogAnalyticsStore((state) => state.setActualModels);
    const setCanonicalModels = useLogAnalyticsStore((state) => state.setCanonicalModels);
    const logDateRange = useToolbarViewOptionsStore((state) => state.logDateRange);
    const logChannelIds = useToolbarViewOptionsStore((state) => state.logChannelIds);
    const setLogDateRange = useToolbarViewOptionsStore((state) => state.setLogDateRange);
    const setLogChannelIds = useToolbarViewOptionsStore((state) => state.setLogChannelIds);
    const refreshRequestId = useLogUIStore((state) => state.refreshRequestId);
    const setRefreshing = useLogUIStore((state) => state.setRefreshing);
    const lastAnalyticsRefreshRef = useRef(refreshRequestId);
    const tabIds = useMemo<LogViewTabIds>(() => ({
        analytics: {
            trigger: `${tabsId}-tab-analytics`,
            panel: `${tabsId}-panel-analytics`,
        },
        detail: {
            trigger: `${tabsId}-tab-detail`,
            panel: `${tabsId}-panel-detail`,
        },
    }), [tabsId]);

    useEffect(() => {
        if (logDateRange.start || logDateRange.end) return;
        const now = dayjs().tz(timezone);
        setLogDateRange({ start: now.startOf('day').unix(), end: now.unix() });
    }, [logDateRange.end, logDateRange.start, setLogDateRange, timezone]);

    useEffect(() => {
        if (view !== 'analytics' || refreshRequestId === lastAnalyticsRefreshRef.current) return;
        lastAnalyticsRefreshRef.current = refreshRequestId;
        setRefreshing(true);
        void queryClient.invalidateQueries({ queryKey: ['logs', 'analytics'] }).finally(() => {
            setTimeout(() => setRefreshing(false), 350);
        });
    }, [queryClient, refreshRequestId, setRefreshing, view]);

    const analyticsFilters = useMemo<UsageAnalyticsFilters>(() => ({
        start_time: logDateRange.start,
        end_time: logDateRange.end,
        timezone,
        metric_scope: scope,
        site_ids: siteIds.length > 0 ? siteIds : undefined,
        site_account_ids: siteAccountIds.length > 0 ? siteAccountIds : undefined,
        channel_ids: logChannelIds.length > 0 ? logChannelIds : undefined,
        api_key_ids: apiKeyIds.length > 0 ? apiKeyIds : undefined,
        request_models: requestModels.length > 0 ? requestModels : undefined,
        actual_models: actualModels.length > 0 ? actualModels : undefined,
        canonical_models: canonicalModels.length > 0 ? canonicalModels : undefined,
    }), [
        actualModels,
        apiKeyIds,
        canonicalModels,
        logChannelIds,
        logDateRange.end,
        logDateRange.start,
        requestModels,
        scope,
        siteAccountIds,
        siteIds,
        timezone,
    ]);

    const handleDrilldown = useCallback((item: UsageBreakdownItem) => {
        switch (dimension) {
            case 'site':
                setSiteIds(item.id > 0 ? [item.id] : []);
                break;
            case 'site_account':
                setSiteAccountIds(item.id > 0 ? [item.id] : []);
                break;
            case 'channel':
                setLogChannelIds(item.id > 0 ? [item.id] : []);
                break;
            case 'api_key':
                setAPIKeyIds(item.id > 0 ? [item.id] : []);
                break;
            case 'request_model':
                setRequestModels([item.name]);
                break;
            case 'actual_model':
                setActualModels([item.name]);
                break;
            case 'canonical_model':
                setCanonicalModels([item.name]);
                break;
        }
        setView('detail');
    }, [
        dimension,
        setAPIKeyIds,
        setActualModels,
        setCanonicalModels,
        setLogChannelIds,
        setRequestModels,
        setSiteAccountIds,
        setSiteIds,
        setView,
    ]);

    return (
        <div className="flex h-full min-h-0 flex-col gap-3">
            <LogControls tabIds={tabIds} />
            <div
                id={tabIds.analytics.panel}
                role="tabpanel"
                aria-labelledby={tabIds.analytics.trigger}
                hidden={view !== 'analytics'}
                tabIndex={view === 'analytics' ? 0 : -1}
                className="min-h-0 flex-1"
            >
                {view === 'analytics' ? (
                    <UsageAnalytics filters={analyticsFilters} dimension={dimension} onDrilldown={handleDrilldown} />
                ) : null}
            </div>
            <div
                id={tabIds.detail.panel}
                role="tabpanel"
                aria-labelledby={tabIds.detail.trigger}
                hidden={view !== 'detail'}
                tabIndex={view === 'detail' ? 0 : -1}
                className="min-h-0 flex-1"
            >
                {view === 'detail' ? <LogDetailList /> : null}
            </div>
        </div>
    );
}
