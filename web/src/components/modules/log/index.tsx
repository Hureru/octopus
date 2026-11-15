'use client';

import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useLogs, useLogSiteActionTargets, type LogKeywordMode, type LogKeywordScope } from '@/api/endpoints/log';
import { LogCard, type LogSiteActionTargets } from './Item';
import { ChevronDown, ChevronUp, Layers, Loader2 } from 'lucide-react';
import { useTranslations } from 'next-intl';
import { motion } from 'motion/react';
import { VirtualizedGrid } from '@/components/common/VirtualizedGrid';
import { useSearchStore } from '@/components/modules/toolbar';
import { useToolbarViewOptionsStore } from '@/components/modules/toolbar/view-options-store';
import { useLogUIStore } from './ui-store';
import { groupLogRows } from './stack';
import { cn } from '@/lib/utils';

type LogFilters = {
    keyword: string;
    keywordMode: LogKeywordMode;
    keywordScope: LogKeywordScope;
    channelIds: number[];
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
export function Log() {
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
    const filters = useMemo<LogFilters>(() => ({
        keyword: searchTerm,
        keywordMode: logKeywordMode,
        keywordScope: logKeywordScope,
        channelIds: logChannelIds,
        startTime: logDateRange.start,
        endTime: logDateRange.end,
    }), [logDateRange.end, logDateRange.start, logChannelIds, searchTerm, logKeywordMode, logKeywordScope]);
    const debouncedFilters = useDebouncedValue(filters, 200);
    const filterMode = filtersActive(debouncedFilters);
    const logFilters = useMemo(() => ({
        keyword: debouncedFilters.keyword.trim() || undefined,
        keyword_mode: debouncedFilters.keyword.trim() ? debouncedFilters.keywordMode : undefined,
        keyword_scope: debouncedFilters.keyword.trim() ? debouncedFilters.keywordScope : undefined,
        channel_ids: debouncedFilters.channelIds.length > 0 ? debouncedFilters.channelIds : undefined,
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

    // 进行中的请求还没落库，查不到站点操作目标；结束后 ID 集合变化会重新查询
    const logIDs = useMemo(() => logs.filter((log) => !log.pending).map((log) => log.id), [logs]);
    const siteActionTargetsQuery = useLogSiteActionTargets(logIDs, logs.length > 0);
    const siteActionTargets = useMemo(() => {
        const next = new Map<number, LogSiteActionTargets>();
        const data = siteActionTargetsQuery.data ?? {};
        for (const [id, targets] of Object.entries(data)) {
            next.set(Number(id), targets);
        }
        return next;
    }, [siteActionTargetsQuery.data]);

    const [expandedIds, setExpandedIds] = useState<ReadonlySet<number>>(new Set());
    const toggleStack = useCallback((ids: number[], expand: boolean) => {
        setExpandedIds((prev) => {
            const next = new Set(prev);
            for (const id of ids) {
                if (expand) next.add(id);
                else next.delete(id);
            }
            return next;
        });
    }, []);
    const rows = useMemo(() => groupLogRows(logs, expandedIds), [logs, expandedIds]);
    // 并入展开叠层的新成员也记为已展开，否则原成员被分页挤出后整叠会自己收起
    useEffect(() => {
        const missing = rows.flatMap((row) => (row.stack?.expanded && row.stack.head ? row.stack.ids : [])).filter((id) => !expandedIds.has(id));
        if (missing.length > 0) toggleStack(missing, true);
    }, [rows, expandedIds, toggleStack]);

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
            <div className="relative min-h-0 flex-1">
                <VirtualizedGrid
                    items={rows}
                    layout="list"
                    columns={{ default: 1 }}
                    estimateItemHeight={80}
                    overscan={8}
                    anchorScroll
                    // 折叠的一叠用最老成员当 key：新失败并入时行身份和已测高度都不变，列表不会跳
                    getItemKey={(row) => `log-${row.stack && !row.stack.expanded ? row.stack.ids[row.stack.ids.length - 1] : row.log.id}`}
                    renderItem={({ log, stack }) => {
                        const collapsed = !!stack && !stack.expanded;
                        // 刚到的日志淡入，免得在其它卡片还在让位时突然盖上来
                        const fresh = Date.now() / 1000 - log.time < 3;
                        const label = stack ? t('list.stackedFailures', { count: stack.ids.length }) : '';
                        // 三种状态共用同一棵结构，卡片始终在同一个位置，切换时不会被重新挂载。
                        // 卡片本身带 layoutId（详情弹窗用），行位置一变它就会滑过去；
                        // 叠层的装饰必须同样是 layout 元素才会跟着滑，否则会先瞬移到位、卡片再追上来。
                        return (
                            <div className={cn(stack?.expanded && 'flex flex-col gap-2 pl-4', fresh && 'animate-in fade-in duration-300')}>
                                {stack?.expanded && stack.head ? (
                                    <motion.button
                                        layout="position"
                                        type="button"
                                        onClick={() => toggleStack(stack.ids, false)}
                                        className="flex items-center gap-1.5 self-start text-xs text-muted-foreground transition-colors hover:text-foreground"
                                    >
                                        <Layers className="size-3.5" />
                                        <span className="tabular-nums">{label} · {t('list.collapse')}</span>
                                        <ChevronUp className="size-3.5" />
                                    </motion.button>
                                ) : null}
                                <div className={cn('relative', collapsed && 'z-20')}>
                                    {stack?.expanded ? (
                                        // 竖线和卡片同框，位移才会和卡片完全一致
                                        <motion.div layout="position" aria-hidden className="absolute inset-y-0 -left-2.5 w-0.5 rounded-full bg-destructive/30" />
                                    ) : null}
                                    {/* key：一叠的头换成新失败时重新挂载，卡片内部状态（详情、弹窗）不会串到另一条日志上 */}
                                    <LogCard key={log.id} log={log} siteTargets={siteActionTargets.get(log.id) ?? null} />
                                </div>
                                {collapsed ? (
                                    // 卡片下方露出两层卡片边，第一层兼作展开按钮
                                    <>
                                        <motion.button
                                            layout="position"
                                            type="button"
                                            onClick={() => toggleStack(stack.ids, true)}
                                            className="relative z-10 mx-3 -mt-6 flex w-[calc(100%-1.5rem)] items-center justify-center gap-1.5 rounded-b-3xl border border-destructive/30 bg-card pt-7 pb-1.5 text-xs text-muted-foreground transition-colors hover:text-foreground"
                                        >
                                            <Layers className="size-3.5" />
                                            <span className="tabular-nums">{label} · {t('list.expand')}</span>
                                            <ChevronDown className="size-3.5" />
                                        </motion.button>
                                        <motion.div layout="position" aria-hidden className="mx-6 -mt-5 h-7 rounded-b-3xl border border-destructive/20 bg-card" />
                                    </>
                                ) : null}
                            </div>
                        );
                    }}
                    footer={footer}
                    onReachEnd={handleReachEnd}
                    reachEndEnabled={canLoadMore}
                    reachEndOffset={2}
                />
            </div>
        </div>
    );
}
