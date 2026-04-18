'use client';

import { useEffect, useMemo, useState } from 'react';
import { Clock, Cpu, Zap, AlertCircle, ArrowDownToLine, ArrowUpFromLine, DollarSign, ArrowRight, ArrowDown, Send, MessageSquare, Loader2, RotateCw, ChevronDown, ChevronUp, Pin, KeyRound, CircleOff, Info, Link } from 'lucide-react';
import { useTranslations } from 'next-intl';
import { motion, AnimatePresence } from 'motion/react';
import JsonView from '@uiw/react-json-view';
import { githubDarkTheme } from '@uiw/react-json-view/githubDark';
import { githubLightTheme } from '@uiw/react-json-view/githubLight';
import { useTheme } from 'next-themes';
import { type RelayLog, type RelayLogWSMode, type RelayLogWSRecovery, type ChannelAttempt, type AttemptStatus } from '@/api/endpoints/log';
import { getModelIcon } from '@/lib/model-icons';
import { Badge } from '@/components/ui/badge';
import { cn } from '@/lib/utils';
import { CopyIconButton } from '@/components/common/CopyButton';
import {
    AlertDialog,
    AlertDialogAction,
    AlertDialogCancel,
    AlertDialogContent,
    AlertDialogDescription,
    AlertDialogFooter,
    AlertDialogHeader,
    AlertDialogTitle,
} from '@/components/ui/alert-dialog';
import {
    MorphingDialog,
    MorphingDialogTrigger,
    MorphingDialogContainer,
    MorphingDialogContent,
    MorphingDialogClose,
    MorphingDialogTitle,
    MorphingDialogDescription,
    useMorphingDialog,
} from '@/components/ui/morphing-dialog';
import { Tooltip, TooltipContent, TooltipTrigger, TooltipProvider } from '@/components/animate-ui/components/animate/tooltip';
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover';
import { toast } from '@/components/common/Toast';
import { useUpdateSiteChannelModelDisabled } from '@/api/endpoints/site-channel';

export type LogSiteActionTarget = {
    siteId: number;
    siteName: string;
    accountId: number;
    accountName: string;
    groupKey: string;
    groupName: string;
    modelName: string;
    modelDisabled: boolean;
    canDisableModel: boolean;
    channelId: number;
    channelName: string;
};

export type LogSiteActionTargets = {
    attemptTargets: Array<LogSiteActionTarget | null>;
    legacyErrorTarget: LogSiteActionTarget | null;
};

function formatTime(timestamp: number): string {
    const date = new Date(timestamp * 1000);
    return date.toLocaleString('zh-CN', {
        month: '2-digit',
        day: '2-digit',
        hour: '2-digit',
        minute: '2-digit',
        second: '2-digit',
    });
}

function formatDuration(ms: number): string {
    if (ms < 1000) return `${ms}ms`;
    return `${(ms / 1000).toFixed(2)}s`;
}

function sanitizeErrorMessage(raw: string | undefined | null): string {
    if (!raw) return '';
    let text = raw.replace(/^upstream error:\s*(\d+):\s*/i, (_m, code) => `[HTTP ${code}] `);
    if (/<\/?(html|body|head|title|div|p|h[1-6]|br|script|style)[\s>]/i.test(text)) {
        const titleMatch = text.match(/<title[^>]*>([\s\S]*?)<\/title>/i);
        const h1Match = text.match(/<h1[^>]*>([\s\S]*?)<\/h1>/i);
        const summarySource = titleMatch?.[1] || h1Match?.[1] || '';
        const summary = summarySource
            ? summarySource.replace(/<[^>]+>/g, ' ').replace(/\s+/g, ' ').trim()
            : '(HTML response)';
        const stripped = text
            .replace(/<script[\s\S]*?<\/script>/gi, ' ')
            .replace(/<style[\s\S]*?<\/style>/gi, ' ')
            .replace(/<[^>]+>/g, ' ')
            .replace(/&nbsp;/gi, ' ')
            .replace(/&amp;/gi, '&')
            .replace(/&lt;/gi, '<')
            .replace(/&gt;/gi, '>')
            .replace(/&quot;/gi, '"')
            .replace(/\s+/g, ' ')
            .trim();
        const detail = stripped.length > 500 ? `${stripped.slice(0, 500)}…` : stripped;
        text = summary && detail && detail !== summary ? `${summary} — ${detail}` : (summary || detail || '(HTML response)');
    }
    return text;
}

interface MergedAttempt extends ChannelAttempt {
    repeat: number;
    lastAttemptNum: number;
    totalDuration: number;
}

function mergeAdjacentAttempts(attempts: ChannelAttempt[]): MergedAttempt[] {
    const out: MergedAttempt[] = [];
    for (const a of attempts) {
        const last = out[out.length - 1];
        if (
            last
            && last.channel_id === a.channel_id
            && last.channel_key_id === a.channel_key_id
            && last.model_name === a.model_name
            && last.status === a.status
            && (last.msg ?? '') === (a.msg ?? '')
        ) {
            last.repeat += 1;
            last.lastAttemptNum = a.attempt_num;
            last.totalDuration += a.duration;
            continue;
        }
        out.push({
            ...a,
            repeat: 1,
            lastAttemptNum: a.attempt_num,
            totalDuration: a.duration,
        });
    }
    return out;
}

