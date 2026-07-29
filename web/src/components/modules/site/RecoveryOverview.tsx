'use client';

import { useMemo } from 'react';
import { useTranslations } from 'next-intl';
import { Clock3, Route, Trash2 } from 'lucide-react';
import type { Site, SiteAccount } from '@/api/endpoints/site';
import type { ProxyConfiguration } from '@/api/endpoints/proxy-pool';
import type { SiteProxyPreference } from '@/api/endpoints/site-recovery';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { cn } from '@/lib/utils';

type RecoveryOverviewProps = {
    site: Site;
    account: SiteAccount;
    preferences: SiteProxyPreference[];
    proxies: ProxyConfiguration[];
    isLoading: boolean;
    error: unknown;
    onClearAccountPreference: () => void;
    onClearSitePreference: () => void;
    clearingAccount: boolean;
    clearingSite: boolean;
};

function formatDateTime(value?: string | null) {
    if (!value) return null;
    const date = new Date(value);
    return Number.isNaN(date.getTime()) ? null : date.toLocaleString();
}

function errorMessage(error: unknown, fallback: string) {
    if (error instanceof Error) return error.message;
    if (typeof error === 'object' && error !== null && 'message' in error) {
        const message = (error as { message?: unknown }).message;
        if (typeof message === 'string') return message;
    }
    return fallback;
}

