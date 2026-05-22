'use client';

import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useLogPage, useLogs, useLogSiteActionTargets } from '@/api/endpoints/log';
import { LogCard, type LogSiteActionTargets } from './Item';
import { ChevronLeft, ChevronRight, Loader2 } from 'lucide-react';
import { useTranslations } from 'next-intl';
import { VirtualizedGrid } from '@/components/common/VirtualizedGrid';
import { useSearchStore, useToolbarViewOptionsStore } from '@/components/modules/toolbar';
import { Button } from '@/components/ui/button';
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover';
import { cn } from '@/lib/utils';
import { useLogUIStore } from './ui-store';

type LogFilters = {
    keyword: string;
    channelIds: number[];
    startTime?: number;
    endTime?: number;
};

type PageItem = number | { kind: 'ellipsis'; from: number; to: number };

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

function buildPageItems(current: number, total: number): PageItem[] {
    if (total <= 1) return [1];
    const show = new Set<number>();
    show.add(1);
    show.add(total);
    if (total >= 2) {
        show.add(2);
        show.add(total - 1);
    }
    for (let p = current - 1; p <= current + 1; p += 1) {
        if (p >= 1 && p <= total) show.add(p);
    }
    if (current <= 4) {
        for (let p = 1; p <= Math.min(4, total); p += 1) show.add(p);
    }
    if (current >= total - 3) {
        for (let p = Math.max(1, total - 3); p <= total; p += 1) show.add(p);
    }
    const sorted = Array.from(show).sort((a, b) => a - b);
    const items: PageItem[] = [];
    for (let i = 0; i < sorted.length; i += 1) {
        const p = sorted[i];
        if (i > 0 && p - sorted[i - 1] > 1) {
            items.push({ kind: 'ellipsis', from: sorted[i - 1] + 1, to: p - 1 });
        }
        items.push(p);
    }
    return items;
}