function makeDisableTargetKey(target: LogSiteActionTarget | null | undefined) {
    if (!target) return '';
    return `${target.siteId}\u0000${target.accountId}\u0000${target.groupKey}\u0000${target.modelName}`;
}

function formatOptionalTokenCount(value: number | null | undefined) {
    if (typeof value !== 'number') return '—';
    return value.toLocaleString();
}

function hasInputTokenDetails(log: RelayLog) {
    return (
        log.transport_input_tokens != null ||
        log.bill_input_tokens != null ||
        log.cache_read_tokens != null ||
        log.cache_write_tokens != null
    );
}

function getHeadlineInputTokens(log: RelayLog) {
    const hasCache = log.cache_read_tokens != null || log.cache_write_tokens != null;
    if (!hasCache) return log.input_tokens;
    return log.input_tokens + (log.cache_write_tokens ?? 0);
}

function getWSBadgeMeta(mode: RelayLogWSMode | null | undefined, usedWS: boolean | undefined, t: ReturnType<typeof useTranslations<'log.card'>>) {
    if (!usedWS && !mode) return null;

    switch (mode) {
        case 'continuation':
            return {
                label: t('wsContinuation'),
                className: 'bg-emerald-500/10 text-emerald-600 dark:text-emerald-400',
                description: t('wsContinuationHint'),
            };
        case 'replay':
            return {
                label: t('wsReplay'),
                className: 'bg-amber-500/10 text-amber-700 dark:text-amber-300',
                description: t('wsReplayHint'),
            };
        case 'fresh':
        default:
            return {
                label: t('ws'),
                className: 'bg-cyan-500/10 text-cyan-600 dark:text-cyan-400',
                description: t('wsFreshHint'),
            };
    }
}

function getWSRecoveryBadgeMeta(recovery: RelayLogWSRecovery | null | undefined, t: ReturnType<typeof useTranslations<'log.card'>>) {
    switch (recovery) {
        case 'reconnect':
            return {
                label: t('wsReconnect'),
                className: 'bg-sky-500/10 text-sky-700 dark:text-sky-300',
                description: t('wsReconnectHint'),
            };
        case 'replay':
            return {
                label: t('wsReplayRecovery'),
                className: 'bg-amber-500/10 text-amber-700 dark:text-amber-300',
                description: t('wsReplayRecoveryHint'),
            };
        case 'downgrade':
            return {
                label: t('wsDowngrade'),
                className: 'bg-slate-500/10 text-slate-700 dark:text-slate-300',
                description: t('wsDowngradeHint'),
            };
        default:
            return null;
    }
}

function getAttemptStatusMeta(status: AttemptStatus, t: ReturnType<typeof useTranslations<'log.card'>>) {
    switch (status) {
        case 'success':
            return {
                label: t('success'),
                badgeClassName: 'bg-primary/15 text-primary',
                containerClassName: 'bg-primary/5 border-primary/20 hover:bg-primary/10',
                messageClassName: 'text-primary/90 border-primary/30',
            };
        case 'skipped':
            return {
                label: t('skipped'),
                badgeClassName: 'bg-muted text-muted-foreground',
                containerClassName: 'bg-muted/40 border-border/60 hover:bg-muted/60',
                messageClassName: 'text-muted-foreground border-border/50',
            };
        case 'circuit_break':
            return {
                label: t('circuitBreak'),
                badgeClassName: 'bg-amber-500/15 text-amber-700 dark:text-amber-300',
                containerClassName: 'bg-amber-500/5 border-amber-500/20 hover:bg-amber-500/10',
                messageClassName: 'text-amber-700 dark:text-amber-300 border-amber-500/30',
            };
        case 'failed':
        default:
            return {
                label: t('failed'),
                badgeClassName: 'bg-destructive/15 text-destructive',
                containerClassName: 'bg-destructive/5 border-destructive/20 hover:bg-destructive/10',
                messageClassName: 'text-destructive/90 border-destructive/30',
            };
    }
}

interface RetryBadgeWithTooltipProps {
    channelName: string;
    brandColor: string;
    attempts: ChannelAttempt[];
}

