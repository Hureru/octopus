import type { InfiniteData } from '@tanstack/react-query';
import { keepPreviousData, useInfiniteQuery, useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { apiClient, API_BASE_URL } from '../client';
import { logger } from '@/lib/logger';
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';

/**
 * 尝试状态
 */
export type AttemptStatus = 'success' | 'failed' | 'circuit_break' | 'skipped';

export type RelayLogWSMode = 'fresh' | 'continuation' | 'replay';

export type RelayLogWSExecMode = 'passthrough' | 'transform';

export type RelayLogWSRecovery = 'reconnect' | 'replay' | 'downgrade';

/**
 * 单次渠道尝试信息
 */
export interface ChannelAttempt {
    channel_id: number;
    channel_key_id?: number;
    channel_name: string;
    model_name: string;
    attempt_num: number;    // 第几次尝试
    status: AttemptStatus;
    duration: number;       // 耗时(毫秒)
    sticky?: boolean;
    msg?: string;
}

/**
 * 日志数据
 */
export interface LogSiteActionTarget {
    site_id: number;
    site_name: string;
    account_id: number;
    account_name: string;
    group_key: string;
    group_name: string;
    model_name: string;
    model_disabled: boolean;
    can_disable_model: boolean;
    channel_id: number;
    channel_name: string;
}

export interface LogSiteActionTargets {
    attempt_targets: Array<LogSiteActionTarget | null>;
    legacy_error_target?: LogSiteActionTarget | null;
}

export interface RelayLog {
    id: number;
    time: number;                // 时间戳
    request_model_name: string;  // 请求模型名称
    request_api_key_name?: string; // 请求使用的 API Key 名称
    channel: number;             // 实际使用的渠道ID
    channel_name: string;        // 渠道名称
    actual_model_name: string;   // 实际使用模型名称
    input_tokens: number;        // 输入Token
    transport_input_tokens?: number | null; // 实际发送到上游请求体的 Token 估算
    bill_input_tokens?: number | null; // 按常规输入价格计费的 Token
    cache_read_tokens?: number | null; // 从缓存读取的 Token
    cache_write_tokens?: number | null; // 写入缓存的 Token
    output_tokens: number;       // 输出Token
    ftut: number;                // 首字时间(毫秒)
    use_time: number;            // 总用时(毫秒)
    cost: number;                // 消耗费用
    request_content: string;     // 请求内容
    response_content: string;    // 响应内容
    error: string;               // 错误信息
    attempts?: ChannelAttempt[]; // 所有尝试记录
    total_attempts?: number;     // 总尝试次数
    used_ws?: boolean;           // 是否使用了上游WebSocket
    ws_mode?: RelayLogWSMode | null; // 上游 WebSocket 会话模式
    ws_exec_mode?: RelayLogWSExecMode | null; // 上游 WebSocket 事件处理方式
    ws_recovery?: RelayLogWSRecovery | null; // 本次请求触发的恢复动作
}

export type LogStatusFilter = 'all' | 'success' | 'error';

/**
 * 日志列表查询参数
 */
export type LogKeywordScope = 'default' | 'content';

export interface LogCursor {
    time: number;
    id: number;
}

export interface LogListParams {
    page?: number;
    page_size?: number;
    limit?: number;
    before_time?: number;
    before_id?: number;
    start_time?: number;
    end_time?: number;
    channel_ids?: number[];
    status?: LogStatusFilter;
    keyword?: string;
    keyword_scope?: LogKeywordScope;
    include_content?: boolean;
    with_total?: boolean;
    enabled?: boolean;
}

export interface UseLogsOptions {
    pageSize?: number;
    filters?: Omit<LogListParams, 'page' | 'page_size'>;
    mode?: 'stream' | 'paged';
}

const logFiltersKey = (filters?: UseLogsOptions['filters']) => ({
    start_time: filters?.start_time ?? null,
    end_time: filters?.end_time ?? null,
    channel_ids: filters?.channel_ids?.filter((id) => id > 0).sort((a, b) => a - b) ?? [],
    status: filters?.status && filters.status !== 'all' ? filters.status : 'all',
    keyword: filters?.keyword?.trim() ?? '',
});

function appendLogListParams(params: URLSearchParams, filters?: UseLogsOptions['filters']) {
    if (filters?.start_time) params.set('start_time', String(filters.start_time));
    if (filters?.end_time) params.set('end_time', String(filters.end_time));
    const channelIds = filters?.channel_ids?.filter((id) => id > 0) ?? [];
    if (channelIds.length > 0) params.set('channel_ids', channelIds.join(','));
    if (filters?.status && filters.status !== 'all') params.set('status', filters.status);
    const keyword = filters?.keyword?.trim();
    if (keyword) params.set('keyword', keyword);
    if (filters?.keyword_scope && filters.keyword_scope !== 'default') params.set('keyword_scope', filters.keyword_scope);
}

export interface LogPageResponse {
    logs: RelayLog[];
    total: number;
    has_more?: boolean;
    next_cursor?: LogCursor | null;
}

export function useLogPage(params: LogListParams) {
    const page = params.page ?? 1;
    const pageSize = params.page_size ?? 20;
    const filters = logFiltersKey(params);

    return useQuery({
        queryKey: ['logs', 'page', pageSize, page, filters],
        queryFn: async (): Promise<LogPageResponse> => {
            const search = new URLSearchParams();
            search.set('page', String(page));
            search.set('page_size', String(pageSize));
            search.set('include_content', String(params.include_content ?? false));
            search.set('with_total', String(params.with_total ?? true));
            appendLogListParams(search, params);
            const result = await apiClient.get<{ logs: RelayLog[] | null; total: number; has_more?: boolean; next_cursor?: LogCursor | null } | null>(
                `/api/v1/log/list?${search.toString()}`,
            );
            return {
                logs: result?.logs ?? [],
                total: result?.total ?? 0,
                has_more: result?.has_more ?? false,
                next_cursor: result?.next_cursor ?? null,
            };
        },
        placeholderData: keepPreviousData,
        staleTime: 0,
        refetchOnMount: 'always',
        refetchOnWindowFocus: false,
        enabled: params.enabled ?? true,
    });
}

/**
 * 清空日志 Hook
 * 
 * @example
 * const clearLogs = useClearLogs();
 * 
 * clearLogs.mutate();
 */
export async function getLogDetail(id: number): Promise<RelayLog> {
    return apiClient.get<RelayLog>(`/api/v1/log/${id}`);
}

export function useLogSiteActionTargets(ids: number[], enabled = true) {
    const stableIds = useMemo(() => Array.from(new Set(ids.filter((id) => id > 0))).sort((a, b) => a - b), [ids]);
    return useQuery({
        queryKey: ['logs', 'site-action-targets', stableIds],
        queryFn: async () => {
            if (stableIds.length === 0) return {} as Record<number, LogSiteActionTargets>;
            const chunkSize = 100;
            const chunks: number[][] = [];
            for (let i = 0; i < stableIds.length; i += chunkSize) {
                chunks.push(stableIds.slice(i, i + chunkSize));
            }
            const results = await Promise.all(
                chunks.map((chunk) =>
                    apiClient.get<Record<number, LogSiteActionTargets>>(
                        `/api/v1/log/site-action-targets?ids=${chunk.join(',')}`,
                    ),
                ),
            );
            return Object.assign({}, ...results) as Record<number, LogSiteActionTargets>;
        },
        enabled: enabled && stableIds.length > 0,
        staleTime: 30000,
        refetchOnWindowFocus: false,
    });
}

export function useClearLogs() {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: async () => {
            return apiClient.delete<null>('/api/v1/log/clear');
        },
        onSuccess: () => {
            logger.log('日志清空成功');
            queryClient.invalidateQueries({ queryKey: ['logs'] });
        },
        onError: (error) => {
            logger.error('日志清空失败:', error);
        },
    });
}

