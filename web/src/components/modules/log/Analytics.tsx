'use client';

import { useDeferredValue, useMemo, useState } from 'react';
import { useTranslations } from 'next-intl';
import {
    Activity,
    ArrowDown,
    ArrowUp,
    ChevronLeft,
    ChevronRight,
    CircleDollarSign,
    Clock3,
    Download,
    Gauge,
    Loader2,
    MousePointerClick,
    ShieldCheck,
    Timer,
    XCircle,
} from 'lucide-react';
import { CartesianGrid, Line, LineChart, XAxis, YAxis } from 'recharts';
import {
    type UsageAnalyticsFilters,
    type UsageAnalyticsMetrics,
    type UsageBreakdownItem,
    type UsageBreakdownSort,
    type UsageDimension,
    useUsageAnalyticsBreakdown,
    useUsageAnalyticsExport,
    useUsageAnalyticsSummary,
    useUsageAnalyticsTimeseries,
} from '@/api/endpoints/log-analytics';
import {
    ChartContainer,
    ChartTooltip,
    ChartTooltipContent,
    type ChartConfig,
} from '@/components/ui/chart';
import { formatCount, formatMoney, formatTime } from '@/lib/utils';
import { cn } from '@/lib/utils';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { toast } from '@/components/common/Toast';

interface UsageAnalyticsProps {
    filters: UsageAnalyticsFilters;
    dimension: UsageDimension;
    onDrilldown: (item: UsageBreakdownItem) => void;
}

