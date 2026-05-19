'use client';

import { useMemo, useState } from 'react';
import { Activity, CheckCircle2, ChevronDown, Clock3, FolderTree, LoaderCircle, Play, Siren, XCircle } from 'lucide-react';
import { useTranslations } from 'next-intl';
import { useGroupHealthList, useRunAllGroupHealth, useRunGroupHealth, type GroupHealthGroupView } from '@/api/endpoints/group-health';
import { Button } from '@/components/ui/button';
import { Badge } from '@/components/ui/badge';
import { cn } from '@/lib/utils';
import { GroupHealthAttemptDetails } from '../group/health';

function formatDateTime(value?: string | null, fallback?: string) {
    if (!value) return fallback ?? '';
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) return fallback ?? '';
    return date.toLocaleString();
}

function statusTone(status?: string | null) {
    switch (status) {
        case 'success':
            return 'border-emerald-500/20 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300';
        case 'partial':
            return 'border-amber-500/20 bg-amber-500/10 text-amber-700 dark:text-amber-300';
        case 'running':
            return 'border-sky-500/20 bg-sky-500/10 text-sky-700 dark:text-sky-300';
        case 'failed':
        default:
            return 'border-destructive/20 bg-destructive/10 text-destructive';
    }
}

function probeModeTone(mode?: string | null) {
    return mode === 'full'
        ? 'border-amber-500/20 bg-amber-500/10 text-amber-700 dark:text-amber-300'
        : 'border-border bg-muted/40 text-muted-foreground';
}

function summarize(view: GroupHealthGroupView) {
    const attempts = view.latest?.attempts ?? [];
    const successCount = attempts.filter((attempt) => attempt.status === 'success').length;
    return {
        attempts,
        successCount,
    };
}

function GroupHealthCard({
    view,
    onRun,
    isRunningMutation,
}: {
    view: GroupHealthGroupView;
    onRun: (groupId: number, probeMode?: 'full') => void;
    isRunningMutation: boolean;
}) {
    const t = useTranslations('group.health');
    const [expanded, setExpanded] = useState(false);
    const { attempts, successCount } = summarize(view);
    const latest = view.latest;
    return (
        <article className="min-w-0 overflow-hidden rounded-3xl border border-border/70 bg-card p-3.5">
            <header className="flex items-start justify-between gap-3">
                <div className="min-w-0">
                    <div className="flex items-center gap-2">
                        <FolderTree className="size-4 text-primary" />
                        <h3 className="truncate text-sm font-semibold">{view.group_name}</h3>
                    </div>
                    <div className="mt-1 text-xs text-muted-foreground">
                        {t('lastRun', { time: formatDateTime(latest?.finished_at ?? latest?.started_at ?? null, t('never')) })}
                    </div>
                </div>
                <div className="flex shrink-0 flex-wrap items-center justify-end gap-1.5">
                    <Badge variant="outline" className={cn('h-6 px-2 text-[11px]', latest ? statusTone(latest.status) : 'border-border bg-muted/40 text-muted-foreground')}>
                        {t(`statusValue.${latest?.status ?? 'idle'}`)}
                    </Badge>
                    <Badge variant="outline" className={cn('h-6 px-2 text-[11px] uppercase tracking-wide', probeModeTone(latest?.probe_mode ?? 'standard'))}>
                        {t(`probeMode.${latest?.probe_mode ?? 'standard'}`)}
                    </Badge>
                    <Button
                        type="button"
                        size="sm"
                        variant="outline"
                        className="h-7 rounded-xl px-2 text-xs"
                        disabled={isRunningMutation || latest?.status === 'running'}
                        onClick={() => onRun(view.group_id)}
                    >
                        {latest?.status === 'running' ? <LoaderCircle className="size-4 animate-spin" /> : <Play className="size-4" />}
                        {t('run')}
                    </Button>
                    <Button
                        type="button"
                        size="sm"
                        variant="outline"
                        className="h-7 rounded-xl px-2 text-xs"
                        disabled={isRunningMutation || latest?.status === 'running'}
                        onClick={() => onRun(view.group_id, 'full')}
                    >
                        <Play className="size-4" />
                        {t('runFull')}
                    </Button>
                </div>
            </header>

            <div className="mt-3 flex flex-wrap items-center gap-3 text-xs">
                <span className="inline-flex items-center gap-1 text-muted-foreground">
                    <Activity className="size-4" />
                    {t('healthyCount', { success: successCount, total: attempts.length || 0 })}
                </span>
                <span className="inline-flex items-center gap-1 text-muted-foreground">
                    <Clock3 className="size-4" />
                    {latest?.duration_ms ?? 0}ms
                </span>
            </div>

            {attempts.length > 0 ? (
                <div className="mt-3 space-y-2">
                    <button
                        type="button"
                        className="flex w-full items-center justify-between rounded-2xl border border-border/60 bg-muted/20 px-3 py-2 text-left text-xs text-muted-foreground transition-colors hover:bg-muted/30"
                        onClick={() => setExpanded((value) => !value)}
                    >
                        <span>{t('attemptListToggle', { count: attempts.length })}</span>
                        <ChevronDown className={cn('size-4 shrink-0 transition-transform', expanded && 'rotate-180')} />
                    </button>

                    {expanded ? (
                        <div className="flex max-h-[22rem] flex-col gap-2 overflow-y-auto pr-1">
                            {attempts.map((attempt) => (
                                <GroupHealthAttemptDetails key={attempt.id} attempt={attempt} />
                            ))}
                        </div>
                    ) : null}
                </div>
            ) : (
                <div className="mt-3 rounded-2xl border border-dashed border-border/70 bg-muted/20 px-3 py-4 text-xs text-muted-foreground">
                    {t('empty')}
                </div>
            )}
        </article>
    );
}