function RetryBadgeWithTooltip({ channelName, brandColor, attempts }: RetryBadgeWithTooltipProps) {
    const t = useTranslations('log.card');
    const merged = useMemo(() => mergeAdjacentAttempts(attempts), [attempts]);

    return (
        <Tooltip>
            <TooltipTrigger asChild>
                <Badge
                    variant="secondary"
                    className="shrink-0 text-xs px-1.5 py-0 cursor-help"
                    style={{ backgroundColor: `${brandColor}15`, color: brandColor }}
                >
                    <RotateCw className="size-3 mr-1 opacity-80" />
                    {channelName}
                </Badge>
            </TooltipTrigger>
            <TooltipContent className="border bg-card p-2 min-w-[280px] shadow-sm rounded-3xl flex flex-col gap-1">
                {merged.map((attempt, idx) => {
                    const statusMeta = getAttemptStatusMeta(attempt.status, t);

                    return (
                        <div key={idx} className="flex flex-col w-full">
                            <div className="flex items-center gap-2 rounded-md px-2 py-1.5 hover:bg-muted/50 transition-colors">
                                <Badge
                                    className={cn(
                                        'h-5 shrink-0 px-1.5 text-[10px] font-bold uppercase shadow-none border-0',
                                        statusMeta.badgeClassName,
                                    )}
                                >
                                    {statusMeta.label}
                                </Badge>
                                <div className="flex min-w-0 flex-col flex-1">
                                    <span className="truncate text-xs font-semibold text-foreground">
                                        {attempt.channel_name}
                                    </span>
                                    <span className="text-[10px] text-muted-foreground">
                                        {attempt.model_name} • {formatDuration(attempt.totalDuration)}
                                    </span>
                                </div>
                                {attempt.repeat > 1 ? (
                                    <Badge variant="outline" className="shrink-0 h-5 px-1.5 text-[10px] font-semibold tabular-nums">
                                        ×{attempt.repeat}
                                    </Badge>
                                ) : null}
                            </div>
                            {idx < merged.length - 1 ? (
                                <div className="flex justify-center py-0.5">
                                    <ArrowDown className="size-3 text-muted-foreground/30" />
                                </div>
                            ) : null}
                        </div>
                    );
                })}
            </TooltipContent>
        </Tooltip>
    );
}

function WSModeBadge({ log }: { log: RelayLog }) {
    const t = useTranslations('log.card');
    const modeMeta = getWSBadgeMeta(log.ws_mode, log.used_ws, t);
    const recoveryMeta = getWSRecoveryBadgeMeta(log.ws_recovery, t);

    if (!modeMeta && !recoveryMeta) return null;

    return (
        <div className="flex items-center gap-1.5 shrink-0">
            {modeMeta ? (
                <Tooltip>
                    <TooltipTrigger asChild>
                        <Badge
                            variant="secondary"
                            className={cn('shrink-0 gap-1 px-1.5 py-0 text-xs', modeMeta.className)}
                        >
                            <Link className="size-3.5 shrink-0" />
                            {modeMeta.label}
                        </Badge>
                    </TooltipTrigger>
                    <TooltipContent>{modeMeta.description}</TooltipContent>
                </Tooltip>
            ) : null}
            {recoveryMeta ? (
                <Tooltip>
                    <TooltipTrigger asChild>
                        <Badge
                            variant="secondary"
                            className={cn('shrink-0 gap-1 px-1.5 py-0 text-xs', recoveryMeta.className)}
                        >
                            <RotateCw className="size-3.5 shrink-0" />
                            {recoveryMeta.label}
                        </Badge>
                    </TooltipTrigger>
                    <TooltipContent>{recoveryMeta.description}</TooltipContent>
                </Tooltip>
            ) : null}
        </div>
    );
}

function InputTokenDetailsPopover({ log }: { log: RelayLog }) {
    const t = useTranslations('log.card');

    if (!hasInputTokenDetails(log)) return null;

    return (
        <Popover>
            <PopoverTrigger asChild>
                <button
                    type="button"
                    aria-label={t('inputDetails')}
                    onClick={(event) => event.stopPropagation()}
                    onKeyDown={(event) => event.stopPropagation()}
                    onPointerDown={(event) => event.stopPropagation()}
                    className="inline-flex size-4 items-center justify-center rounded-full text-muted-foreground/80 transition hover:bg-muted/70 hover:text-foreground"
                >
                    <Info className="size-3.5" />
                </button>
            </PopoverTrigger>
            <PopoverContent
                side="top"
                align="start"
                onOpenAutoFocus={(event) => event.preventDefault()}
                className="w-56 rounded-2xl border border-border/70 bg-card p-3 shadow-xl"
            >
                <div className="space-y-2">
                    <div className="text-xs font-semibold text-foreground">{t('inputDetails')}</div>
                    <div className="grid grid-cols-[auto_1fr] gap-x-3 gap-y-1.5 text-xs">
                        <span className="text-muted-foreground">{t('transportInput')}</span>
                        <span className="text-right font-mono text-foreground">{formatOptionalTokenCount(log.transport_input_tokens)}</span>
                        <span className="text-muted-foreground">{t('billInput')}</span>
                        <span className="text-right font-mono text-foreground">{formatOptionalTokenCount(log.bill_input_tokens)}</span>
                        <span className="text-muted-foreground">{t('cacheRead')}</span>
                        <span className="text-right font-mono text-foreground">{formatOptionalTokenCount(log.cache_read_tokens)}</span>
                        <span className="text-muted-foreground">{t('cacheWrite')}</span>
                        <span className="text-right font-mono text-foreground">{formatOptionalTokenCount(log.cache_write_tokens)}</span>
                    </div>
                </div>
            </PopoverContent>
        </Popover>
    );
}

