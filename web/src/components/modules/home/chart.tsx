'use client';

import { useStatsDaily, useStatsHourly, useStatsTotal, type StatsMetricsFormatted } from '@/api/endpoints/stats';
import { ChartContainer, ChartTooltip, ChartTooltipContent } from '@/components/ui/chart';
import { useMemo } from 'react';
import { Area, AreaChart, CartesianGrid, XAxis, YAxis } from 'recharts';
import { useTranslations } from 'next-intl';
import { formatCount, formatMoney, formatTime } from '@/lib/utils';
import dayjs from 'dayjs';
import { AnimatedNumber } from '@/components/common/AnimatedNumber';
import { Tabs, TabsList, TabsTrigger } from '@/components/animate-ui/components/animate/tabs';
import { useHomeViewStore, type ChartPeriod } from '@/components/modules/home/store';

type Formatted = { value: string; unit: string };

// 顺序兼顾 2 / 4 / 7 列三种折行：相邻两项、前四后三都是成组的
const METRIC_KEYS = ['requests', 'tokens', 'inputTokens', 'outputTokens', 'cacheHit', 'cacheHitRate', 'waitTime'] as const;

type MetricsRow = Record<(typeof METRIC_KEYS)[number], Formatted | undefined>;

type HeroValue = {
    value: string | undefined;
    unit: string;
};

type ChartPoint = { date: string; total_cost: number };

const PERIOD_KEY: Record<ChartPeriod, 'today' | 'last7Days' | 'last30Days' | 'allTime'> = {
    '1': 'today',
    '7': 'last7Days',
    '30': 'last30Days',
    all: 'allTime',
};