const logsInfiniteQueryKey = (pageSize: number, filters?: UseLogsOptions['filters']) => ['logs', 'infinite', pageSize, logFiltersKey(filters)] as const;

/**
 * 日志管理 Hook
 * 整合初始加载、SSE 实时推送、滚动加载更多
 * 
 * @example
 * const { logs, isConnected, hasMore, isLoadingMore, loadMore, clear } = useLogs();
 * 
 * // logs 自动包含历史日志和实时日志，按时间倒序
 * logs.forEach(log => console.log(log.request_model_name));
 * 
 * // 滚动到底部时加载更多
 * if (hasMore && !isLoadingMore) loadMore();
 */
export function useLogs(options: UseLogsOptions = {}) {
    const { pageSize = 20, filters, mode = 'stream' } = options;
    const streamEnabled = mode === 'stream';

    const [isConnected, setIsConnected] = useState(false);
    const [error, setError] = useState<Error | null>(null);
    const eventSourceRef = useRef<EventSource | null>(null);

    const queryClient = useQueryClient();

    type CursorPage = { logs: RelayLog[]; next_cursor?: LogCursor | null; has_more: boolean };

    const logsQuery = useInfiniteQuery({
        queryKey: logsInfiniteQueryKey(pageSize, filters),
        initialPageParam: null as LogCursor | null,
        queryFn: async ({ pageParam }) => {
            const params = new URLSearchParams();
            params.set('limit', String(pageSize));
            params.set('with_total', 'false');
            params.set('include_content', 'false');
            if (pageParam?.time && pageParam?.id) {
                params.set('before_time', String(pageParam.time));
                params.set('before_id', String(pageParam.id));
            }
            appendLogListParams(params, filters);
            const result = await apiClient.get<{ logs: RelayLog[] | null; has_more?: boolean; next_cursor?: LogCursor | null } | null>(
                `/api/v1/log/list?${params.toString()}`,
            );
            return {
                logs: result?.logs ?? [],
                has_more: result?.has_more ?? false,
                next_cursor: result?.next_cursor ?? null,
            } satisfies CursorPage;
        },
        getNextPageParam: (lastPage) => {
            if (!lastPage?.has_more) return undefined;
            return lastPage.next_cursor ?? undefined;
        },
        staleTime: 0,
        refetchOnMount: 'always',
        refetchOnWindowFocus: streamEnabled,
    });

    const logs = useMemo(() => {
        const pages = logsQuery.data?.pages ?? [];
        const seen = new Set<number>();
        const merged: RelayLog[] = [];

        for (const page of pages) {
            for (const log of page.logs) {
                if (seen.has(log.id)) continue;
                seen.add(log.id);
                merged.push(log);
            }
        }

        merged.sort((a, b) => b.time - a.time);
        return merged;
    }, [logsQuery.data]);

    const loadMore = useCallback(async () => {
        if (!logsQuery.hasNextPage) return;
        if (logsQuery.isFetchingNextPage) return;

        try {
            await logsQuery.fetchNextPage();
        } catch (e) {
            logger.error('加载更多日志失败:', e);
        }
    }, [logsQuery]);

    useEffect(() => {
        if (!streamEnabled) {
            eventSourceRef.current?.close();
            eventSourceRef.current = null;
            return;
        }

        let cancelled = false;
        let retryTimer: ReturnType<typeof setTimeout> | null = null;
        let retryAttempt = 0;

        const scheduleReconnect = () => {
            if (cancelled) return;
            const delay = Math.min(30000, 1000 * 2 ** retryAttempt);
            retryAttempt += 1;
            retryTimer = setTimeout(() => {
                retryTimer = null;
                connect(true);
            }, delay);
        };

        const connect = async (isReconnect = false) => {
            try {
                const { token } = await apiClient.get<{ token: string }>('/api/v1/log/stream-token');
                if (cancelled) return;

                const eventSource = new EventSource(`${API_BASE_URL}/api/v1/log/stream?token=${token}`);
                eventSourceRef.current = eventSource;

                eventSource.onopen = () => {
                    retryAttempt = 0;
                    setIsConnected(true);
                    setError(null);
                    if (isReconnect) {
                        queryClient.invalidateQueries({ queryKey: logsInfiniteQueryKey(pageSize, filters) });
                    }
                };

                eventSource.onmessage = (event) => {
                    try {
                        const log: RelayLog = JSON.parse(event.data);
                        queryClient.setQueryData(
                            logsInfiniteQueryKey(pageSize, filters),
                            (old: InfiniteData<CursorPage, LogCursor | null> | undefined) => {
                                if (!old) {
                                    return { pages: [{ logs: [log], has_more: false, next_cursor: null }], pageParams: [null] };
                                }

                                const exists = old.pages.some((p) => p?.logs.some((x) => x.id === log.id));
                                if (exists) return old;

                                const firstPage = old.pages[0] ?? { logs: [], has_more: false, next_cursor: null };
                                const prepended = [log, ...firstPage.logs];
                                const nextFirstPage = { ...firstPage, logs: prepended.slice(0, pageSize) };
                                if (prepended.length > pageSize && old.pages.length > 1) {
                                    queryClient.invalidateQueries({ queryKey: logsInfiniteQueryKey(pageSize, filters) });
                                }
                                return { ...old, pages: [nextFirstPage, ...old.pages.slice(1)] };
                            }
                        );
                    } catch (e) {
                        logger.error('解析日志数据失败:', e);
                    }
                };

                eventSource.onerror = () => {
                    setIsConnected(false);
                    setError(new Error('SSE 连接断开'));
                    eventSource.close();
                    eventSourceRef.current = null;
                    scheduleReconnect();
                };
            } catch (e) {
                if (cancelled) return;
                setError(e instanceof Error ? e : new Error('获取 stream token 失败'));
                logger.error('获取 stream token 失败:', e);
                scheduleReconnect();
            }
        };

        connect(false);

        return () => {
            cancelled = true;
            if (retryTimer) clearTimeout(retryTimer);
            eventSourceRef.current?.close();
            eventSourceRef.current = null;
            setIsConnected(false);
        };
    }, [pageSize, filters, queryClient, streamEnabled]);

    const clear = useCallback(() => {
        queryClient.removeQueries({ queryKey: logsInfiniteQueryKey(pageSize, filters) });
    }, [pageSize, filters, queryClient]);

    return {
        logs,
        isConnected: streamEnabled && isConnected,
        error: streamEnabled ? error : null,
        hasMore: !!logsQuery.hasNextPage,
        isLoading: logsQuery.isLoading,
        isLoadingMore: logsQuery.isFetchingNextPage,
        refetch: logsQuery.refetch,
        isRefetching: logsQuery.isRefetching,
        loadMore,
        clear,
    };
}
