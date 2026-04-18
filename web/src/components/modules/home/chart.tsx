'use client';

import { useStatsDaily, useStatsHourly, useStatsTotal } from '@/api/endpoints/stats';
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

type MetricsRow = {
    requests: Formatted;
    tokens: Formatted;
    waitTime: Formatted;
};

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
        const emptyMetrics: MetricsRow = {
            requests: formatCount(0).formatted,
            tokens: formatCount(0).formatted,
            waitTime: formatTime(0).formatted,
        };
        const emptyHero: HeroValue = { value: undefined, unit: '' };

        if (period === 'all') {
            // 累计档：优先使用 statsTotal；否则 fallback 到 statsDaily 全量聚合
            const points: ChartPoint[] = sortedDaily.map((stat) => ({
                date: dayjs(stat.date).format('MM/DD'),
                total_cost: stat.total_cost.raw,
            }));

            if (statsTotal) {
                return {
                    hero: {
                        value: statsTotal.total_cost.formatted.value,
                        unit: statsTotal.total_cost.formatted.unit,
                    },
                    metrics: {
                        requests: statsTotal.request_count.formatted,
                        tokens: statsTotal.total_token.formatted,
                        waitTime: statsTotal.wait_time.formatted,
                    },
                    chartData: points,
                };
            }

            if (sortedDaily.length === 0) {
                return { hero: emptyHero, metrics: emptyMetrics, chartData: [] };
            }

            const cost = sortedDaily.reduce((acc, s) => acc + s.total_cost.raw, 0);
            const requests = sortedDaily.reduce((acc, s) => acc + s.request_count.raw, 0);
            const tokens = sortedDaily.reduce((acc, s) => acc + s.total_token.raw, 0);
            const wait = sortedDaily.reduce((acc, s) => acc + s.wait_time.raw, 0);
            const costFmt = formatMoney(cost).formatted;
            return {
                hero: { value: costFmt.value, unit: costFmt.unit },
                metrics: {
                    requests: formatCount(requests).formatted,
                    tokens: formatCount(tokens).formatted,
                    waitTime: formatTime(wait).formatted,
                },
                chartData: points,
            };
        }

        if (period === '1') {
            // 今日档：聚合 statsHourly
            if (!statsHourly) {
                return { hero: emptyHero, metrics: emptyMetrics, chartData: [] };
            }
            const points: ChartPoint[] = statsHourly.map((stat) => ({
                date: `${stat.hour}:00`,
                total_cost: stat.total_cost.raw,
            }));
            const cost = statsHourly.reduce((acc, s) => acc + s.total_cost.raw, 0);
            const requests = statsHourly.reduce((acc, s) => acc + s.request_count.raw, 0);
            const tokens = statsHourly.reduce((acc, s) => acc + s.total_token.raw, 0);
            const wait = statsHourly.reduce((acc, s) => acc + s.wait_time.raw, 0);
            const costFmt = formatMoney(cost).formatted;
            return {
                hero: { value: costFmt.value, unit: costFmt.unit },
                metrics: {
                    requests: formatCount(requests).formatted,
                    tokens: formatCount(tokens).formatted,
                    waitTime: formatTime(wait).formatted,
                },
                chartData: points,
            };
        }

        // 7 / 30 天：聚合 statsDaily
        const days = Number(period);
        const recent = sortedDaily.slice(-days);
        const points: ChartPoint[] = recent.map((stat) => ({
            date: dayjs(stat.date).format('MM/DD'),
            total_cost: stat.total_cost.raw,
        }));

        if (recent.length === 0) {
            return { hero: emptyHero, metrics: emptyMetrics, chartData: [] };
        }

        const cost = recent.reduce((acc, s) => acc + s.total_cost.raw, 0);
        const requests = recent.reduce((acc, s) => acc + s.request_count.raw, 0);
        const tokens = recent.reduce((acc, s) => acc + s.total_token.raw, 0);
        const wait = recent.reduce((acc, s) => acc + s.wait_time.raw, 0);
        const costFmt = formatMoney(cost).formatted;
        return {
            hero: { value: costFmt.value, unit: costFmt.unit },
            metrics: {
                requests: formatCount(requests).formatted,
                tokens: formatCount(tokens).formatted,
                waitTime: formatTime(wait).formatted,
            },
            chartData: points,
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

            {/* Metrics row */}
            <div className="mx-5 flex items-baseline gap-6 border-t border-border/60 py-3 text-sm tabular-nums">
                <StatItem label={t('metrics.requests')} value={metrics.requests} />
                <span className="h-4 w-px bg-border/60" />
                <StatItem label={t('metrics.tokens')} value={metrics.tokens} />
                <span className="h-4 w-px bg-border/60" />
                <StatItem label={t('metrics.waitTime')} value={metrics.waitTime} />
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
        <div className="flex items-baseline gap-1.5">
            <span className="text-xs text-muted-foreground">{label}</span>
            <span className="font-medium">
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
