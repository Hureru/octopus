'use client';

import { useMemo, useState } from 'react';
import { Activity, ChevronDown, Clock3, LoaderCircle, Play } from 'lucide-react';
import { useLocale, useTranslations } from 'next-intl';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Card, CardContent } from '@/components/ui/card';
import {
    Dialog,
    DialogContent,
    DialogDescription,
    DialogHeader,
    DialogTitle,
    DialogTrigger,
} from '@/components/ui/dialog';
import { cn } from '@/lib/utils';
import { useGroupHealthEnabled } from '@/api/endpoints/setting';
import {
    useGroupHealthList,
    useRunGroupHealth,
    type GroupHealthAttempt,
    type GroupHealthAttemptStatus,
    type GroupHealthProbeMode,
    type GroupHealthStatus,
} from '@/api/endpoints/group-health';

function formatDateTime(value?: string | null) {
    if (!value) return 'Never';
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) return 'Never';
    return date.toLocaleString();
}

function formatRelativeTime(value: string | null | undefined, locale: string, fallback: string) {
    if (!value) return fallback;
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) return fallback;

    const diffSeconds = Math.round((date.getTime() - Date.now()) / 1000);
    const absSeconds = Math.abs(diffSeconds);
    const formatter = new Intl.RelativeTimeFormat(locale, { numeric: 'always' });

    if (absSeconds < 60) return formatter.format(diffSeconds, 'second');
    const diffMinutes = Math.round(diffSeconds / 60);
    if (Math.abs(diffMinutes) < 60) return formatter.format(diffMinutes, 'minute');
    const diffHours = Math.round(diffMinutes / 60);
    if (Math.abs(diffHours) < 24) return formatter.format(diffHours, 'hour');
    const diffDays = Math.round(diffHours / 24);
    return formatter.format(diffDays, 'day');
}

function statusLabel(status?: GroupHealthStatus | null) {
    return status ?? 'idle';
}

function statusDotTone(status?: GroupHealthStatus | null) {
    switch (status) {
        case 'success':
            return 'bg-emerald-500';
        case 'partial':
            return 'bg-amber-500';
        case 'running':
            return 'bg-sky-500 animate-pulse';
        case 'failed':
            return 'bg-destructive';
        default:
            return 'bg-muted-foreground/40';
    }
}

function statusTextTone(status?: GroupHealthStatus | null) {
    switch (status) {
        case 'success':
            return 'text-emerald-600 dark:text-emerald-400';
        case 'partial':
            return 'text-amber-600 dark:text-amber-400';
        case 'running':
            return 'text-sky-600 dark:text-sky-400';
        case 'failed':
            return 'text-destructive';
        default:
            return 'text-muted-foreground';
    }
}

function probeModeTone(mode?: GroupHealthProbeMode | null) {
    return mode === 'full'
        ? 'border-amber-500/20 bg-amber-500/10 text-amber-700 dark:text-amber-300'
        : 'border-border bg-muted/40 text-muted-foreground';
}

function attemptBadgeTone(status: GroupHealthAttemptStatus) {
    switch (status) {
        case 'success':
            return 'border-emerald-500/20 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300';
        case 'skipped':
            return 'border-border bg-muted/40 text-muted-foreground';
        case 'failed':
        default:
            return 'border-destructive/20 bg-destructive/10 text-destructive';
    }
}

