'use client';

import { useMemo, useState, type FormEvent } from 'react';
import { useTranslations } from 'next-intl';
import {
    Eye,
    EyeOff,
    KeyRound,
    Plus,
    RefreshCw,
    ShieldCheck,
    Trash2,
} from 'lucide-react';
import type { SiteAccount } from '@/api/endpoints/site';
import {
    useClearVerificationAccount,
    useCompleteVerificationSession,
    useCreateVerificationSession,
    useRetryVerificationOperation,
    useRevokeVerificationSession,
    useVerificationSessions,
    useVerificationTasks,
    type SiteRecoveryOperation,
    type VerificationSession,
    type VerificationTask,
} from '@/api/endpoints/site-recovery';
import { toast } from '@/components/common/Toast';
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
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import {
    Select,
    SelectContent,
    SelectItem,
    SelectTrigger,
    SelectValue,
} from '@/components/ui/select';
import { cn } from '@/lib/utils';

type VerificationPanelProps = {
    open: boolean;
    account: SiteAccount;
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

function statusClass(status: string) {
    if (status === 'completed' || status === 'succeeded') {
        return 'border-emerald-500/40 text-emerald-600';
    }
    if (status === 'failed' || status === 'expired' || status === 'canceled') {
        return 'border-destructive/40 text-destructive';
    }
    if (status === 'pending' || status === 'claimed' || status === 'running') {
        return 'border-amber-500/40 text-amber-600';
    }
    return 'text-muted-foreground';
}

export function VerificationPanel({ open, account }: VerificationPanelProps) {
    const t = useTranslations('siteRecovery');
    const sessionsQuery = useVerificationSessions(account.id, open);
    const tasksQuery = useVerificationTasks(account.id, open);
    const createSession = useCreateVerificationSession();
    const completeSession = useCompleteVerificationSession();
    const retryOperation = useRetryVerificationOperation();
    const revokeSession = useRevokeVerificationSession();
    const clearAccount = useClearVerificationAccount();
    const [operation, setOperation] = useState<SiteRecoveryOperation>('sync');
    const [ttlMinutes, setTTLMinutes] = useState(15);
    const [userAgent, setUserAgent] = useState(
        () => account.verification_user_agent ?? '',
    );
    const [manualSessionId, setManualSessionId] = useState<number | null>(null);
    const [manualCookie, setManualCookie] = useState('');
    const [manualUserAgent, setManualUserAgent] = useState(
        () => account.verification_user_agent ?? '',
    );
    const [showCookie, setShowCookie] = useState(false);
    const [confirmAction, setConfirmAction] = useState<
        { type: 'revoke'; session: VerificationSession } | { type: 'clear-account' } | null
    >(null);

    const sessions = sessionsQuery.data ?? [];
    const taskBySession = useMemo(
        () =>
            new Map(
                (tasksQuery.data ?? []).map((task) => [
                    task.session_id,
                    task,
                ]),
            ),
        [tasksQuery.data],
    );

    async function handleCreate(event: FormEvent<HTMLFormElement>) {
        event.preventDefault();
        try {
            await createSession.mutateAsync({
                site_account_id: account.id,
                use_account_preference: true,
                operation,
                ttl_minutes: Math.min(60, Math.max(1, Math.trunc(ttlMinutes))),
                user_agent: userAgent.trim() || undefined,
            });
            toast.success(t('verification.created'));
        } catch (error) {
            toast.error(errorMessage(error, t('operationFailed')));
        }
    }

    async function handleManualComplete(event: FormEvent<HTMLFormElement>) {
        event.preventDefault();
        if (!manualSessionId || !manualCookie.trim()) {
            toast.error(t('verification.cookieRequired'));
            return;
        }
        try {
            await completeSession.mutateAsync({
                sessionId: manualSessionId,
                cookie: manualCookie.trim(),
                userAgent: manualUserAgent,
            });
            setManualCookie('');
            setManualSessionId(null);
            toast.success(t('verification.completed'));
        } catch (error) {
            toast.error(errorMessage(error, t('operationFailed')));
        }
    }

    async function handleRetry(task: VerificationTask) {
        try {
            await retryOperation.mutateAsync(task.session_id);
            toast.success(t('verification.retryQueued'));
        } catch (error) {
            toast.error(errorMessage(error, t('operationFailed')));
        }
    }

    async function runConfirmedAction() {
        if (!confirmAction) return;
        try {
            if (confirmAction.type === 'revoke') {
                await revokeSession.mutateAsync(confirmAction.session.id);
                toast.success(t('verification.revoked'));
            } else {
                await clearAccount.mutateAsync(account.id);
                toast.success(t('verification.cleared'));
            }
            setConfirmAction(null);
        } catch (error) {
            toast.error(errorMessage(error, t('operationFailed')));
        }
    }

    const loading = sessionsQuery.isLoading || tasksQuery.isLoading;
    const queryError = sessionsQuery.error ?? tasksQuery.error;

    return (
        <div className="space-y-6">
            <section className="border-b border-border/60 pb-6">
                <div className="mb-4 flex flex-wrap items-center justify-between gap-3">
                    <div>
                        <h3 className="text-sm font-semibold">
                            {t('verification.newSession')}
                        </h3>
                        <p className="text-xs text-muted-foreground">
                            {t('verification.newSessionHint')}
                        </p>
                    </div>
                    <Button
                        type="button"
                        size="sm"
                        variant="outline"
                        className="rounded-xl text-destructive hover:text-destructive"
                        onClick={() => setConfirmAction({ type: 'clear-account' })}
                        disabled={clearAccount.isPending}
                    >
                        <Trash2 className="size-4" />
                        {t('verification.clearAccount')}
                    </Button>
                </div>

                <form
                    onSubmit={handleCreate}
                    className="grid gap-3 sm:grid-cols-[minmax(0,1fr)_7rem_minmax(0,1.4fr)_auto]"
                >
                    <label className="grid gap-1.5 text-sm">
                        <span className="text-xs font-medium">
                            {t('verification.operation')}
                        </span>
                        <Select
                            value={operation}
                            onValueChange={(value) =>
                                setOperation(value as SiteRecoveryOperation)
                            }
                        >
                            <SelectTrigger className="w-full rounded-xl">
                                <SelectValue />
                            </SelectTrigger>
                            <SelectContent>
                                <SelectItem value="sync">
                                    {t('operation.sync')}
                                </SelectItem>
                                <SelectItem value="checkin">
                                    {t('operation.checkin')}
                                </SelectItem>
                            </SelectContent>
                        </Select>
                    </label>
                    <label className="grid gap-1.5 text-sm">
                        <span className="text-xs font-medium">
                            {t('verification.ttlMinutes')}
                        </span>
                        <Input
                            type="number"
                            min={1}
                            max={60}
                            value={ttlMinutes}
                            onChange={(event) =>
                                setTTLMinutes(Number(event.target.value))
                            }
                            className="rounded-xl"
                        />
                    </label>
                    <label className="grid gap-1.5 text-sm">
                        <span className="text-xs font-medium">
                            {t('verification.userAgent')}
                        </span>
                        <Input
                            value={userAgent}
                            onChange={(event) => setUserAgent(event.target.value)}
                            placeholder={t('verification.optional')}
                            className="rounded-xl"
                        />
                    </label>
                    <Button
                        type="submit"
                        className="self-end rounded-xl"
                        disabled={createSession.isPending}
                    >
                        <Plus className="size-4" />
                        {t('verification.create')}
                    </Button>
                </form>
            </section>

            {manualSessionId ? (
                <section className="border-b border-border/60 pb-6">
                    <div className="mb-3 flex items-center gap-2">
                        <KeyRound className="size-4 text-muted-foreground" />
                        <h3 className="text-sm font-semibold">
                            {t('verification.manualImport')}
                        </h3>
                    </div>
                    <form
                        onSubmit={handleManualComplete}
                        className="grid gap-3 sm:grid-cols-[minmax(0,1.5fr)_minmax(0,1fr)_auto]"
                    >
                        <label className="grid gap-1.5 text-sm">
                            <span className="text-xs font-medium">
                                {t('verification.cookie')}
                            </span>
                            <div className="relative">
                                <Input
                                    type={showCookie ? 'text' : 'password'}
                                    value={manualCookie}
                                    onChange={(event) =>
                                        setManualCookie(event.target.value)
                                    }
                                    autoComplete="off"
                                    className="rounded-xl pr-10 font-mono text-xs"
                                />
                                <button
                                    type="button"
                                    onClick={() => setShowCookie((value) => !value)}
                                    className="absolute inset-y-0 right-0 flex w-10 items-center justify-center rounded-r-xl text-muted-foreground hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                                    aria-label={
                                        showCookie
                                            ? t('verification.hideCookie')
                                            : t('verification.showCookie')
                                    }
                                >
                                    {showCookie ? (
                                        <EyeOff className="size-4" />
                                    ) : (
                                        <Eye className="size-4" />
                                    )}
                                </button>
                            </div>
                        </label>
                        <label className="grid gap-1.5 text-sm">
                            <span className="text-xs font-medium">
                                {t('verification.userAgent')}
                            </span>
                            <Input
                                value={manualUserAgent}
                                onChange={(event) =>
                                    setManualUserAgent(event.target.value)
                                }
                                className="rounded-xl"
                            />
                        </label>
                        <div className="flex items-end gap-2">
                            <Button
                                type="submit"
                                className="rounded-xl"
                                disabled={completeSession.isPending}
                            >
                                <ShieldCheck className="size-4" />
                                {t('verification.complete')}
                            </Button>
                            <Button
                                type="button"
                                variant="ghost"
                                className="rounded-xl"
                                onClick={() => {
                                    setManualSessionId(null);
                                    setManualCookie('');
                                }}
                            >
                                {t('cancel')}
                            </Button>
                        </div>
                    </form>
                </section>
            ) : null}

            <section>
                <h3 className="mb-3 text-sm font-semibold">
                    {t('verification.history')}
                </h3>
                {loading ? (
                    <div className="border-y border-border/60 py-8 text-sm text-muted-foreground">
                        {t('loading')}
                    </div>
                ) : queryError ? (
                    <div className="break-words border-y border-destructive/30 bg-destructive/5 px-3 py-5 text-sm text-destructive">
                        {errorMessage(queryError, t('operationFailed'))}
                    </div>
                ) : sessions.length === 0 ? (
                    <div className="border-y border-dashed border-border/70 py-10 text-center text-sm text-muted-foreground">
                        {t('verification.empty')}
                    </div>
                ) : (
                    <div className="divide-y divide-border/60 border-y border-border/60">
                        {sessions.map((session) => {
                            const task = taskBySession.get(session.id);
                            const expiresAt = formatDateTime(session.expires_at);
                            const completedAt = formatDateTime(session.completed_at);
                            const canRetry =
                                task?.status === 'completed' &&
                                (task.retry_status === 'failed' ||
                                    task.retry_status === 'succeeded');
                            return (
                                <div key={session.id} className="space-y-3 py-4">
                                    <div className="flex flex-wrap items-start justify-between gap-3">
                                        <div className="min-w-0">
                                            <div className="flex flex-wrap items-center gap-2">
                                                <span className="text-sm font-medium">
                                                    {t('verification.sessionId', {
                                                        id: session.id,
                                                    })}
                                                </span>
                                                <Badge
                                                    variant="outline"
                                                    className={statusClass(
                                                        session.status,
                                                    )}
                                                >
                                                    {t(
                                                        `sessionStatus.${session.status}`,
                                                    )}
                                                </Badge>
                                                {session.source ? (
                                                    <Badge variant="secondary">
                                                        {session.source}
                                                    </Badge>
                                                ) : null}
                                            </div>
                                            <div className="mt-1 flex flex-wrap gap-x-3 gap-y-1 text-xs text-muted-foreground">
                                                {expiresAt ? (
                                                    <span>
                                                        {t(
                                                            'verification.expiresAt',
                                                            { value: expiresAt },
                                                        )}
                                                    </span>
                                                ) : null}
                                                {completedAt ? (
                                                    <span>
                                                        {t(
                                                            'verification.completedAt',
                                                            { value: completedAt },
                                                        )}
                                                    </span>
                                                ) : null}
                                                {session.clash_node ? (
                                                    <span>{session.clash_node}</span>
                                                ) : null}
                                            </div>
                                        </div>
                                        <div className="flex flex-wrap gap-2">
                                            {session.status === 'pending' ? (
                                                <Button
                                                    type="button"
                                                    size="sm"
                                                    variant="outline"
                                                    className="rounded-xl"
                                                    onClick={() => {
                                                        setManualSessionId(
                                                            session.id,
                                                        );
                                                        setManualUserAgent(
                                                            session.user_agent ||
                                                                account.verification_user_agent ||
                                                                '',
                                                        );
                                                    }}
                                                >
                                                    <KeyRound className="size-4" />
                                                    {t(
                                                        'verification.manualImport',
                                                    )}
                                                </Button>
                                            ) : null}
                                            {canRetry && task ? (
                                                <Button
                                                    type="button"
                                                    size="sm"
                                                    variant="outline"
                                                    className="rounded-xl"
                                                    onClick={() =>
                                                        handleRetry(task)
                                                    }
                                                    disabled={
                                                        retryOperation.isPending
                                                    }
                                                >
                                                    <RefreshCw
                                                        className={cn(
                                                            'size-4',
                                                            retryOperation.isPending &&
                                                                'animate-spin',
                                                        )}
                                                    />
                                                    {t('verification.retry')}
                                                </Button>
                                            ) : null}
                                            {session.status !== 'revoked' ? (
                                                <Button
                                                    type="button"
                                                    size="icon-sm"
                                                    variant="ghost"
                                                    className="rounded-xl text-destructive hover:text-destructive"
                                                    onClick={() =>
                                                        setConfirmAction({
                                                            type: 'revoke',
                                                            session,
                                                        })
                                                    }
                                                    aria-label={t(
                                                        'verification.revoke',
                                                    )}
                                                    title={t(
                                                        'verification.revoke',
                                                    )}
                                                >
                                                    <Trash2 className="size-4" />
                                                </Button>
                                            ) : null}
                                        </div>
                                    </div>

                                    {task ? (
                                        <div className="grid gap-2 bg-muted/20 px-3 py-2 text-xs sm:grid-cols-[auto_auto_minmax(0,1fr)]">
                                            <span>
                                                {t('verification.task')}{' '}
                                                <Badge
                                                    variant="outline"
                                                    className={statusClass(
                                                        task.status,
                                                    )}
                                                >
                                                    {t(
                                                        `taskStatus.${task.status}`,
                                                    )}
                                                </Badge>
                                            </span>
                                            <span>
                                                {t('verification.retryStatus')}{' '}
                                                <Badge
                                                    variant="outline"
                                                    className={statusClass(
                                                        task.retry_status,
                                                    )}
                                                >
                                                    {t(
                                                        `retryStatus.${task.retry_status}`,
                                                    )}
                                                </Badge>
                                            </span>
                                            {task.retry_message ? (
                                                <span className="min-w-0 break-all text-muted-foreground">
                                                    {task.retry_message}
                                                </span>
                                            ) : null}
                                        </div>
                                    ) : null}
                                </div>
                            );
                        })}
                    </div>
                )}
            </section>

            <AlertDialog
                open={confirmAction !== null}
                onOpenChange={(nextOpen) => !nextOpen && setConfirmAction(null)}
            >
                <AlertDialogContent>
                    <AlertDialogHeader>
                        <AlertDialogTitle>
                            {confirmAction?.type === 'clear-account'
                                ? t('verification.clearConfirmTitle')
                                : t('verification.revokeConfirmTitle')}
                        </AlertDialogTitle>
                        <AlertDialogDescription>
                            {confirmAction?.type === 'clear-account'
                                ? t('verification.clearConfirmDescription')
                                : t('verification.revokeConfirmDescription')}
                        </AlertDialogDescription>
                    </AlertDialogHeader>
                    <AlertDialogFooter>
                        <AlertDialogCancel>{t('cancel')}</AlertDialogCancel>
                        <AlertDialogAction
                            className="bg-destructive text-white hover:bg-destructive/90"
                            onClick={runConfirmedAction}
                            disabled={
                                revokeSession.isPending || clearAccount.isPending
                            }
                        >
                            {t('confirm')}
                        </AlertDialogAction>
                    </AlertDialogFooter>
                </AlertDialogContent>
            </AlertDialog>
        </div>
    );
}