export function UsageAnalytics({ filters, dimension, onDrilldown }: UsageAnalyticsProps) {
    const t = useTranslations('log.analytics');
    const summaryQuery = useUsageAnalyticsSummary(filters);
    const timeseriesQuery = useUsageAnalyticsTimeseries(filters);
    const exportMutation = useUsageAnalyticsExport();
    const [breakdownState, setBreakdownState] = useState<{
        dimension: UsageDimension;
        page: number;
        pageSize: 10 | 20 | 50;
        sort: UsageBreakdownSort;
        descending: boolean;
        search: string;
    }>({
        dimension,
        page: 1,
        pageSize: 20,
        sort: filters.metric_scope === 'attempt' ? 'attempt_count' : 'request_count',
        descending: true,
        search: '',
    });
    const deferredSearch = useDeferredValue(breakdownState.search);
    const currentPage = breakdownState.dimension === dimension ? breakdownState.page : 1;
    const countSort: UsageBreakdownSort =
        filters.metric_scope === 'attempt' ? 'attempt_count' : 'request_count';
    const effectiveSort: UsageBreakdownSort =
        breakdownState.sort === 'request_count' || breakdownState.sort === 'attempt_count'
            ? countSort
            : breakdownState.sort;
    const effectiveBreakdownState = {
        ...breakdownState,
        sort: effectiveSort,
    };

    const breakdownQuery = useUsageAnalyticsBreakdown(filters, dimension, {
        page: currentPage,
        pageSize: effectiveBreakdownState.pageSize,
        sort: effectiveBreakdownState.sort,
        descending: effectiveBreakdownState.descending,
        search: deferredSearch,
    });

    const summary = summaryQuery.data;
    const metricLabel = filters.metric_scope === 'attempt' ? t('kpi.attempts') : t('kpi.requests');
    const chartConfig = useMemo<ChartConfig>(
        () => ({
            metric_count: { label: metricLabel, color: 'var(--chart-1)' },
            cost_usd: { label: t('kpi.cost'), color: 'var(--chart-2)' },
        }),
        [metricLabel, t],
    );

    if (summaryQuery.isLoading && !summary) {
        return (
            <div className="flex min-h-64 items-center justify-center">
                <Loader2 className="size-6 animate-spin text-muted-foreground" />
            </div>
        );
    }

    if (summaryQuery.error || timeseriesQuery.error || breakdownQuery.error) {
        const error = summaryQuery.error ?? timeseriesQuery.error ?? breakdownQuery.error;
        return (
            <div className="rounded-lg border border-destructive/30 bg-destructive/5 px-4 py-3 text-sm text-destructive">
                {error instanceof Error ? error.message : t('error')}
            </div>
        );
    }

    const kpis = summary ? buildKPIs(summary, metricLabel, t) : [];
    const chartData = timeseriesQuery.data?.points ?? [];
    const rows = breakdownQuery.isPlaceholderData ? [] : breakdownQuery.data?.items ?? [];
    const exportBreakdown = () => {
        exportMutation.mutate(
            {
                filters,
                dimension,
                sort: effectiveBreakdownState.sort,
                descending: effectiveBreakdownState.descending,
                search: breakdownState.search,
            },
            {
                onSuccess: ({ filename }) =>
                    toast.success(t('breakdown.exportSuccess', { filename })),
                onError: (error) =>
                    toast.error(t('breakdown.exportFailed'), {
                        description: error instanceof Error ? error.message : String(error),
                    }),
            },
        );
    };

    return (
        <div className="min-h-0 flex-1 overflow-auto pb-24 md:pb-4">
            <section className="grid grid-cols-2 gap-2 pb-4 md:grid-cols-4 xl:grid-cols-5 2xl:grid-cols-9">
                {kpis.map((item) => (
                    <div key={item.label} className="min-w-0 rounded-lg border border-border/70 bg-card px-3 py-3">
                        <div className="flex items-center gap-2 text-muted-foreground">
                            <item.icon className="size-3.5 shrink-0" />
                            <span className="truncate text-[11px] font-medium">{item.label}</span>
                        </div>
                        <div className="mt-2 truncate text-lg font-semibold tabular-nums">{item.value}</div>
                        {(item.details ?? []).map((detail) => (
                            <div
                                key={detail}
                                title={detail}
                                className="mt-1 truncate text-[10px] tabular-nums text-muted-foreground"
                            >
                                {detail}
                            </div>
                        ))}
                    </div>
                ))}
            </section>

            <section className="border-y border-border/70 py-4">
                <div className="mb-3 flex items-center justify-between gap-3">
                    <h2 className="text-sm font-semibold">{t('trend.title')}</h2>
                    <span className="text-[11px] text-muted-foreground">
                        {timeseriesQuery.data?.granularity === 'hour' ? t('trend.hourly') : t('trend.daily')}
                    </span>
                </div>
                <ChartContainer config={chartConfig} className="h-64 w-full">
                    <LineChart accessibilityLayer data={chartData} margin={{ left: 4, right: 8 }}>
                        <CartesianGrid vertical={false} strokeDasharray="3 3" />
                        <XAxis dataKey="label" tickLine={false} axisLine={false} minTickGap={28} />
                        <YAxis
                            yAxisId="count"
                            tickLine={false}
                            axisLine={false}
                            tickFormatter={(value) => compactCount(Number(value))}
                            width={48}
                        />
                        <YAxis
                            yAxisId="cost"
                            orientation="right"
                            tickLine={false}
                            axisLine={false}
                            tickFormatter={(value) => compactMoney(Number(value))}
                            width={52}
                        />
                        <ChartTooltip content={<ChartTooltipContent indicator="line" />} />
                        <Line
                            yAxisId="count"
                            type="monotone"
                            dataKey="metric_count"
                            stroke="var(--chart-1)"
                            strokeWidth={2}
                            dot={false}
                        />
                        <Line
                            yAxisId="cost"
                            type="monotone"
                            dataKey="cost_usd"
                            stroke="var(--chart-2)"
                            strokeWidth={2}
                            dot={false}
                        />
                    </LineChart>
                </ChartContainer>
            </section>

            <section className="py-4">
                <div className="mb-3 flex flex-wrap items-center justify-between gap-3">
                    <div>
                        <h2 className="text-sm font-semibold">{t(`dimension.${dimension}`)}</h2>
                        <span className="text-[11px] text-muted-foreground">
                            {t('breakdown.total', { count: breakdownQuery.data?.total ?? 0 })}
                        </span>
                    </div>
                    <div className="flex flex-wrap items-center gap-2">
                        <Button
                            type="button"
                            variant="outline"
                            size="icon-sm"
                            onClick={exportBreakdown}
                            disabled={exportMutation.isPending}
                            aria-label={
                                exportMutation.isPending
                                    ? t('breakdown.exporting')
                                    : t('breakdown.export')
                            }
                            title={
                                exportMutation.isPending
                                    ? t('breakdown.exporting')
                                    : t('breakdown.export')
                            }
                        >
                            {exportMutation.isPending ? (
                                <Loader2 className="animate-spin" />
                            ) : (
                                <Download />
                            )}
                        </Button>
                        <Input
                            value={breakdownState.search}
                            onChange={(event) =>
                                setBreakdownState((current) => ({
                                    ...current,
                                    dimension,
                                    page: 1,
                                    search: event.target.value,
                                }))
                            }
                            aria-label={t('breakdown.search')}
                            placeholder={t('breakdown.search')}
                            className="h-8 w-40 text-xs"
                        />
                        <select
                            value={breakdownState.pageSize}
                            onChange={(event) =>
                                setBreakdownState((current) => ({
                                    ...current,
                                    dimension,
                                    page: 1,
                                    pageSize: Number(event.target.value) as 10 | 20 | 50,
                                }))
                            }
                            className="h-8 rounded-md border bg-background px-2 text-xs"
                            aria-label={t('breakdown.topN')}
                        >
                            {[10, 20, 50].map((size) => (
                                <option key={size} value={size}>{t('breakdown.top', { count: size })}</option>
                            ))}
                        </select>
                    </div>
                </div>
                <div className="overflow-x-auto rounded-lg border border-border/70">
                    <table className="w-full min-w-[720px] text-sm">
                        <thead className="bg-muted/40 text-[11px] text-muted-foreground">
                            <tr>
                                <th className="px-3 py-2 text-left font-medium">{t('breakdown.name')}</th>
                                <SortableHead
                                    label={metricLabel}
                                    sort={countSort}
                                    state={effectiveBreakdownState}
                                    onChange={(sort, descending) =>
                                        setBreakdownState((current) => ({
                                            ...current,
                                            dimension,
                                            page: 1,
                                            sort,
                                            descending,
                                        }))
                                    }
                                />
                                <SortableHead label={t('kpi.tokens')} sort="total_tokens" state={effectiveBreakdownState} onChange={(sort, descending) => setBreakdownState((current) => ({ ...current, dimension, page: 1, sort, descending }))} />
                                <SortableHead label={t('kpi.successRate')} sort="success_rate" state={effectiveBreakdownState} onChange={(sort, descending) => setBreakdownState((current) => ({ ...current, dimension, page: 1, sort, descending }))} />
                                <SortableHead label={t('kpi.cost')} sort="cost" state={effectiveBreakdownState} onChange={(sort, descending) => setBreakdownState((current) => ({ ...current, dimension, page: 1, sort, descending }))} />
                                <SortableHead label={t('kpi.averageLatency')} sort="duration" state={effectiveBreakdownState} onChange={(sort, descending) => setBreakdownState((current) => ({ ...current, dimension, page: 1, sort, descending }))} />
                            </tr>
                        </thead>
                        <tbody>
                            {rows.map((item) => (
                                <tr
                                    key={`${dimension}-${item.id}-${item.name}`}
                                    className={cn(
                                        'border-t border-border/60',
                                        summary?.drilldown_available && 'hover:bg-muted/30',
                                    )}
                                >
                                    <td className="max-w-64 px-3 py-2.5 font-medium">
                                        {summary?.drilldown_available ? (
                                            <button
                                                type="button"
                                                className="max-w-60 truncate text-left hover:underline"
                                                onClick={() => onDrilldown(item)}
                                            >
                                                {item.name}
                                            </button>
                                        ) : (
                                            <span className="block max-w-60 truncate">{item.name}</span>
                                        )}
                                    </td>
                                    <td className="px-3 py-2.5 text-right tabular-nums">{compactCount(item.metric_count)}</td>
                                    <td className="px-3 py-2.5 text-right tabular-nums">{compactCount(item.total_tokens)}</td>
                                    <td className="px-3 py-2.5 text-right tabular-nums">{formatPercent(item.success_rate)}</td>
                                    <td className="px-3 py-2.5 text-right tabular-nums">{compactMoney(item.cost_usd)}</td>
                                    <td className="px-3 py-2.5 text-right tabular-nums">{compactTime(item.average_duration_ms)}</td>
                                </tr>
                            ))}
                            {breakdownQuery.isPlaceholderData ? (
                                <tr>
                                    <td colSpan={6} className="px-3 py-10 text-center text-muted-foreground">
                                        <Loader2 className="mx-auto size-4 animate-spin" />
                                    </td>
                                </tr>
                            ) : rows.length === 0 ? (
                                <tr>
                                    <td colSpan={6} className="px-3 py-10 text-center text-sm text-muted-foreground">
                                        {t('empty')}
                                    </td>
                                </tr>
                            ) : null}
                        </tbody>
                    </table>
                </div>
                <div className="mt-3 flex items-center justify-end gap-2">
                    <span className="text-xs text-muted-foreground">
                        {t('breakdown.pageOf', {
                            page: currentPage,
                            pages: Math.max(1, Math.ceil((breakdownQuery.data?.total ?? 0) / breakdownState.pageSize)),
                        })}
                    </span>
                    <Button
                        type="button"
                        variant="outline"
                        size="icon-sm"
                        aria-label={t('breakdown.previous')}
                        disabled={currentPage <= 1}
                        onClick={() =>
                            setBreakdownState((current) => ({
                                ...current,
                                dimension,
                                page: Math.max(1, currentPage - 1),
                            }))
                        }
                    >
                        <ChevronLeft />
                    </Button>
                    <Button
                        type="button"
                        variant="outline"
                        size="icon-sm"
                        aria-label={t('breakdown.next')}
                        disabled={currentPage * breakdownState.pageSize >= (breakdownQuery.data?.total ?? 0)}
                        onClick={() =>
                            setBreakdownState((current) => ({
                                ...current,
                                dimension,
                                page: currentPage + 1,
                            }))
                        }
                    >
                        <ChevronRight />
                    </Button>
                </div>
                {summary && !summary.drilldown_available ? (
                    <p className="mt-2 text-[11px] text-amber-600 dark:text-amber-400">{t('breakdown.noDrilldown')}</p>
                ) : null}
            </section>
        </div>
    );
}

