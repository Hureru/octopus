'use client';
import { useState } from 'react';
import { useTranslations } from 'next-intl';
import { Cloud, Download, ExternalLink, LoaderCircle, LogIn, Play, RefreshCw, Square, Terminal } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { CopyIconButton } from '@/components/common/CopyButton';
import { toast } from '@/components/common/Toast';
import {
    type TailscalePhase,
    useInstallTailscale,
    useLoginTailscale,
    useStartTailscaleFunnel,
    useStopTailscaleFunnel,
    useTailscaleStatus,
} from '@/api/endpoints/tailscale';
import type { ApiError } from '@/api/types';
import { SettingCard, SettingRow } from './shared';

const PHASE_STYLES: Record<TailscalePhase, string> = {
    unsupported: 'border-border text-muted-foreground',
    not_installed: 'border-border text-muted-foreground',
    needs_login: 'border-amber-500/40 bg-amber-500/10 text-amber-600 dark:text-amber-400',
    stopped: 'border-border text-muted-foreground',
    running: 'border-emerald-500/40 bg-emerald-500/10 text-emerald-600 dark:text-emerald-400',
    conflict: 'border-amber-500/40 bg-amber-500/10 text-amber-600 dark:text-amber-400',
    credentials_needed: 'border-amber-500/40 bg-amber-500/10 text-amber-600 dark:text-amber-400',
    error: 'border-destructive/40 bg-destructive/10 text-destructive',
};

function URLValue({ value }: { value?: string }) {
    if (!value) return <span className="text-sm text-muted-foreground">—</span>;
    return (
        <div className="flex min-w-0 max-w-64 items-center gap-2">
            <a
                href={value}
                target="_blank"
                rel="noreferrer"
                className="truncate font-mono text-xs text-primary hover:underline"
                title={value}
            >
                {value}
            </a>
            <CopyIconButton
                text={value}
                className="shrink-0 text-muted-foreground transition-colors hover:text-foreground"
                copyIconClassName="size-4"
                checkIconClassName="size-4 text-emerald-500"
            />
        </div>
    );
}