export function StatsChart() {
    const t = useTranslations('home.summary');

    const { data: statsTotal } = useStatsTotal();
    const { data: statsDaily } = useStatsDaily();
    const { data: statsHourly } = useStatsHourly();

    const period = useHomeViewStore((state) => state.chartPeriod);
    const setChartPeriod = useHomeViewStore((state) => state.setChartPeriod);

    const sortedDaily = useMemo(() => {
        if (!statsDaily) return [];
        return [...statsDaily].sort((a, b) => a.date.localeCompare(b.date));
    }, [statsDaily]);

    const { hero, metrics, chartData } = useMemo<{
        hero: HeroValue;
        metrics: MetricsRow;
        chartData: ChartPoint[];
    }>(() => {
        // rows：当前时间档要求和的统计行；undefined 表示还没有数据，hero 显示「—」。
        let rows: StatsMetricsFormatted[] | undefined;
        let chartData: ChartPoint[];
        if (period === '1') {
            // 今日档：聚合 statsHourly
            rows = statsHourly;
            chartData = (statsHourly ?? []).map((stat) => ({
                date: `${stat.hour}:00`,
                total_cost: stat.total_cost.raw,
            }));
        } else {
            // 7 / 30 天聚合 statsDaily；累计档优先用 statsTotal，否则 fallback 到 statsDaily 全量
            const daily = period === 'all' ? sortedDaily : sortedDaily.slice(-Number(period));
            chartData = daily.map((stat) => ({
                date: dayjs(stat.date).format('MM/DD'),
                total_cost: stat.total_cost.raw,
            }));
            if (period === 'all' && statsTotal) rows = [statsTotal];
            else if (daily.length > 0) rows = daily;
        }

        const sum = (key: keyof StatsMetricsFormatted) => (rows ?? []).reduce((acc, r) => acc + r[key].raw, 0);
        const input = sum('input_token');
        const cacheRead = sum('cache_read_token');
        return {
            hero: rows ? formatMoney(sum('total_cost')).formatted : { value: undefined, unit: '' },
            metrics: {
                requests: formatCount(sum('request_count')).formatted,
                inputTokens: formatCount(input).formatted,
                outputTokens: formatCount(sum('output_token')).formatted,
                cacheHit: formatCount(cacheRead).formatted,
                // 命中率必须用求和后的值算，不能对各行的比率取平均
                cacheHitRate: input > 0 ? { value: ((cacheRead / input) * 100).toFixed(2), unit: '%' } : undefined,
                tokens: formatCount(sum('total_token')).formatted,
                waitTime: formatTime(sum('wait_time')).formatted,
            },
            chartData,
        };
    }, [period, statsTotal, statsHourly, sortedDaily]);

    const chartConfig = useMemo(
        () => ({
            total_cost: { label: t('headline.allTime') },
        }),
        [t]
    );

    // hero unit 处理：formatMoney 返回 unit 形如 '$' / 'K$' / 'M$' / 'B$'
    // 展示时 $ 前置、其余单位（K/M/B）后置。
    const heroUnitSuffix = useMemo(() => {
        if (!hero.unit) return '';
        // 去掉结尾 $ 留下数量级字符
        if (hero.unit === '$') return '';
        return hero.unit.replace(/\$$/, '');
    }, [hero.unit]);

    return (
        <section className="rounded-3xl bg-card border-card-border border text-card-foreground custom-shadow">
            {/* Header: hero + tabs */}
            <header className="px-5 pt-5 pb-4 flex flex-col gap-4 md:flex-row md:items-start md:justify-between">
                <div>
                    <p className="text-xs text-muted-foreground">{t(`headline.${PERIOD_KEY[period]}`)}</p>
                    <p className="mt-1 text-4xl md:text-5xl font-semibold tabular-nums tracking-tight">
                        {hero.value === undefined ? (
                            <span className="text-muted-foreground">—</span>
                        ) : (
                            <>
                                <span className="text-muted-foreground text-2xl mr-1">$</span>
                                <AnimatedNumber value={hero.value} />
                                {heroUnitSuffix && (
                                    <span className="ml-1 text-xl text-muted-foreground">{heroUnitSuffix}</span>
                                )}
                            </>
                        )}
                    </p>
                </div>
                <Tabs value={period} onValueChange={(v) => setChartPeriod(v as ChartPeriod)}>
                    <TabsList>
                        <TabsTrigger value="1">{t('periods.today')}</TabsTrigger>
                        <TabsTrigger value="7">{t('periods.last7Days')}</TabsTrigger>
                        <TabsTrigger value="30">{t('periods.last30Days')}</TabsTrigger>
                        <TabsTrigger value="all">{t('periods.allTime')}</TabsTrigger>
                    </TabsList>
                </Tabs>
            </header>

            {/* Metrics：等宽网格，标签在上、数值在下。列数跟卡片宽度走（容器查询），
                不跟窗口宽度走：侧边栏占掉一截后，窗口断点会在卡片还很宽时就折行。 */}
            <div className="@container mx-5 border-t border-border/60 py-3">
                <div className="grid grid-cols-2 gap-x-4 gap-y-3 tabular-nums @md:grid-cols-4 @3xl:grid-cols-7">
                    {METRIC_KEYS.map((key) => (
                        <StatItem key={key} label={t(`metrics.${key}`)} value={metrics[key]} />
                    ))}
                </div>
            </div>

            {/* Area chart — only total_cost */}
            <ChartContainer config={chartConfig} className="h-40 w-full">
                <AreaChart accessibilityLayer data={chartData}>
                    <defs>
                        <linearGradient id="fillCost" x1="0" y1="0" x2="0" y2="1">
                            <stop offset="5%" stopColor="var(--chart-1)" stopOpacity={0.35} />
                            <stop offset="95%" stopColor="var(--chart-1)" stopOpacity={0.05} />
                        </linearGradient>
                    </defs>
                    <CartesianGrid strokeDasharray="3 3" vertical={false} />
                    <XAxis dataKey="date" tickLine={false} axisLine={false} />
                    <YAxis
                        tickLine={false}
                        axisLine={false}
                        tickFormatter={(value) => {
                            const formatted = formatMoney(value);
                            return `${formatted.formatted.value}${formatted.formatted.unit}`;
                        }}
                    />
                    <ChartTooltip cursor={false} content={<ChartTooltipContent indicator="line" />} />
                    <Area
                        type="monotone"
                        dataKey="total_cost"
                        stroke="var(--chart-1)"
                        fill="url(#fillCost)"
                    />
                </AreaChart>
            </ChartContainer>
        </section>
    );
}

function StatItem({ label, value }: { label: string; value: Formatted | undefined }) {
    return (
        <div className="flex flex-col gap-0.5">
            <span className="text-xs text-muted-foreground">{label}</span>
            <span className="text-base font-medium">
                {value ? (
                    <>
                        <AnimatedNumber value={value.value} />
                        {value.unit && (
                            <span className="ml-0.5 text-xs text-muted-foreground">{value.unit}</span>
                        )}
                    </>
                ) : (
                    <span className="text-muted-foreground">—</span>
                )}
            </span>
        </div>
    );
}