export function GroupHealthOverview() {
    const tHome = useTranslations('home.groupHealth');
    const t = useTranslations('group.health');
    const { data: views = [] } = useGroupHealthList();
    const runGroupHealth = useRunGroupHealth();
    const runAllGroupHealth = useRunAllGroupHealth();

    const summary = useMemo(() => {
        const running = views.filter((view) => view.latest?.status === 'running').length;
        const failed = views.filter((view) => view.latest?.status === 'failed').length;
        const partial = views.filter((view) => view.latest?.status === 'partial').length;
        const success = views.filter((view) => view.latest?.status === 'success').length;
        return { running, failed, partial, success };
    }, [views]);

    return (
        <section className="flex h-full min-h-0 flex-col space-y-4">
            <header className="flex shrink-0 flex-col gap-3 md:flex-row md:items-center md:justify-between">
                <div>
                    <div className="flex items-center gap-2 text-lg font-semibold">
                        <Siren className="size-5 text-primary" />
                        {tHome('title')}
                    </div>
                    <div className="mt-1 flex flex-wrap gap-2 text-xs text-muted-foreground">
                        <span className="inline-flex items-center gap-1"><LoaderCircle className="size-3.5" />{tHome('running', { count: summary.running })}</span>
                        <span className="inline-flex items-center gap-1"><CheckCircle2 className="size-3.5" />{tHome('success', { count: summary.success })}</span>
                        <span className="inline-flex items-center gap-1"><Activity className="size-3.5" />{tHome('partial', { count: summary.partial })}</span>
                        <span className="inline-flex items-center gap-1"><XCircle className="size-3.5" />{tHome('failed', { count: summary.failed })}</span>
                    </div>
                </div>

                <div className="flex flex-wrap items-center gap-2">
                    <Button
                        type="button"
                        className="h-8 rounded-2xl px-3 text-xs"
                        onClick={() => runAllGroupHealth.mutate({})}
                        disabled={runAllGroupHealth.isPending}
                    >
                        {runAllGroupHealth.isPending ? <LoaderCircle className="size-4 animate-spin" /> : <Play className="size-4" />}
                        {tHome('runAll')}
                    </Button>
                    <Button
                        type="button"
                        variant="outline"
                        className="h-8 rounded-2xl px-3 text-xs"
                        onClick={() => runAllGroupHealth.mutate({ probeMode: 'full' })}
                        disabled={runAllGroupHealth.isPending}
                    >
                        {runAllGroupHealth.isPending ? <LoaderCircle className="size-4 animate-spin" /> : <Play className="size-4" />}
                        {tHome('runAllFull')}
                    </Button>
                </div>
            </header>

            <div className="min-h-0 flex-1 overflow-y-auto pr-1">
                <div className="flex flex-col gap-3">
                    {views.map((view) => (
                        <GroupHealthCard
                            key={view.group_id}
                            view={view}
                            onRun={(groupId, probeMode) => runGroupHealth.mutate({ groupId, probeMode })}
                            isRunningMutation={runGroupHealth.isPending && runGroupHealth.variables?.groupId === view.group_id}
                        />
                    ))}
                </div>
                {!views.length ? (
                    <div className="mt-3 rounded-2xl border border-dashed border-border/70 bg-muted/20 px-3 py-6 text-center text-xs text-muted-foreground">
                        {t('empty')}
                    </div>
                ) : null}
            </div>
        </section>
    );
}
