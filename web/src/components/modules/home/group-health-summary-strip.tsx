'use client';

import { useMemo } from 'react';
import { Activity, CheckCircle2, LoaderCircle, Play, XCircle } from 'lucide-react';
import { useTranslations } from 'next-intl';
import { Button } from '@/components/ui/button';
import { useGroupHealthEnabled } from '@/api/endpoints/setting';
import { useGroupHealthList, useRunAllGroupHealth } from '@/api/endpoints/group-health';

export function GroupHealthSummaryStrip() {
    const t = useTranslations('home.groupHealth');
    const { enabled } = useGroupHealthEnabled();
    const { data: views = [] } = useGroupHealthList();
    const runAllGroupHealth = useRunAllGroupHealth();

    const summary = useMemo(() => {
        const running = views.filter((view) => view.latest?.status === 'running').length;
        const failed = views.filter((view) => view.latest?.status === 'failed').length;
        const partial = views.filter((view) => view.latest?.status === 'partial').length;
        const success = views.filter((view) => view.latest?.status === 'success').length;
        const idle = views.filter((view) => !view.latest).length;
        return { running, failed, partial, success, idle, total: views.length };
    }, [views]);

    if (!enabled) return null;

    return (
        <section className="rounded-3xl bg-card border-card-border border text-card-foreground custom-shadow px-4 py-3">
            <div className="flex flex-col gap-3 md:flex-row md:items-center md:justify-between">
                <div className="min-w-0">
                    <div className="flex items-center gap-2 text-sm font-semibold">
                        <Activity className="size-4 text-primary" />
                        {t('title')}
                        <span className="text-xs font-normal text-muted-foreground">{t('total', { count: summary.total })}</span>
                    </div>
                    <div className="mt-1 flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted-foreground">
                        <span className="inline-flex items-center gap-1">
                            <LoaderCircle className="size-3.5" />
                            {t('running', { count: summary.running })}
                        </span>
                        <span className="inline-flex items-center gap-1 text-emerald-600 dark:text-emerald-400">
                            <CheckCircle2 className="size-3.5" />
                            {t('success', { count: summary.success })}
                        </span>
                        <span className="inline-flex items-center gap-1 text-amber-600 dark:text-amber-400">
                            <Activity className="size-3.5" />
                            {t('partial', { count: summary.partial })}
                        </span>
                        <span className="inline-flex items-center gap-1 text-destructive">
                            <XCircle className="size-3.5" />
                            {t('failed', { count: summary.failed })}
                        </span>
                        {summary.idle ? <span>{t('idle', { count: summary.idle })}</span> : null}
                    </div>
                </div>

                <Button
                    type="button"
                    size="sm"
                    variant="outline"
                    className="h-8 rounded-xl md:self-center"
                    onClick={() => runAllGroupHealth.mutate()}
                    disabled={runAllGroupHealth.isPending}
                >
                    {runAllGroupHealth.isPending ? <LoaderCircle className="size-4 animate-spin" /> : <Play className="size-4" />}
                    {t('runAll')}
                </Button>
            </div>
        </section>
    );
}