export function SettingTailscale() {
    const t = useTranslations('setting.tailscale');
    const statusQuery = useTailscaleStatus();
    const startFunnel = useStartTailscaleFunnel();
    const stopFunnel = useStopTailscaleFunnel();
    const installTailscale = useInstallTailscale();
    const loginTailscale = useLoginTailscale();
    const [sudoPassword, setSudoPassword] = useState('');
    const status = statusQuery.data;
    const actionPending = startFunnel.isPending || stopFunnel.isPending;
    const setupPending = installTailscale.isPending || loginTailscale.isPending;
    const handleToggle = async () => {
        if (!status) return;
        if (!status.running && !window.confirm(t('confirmStart'))) return;

        try {
            if (status.running) {
                await stopFunnel.mutateAsync();
                toast.success(t('stopSuccess'));
            } else {
                await startFunnel.mutateAsync();
                toast.success(t('startSuccess'));
            }
        } catch (error) {
            toast.error(t('actionFailed'), { description: (error as ApiError)?.message });
        }
    };
    const handleInstall = async () => {
        try {
            await installTailscale.mutateAsync(sudoPassword);
            setSudoPassword('');
            toast.success(t('installSuccess'));
        } catch (error) {
            toast.error(t('installFailed'), { description: (error as ApiError)?.message });
        }
    };

    const handleLogin = async () => {
        try {
            await loginTailscale.mutateAsync();
            toast.success(t('loginStarted'));
        } catch (error) {
            toast.error(t('loginFailed'), { description: (error as ApiError)?.message });
        }
    };

    const actionDisabled = actionPending
        || !status
        || !status.supported
        || !status.installed
        || status.phase === 'needs_login'
        || status.phase === 'credentials_needed'
        || (status.default_credentials && !status.running)
        || status.phase === 'conflict';

    return (
        <SettingCard icon={Cloud} title={t('title')}>
            <div className="space-y-2 text-sm text-muted-foreground">
                <p>{t('description')}</p>
                <p className="text-amber-600 dark:text-amber-400">{t('warning')}</p>
            </div>

            <SettingRow label={t('status')}>
                {status ? (
                    <span className={`rounded-full border px-2.5 py-1 text-xs font-medium ${PHASE_STYLES[status.phase]}`}>
                        {t(`phase.${status.phase}`)}
                    </span>
                ) : (
                    <span className="text-sm text-muted-foreground">{statusQuery.isPending ? t('loading') : t('loadFailed')}</span>
                )}
            </SettingRow>

            {status ? (
                <>
                    <SettingRow label={t('publicUrl')}>
                        <URLValue value={status.public_url} />
                    </SettingRow>
                    <SettingRow label={t('apiUrl')}>
                        <URLValue value={status.api_url} />
                    </SettingRow>
                    <SettingRow label={t('target')}>
                        <code className="max-w-64 truncate text-xs" title={status.target_url}>{status.target_url}</code>
                    </SettingRow>
                    <SettingRow icon={Terminal} label={t('binary')}>
                        <code className="max-w-64 truncate text-xs text-muted-foreground" title={status.binary_path || t('notFound')}>
                            {status.binary_path || t('notFound')}
                        </code>
                    </SettingRow>
                </>
            ) : null}

            {status?.phase === 'not_installed' ? (
                <div className="space-y-3 rounded-2xl bg-muted/60 p-3">
                    <p className="text-xs text-muted-foreground">{t('installDesc')}</p>
                    <div className="flex flex-col gap-2 sm:flex-row">
                        <Input
                            type="password"
                            value={sudoPassword}
                            onChange={(event) => setSudoPassword(event.target.value)}
                            placeholder={t('sudoPlaceholder')}
                            autoComplete="off"
                            className="rounded-xl text-sm"
                        />
                        <Button
                            size="sm"
                            className="shrink-0 rounded-xl"
                            onClick={handleInstall}
                            disabled={setupPending}
                        >
                            {installTailscale.isPending ? <LoaderCircle className="size-4 animate-spin" /> : <Download className="size-4" />}
                            {installTailscale.isPending ? t('installing') : t('installAction')}
                        </Button>
                    </div>
                    <p className="text-xs text-muted-foreground">
                        {t('sudoHint')}{' '}
                        <a
                            href="https://tailscale.com/download"
                            target="_blank"
                            rel="noreferrer"
                            className="inline-flex items-center gap-1 text-primary hover:underline"
                        >
                            {t('installLink')} <ExternalLink className="size-3" />
                        </a>
                    </p>
                </div>
            ) : null}

            {status?.phase === 'credentials_needed' ? (
                <p className="rounded-2xl bg-amber-500/10 p-3 text-xs text-amber-700 dark:text-amber-300">{t('credentialHint')}</p>
            ) : null}

            {status?.phase === 'needs_login' ? (
                <div className="flex flex-col gap-2 rounded-2xl bg-amber-500/10 p-3 sm:flex-row sm:items-center sm:justify-between">
                    <p className="text-xs text-amber-700 dark:text-amber-300">{t('loginHint')}</p>
                    <Button
                        size="sm"
                        variant="outline"
                        className="shrink-0 rounded-xl"
                        onClick={handleLogin}
                        disabled={setupPending}
                    >
                        {loginTailscale.isPending ? <LoaderCircle className="size-4 animate-spin" /> : <LogIn className="size-4" />}
                        {loginTailscale.isPending ? t('loggingIn') : t('loginAction')}
                    </Button>
                </div>
            ) : null}

            {status?.approval_url ? (
                <a
                    href={status.approval_url}
                    target="_blank"
                    rel="noreferrer"
                    className="flex items-center gap-1 rounded-2xl bg-amber-500/10 p-3 text-xs text-amber-700 hover:underline dark:text-amber-300"
                >
                    {t('approvalHint')} <ExternalLink className="size-3" />
                </a>
            ) : null}

            {status?.last_error ? (
                <p className="break-words text-xs text-destructive">{t('lastError')}: {status.last_error}</p>
            ) : null}

            <div className="flex justify-end gap-2 border-t border-border pt-4">
                <Button
                    variant="ghost"
                    size="sm"
                    className="rounded-xl"
                        onClick={() => statusQuery.refetch()}
                        disabled={statusQuery.isFetching || actionPending || setupPending}
                >
                    <RefreshCw className={`size-4 ${statusQuery.isFetching ? 'animate-spin' : ''}`} />
                    {t('refresh')}
                </Button>
                <Button
                    variant={status?.running ? 'destructive' : 'default'}
                    size="sm"
                    className="rounded-xl"
                    onClick={handleToggle}
                    disabled={actionDisabled}
                >
                    {actionPending ? (
                        <LoaderCircle className="size-4 animate-spin" />
                    ) : status?.running ? (
                        <Square className="size-4" />
                    ) : (
                        <Play className="size-4" />
                    )}
                    {actionPending
                        ? (status?.running ? t('stopping') : t('starting'))
                        : (status?.running ? t('stop') : t('start'))}
                </Button>
            </div>
        </SettingCard>
    );
}