export function GroupHealthAttemptDetails({ attempt }: { attempt: GroupHealthAttempt }) {
    const t = useTranslations('group.health');
    const hasError = Boolean(attempt.error_message);

    const content = (
        <div className="grid grid-cols-[1rem_minmax(0,1fr)_auto] items-start gap-x-2 text-xs">
            <div className="flex h-5 items-center justify-center text-muted-foreground">
                {hasError ? <ChevronDown className="size-3.5 transition-transform group-open:rotate-180" /> : null}
            </div>
            <div className="min-w-0">
                <div className="truncate font-medium leading-5">
                    {attempt.channel_name}
                    {attempt.key_remark ? ` / ${attempt.key_remark}` : ''}
                </div>
                <div className="mt-1 flex min-w-0 items-center gap-2 overflow-hidden whitespace-nowrap leading-4 text-muted-foreground">
                    <span className="shrink-0">{attempt.http_status ? `HTTP ${attempt.http_status}` : t('noHttpStatus')}</span>
                    <span className="shrink-0">·</span>
                    <span className="shrink-0">{attempt.duration_ms}ms</span>
                    {attempt.model_name ? <><span className="shrink-0">·</span><span className="min-w-0 truncate">{attempt.model_name}</span></> : null}
                </div>
            </div>
            <Badge variant="outline" className={cn('shrink-0 text-[11px]', attemptBadgeTone(attempt.status))}>
                {t(`attemptStatus.${attempt.status}`)}
            </Badge>
        </div>
    );

    if (!hasError) {
        return (
            <Card className="gap-0 rounded-2xl border-border/60 bg-card/80 py-0 shadow-xs transition-[border-color,box-shadow] hover:border-border hover:shadow-sm">
                <CardContent className="px-3 py-2 text-xs">
                    {content}
                </CardContent>
            </Card>
        );
    }

    return (
        <Card className="gap-0 rounded-2xl border-border/60 bg-card/80 py-0 shadow-xs transition-[border-color,box-shadow] hover:border-border hover:shadow-sm">
            <details className="group">
                <summary className="cursor-pointer list-none px-3 py-2 text-xs [&::-webkit-details-marker]:hidden">
                    {content}
                </summary>
                <div className="mx-3 mb-2 ml-9 max-h-36 overflow-y-auto whitespace-pre-wrap break-all border-t border-border/60 pt-2 text-xs leading-relaxed text-muted-foreground">
                    <div className="mb-1 font-medium text-foreground">{t('errorDetails')}</div>
                    {attempt.error_message}
                </div>
            </details>
        </Card>
    );
}