/**
 * 日志页面组件
 * - 初始加载 pageSize 条历史日志
 * - SSE 实时推送新日志
 * - 滚动自动加载更多
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
    const [pageState, setPageState] = useState({ key: '', page: 1 });
    const filters = useMemo<LogFilters>(() => ({
        keyword: searchTerm,
        channelIds: logChannelIds,
        startTime: logDateRange.start,
        endTime: logDateRange.end,
    }), [logDateRange.end, logDateRange.start, logChannelIds, searchTerm]);
    const debouncedFilters = useDebouncedValue(filters, 200);
    const filterMode = filtersActive(debouncedFilters);
    const filterKey = `${debouncedFilters.keyword.trim()}|${debouncedFilters.channelIds.join(',')}|${debouncedFilters.startTime ?? ''}|${debouncedFilters.endTime ?? ''}`;
    const page = pageState.key === filterKey ? pageState.page : 1;
    const logFilters = useMemo(() => ({
        keyword: debouncedFilters.keyword.trim() || undefined,
        channel_ids: debouncedFilters.channelIds.length > 0 ? debouncedFilters.channelIds : undefined,
        start_time: debouncedFilters.startTime,
        end_time: debouncedFilters.endTime,
    }), [debouncedFilters]);
    const liveLogsQuery = useLogs({ pageSize: LOG_PAGE_SIZE, mode: filterMode ? 'paged' : 'stream' });
    const pagedLogsQuery = useLogPage({
        page,
        page_size: LOG_PAGE_SIZE,
        ...logFilters,
        enabled: filterMode,
    });
    const totalMatches = pagedLogsQuery.data?.total ?? 0;
    const totalPages = filterMode ? Math.max(1, Math.ceil(totalMatches / LOG_PAGE_SIZE)) : 1;
    const logs = useMemo(() => (filterMode ? (pagedLogsQuery.data?.logs ?? []) : liveLogsQuery.logs), [filterMode, liveLogsQuery.logs, pagedLogsQuery.data]);
    const hasMore = filterMode ? page < totalPages : liveLogsQuery.hasMore;
    const isLoading = filterMode ? pagedLogsQuery.isLoading : liveLogsQuery.isLoading;
    const isLoadingMore = !filterMode && liveLogsQuery.isLoadingMore;
    const loadMore = liveLogsQuery.loadMore;

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

    const canLoadMore = !filterMode && hasMore && !isLoading && !isLoadingMore && logs.length > 0;
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
            if (filterMode) {
                await pagedLogsQuery.refetch();
            } else {
                await liveLogsQuery.refetch();
            }
        } finally {
            const elapsed = Date.now() - startedAt;
            const remaining = Math.max(0, 500 - elapsed);
            setTimeout(() => {
                if (refreshIdRef.current === myId) setRefreshing(false);
            }, remaining);
        }
    }, [filterMode, liveLogsQuery, pagedLogsQuery, setRefreshing]);

    useEffect(() => {
        if (refreshRequestId === lastHandledRefreshRequestIdRef.current) return;
        lastHandledRefreshRequestIdRef.current = refreshRequestId;
        void handleRefresh();
    }, [handleRefresh, refreshRequestId]);

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

    const showPagination = filterMode && totalPages > 1;

    const pageItems = useMemo<PageItem[]>(() => {
        if (!showPagination) return [];
        return buildPageItems(page, totalPages);
    }, [page, showPagination, totalPages]);

    const goToPage = (target: number) => {
        if (target < 1 || target > totalPages || pagedLogsQuery.isFetching) return;
        setPageState({ key: filterKey, page: target });
    };

    const [paginationDimmed, setPaginationDimmed] = useState(false);
    const [paginationHovered, setPaginationHovered] = useState(false);

    const handleListScroll = useCallback((info: { scrollTop: number; scrollHeight: number; clientHeight: number }) => {
        const distance = info.scrollHeight - info.clientHeight - info.scrollTop;
        setPaginationDimmed(distance < 80);
    }, []);

    return (
        <div className="flex h-full min-h-0 flex-col gap-3">
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
                    onScroll={showPagination ? handleListScroll : undefined}
                />

                {showPagination && (
                    <div className="pointer-events-none absolute bottom-3 right-3 z-10">
                        <div
                            onMouseEnter={() => setPaginationHovered(true)}
                            onMouseLeave={() => setPaginationHovered(false)}
                            className={cn(
                                'pointer-events-auto inline-flex items-center gap-0.5 rounded-full border border-border/60 bg-card/95 px-1.5 py-1 shadow-lg backdrop-blur transition-opacity duration-200',
                                paginationDimmed && !paginationHovered ? 'opacity-40' : 'opacity-100',
                            )}
                        >
                            <Button
                                variant="ghost"
                                size="icon"
                                onClick={() => goToPage(page - 1)}
                                disabled={page <= 1 || pagedLogsQuery.isFetching}
                                aria-label={t('pagination.prev')}
                                className="size-7 rounded-full"
                            >
                                <ChevronLeft className="size-4" />
                            </Button>
                            {pageItems.map((item, idx) => {
                                if (typeof item === 'object') {
                                    return (
                                        <EllipsisPagePopover
                                            key={`ellipsis-${idx}`}
                                            from={item.from}
                                            to={item.to}
                                            onSelect={goToPage}
                                            disabled={pagedLogsQuery.isFetching}
                                        />
                                    );
                                }
                                const active = item === page;
                                return (
                                    <button
                                        key={item}
                                        type="button"
                                        onClick={() => goToPage(item)}
                                        disabled={pagedLogsQuery.isFetching}
                                        aria-current={active ? 'page' : undefined}
                                        className={cn(
                                            'inline-flex size-7 items-center justify-center rounded-full text-xs font-medium tabular-nums transition-colors',
                                            active
                                                ? 'bg-primary text-primary-foreground shadow-sm'
                                                : 'text-foreground hover:bg-muted',
                                            pagedLogsQuery.isFetching && !active && 'cursor-not-allowed opacity-60',
                                        )}
                                    >
                                        {item}
                                    </button>
                                );
                            })}
                            <Button
                                variant="ghost"
                                size="icon"
                                onClick={() => goToPage(page + 1)}
                                disabled={page >= totalPages || pagedLogsQuery.isFetching}
                                aria-label={t('pagination.next')}
                                className="size-7 rounded-full"
                            >
                                <ChevronRight className="size-4" />
                            </Button>
                        </div>
                    </div>
                )}
            </div>
        </div>
    );
}

function EllipsisPagePopover({
    from,
    to,
    onSelect,
    disabled,
}: {
    from: number;
    to: number;
    onSelect: (page: number) => void;
    disabled?: boolean;
}) {
    const t = useTranslations('log');
    const [open, setOpen] = useState(false);
    const pages = useMemo(() => {
        const out: number[] = [];
        for (let p = from; p <= to; p += 1) out.push(p);
        return out;
    }, [from, to]);

    return (
        <Popover open={open} onOpenChange={setOpen}>
            <PopoverTrigger asChild>
                <button
                    type="button"
                    disabled={disabled}
                    aria-label={t('pagination.pagesRange', { from, to })}
                    className="inline-flex size-7 items-center justify-center rounded-full text-xs text-muted-foreground transition-colors hover:bg-muted hover:text-foreground disabled:cursor-not-allowed disabled:opacity-60"
                >
                    …
                </button>
            </PopoverTrigger>
            <PopoverContent
                align="center"
                side="top"
                sideOffset={6}
                className="w-14 rounded-2xl border border-border/60 bg-card p-1 shadow-xl"
            >
                <div className="flex max-h-56 flex-col overflow-y-auto">
                    {pages.map((p) => (
                        <button
                            key={p}
                            type="button"
                            onClick={() => {
                                onSelect(p);
                                setOpen(false);
                            }}
                            className="inline-flex h-7 items-center justify-center rounded-md text-xs font-medium tabular-nums text-foreground transition-colors hover:bg-muted"
                        >
                            {p}
                        </button>
                    ))}
                </div>
            </PopoverContent>
        </Popover>
    );
}
