import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { apiClient, downloadApiFile } from '../client';

export type UsageMetricScope = 'request' | 'attempt';
export type UsageDimension =
    | 'site'
    | 'site_account'
    | 'channel'
    | 'api_key'
    | 'request_model'
    | 'actual_model'
    | 'canonical_model';

export interface UsageAnalyticsFilters {
    start_time?: number;
    end_time?: number;
    timezone: string;
    metric_scope: UsageMetricScope;
    site_ids?: number[];
    site_account_ids?: number[];
    channel_ids?: number[];
    api_key_ids?: number[];
    request_models?: string[];
    actual_models?: string[];
    canonical_models?: string[];
}

export interface UsageAnalyticsMetrics {
    metric_count: number;
    request_count: number;
    attempt_count: number;
    success_count: number;
    failed_count: number;
    canceled_count: number;
    indeterminate_count: number;
    success_rate: number;
    input_tokens: number;
    output_tokens: number;
    cache_read_tokens: number;
    cache_write_tokens: number;
    total_tokens: number;
    cost_usd: number;
    average_duration_ms: number;
    p95_duration_ms: number;
    average_ftut_ms: number;
    p95_ftut_ms: number;
    ftut_samples: number;
}

export interface UsageAnalyticsSummary extends UsageAnalyticsMetrics {
    scope: UsageMetricScope;
    start_time: number;
    end_time: number;
    timezone: string;
    drilldown_available: boolean;
    earliest_relay_log_time?: number | null;
}

export interface UsageTimeseriesPoint extends UsageAnalyticsMetrics {
    bucket_start: number;
    label: string;
}

export interface UsageTimeseries {
    scope: UsageMetricScope;
    granularity: 'hour' | 'day';
    timezone: string;
    points: UsageTimeseriesPoint[];
}

export interface UsageBreakdownItem extends UsageAnalyticsMetrics {
    id: number;
    name: string;
}

export interface UsageBreakdown {
    scope: UsageMetricScope;
    dimension: UsageDimension;
    page: number;
    page_size: number;
    total: number;
    items: UsageBreakdownItem[];
}

export type UsageBreakdownSort =
    | 'request_count'
    | 'attempt_count'
    | 'total_tokens'
    | 'cost'
    | 'success_rate'
    | 'duration';

export interface UsageBreakdownOptions {
    page: number;
    pageSize: 10 | 20 | 50;
    sort: UsageBreakdownSort;
    descending: boolean;
    search?: string;
}

export interface UsageAnalyticsExportOptions {
    filters: UsageAnalyticsFilters;
    dimension: UsageDimension;
    sort: UsageBreakdownSort;
    descending: boolean;
    search?: string;
}

export interface UsageDimensionOption {
    id: number;
    name: string;
}

export interface UsageDimensionsResult {
    dimension: UsageDimension;
    page: number;
    page_size: number;
    has_more: boolean;
    items: UsageDimensionOption[];
}

export interface RelayLogRepairPreview {
    rule_version: string;
    audit_id: string;
    matched: number;
    excluded: number;
    reasons: Record<string, number>;
    samples: Array<{
        id: number;
        time: number;
        model: string;
        channel: string;
        output_tokens: number;
    }>;
}

export interface RelayLogRepairResult extends RelayLogRepairPreview {
    batch_id: string;
    updated: number;
}

function appendArray(params: URLSearchParams, key: string, values?: Array<number | string>) {
    if (values?.length) params.set(key, values.join(','));
}

function analyticsSearch(filters: UsageAnalyticsFilters) {
    const params = new URLSearchParams();
    if (filters.start_time) params.set('start_time', String(filters.start_time));
    // UI date filters include the selected end second, while analytics
    // queries use a half-open [start, end) interval.
    if (filters.end_time) params.set('end_time', String(filters.end_time + 1));
    params.set('timezone', filters.timezone);
    params.set('metric_scope', filters.metric_scope);
    appendArray(params, 'site_ids', filters.site_ids);
    appendArray(params, 'site_account_ids', filters.site_account_ids);
    appendArray(params, 'channel_ids', filters.channel_ids);
    appendArray(params, 'api_key_ids', filters.api_key_ids);
    appendArray(params, 'request_models', filters.request_models);
    appendArray(params, 'actual_models', filters.actual_models);
    appendArray(params, 'canonical_models', filters.canonical_models);
    return params;
}