export function GroupHealthBadge({ groupId }: { groupId?: number }) {
    const t = useTranslations('group.health');
    const locale = useLocale();
    const { enabled } = useGroupHealthEnabled();
    const { data: views = [] } = useGroupHealthList();
    const runGroupHealth = useRunGroupHealth();
    const [open, setOpen] = useState(false);

    const view = useMemo(
        () => views.find((item) => item.group_id === groupId),
        [groupId, views]
    );
    const latest = view?.latest ?? null;
    const attempts = latest?.attempts ?? [];
    const successCount = attempts.filter((attempt) => attempt.status === 'success').length;

    if (!enabled || !groupId) return null;

    const isRunning = latest?.status === 'running';
    const isRunPendingForGroup = runGroupHealth.isPending
        && runGroupHealth.variables?.groupId === groupId;
    const isStandardRunPending = isRunPendingForGroup
        && (runGroupHealth.variables?.probeMode ?? 'standard') === 'standard';
    const isFullRunPending = isRunPendingForGroup
        && runGroupHealth.variables?.probeMode === 'full';
    const lastRunRelative = formatRelativeTime(latest?.finished_at ?? latest?.started_at ?? null, locale, t('never'));

    return (
        <Dialog open={open} onOpenChange={setOpen}>
            <Card className="mb-3 gap-0 rounded-xl border-border/70 bg-background/80 py-0 shadow-none">
                <CardContent className="flex items-center justify-between gap-2 px-3 py-1.5">
                    <DialogTrigger asChild>
                        <button type="button" className="grid min-w-0 flex-1 grid-cols-[auto_auto_minmax(0,1fr)] items-center gap-x-2 gap-y-0.5 text-left">
                            <span className={cn('row-span-2 size-2 rounded-full self-center', statusDotTone(latest?.status))} />
                            <span className="text-sm font-medium leading-5 text-foreground">{t('title')}</span>
                            <span className="min-w-0 truncate text-xs leading-5 text-muted-foreground">
                                {lastRunRelative}
                            </span>
                            <span className="col-start-2 col-span-2 flex min-w-0 items-center gap-3 text-xs leading-4 text-muted-foreground">
                                <Badge variant="outline" className={cn('h-5 px-1.5 text-[10px] uppercase tracking-wide', probeModeTone(latest?.probe_mode ?? 'standard'))}>
                                    {t(`probeMode.${latest?.probe_mode ?? 'standard'}`)}
                                </Badge>
                                <span className="inline-flex items-center gap-1">
                                    <Activity className="size-3.5" />
                                    {successCount}/{attempts.length || 0}
                                </span>
                                <span className="inline-flex items-center gap-1">
                                    <Clock3 className="size-3.5" />
                                    {latest?.duration_ms ?? 0}ms
                                </span>
                            </span>
                        </button>
                    </DialogTrigger>
                    <Button
                        type="button"
                        size="sm"
                        variant="outline"
                        className="h-7 rounded-lg px-2 text-xs"
                        disabled={isRunPendingForGroup || isRunning}
                        onClick={() => runGroupHealth.mutate({ groupId })}
                    >
                        {isRunning || isStandardRunPending ? <LoaderCircle className="size-3.5 animate-spin" /> : <Play className="size-3.5" />}
                        {t('run')}
                    </Button>
                    <Button
                        type="button"
                        size="sm"
                        variant="outline"
                        className="h-7 rounded-lg px-2 text-xs"
                        disabled={isRunPendingForGroup || isRunning}
                        onClick={() => runGroupHealth.mutate({ groupId, probeMode: 'full' })}
                    >
                        {isFullRunPending ? <LoaderCircle className="size-3.5 animate-spin" /> : <Play className="size-3.5" />}
                        {t('runFull')}
                    </Button>
                </CardContent>
            </Card>

            <DialogContent className="flex h-[min(85vh,42rem)] flex-col overflow-hidden rounded-3xl sm:max-w-2xl">
                <DialogHeader>
                    <DialogTitle className="flex items-center gap-2">
                        <span className={cn('size-2.5 rounded-full', statusDotTone(latest?.status))} />
                        {t('detailTitle')}
                    </DialogTitle>
                    <DialogDescription>
                        {t('lastRun', { time: formatDateTime(latest?.finished_at ?? latest?.started_at ?? null) })}
                    </DialogDescription>
                </DialogHeader>

                <div className="grid grid-cols-2 gap-2 text-sm md:grid-cols-4">
                    <Card className="gap-0 rounded-2xl border-border/60 bg-card/80 py-0 shadow-xs">
                        <CardContent className="p-3">
                            <div className="text-xs text-muted-foreground">{t('status')}</div>
                            <div className={cn('mt-1 font-medium', statusTextTone(latest?.status))}>{t(`statusValue.${statusLabel(latest?.status)}`)}</div>
                            <Badge variant="outline" className={cn('mt-2 h-5 px-1.5 text-[10px] uppercase tracking-wide', probeModeTone(latest?.probe_mode ?? 'standard'))}>
                                {t(`probeMode.${latest?.probe_mode ?? 'standard'}`)}
                            </Badge>
                        </CardContent>
                    </Card>
                    <Card className="gap-0 rounded-2xl border-border/60 bg-card/80 py-0 shadow-xs">
                        <CardContent className="p-3">
                            <div className="text-xs text-muted-foreground">{t('healthy')}</div>
                            <div className="mt-1 font-medium">{successCount}/{attempts.length || 0}</div>
                        </CardContent>
                    </Card>
                    <Card className="gap-0 rounded-2xl border-border/60 bg-card/80 py-0 shadow-xs">
                        <CardContent className="p-3">
                            <div className="text-xs text-muted-foreground">{t('duration')}</div>
                            <div className="mt-1 font-medium">{latest?.duration_ms ?? 0}ms</div>
                        </CardContent>
                    </Card>
                    <Card className="gap-0 rounded-2xl border-border/60 bg-card/80 py-0 shadow-xs">
                        <CardContent className="p-3">
                            <div className="text-xs text-muted-foreground">{t('attempts')}</div>
                            <div className="mt-1 font-medium">{attempts.length}</div>
                        </CardContent>
                    </Card>
                </div>

                <div className="min-h-0 flex-1 space-y-2 overflow-y-auto pr-1">
                    {attempts.length ? attempts.map((attempt) => (
                        <GroupHealthAttemptDetails key={attempt.id} attempt={attempt} />
                    )) : (
                        <div className="rounded-2xl border border-dashed border-border/70 bg-muted/20 px-3 py-6 text-center text-xs text-muted-foreground">
                            {t('empty')}
                        </div>
                    )}
                </div>
            </DialogContent>
        </Dialog>
    );
}