function SortableHead({
    label,
    sort,
    state,
    onChange,
}: {
    label: string;
    sort: UsageBreakdownSort;
    state: { sort: UsageBreakdownSort; descending: boolean };
    onChange: (sort: UsageBreakdownSort, descending: boolean) => void;
}) {
    const active = state.sort === sort;
    const Icon = active && !state.descending ? ArrowUp : ArrowDown;
    return (
        <th
            className="px-3 py-2 text-right font-medium"
            aria-sort={active ? (state.descending ? 'descending' : 'ascending') : 'none'}
        >
            <button
                type="button"
                className="ml-auto inline-flex min-h-8 items-center gap-1"
                onClick={() => onChange(sort, active ? !state.descending : true)}
            >
                {label}
                <Icon className={cn('size-3', active ? 'opacity-100' : 'opacity-30')} />
            </button>
        </th>
    );
}

type Translation = ReturnType<typeof useTranslations<'log.analytics'>>;
type KPI = {
    label: string;
    value: string;
    details?: string[];
    icon: typeof Activity;
};

function buildKPIs(metrics: UsageAnalyticsMetrics, metricLabel: string, t: Translation): KPI[] {
    return [
        {
            label: metricLabel,
            value: compactCount(metrics.metric_count),
            details: [
                t('kpi.successFailed', {
                    success: compactCount(metrics.success_count),
                    failed: compactCount(metrics.failed_count),
                }),
            ],
            icon: Activity,
        },
        {
            label: t('kpi.successRate'),
            value: formatPercent(metrics.success_rate),
            details: [`${t('kpi.canceled')} ${compactCount(metrics.canceled_count)}`],
            icon: ShieldCheck,
        },
        {
            label: t('kpi.tokens'),
            value: compactCount(metrics.total_tokens),
            details: [
                t('kpi.inputOutput', {
                    input: compactCount(metrics.input_tokens),
                    output: compactCount(metrics.output_tokens),
                }),
                t('kpi.cacheTokens', {
                    read: compactCount(metrics.cache_read_tokens),
                    write: compactCount(metrics.cache_write_tokens),
                }),
            ],
            icon: Gauge,
        },
        {
            label: t('kpi.cost'),
            value: compactMoney(metrics.cost_usd),
            details: ['USD'],
            icon: CircleDollarSign,
        },
        {
            label: t('kpi.averageLatency'),
            value: compactTime(metrics.average_duration_ms),
            icon: Clock3,
        },
        {
            label: t('kpi.p95Latency'),
            value: compactTime(metrics.p95_duration_ms),
            icon: Timer,
        },
        {
            label: t('kpi.averageFTUT'),
            value: metrics.ftut_samples > 0 ? compactTime(metrics.average_ftut_ms) : '—',
            details: [`${compactCount(metrics.ftut_samples)} ${t('kpi.samples')}`],
            icon: MousePointerClick,
        },
        {
            label: t('kpi.p95FTUT'),
            value: metrics.ftut_samples > 0 ? compactTime(metrics.p95_ftut_ms) : '—',
            details: [`${compactCount(metrics.ftut_samples)} ${t('kpi.samples')}`],
            icon: MousePointerClick,
        },
        {
            label: t('kpi.canceled'),
            value: compactCount(metrics.canceled_count),
            details: metrics.indeterminate_count > 0
                ? [`${t('kpi.indeterminate')} ${compactCount(metrics.indeterminate_count)}`]
                : undefined,
            icon: XCircle,
        },
    ];
}

function compactCount(value: number) {
    const formatted = formatCount(value).formatted;
    return `${trimTrailingZeros(formatted.value)}${formatted.unit}`;
}

function compactMoney(value: number) {
    const formatted = formatMoney(value).formatted;
    if (formatted.unit === '$') return `$${formatted.value}`;
    return `${formatted.value}${formatted.unit}`;
}

function compactTime(value: number) {
    const formatted = formatTime(value).formatted;
    return `${trimTrailingZeros(formatted.value)}${formatted.unit}`;
}

function formatPercent(value: number) {
    return `${(value * 100).toFixed(1)}%`;
}

function trimTrailingZeros(value: string) {
    return value.includes('.') ? value.replace(/\.?0+$/, '') : value;
}
