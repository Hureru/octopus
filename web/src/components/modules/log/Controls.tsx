'use client';

import dayjs from 'dayjs';
import timezonePlugin from 'dayjs/plugin/timezone';
import utc from 'dayjs/plugin/utc';
import { useTranslations } from 'next-intl';
import { X } from 'lucide-react';
import type {
    UsageAnalyticsFilters,
    UsageDimension,
    UsageMetricScope,
} from '@/api/endpoints/log-analytics';
import { useSiteList } from '@/api/endpoints/site';
import { Tabs, TabsList, TabsTrigger } from '@/components/animate-ui/components/animate/tabs';
import { useToolbarViewOptionsStore } from '@/components/modules/toolbar/view-options-store';
import { cn } from '@/lib/utils';
import { useLogAnalyticsStore, type LogView } from './analytics-store';
import { DimensionPicker } from './DimensionPicker';
import { HistoricalRepairDialog } from './RepairDialog';

dayjs.extend(utc);
dayjs.extend(timezonePlugin);

const dimensions: UsageDimension[] = [
    'site',
    'site_account',
    'channel',
    'api_key',
    'request_model',
    'actual_model',
    'canonical_model',
];

export function LogControls() {
    const t = useTranslations('log.analytics');
    const { data: sites } = useSiteList();
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
    const setScope = useLogAnalyticsStore((state) => state.setScope);
    const setDimension = useLogAnalyticsStore((state) => state.setDimension);
    const setTimezone = useLogAnalyticsStore((state) => state.setTimezone);
    const setSiteIds = useLogAnalyticsStore((state) => state.setSiteIds);
    const setSiteAccountIds = useLogAnalyticsStore((state) => state.setSiteAccountIds);
    const setAPIKeyIds = useLogAnalyticsStore((state) => state.setAPIKeyIds);
    const setRequestModels = useLogAnalyticsStore((state) => state.setRequestModels);
    const setActualModels = useLogAnalyticsStore((state) => state.setActualModels);
    const setCanonicalModels = useLogAnalyticsStore((state) => state.setCanonicalModels);
    const clearBusinessFilters = useLogAnalyticsStore((state) => state.clearBusinessFilters);
    const logDateRange = useToolbarViewOptionsStore((state) => state.logDateRange);
    const logChannelIds = useToolbarViewOptionsStore((state) => state.logChannelIds);
    const setLogDateRange = useToolbarViewOptionsStore((state) => state.setLogDateRange);

    const selectedSite = siteIds[0] ?? 0;
    const accounts = selectedSite > 0
        ? sites?.find((site) => site.id === selectedSite)?.accounts ?? []
        : (sites ?? []).flatMap((site) => site.accounts);
    const timezoneOptions = Array.from(
        new Set([timezone, 'UTC', 'Asia/Shanghai', 'America/New_York', 'Europe/London']),
    );
    const hasBusinessFilters =
        siteIds.length > 0 ||
        siteAccountIds.length > 0 ||
        apiKeyIds.length > 0 ||
        requestModels.length > 0 ||
        actualModels.length > 0 ||
        canonicalModels.length > 0;
    const dimensionFilters: UsageAnalyticsFilters = {
        start_time: logDateRange.start,
        end_time: logDateRange.end,
        timezone,
        metric_scope: scope,
        site_ids: siteIds.length ? siteIds : undefined,
        site_account_ids: siteAccountIds.length ? siteAccountIds : undefined,
        channel_ids: logChannelIds.length ? logChannelIds : undefined,
        api_key_ids: apiKeyIds.length ? apiKeyIds : undefined,
        request_models: requestModels.length ? requestModels : undefined,
        actual_models: actualModels.length ? actualModels : undefined,
        canonical_models: canonicalModels.length ? canonicalModels : undefined,
    };

    const applyPreset = (preset: 'today' | 'yesterday' | '7d' | '30d' | 'month') => {
        const now = dayjs().tz(timezone);
        switch (preset) {
            case 'yesterday': {
                const value = now.subtract(1, 'day');
                setLogDateRange({
                    start: value.startOf('day').unix(),
                    end: value.endOf('day').unix(),
                });
                break;
            }
            case '7d':
                setLogDateRange({ start: now.subtract(6, 'day').startOf('day').unix(), end: now.unix() });
                break;
            case '30d':
                setLogDateRange({ start: now.subtract(29, 'day').startOf('day').unix(), end: now.unix() });
                break;
            case 'month':
                setLogDateRange({ start: now.startOf('month').unix(), end: now.unix() });
                break;
            default:
                setLogDateRange({ start: now.startOf('day').unix(), end: now.unix() });
        }
    };

    return (
        <div className="flex shrink-0 flex-col gap-2 border-b border-border/70 pb-3">
            <div className="flex flex-wrap items-center justify-between gap-2">
                <Tabs value={view} onValueChange={(value) => setView(value as LogView)}>
                    <TabsList>
                        <TabsTrigger value="analytics">{t('view.analytics')}</TabsTrigger>
                        <TabsTrigger value="detail">{t('view.detail')}</TabsTrigger>
                    </TabsList>
                </Tabs>
                <div className="flex flex-wrap items-center gap-1">
                    {(['today', 'yesterday', '7d', '30d', 'month'] as const).map((preset) => (
                        <button
                            key={preset}
                            type="button"
                            onClick={() => applyPreset(preset)}
                            className="h-8 rounded-md border border-border/70 px-2.5 text-[11px] text-muted-foreground hover:bg-muted/50 hover:text-foreground"
                        >
                            {t(`range.${preset}`)}
                        </button>
                    ))}
                </div>
            </div>

            <div className="flex flex-wrap items-center gap-2">
                {view === 'analytics' ? (
                    <>
                        <select
                            value={scope}
                            onChange={(event) => setScope(event.target.value as UsageMetricScope)}
                            className={selectClassName}
                            aria-label={t('scope.label')}
                        >
                            <option value="request">{t('scope.request')}</option>
                            <option value="attempt">{t('scope.attempt')}</option>
                        </select>
                        <select
                            value={dimension}
                            onChange={(event) => setDimension(event.target.value as UsageDimension)}
                            className={selectClassName}
                            aria-label={t('dimension.label')}
                        >
                            {dimensions.map((value) => (
                                <option key={value} value={value}>
                                    {t(`dimension.${value}`)}
                                </option>
                            ))}
                        </select>
                    </>
                ) : null}
                <select
                    value={selectedSite}
                    onChange={(event) => setSiteIds(Number(event.target.value) > 0 ? [Number(event.target.value)] : [])}
                    className={selectClassName}
                    aria-label={t('filters.site')}
                >
                    <option value={0}>{t('filters.allSites')}</option>
                    {(sites ?? []).map((site) => (
                        <option key={site.id} value={site.id}>{site.name}</option>
                    ))}
                </select>
                <select
                    value={siteAccountIds[0] ?? 0}
                    onChange={(event) => setSiteAccountIds(Number(event.target.value) > 0 ? [Number(event.target.value)] : [])}
                    className={selectClassName}
                    aria-label={t('filters.account')}
                >
                    <option value={0}>{t('filters.allAccounts')}</option>
                    {accounts.map((account) => (
                        <option key={account.id} value={account.id}>{account.name}</option>
                    ))}
                </select>
                <select
                    value={timezone}
                    onChange={(event) => setTimezone(event.target.value)}
                    className={cn(selectClassName, 'max-w-48')}
                    aria-label={t('filters.timezone')}
                >
                    {timezoneOptions.map((value) => (
                        <option key={value} value={value}>{value}</option>
                    ))}
                </select>
                <DimensionPicker
                    filters={withoutDimensionFilter(dimensionFilters, 'api_key')}
                    dimension="api_key"
                    selected={apiKeyIds[0] ?? null}
                    selectedLabel={apiKeyIds[0] ? `API Key #${apiKeyIds[0]}` : undefined}
                    onSelect={(option) => setAPIKeyIds(option && option.id > 0 ? [option.id] : [])}
                />
                <DimensionPicker
                    filters={withoutDimensionFilter(dimensionFilters, 'request_model')}
                    dimension="request_model"
                    selected={requestModels[0] ?? null}
                    selectedLabel={requestModels[0]}
                    onSelect={(option) => setRequestModels(option ? [option.name] : [])}
                />
                <DimensionPicker
                    filters={withoutDimensionFilter(dimensionFilters, 'actual_model')}
                    dimension="actual_model"
                    selected={actualModels[0] ?? null}
                    selectedLabel={actualModels[0]}
                    onSelect={(option) => setActualModels(option ? [option.name] : [])}
                />
                <DimensionPicker
                    filters={withoutDimensionFilter(dimensionFilters, 'canonical_model')}
                    dimension="canonical_model"
                    selected={canonicalModels[0] ?? null}
                    selectedLabel={canonicalModels[0]}
                    onSelect={(option) => setCanonicalModels(option ? [option.name] : [])}
                />
                {hasBusinessFilters ? (
                    <button
                        type="button"
                        onClick={clearBusinessFilters}
                        className="flex size-8 items-center justify-center rounded-md border border-border/70 text-muted-foreground hover:bg-muted/50 hover:text-foreground"
                        title={t('filters.clear')}
                        aria-label={t('filters.clear')}
                    >
                        <X className="size-3.5" />
                    </button>
                ) : null}
                <HistoricalRepairDialog startTime={logDateRange.start} endTime={logDateRange.end} />
            </div>
        </div>
    );
}

const selectClassName =
    'h-8 max-w-44 rounded-md border border-border/70 bg-background px-2 text-[11px] text-foreground outline-none focus:ring-1 focus:ring-ring';

function withoutDimensionFilter(
    filters: UsageAnalyticsFilters,
    dimension: UsageDimension,
): UsageAnalyticsFilters {
    const next = { ...filters };
    switch (dimension) {
        case 'site':
            delete next.site_ids;
            break;
        case 'site_account':
            delete next.site_account_ids;
            break;
        case 'channel':
            delete next.channel_ids;
            break;
        case 'api_key':
            delete next.api_key_ids;
            break;
        case 'request_model':
            delete next.request_models;
            break;
        case 'actual_model':
            delete next.actual_models;
            break;
        case 'canonical_model':
            delete next.canonical_models;
            break;
    }
    return next;
}