function DeferredJsonContent({ content, fallbackText }: { content: string | undefined; fallbackText: string }) {
    const { resolvedTheme } = useTheme();
    const { isOpen } = useMorphingDialog();
    const [shouldRender, setShouldRender] = useState(false);

    const parsed = useMemo(() => {
        if (!content) return { isJson: false, data: null };
        try {
            return { isJson: true, data: JSON.parse(content) };
        } catch {
            return { isJson: false, data: content };
        }
    }, [content]);

    useEffect(() => {
        if (isOpen) {
            const timer = setTimeout(() => setShouldRender(true), 300);
            return () => clearTimeout(timer);
        }
    }, [isOpen]);

    if (!isOpen) {
        if (shouldRender) setShouldRender(false);
        return null;
    }

    if (!content) {
        return (
            <pre className="p-4 text-xs text-muted-foreground whitespace-pre-wrap wrap-break-word leading-relaxed">
                {fallbackText}
            </pre>
        );
    }

    return (
        <AnimatePresence mode="wait">
            {!shouldRender ? (
                <motion.div
                    key="loading"
                    initial={{ opacity: 0 }}
                    animate={{ opacity: 1 }}
                    exit={{ opacity: 0 }}
                    transition={{ duration: 0.15 }}
                    className="p-4 flex items-center justify-center h-full"
                >
                    <Loader2 className="h-5 w-5 text-muted-foreground animate-spin" />
                </motion.div>
            ) : parsed.isJson ? (
                <motion.div
                    key="json"
                    initial={{ opacity: 0 }}
                    animate={{ opacity: 1 }}
                    exit={{ opacity: 0 }}
                    transition={{ duration: 0.2 }}
                    className="p-4"
                >
                    <JsonView
                        value={parsed.data as object}
                        style={{
                            ...(resolvedTheme === 'dark' ? githubDarkTheme : githubLightTheme),
                            fontSize: '12px',
                            fontFamily: 'ui-monospace, SFMono-Regular, "SF Mono", Menlo, Consolas, monospace',
                            backgroundColor: 'transparent',
                        }}
                        displayDataTypes={false}
                        displayObjectSize={false}
                        collapsed={false}
                    />
                </motion.div>
            ) : (
                <motion.pre
                    key="text"
                    initial={{ opacity: 0 }}
                    animate={{ opacity: 1 }}
                    exit={{ opacity: 0 }}
                    transition={{ duration: 0.2 }}
                    className="p-4 text-xs text-muted-foreground whitespace-pre-wrap wrap-break-word font-mono leading-relaxed"
                >
                    {content}
                </motion.pre>
            )}
        </AnimatePresence>
    );
}

function AttemptDisableButton({
    target,
    pending,
    onDisable,
}: {
    target: LogSiteActionTarget | null;
    pending: boolean;
    onDisable: (target: LogSiteActionTarget) => void;
}) {
    const t = useTranslations('log.card');

    if (!target?.canDisableModel) return null;

    const tooltipLabel = target.modelDisabled
        ? t('disabled')
        : pending
            ? t('disabling')
            : t('disableModel');

    return (
        <Tooltip>
            <TooltipTrigger asChild>
                <button
                    type="button"
                    disabled={pending || target.modelDisabled}
                    onClick={() => onDisable(target)}
                    className={cn(
                        'inline-flex size-7 items-center justify-center rounded-lg transition disabled:cursor-not-allowed disabled:opacity-60',
                        target.modelDisabled
                            ? 'text-destructive hover:bg-destructive/10'
                            : 'text-muted-foreground hover:bg-destructive/10 hover:text-destructive',
                    )}
                >
                    {pending ? (
                        <Loader2 className="size-4 animate-spin" />
                    ) : (
                        <CircleOff className="size-4" />
                    )}
                </button>
            </TooltipTrigger>
            <TooltipContent>{tooltipLabel}</TooltipContent>
        </Tooltip>
    );
}

