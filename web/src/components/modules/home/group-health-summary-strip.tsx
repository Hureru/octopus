'use client';

import { useMemo, useState } from 'react';
import { Activity, CheckCircle2, ChevronRight, LoaderCircle, Play, XCircle } from 'lucide-react';
import { useTranslations } from 'next-intl';
import { Button } from '@/components/ui/button';
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle, DialogTrigger } from '@/components/ui/dialog';
import { useGroupHealthEnabled } from '@/api/endpoints/setting';
import { useGroupHealthList, useRunAllGroupHealth } from '@/api/endpoints/group-health';
import { GroupHealthOverview } from './group-health-overview';

export function GroupHealthSummaryStrip() {
    const t = useTranslations('home.groupHealth');
    const { enabled } = useGroupHealthEnabled();
    const { data: views = [] } = useGroupHealthList();
    const runAllGroupHealth = useRunAllGroupHealth();
    const [open, setOpen] = useState(false);

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
        <Dialog open={open} onOpenChange={setOpen}>
            <section className="rounded-3xl bg-card border-card-border border text-card-foreground custom-shadow px-4 py-3">
                <div className="flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between">
                    <div className="min-w-0">
                        <div className="flex items-center gap-2 text-sm font-semibold">
                            <Activity className="size-4 text-primary" />
                            {t('title')}
                            <span className="truncate text-xs font-normal text-muted-foreground">{t('total', { count: summary.total })}</span>
                        </div>
                        <div className="mt-1 flex flex-wrap gap-x-3 gap-y-1 text-xs text-muted-foreground">
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

                    <div className="flex flex-wrap items-center gap-2">
                        <DialogTrigger asChild>
                            <Button type="button" size="sm" variant="ghost" className="h-8 rounded-xl px-2.5 text-xs">
                                {t('viewDetails')}
                                <ChevronRight className="size-4" />
                            </Button>
                        </DialogTrigger>
                        <Button
                            type="button"
                            size="sm"
                            variant="outline"
                            className="h-8 rounded-xl text-xs"
                            onClick={() => runAllGroupHealth.mutate({})}
                            disabled={runAllGroupHealth.isPending}
                        >
                            {runAllGroupHealth.isPending ? <LoaderCircle className="size-4 animate-spin" /> : <Play className="size-4" />}
                            {t('runAll')}
                        </Button>
                        <Button
                            type="button"
                            size="sm"
                            variant="outline"
                            className="h-8 rounded-xl text-xs"
                            onClick={() => runAllGroupHealth.mutate({ probeMode: 'full' })}
                            disabled={runAllGroupHealth.isPending}
                        >
                            {runAllGroupHealth.isPending ? <LoaderCircle className="size-4 animate-spin" /> : <Play className="size-4" />}
                            {t('runAllFull')}
                        </Button>
                    </div>
                </div>
            </section>

            <DialogContent className="flex h-[min(88vh,52rem)] max-w-[min(1100px,calc(100vw-1.5rem))] flex-col overflow-hidden rounded-3xl border-border/70 p-0 sm:max-w-[min(1100px,calc(100vw-1.5rem))]">
                <DialogHeader className="shrink-0 border-b border-border/60 px-5 py-4">
                    <DialogTitle>{t('detailTitle')}</DialogTitle>
                    <DialogDescription>{t('detailDescription')}</DialogDescription>
                </DialogHeader>
                <div className="min-h-0 flex-1 overflow-hidden p-4">
                    <GroupHealthOverview />
                </div>
            </DialogContent>
        </Dialog>
    );
}
