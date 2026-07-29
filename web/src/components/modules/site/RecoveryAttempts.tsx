'use client';

import { useMemo } from 'react';
import { useTranslations } from 'next-intl';
import { CheckCircle2, Clock3, XCircle } from 'lucide-react';
import type { SiteOperationAttempt } from '@/api/endpoints/site-recovery';
import { Badge } from '@/components/ui/badge';
import { cn } from '@/lib/utils';

type RecoveryAttemptsProps = {
    attempts: SiteOperationAttempt[];
    isLoading: boolean;
    error: unknown;
};

type AttemptGroup = {
    key: string;
    attempts: SiteOperationAttempt[];
};

function formatDateTime(value: string) {
    const date = new Date(value);
    return Number.isNaN(date.getTime()) ? value : date.toLocaleString();
}

function errorMessage(error: unknown, fallback: string) {
    if (error instanceof Error) return error.message;
    if (typeof error === 'object' && error !== null && 'message' in error) {
        const message = (error as { message?: unknown }).message;
        if (typeof message === 'string') return message;
    }
    return fallback;
}

export function RecoveryAttempts({
    attempts,
    isLoading,
    error,
}: RecoveryAttemptsProps) {
    const t = useTranslations('siteRecovery');
    const groups = useMemo(() => {
        const result: AttemptGroup[] = [];
        const byKey = new Map<string, AttemptGroup>();
        for (const attempt of attempts) {
            const key = attempt.operation_id || `attempt-${attempt.id}`;
            let group = byKey.get(key);
            if (!group) {
                group = { key, attempts: [] };
                byKey.set(key, group);
                result.push(group);
            }
            group.attempts.push(attempt);
        }
        for (const group of result) {
            group.attempts.sort((left, right) => left.attempt_number - right.attempt_number);
        }
        return result;
    }, [attempts]);

    if (isLoading) {
        return (
            <div className="border-y border-border/60 py-8 text-sm text-muted-foreground">
                {t('loading')}
            </div>
        );
    }
    if (error) {
        return (
            <div className="break-words border-y border-destructive/30 bg-destructive/5 px-3 py-5 text-sm text-destructive">
                {errorMessage(error, t('operationFailed'))}
            </div>
        );
    }
    if (groups.length === 0) {
        return (
            <div className="border-y border-dashed border-border/70 py-10 text-center text-sm text-muted-foreground">
                {t('attempts.empty')}
            </div>
        );
    }

    return (
        <div className="divide-y divide-border/70 border-y border-border/70">
            {groups.map((group) => {
                const first = group.attempts[0];
                const last = group.attempts[group.attempts.length - 1];
                const succeeded = group.attempts.some((attempt) => attempt.success);
                return (
                    <section key={group.key} className="py-5">
                        <div className="mb-4 flex flex-wrap items-start justify-between gap-3">
                            <div>
                                <div className="flex items-center gap-2">
                                    <span className="text-sm font-semibold">
                                        {t(`operation.${first.operation}`)}
                                    </span>
                                    <Badge
                                        variant="outline"
                                        className={cn(
                                            succeeded
                                                ? 'border-emerald-500/40 text-emerald-600'
                                                : 'border-destructive/40 text-destructive',
                                        )}
                                    >
                                        {succeeded
                                            ? t('attempts.succeeded')
                                            : t('attempts.failed')}
                                    </Badge>
                                </div>
                                <div className="mt-1 text-xs text-muted-foreground">
                                    {formatDateTime(first.started_at)}
                                </div>
                            </div>
                            {last.stop_reason ? (
                                <Badge variant="secondary" className="max-w-full whitespace-normal break-words">
                                    {t('attempts.stopReason', {
                                        value: last.stop_reason,
                                    })}
                                </Badge>
                            ) : null}
                        </div>

                        <ol className="space-y-3">
                            {group.attempts.map((attempt) => (
                                <li
                                    key={attempt.id}
                                    className="grid grid-cols-[1.5rem_minmax(0,1fr)] gap-3"
                                >
                                    <div className="flex justify-center pt-0.5">
                                        {attempt.success ? (
                                            <CheckCircle2 className="size-4 text-emerald-600" />
                                        ) : (
                                            <XCircle className="size-4 text-destructive" />
                                        )}
                                    </div>
                                    <div className="min-w-0 border-b border-border/40 pb-3 last:border-0">
                                        <div className="flex flex-wrap items-center gap-2">
                                            <span className="break-words text-sm font-medium">
                                                {attempt.path_label ||
                                                    attempt.proxy_mode}
                                            </span>
                                            <Badge variant="outline">
                                                {t('attempts.number', {
                                                    value: attempt.attempt_number,
                                                })}
                                            </Badge>
                                            {attempt.failure_class ? (
                                                <Badge
                                                    variant="outline"
                                                    className="text-muted-foreground"
                                                >
                                                    {attempt.failure_class}
                                                </Badge>
                                            ) : null}
                                        </div>
                                        <div className="mt-1 flex items-center gap-1 text-xs text-muted-foreground">
                                            <Clock3 className="size-3.5" />
                                            {t('attempts.duration', {
                                                value: attempt.duration_ms,
                                            })}
                                            {attempt.clash_node
                                                ? ` · ${attempt.clash_node}`
                                                : ''}
                                        </div>
                                        {attempt.message ? (
                                            <p className="mt-2 break-words text-xs text-muted-foreground">
                                                {attempt.message}
                                            </p>
                                        ) : null}
                                    </div>
                                </li>
                            ))}
                        </ol>
                    </section>
                );
            })}
        </div>
    );
}