export function RecoveryOverview({
    site,
    account,
    preferences,
    proxies,
    isLoading,
    error,
    onClearAccountPreference,
    onClearSitePreference,
    clearingAccount,
    clearingSite,
}: RecoveryOverviewProps) {
    const t = useTranslations('siteRecovery');
    const tProxy = useTranslations('proxyPool');
    const proxyNames = useMemo(
        () => new Map(proxies.map((proxy) => [proxy.id, proxy.name])),
        [proxies],
    );
    const effectiveEnabled = account.auto_proxy_recovery ?? site.auto_proxy_recovery;
    const accountPreferenceCount = preferences.filter(
        (item) => item.site_account_id === account.id,
    ).length;
    const sitePreferenceCount = preferences.filter(
        (item) => item.site_account_id === 0,
    ).length;

    function pathLabel(preference: SiteProxyPreference) {
        if (preference.proxy_mode !== 'pool') {
            return tProxy(`mode.${preference.proxy_mode}`);
        }
        const proxyName =
            proxyNames.get(preference.proxy_config_id) ??
            t('overview.proxyId', { id: preference.proxy_config_id });
        return preference.clash_node
            ? `${proxyName} / ${preference.clash_node}`
            : proxyName;
    }

    return (
        <div className="space-y-5">
            <section className="grid gap-3 border-b border-border/60 pb-5 sm:grid-cols-3">
                <div>
                    <div className="text-xs text-muted-foreground">
                        {t('overview.effectivePolicy')}
                    </div>
                    <div className="mt-1 flex items-center gap-2">
                        <Badge
                            variant={effectiveEnabled ? 'default' : 'secondary'}
                            className={cn(
                                effectiveEnabled &&
                                    'bg-emerald-600 text-white dark:bg-emerald-700',
                            )}
                        >
                            {effectiveEnabled
                                ? t('overview.enabled')
                                : t('overview.disabled')}
                        </Badge>
                        <span className="text-xs text-muted-foreground">
                            {account.auto_proxy_recovery === null ||
                            account.auto_proxy_recovery === undefined
                                ? t('overview.inherited')
                                : t('overview.accountOverride')}
                        </span>
                    </div>
                </div>
                <div>
                    <div className="text-xs text-muted-foreground">
                        {t('overview.sitePreference')}
                    </div>
                    <div className="mt-1 text-sm font-medium">
                        {sitePreferenceCount > 0
                            ? t('overview.pathCount', { count: sitePreferenceCount })
                            : t('overview.none')}
                    </div>
                </div>
                <div>
                    <div className="text-xs text-muted-foreground">
                        {t('overview.accountPreference')}
                    </div>
                    <div className="mt-1 text-sm font-medium">
                        {accountPreferenceCount > 0
                            ? t('overview.pathCount', { count: accountPreferenceCount })
                            : t('overview.none')}
                    </div>
                </div>
            </section>

            <section>
                <div className="mb-3 flex flex-wrap items-center justify-between gap-3">
                    <div>
                        <h3 className="text-sm font-semibold">
                            {t('overview.learnedPaths')}
                        </h3>
                        <p className="text-xs text-muted-foreground">
                            {t('overview.learnedPathsHint')}
                        </p>
                    </div>
                    <div className="flex flex-wrap gap-2">
                        <Button
                            type="button"
                            size="sm"
                            variant="outline"
                            className="rounded-xl"
                            onClick={onClearAccountPreference}
                            disabled={clearingAccount || accountPreferenceCount === 0}
                        >
                            <Trash2 className="size-4" />
                            {t('overview.clearAccount')}
                        </Button>
                        <Button
                            type="button"
                            size="sm"
                            variant="outline"
                            className="rounded-xl"
                            onClick={onClearSitePreference}
                            disabled={clearingSite || sitePreferenceCount === 0}
                        >
                            <Trash2 className="size-4" />
                            {t('overview.clearSite')}
                        </Button>
                    </div>
                </div>

                {isLoading ? (
                    <div className="border-y border-border/60 py-6 text-sm text-muted-foreground">
                        {t('loading')}
                    </div>
                ) : error ? (
                    <div className="break-words border-y border-destructive/30 bg-destructive/5 px-3 py-4 text-sm text-destructive">
                        {errorMessage(error, t('operationFailed'))}
                    </div>
                ) : preferences.length === 0 ? (
                    <div className="border-y border-dashed border-border/70 py-8 text-center text-sm text-muted-foreground">
                        {t('overview.empty')}
                    </div>
                ) : (
                    <div className="divide-y divide-border/60 border-y border-border/60">
                        {preferences.map((preference) => {
                            const lastSuccess = formatDateTime(
                                preference.last_success_at,
                            );
                            const cooldown = formatDateTime(
                                preference.cooldown_until,
                            );
                            return (
                                <div
                                    key={preference.id}
                                    className="grid gap-3 py-4 sm:grid-cols-[minmax(0,1fr)_auto]"
                                >
                                    <div className="min-w-0">
                                        <div className="flex flex-wrap items-center gap-2">
                                            <Route className="size-4 text-muted-foreground" />
                                            <span className="min-w-0 break-words text-sm font-medium">
                                                {pathLabel(preference)}
                                            </span>
                                            <Badge variant="outline">
                                                {preference.site_account_id === 0
                                                    ? t('overview.siteScope')
                                                    : t('overview.accountScope')}
                                            </Badge>
                                            <Badge
                                                variant="outline"
                                                className={cn(
                                                    preference.status === 'healthy' &&
                                                        'border-emerald-500/40 text-emerald-600',
                                                    preference.status === 'cooling' &&
                                                        'border-amber-500/40 text-amber-600',
                                                    preference.status === 'stale' &&
                                                        'text-muted-foreground',
                                                    preference.status === 'disabled' &&
                                                        'border-destructive/40 text-destructive',
                                                )}
                                            >
                                                {t(`preferenceStatus.${preference.status}`)}
                                            </Badge>
                                        </div>
                                        <div className="mt-2 flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted-foreground">
                                            <span>
                                                {t('overview.successFailure', {
                                                    success: preference.success_count,
                                                    failure: preference.failure_count,
                                                })}
                                            </span>
                                            <span>
                                                {t('overview.averageLatency', {
                                                    value: Math.round(
                                                        preference.average_latency_ms,
                                                    ),
                                                })}
                                            </span>
                                            {lastSuccess ? (
                                                <span>
                                                    {t('overview.lastSuccess', {
                                                        value: lastSuccess,
                                                    })}
                                                </span>
                                            ) : null}
                                        </div>
                                    </div>
                                    {cooldown ? (
                                        <div className="flex items-center gap-1 text-xs text-amber-600">
                                            <Clock3 className="size-3.5" />
                                            {t('overview.cooldownUntil', {
                                                value: cooldown,
                                            })}
                                        </div>
                                    ) : null}
                                </div>
                            );
                        })}
                    </div>
                )}
            </section>
        </div>
    );
}