export function LogCard({ log, siteTargets }: { log: RelayLog; siteTargets: LogSiteActionTargets | null }) {
    const t = useTranslations('log.card');
    const displayActualModelName = useMemo(
        () => log.actual_model_name?.trim() || log.request_model_name?.trim() || '',
        [log.actual_model_name, log.request_model_name],
    );
    const { Avatar: ModelAvatar, color: brandColor } = useMemo(
        () => getModelIcon(displayActualModelName),
        [displayActualModelName]
    );
    const requestAPIKeyName = useMemo(() => log.request_api_key_name?.trim() ?? '', [log.request_api_key_name]);
    const disableMutation = useUpdateSiteChannelModelDisabled();

    const hasError = !!log.error;
    const hasAttempts = (log.attempts?.length ?? 0) > 0;
    const hasMultipleAttempts = (log.attempts?.length ?? 0) > 1;
    const [isDiagnosticExpanded, setIsDiagnosticExpanded] = useState(false);
    const [confirmDisableOpen, setConfirmDisableOpen] = useState(false);
    const [activeDisableTarget, setActiveDisableTarget] = useState<LogSiteActionTarget | null>(null);
    const [pendingDisableKey, setPendingDisableKey] = useState<string | null>(null);

    const attemptTargets = siteTargets?.attemptTargets ?? [];
    const legacyErrorTarget = siteTargets?.legacyErrorTarget ?? null;
    const showDiagnosticPanel = hasError || hasAttempts;
    const diagnosticTitle = hasAttempts ? t('retryDetails') : t('errorInfo');
    const diagnosticIcon = hasAttempts ? RotateCw : AlertCircle;
    const DiagnosticIcon = diagnosticIcon;

    const openDisableDialog = (target: LogSiteActionTarget) => {
        if (!target.canDisableModel || target.modelDisabled) return;
        setActiveDisableTarget(target);
        setConfirmDisableOpen(true);
    };

    const handleConfirmDisableOpenChange = (open: boolean) => {
        if (!open && disableMutation.isPending) return;
        setConfirmDisableOpen(open);
        if (!open) {
            setActiveDisableTarget(null);
        }
    };

    const confirmDisableModel = () => {
        if (!activeDisableTarget || !activeDisableTarget.canDisableModel || activeDisableTarget.modelDisabled) return;

        const target = activeDisableTarget;
        const targetKey = makeDisableTargetKey(target);
        setPendingDisableKey(targetKey);

        disableMutation.mutate(
            {
                siteId: target.siteId,
                accountId: target.accountId,
                payload: [
                    {
                        group_key: target.groupKey,
                        model_name: target.modelName,
                        disabled: true,
                    },
                ],
            },
            {
                onSuccess: () => {
                    setConfirmDisableOpen(false);
                    setActiveDisableTarget(null);
                    toast.success(`已禁用 ${target.groupName} / ${target.modelName}`);
                },
                onError: (error) => {
                    toast.error(error.message);
                },
                onSettled: () => {
                    setPendingDisableKey(null);
                },
            },
        );
    };

    const isDisablePending = (target: LogSiteActionTarget | null) => {
        if (!target || !pendingDisableKey) return false;
        return pendingDisableKey === makeDisableTargetKey(target);
    };

    return (
        <TooltipProvider>
            <MorphingDialog>
                <MorphingDialogTrigger
                    className={cn(
                        'rounded-3xl border bg-card w-full text-left',
                        hasError ? 'border-destructive/40' : 'border-border',
                    )}
                >
                    <div className={cn('p-4 grid grid-cols-[auto_1fr] gap-4', hasError ? 'items-start' : 'items-center')}>
                        <ModelAvatar size={40} />
                        <div className="min-w-0 flex flex-col gap-3">
                            <div className="flex items-start gap-3 min-w-0">
                                <div className="flex min-w-0 flex-1 items-center gap-2 text-sm">
                                    <span className="font-semibold text-card-foreground truncate" title={log.request_model_name}>
                                        {log.request_model_name}
                                    </span>
                                    <ArrowRight className="size-3.5 shrink-0 text-muted-foreground/50" />
                                    {hasMultipleAttempts ? (
                                        <RetryBadgeWithTooltip
                                            channelName={log.channel_name}
                                            brandColor={brandColor}
                                            attempts={log.attempts!}
                                        />
                                    ) : (
                                        <Badge
                                            variant="secondary"
                                            className="shrink-0 text-xs px-1.5 py-0"
                                            style={{ backgroundColor: `${brandColor}15`, color: brandColor }}
                                        >
                                            {log.channel_name}
                                        </Badge>
                                    )}
                                    <span className="text-muted-foreground truncate" title={displayActualModelName}>
                                        {displayActualModelName}
                                    </span>
                                    {log.attempts?.some((attempt) => attempt.sticky) ? (
                                        <Pin className="size-3.5 shrink-0 text-amber-500" />
                                    ) : null}
                                </div>
                                <WSModeBadge log={log} />
                            </div>
                            <div className="grid grid-cols-2 md:grid-cols-7 gap-x-4 gap-y-2 text-xs tabular-nums text-muted-foreground">
                                <div className="flex items-center gap-1.5">
                                    <Clock className="size-3.5 shrink-0" style={{ color: brandColor }} />
                                    <span>{formatTime(log.time)}</span>
                                </div>
                                {requestAPIKeyName ? (
                                    <div className="flex items-center gap-1.5">
                                        <KeyRound className="size-3.5 shrink-0 text-orange-500" />
                                        <span className="truncate" title={requestAPIKeyName}>
                                            {requestAPIKeyName}
                                        </span>
                                    </div>
                                ) : null}
                                <div className="flex items-center gap-1.5">
                                    <Zap className="size-3.5 shrink-0 text-amber-500" />
                                    <span>{t('firstToken')} {formatDuration(log.ftut)}</span>
                                </div>
                                <div className="flex items-center gap-1.5">
                                    <Cpu className="size-3.5 shrink-0 text-blue-500" />
                                    <span>{t('totalTime')} {formatDuration(log.use_time)}</span>
                                </div>
                                <div className="flex items-center gap-1.5">
                                    <ArrowDownToLine className="size-3.5 shrink-0 text-green-500" />
                                    <span className="flex items-center gap-1">
                                        <span>{t('input')} {getHeadlineInputTokens(log).toLocaleString()}</span>
                                        <InputTokenDetailsPopover log={log} />
                                    </span>
                                </div>
                                <div className="flex items-center gap-1.5">
                                    <ArrowUpFromLine className="size-3.5 shrink-0 text-purple-500" />
                                    <span>{t('output')} {log.output_tokens.toLocaleString()}</span>
                                </div>
                                <div className="flex items-center gap-1.5">
                                    <DollarSign className="size-3.5 shrink-0 text-emerald-500" />
                                    <span className="font-medium text-emerald-600 dark:text-emerald-400">
                                        {t('cost')} {Number(log.cost).toFixed(6)}
                                    </span>
                                </div>
                            </div>
                            {hasError ? (
                                <div className="p-2.5 rounded-xl bg-destructive/10 border border-destructive/20 overflow-hidden">
                                    <p className="text-xs text-destructive line-clamp-2">{sanitizeErrorMessage(log.error)}</p>
                                </div>
                            ) : null}
                        </div>
                    </div>
                </MorphingDialogTrigger>

                <MorphingDialogContainer>
                    <MorphingDialogContent className="relative w-[calc(100vw-2rem)] md:w-[80vw] bg-card text-card-foreground px-6 py-4 rounded-3xl h-[calc(100vh-2rem)] flex flex-col overflow-hidden">
                        <MorphingDialogClose className="top-4 right-5 text-muted-foreground hover:text-foreground transition-colors" />
                        <MorphingDialogTitle className="mb-3 flex min-w-0 items-start gap-3 pr-14 text-sm md:pr-16">
                            <div className="flex min-w-0 flex-1 items-center gap-2">
                                <ModelAvatar size={28} />
                                <span className="font-semibold text-card-foreground truncate">{log.request_model_name}</span>
                                <ArrowRight className="size-3.5 shrink-0 text-muted-foreground/50" />
                                {hasMultipleAttempts ? (
                                    <RetryBadgeWithTooltip
                                        channelName={log.channel_name}
                                        brandColor={brandColor}
                                        attempts={log.attempts!}
                                    />
                                ) : (
                                    <Badge
                                        variant="secondary"
                                        className="shrink-0 text-xs px-1.5 py-0"
                                        style={{ backgroundColor: `${brandColor}15`, color: brandColor }}
                                    >
                                        {log.channel_name}
                                    </Badge>
                                )}
                                <span className="text-muted-foreground truncate">{displayActualModelName}</span>
                                {log.attempts?.some((attempt) => attempt.sticky) ? (
                                    <Pin className="size-3.5 shrink-0 text-amber-500" />
                                ) : null}
                            </div>
                            <WSModeBadge log={log} />
                        </MorphingDialogTitle>

                        <MorphingDialogDescription className="flex-1 min-h-0">
                            <div className="flex flex-col min-h-0 h-full gap-4">
                                {showDiagnosticPanel ? (
                                    <div
                                        className={cn(
                                            'flex-initial min-h-0 flex flex-col rounded-2xl border overflow-hidden max-h-[40%]',
                                            hasError
                                                ? 'bg-destructive/5 border-destructive/20'
                                                : 'bg-secondary/30 border-border/50',
                                        )}
                                    >
                                        <div
                                            className={cn(
                                                'flex items-center gap-2 px-3 py-2.5 shrink-0 cursor-pointer select-none hover:bg-muted/50 transition-colors',
                                                hasError && 'hover:bg-destructive/10',
                                            )}
                                            onClick={() => setIsDiagnosticExpanded(!isDiagnosticExpanded)}
                                        >
                                            <DiagnosticIcon className={cn('size-4', hasError ? 'text-destructive' : 'text-muted-foreground')} />
                                            <span className={cn('text-sm font-medium', hasError ? 'text-destructive' : 'text-secondary-foreground')}>
                                                {diagnosticTitle}
                                            </span>
                                            <div className="ml-auto flex items-center gap-2">
                                                {hasAttempts ? (
                                                    <Badge
                                                        variant="outline"
                                                        className={cn(
                                                            'text-xs border-0',
                                                            hasError
                                                                ? 'bg-destructive/10 text-destructive'
                                                                : 'bg-secondary text-secondary-foreground',
                                                        )}
                                                    >
                                                        {log.total_attempts || log.attempts!.length} {t('attempts')}
                                                    </Badge>
                                                ) : null}
                                                {isDiagnosticExpanded ? (
                                                    <ChevronUp className="size-4 text-muted-foreground" />
                                                ) : (
                                                    <ChevronDown className="size-4 text-muted-foreground" />
                                                )}
                                            </div>
                                        </div>

                                        <AnimatePresence initial={false}>
                                            {isDiagnosticExpanded ? (
                                                <motion.div
                                                    initial={{ height: 0, opacity: 0 }}
                                                    animate={{ height: 'auto', opacity: 1 }}
                                                    exit={{ height: 0, opacity: 0 }}
                                                    transition={{ duration: 0.2, ease: 'easeInOut' }}
                                                    className="overflow-hidden flex flex-col min-h-0"
                                                >
                                                    <div className="flex-1 overflow-auto p-2.5 md:p-3 flex flex-col gap-4">
                                                        {hasError ? (
                                                            <div className="relative pl-1">
                                                                <div className="absolute right-0 top-0">
                                                                    <CopyIconButton
                                                                        text={log.error ?? ''}
                                                                        className="p-1 rounded-md text-destructive/60 hover:text-destructive hover:bg-destructive/10 transition-colors"
                                                                        copyIconClassName="size-4"
                                                                        checkIconClassName="size-4"
                                                                    />
                                                                </div>
                                                                <p className="text-sm text-destructive whitespace-pre-wrap wrap-break-word pr-8 leading-relaxed">
                                                                    {sanitizeErrorMessage(log.error)}
                                                                </p>
                                                                {!hasAttempts && legacyErrorTarget ? (
                                                                    <div className="mt-3 flex justify-end">
                                                                        <AttemptDisableButton
                                                                            target={legacyErrorTarget}
                                                                            pending={isDisablePending(legacyErrorTarget)}
                                                                            onDisable={openDisableDialog}
                                                                        />
                                                                    </div>
                                                                ) : null}
                                                            </div>
                                                        ) : null}

                                                        {hasAttempts ? (
                                                            <div className="flex flex-col gap-2">
                                                                {(() => {
                                                                    const attemptsArr = log.attempts!;
                                                                    const merged: Array<MergedAttempt & { originalIndex: number }> = [];
                                                                    for (let i = 0; i < attemptsArr.length; i++) {
                                                                        const a = attemptsArr[i];
                                                                        const last = merged[merged.length - 1];
                                                                        if (
                                                                            last
                                                                            && last.channel_id === a.channel_id
                                                                            && last.channel_key_id === a.channel_key_id
                                                                            && last.model_name === a.model_name
                                                                            && last.status === a.status
                                                                            && (last.msg ?? '') === (a.msg ?? '')
                                                                        ) {
                                                                            last.repeat += 1;
                                                                            last.lastAttemptNum = a.attempt_num;
                                                                            last.totalDuration += a.duration;
                                                                            continue;
                                                                        }
                                                                        merged.push({
                                                                            ...a,
                                                                            repeat: 1,
                                                                            lastAttemptNum: a.attempt_num,
                                                                            totalDuration: a.duration,
                                                                            originalIndex: i,
                                                                        });
                                                                    }
                                                                    return merged.map((attempt, idx) => {
                                                                        const statusMeta = getAttemptStatusMeta(attempt.status, t);
                                                                        const attemptTarget = attemptTargets[attempt.originalIndex] ?? null;
                                                                        const canDisableAttempt = attempt.status === 'failed' && !!attemptTarget?.canDisableModel;
                                                                        const sanitizedMsg = sanitizeErrorMessage(attempt.msg);

                                                                        return (
                                                                            <div
                                                                                key={`${attempt.attempt_num || idx}-${attempt.channel_id}-${attempt.model_name}-${idx}`}
                                                                                className={cn(
                                                                                    'text-xs p-2.5 rounded-xl border transition-colors flex flex-col gap-2',
                                                                                    statusMeta.containerClassName,
                                                                                )}
                                                                            >
                                                                                <div className="flex items-start gap-2">
                                                                                    <Badge
                                                                                        className={cn(
                                                                                            'h-5 shrink-0 px-1.5 text-[10px] font-bold uppercase shadow-none border-0',
                                                                                            statusMeta.badgeClassName,
                                                                                        )}
                                                                                    >
                                                                                        {statusMeta.label}
                                                                                    </Badge>
                                                                                    <div className="min-w-0 flex-1">
                                                                                        <div className="flex items-center gap-2">
                                                                                            <span className="font-semibold text-foreground">
                                                                                                {attempt.channel_name}
                                                                                            </span>
                                                                                            <span className="text-muted-foreground truncate">
                                                                                                ({attempt.model_name})
                                                                                            </span>
                                                                                            {attempt.sticky ? (
                                                                                                <Pin className="size-3.5 shrink-0 text-amber-500" />
                                                                                            ) : null}
                                                                                            {attempt.repeat > 1 ? (
                                                                                                <Badge variant="outline" className="h-5 px-1.5 text-[10px] font-semibold tabular-nums">
                                                                                                    ×{attempt.repeat}
                                                                                                </Badge>
                                                                                            ) : null}
                                                                                        </div>
                                                                                    </div>
                                                                                    <div className="ml-auto flex items-center gap-2 shrink-0">
                                                                                        <span className="text-muted-foreground tabular-nums font-mono">
                                                                                            {formatDuration(attempt.totalDuration)}
                                                                                        </span>
                                                                                        {canDisableAttempt ? (
                                                                                            <AttemptDisableButton
                                                                                                target={attemptTarget}
                                                                                                pending={isDisablePending(attemptTarget)}
                                                                                                onDisable={openDisableDialog}
                                                                                            />
                                                                                        ) : null}
                                                                                    </div>
                                                                                </div>
                                                                                {sanitizedMsg ? (
                                                                                    <div className={cn('pl-2 border-l-2 text-[11px] leading-relaxed whitespace-pre-wrap wrap-break-word', statusMeta.messageClassName)}>
                                                                                        {sanitizedMsg}
                                                                                    </div>
                                                                                ) : null}
                                                                            </div>
                                                                        );
                                                                    });
                                                                })()}
                                                            </div>
                                                        ) : null}
                                                    </div>
                                                </motion.div>
                                            ) : null}
                                        </AnimatePresence>
                                    </div>
                                ) : null}

                                <div className="flex-1 min-h-0 overflow-hidden">
                                    <div className="grid grid-cols-1 md:grid-cols-2 gap-4 h-full min-h-0">
                                        <div className="flex flex-col rounded-2xl border border-border bg-muted/30 overflow-hidden min-h-0">
                                            <div className="flex items-center gap-2 px-3 md:px-4 py-2.5 md:py-3 border-b border-border bg-muted/50 shrink-0">
                                                <Send className="size-4 text-green-500" />
                                                <span className="text-sm font-medium text-card-foreground">{t('requestContent')}</span>
                                                <Badge variant="secondary" className="ml-auto text-xs">
                                                    {getHeadlineInputTokens(log).toLocaleString()} {t('tokens')}
                                                </Badge>
                                            </div>
                                            <div className="flex-1 overflow-auto min-h-0">
                                                <DeferredJsonContent content={log.request_content} fallbackText={t('noRequestContent')} />
                                            </div>
                                        </div>
                                        <div className="flex flex-col rounded-2xl border border-border bg-muted/30 overflow-hidden min-h-0">
                                            <div className="flex items-center gap-2 px-3 md:px-4 py-2.5 md:py-3 border-b border-border bg-muted/50 shrink-0">
                                                <MessageSquare className="size-4 text-purple-500" />
                                                <span className="text-sm font-medium text-card-foreground">{t('responseContent')}</span>
                                                <Badge variant="secondary" className="ml-auto text-xs">
                                                    {log.output_tokens.toLocaleString()} {t('tokens')}
                                                </Badge>
                                            </div>
                                            <div className="flex-1 overflow-auto min-h-0">
                                                <DeferredJsonContent content={log.response_content} fallbackText={t('noResponseContent')} />
                                            </div>
                                        </div>
                                    </div>
                                </div>
                            </div>
                        </MorphingDialogDescription>

                        <div className="flex flex-wrap items-center gap-3 md:gap-4 pt-4 mt-auto text-xs text-muted-foreground shrink-0">
                            <div className="flex items-center gap-1.5">
                                <Clock className="size-3.5" style={{ color: brandColor }} />
                                <span className="tabular-nums">{formatTime(log.time)}</span>
                            </div>
                            {requestAPIKeyName ? (
                                <div className="flex min-w-0 items-center gap-1.5">
                                    <KeyRound className="size-3.5 shrink-0 text-orange-500" />
                                    <span className="truncate" title={requestAPIKeyName}>
                                        {requestAPIKeyName}
                                    </span>
                                </div>
                            ) : null}
                            <div className="flex items-center gap-1.5">
                                <Zap className="size-3.5 text-amber-500" />
                                <span>{t('firstTokenTime')}: {formatDuration(log.ftut)}</span>
                            </div>
                            <div className="flex items-center gap-1.5">
                                <Cpu className="size-3.5 text-blue-500" />
                                <span>{t('totalTime')}: {formatDuration(log.use_time)}</span>
                            </div>
                            <div className="flex items-center gap-1.5">
                                <DollarSign className="size-3.5 text-emerald-500" />
                                <span className="font-medium text-emerald-600 dark:text-emerald-400">
                                    {t('cost')}: {Number(log.cost).toFixed(6)}
                                </span>
                            </div>
                        </div>
                    </MorphingDialogContent>
                </MorphingDialogContainer>
            </MorphingDialog>
            {activeDisableTarget?.canDisableModel ? (
                <AlertDialog open={confirmDisableOpen} onOpenChange={handleConfirmDisableOpenChange}>
                    <AlertDialogContent>
                        <AlertDialogHeader>
                            <AlertDialogTitle>确认禁用站点模型</AlertDialogTitle>
                            <AlertDialogDescription>
                                将在 {activeDisableTarget.siteName} / {activeDisableTarget.accountName} / {activeDisableTarget.groupName} 中禁用模型 {activeDisableTarget.modelName}。
                                禁用后对应投影渠道和分组会刷新为最新状态。
                            </AlertDialogDescription>
                        </AlertDialogHeader>
                        <AlertDialogFooter>
                            <AlertDialogCancel disabled={disableMutation.isPending}>取消</AlertDialogCancel>
                            <AlertDialogAction
                                onClick={confirmDisableModel}
                                disabled={disableMutation.isPending}
                                className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
                            >
                                {disableMutation.isPending ? '禁用中...' : '确认禁用'}
                            </AlertDialogAction>
                        </AlertDialogFooter>
                    </AlertDialogContent>
                </AlertDialog>
            ) : null}
        </TooltipProvider>
    );
}