export function useUsageAnalyticsSummary(filters: UsageAnalyticsFilters) {
    return useQuery({
        queryKey: ['logs', 'analytics', 'summary', filters],
        queryFn: () =>
            apiClient.get<UsageAnalyticsSummary>(
                `/api/v1/log/analytics/summary?${analyticsSearch(filters).toString()}`,
            ),
        placeholderData: keepPreviousData,
        refetchInterval: 30000,
    });
}

export function useUsageAnalyticsTimeseries(filters: UsageAnalyticsFilters) {
    return useQuery({
        queryKey: ['logs', 'analytics', 'timeseries', filters],
        queryFn: () =>
            apiClient.get<UsageTimeseries>(
                `/api/v1/log/analytics/timeseries?${analyticsSearch(filters).toString()}`,
            ),
        placeholderData: keepPreviousData,
        refetchInterval: 30000,
    });
}

export function useUsageAnalyticsBreakdown(
    filters: UsageAnalyticsFilters,
    dimension: UsageDimension,
    options: UsageBreakdownOptions,
) {
    return useQuery({
        queryKey: ['logs', 'analytics', 'breakdown', filters, dimension, options],
        queryFn: () => {
            const params = analyticsSearch(filters);
            params.set('dimension', dimension);
            params.set('page', String(options.page));
            params.set('page_size', String(options.pageSize));
            params.set('sort', options.sort);
            params.set('descending', String(options.descending));
            if (options.search?.trim()) params.set('search', options.search.trim());
            return apiClient.get<UsageBreakdown>(
                `/api/v1/log/analytics/breakdown?${params.toString()}`,
            );
        },
        placeholderData: keepPreviousData,
        refetchInterval: 30000,
    });
}

export function useUsageAnalyticsDimensions(
    filters: UsageAnalyticsFilters,
    dimension: UsageDimension,
    search: string,
    page: number,
    pageSize = 20,
    enabled = true,
) {
    return useQuery({
        queryKey: ['logs', 'analytics', 'dimensions', filters, dimension, search, page, pageSize],
        queryFn: () => {
            const params = analyticsSearch(filters);
            params.set('dimension', dimension);
            params.set('search', search.trim());
            params.set('page', String(page));
            params.set('page_size', String(pageSize));
            return apiClient.get<UsageDimensionsResult>(
                `/api/v1/log/analytics/dimensions?${params.toString()}`,
            );
        },
        placeholderData: keepPreviousData,
        enabled,
    });
}

export function useUsageAnalyticsExport() {
    return useMutation({
        mutationFn: async (options: UsageAnalyticsExportOptions) => {
            const params = analyticsSearch(options.filters);
            params.set('dimension', options.dimension);
            params.set('sort', options.sort);
            params.set('descending', String(options.descending));
            if (options.search?.trim()) params.set('search', options.search.trim());
            return downloadApiFile(
                `/api/v1/log/analytics/export?${params.toString()}`,
                'octopus-usage.csv',
            );
        },
    });
}

export function useRelayLogRepairPreview() {
    return useMutation({
        mutationFn: (filter: { start_time?: number; end_time?: number }) =>
            apiClient.post<RelayLogRepairPreview>('/api/v1/log/repair/preview', filter),
    });
}

export function useRelayLogRepairExecute() {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: (filter: { start_time?: number; end_time?: number }) =>
            apiClient.post<RelayLogRepairResult>('/api/v1/log/repair/execute', {
                ...filter,
                confirm: true,
            }),
        onSuccess: () => {
            queryClient.invalidateQueries({ queryKey: ['logs'] });
        },
    });
}
