'use client';

import { useMemo, useState, type FormEvent } from 'react';
import { useTranslations } from 'next-intl';
import { KeyRound, Plus, Trash2 } from 'lucide-react';
import {
    useCreateVerificationPairing,
    useRevokeVerificationPairing,
    useVerificationPairings,
    type VerificationBridgePairing,
    type VerificationBridgePairingCreated,
} from '@/api/endpoints/site-recovery';
import { CopyIconButton } from '@/components/common/CopyButton';
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
import {
    Dialog,
    DialogContent,
    DialogDescription,
    DialogHeader,
    DialogTitle,
} from '@/components/ui/dialog';
import { Input } from '@/components/ui/input';
import { cn } from '@/lib/utils';

type VerificationPairingPanelProps = {
    enabled: boolean;
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

export function VerificationPairingPanel({
    enabled,
}: VerificationPairingPanelProps) {
    const t = useTranslations('proxyPool.verificationBridge');
    const pairingsQuery = useVerificationPairings(enabled);
    const createPairing = useCreateVerificationPairing();
    const revokePairing = useRevokeVerificationPairing();
    const [name, setName] = useState('');
    const [ttlDays, setTTLDays] = useState(30);
    const [createdPairing, setCreatedPairing] =
        useState<VerificationBridgePairingCreated | null>(null);
    const [revokeTarget, setRevokeTarget] =
        useState<VerificationBridgePairing | null>(null);
    const pairings = useMemo(
        () => pairingsQuery.data ?? [],
        [pairingsQuery.data],
    );
    const now = pairingsQuery.dataUpdatedAt;
    const activeCount = useMemo(
        () =>
            pairings.filter(
                (item) =>
                    !item.revoked_at &&
                    new Date(item.expires_at).getTime() > now,
            ).length,
        [pairings, now],
    );

    async function submitForm(event: FormEvent<HTMLFormElement>) {
        event.preventDefault();
        if (!name.trim()) {
            toast.error(t('nameRequired'));
            return;
        }
        try {
            const result = await createPairing.mutateAsync({
                name: name.trim(),
                ttl_days: Math.min(365, Math.max(1, Math.trunc(ttlDays))),
            });
            setName('');
            setCreatedPairing(result);
            toast.success(t('created'));
        } catch (error) {
            toast.error(errorMessage(error, t('operationFailed')));
        }
    }

    async function confirmRevoke() {
        if (!revokeTarget) return;
        try {
            await revokePairing.mutateAsync(revokeTarget.id);
            setRevokeTarget(null);
            toast.success(t('revoked'));
        } catch (error) {
            toast.error(errorMessage(error, t('operationFailed')));
        }
    }

    return (
        <>
            <div className="grid h-full min-h-0 grid-cols-1 overflow-hidden md:grid-cols-[1.1fr_0.9fr]">
                <section className="flex min-h-0 flex-col border-b md:border-b-0 md:border-r">
                    <div className="flex shrink-0 items-center justify-between gap-3 px-5 py-5 sm:px-6">
                        <div className="flex items-center gap-2">
                            <KeyRound className="size-5" />
                            <h3 className="text-lg font-semibold">{t('title')}</h3>
                        </div>
                        <Badge variant="outline">
                            {t('activeCount', { count: activeCount })}
                        </Badge>
                    </div>
                    <div className="min-h-0 flex-1 divide-y divide-border/60 overflow-y-auto border-y border-border/60 px-5 sm:px-6">
                        {pairingsQuery.isLoading ? (
                            <div className="py-6 text-sm text-muted-foreground">
                                {t('loading')}
                            </div>
                        ) : pairingsQuery.error ? (
                            <div className="py-5 text-sm text-destructive">
                                {errorMessage(
                                    pairingsQuery.error,
                                    t('operationFailed'),
                                )}
                            </div>
                        ) : pairings.length === 0 ? (
                            <div className="py-10 text-center text-sm text-muted-foreground">
                                {t('empty')}
                            </div>
                        ) : (
                            pairings.map((pairing) => {
                                const expiresAt = formatDateTime(
                                    pairing.expires_at,
                                );
                                const lastSeen = formatDateTime(
                                    pairing.last_seen_at,
                                );
                                const revoked = Boolean(pairing.revoked_at);
                                const expired =
                                    new Date(pairing.expires_at).getTime() <= now;
                                const status = revoked
                                    ? 'revoked'
                                    : expired
                                      ? 'expired'
                                      : 'active';
                                return (
                                    <article key={pairing.id} className="py-4">
                                        <div className="flex items-start justify-between gap-3">
                                            <div className="min-w-0">
                                                <div className="flex flex-wrap items-center gap-2">
                                                    <span className="truncate text-sm font-semibold">
                                                        {pairing.name}
                                                    </span>
                                                    <Badge
                                                        variant="outline"
                                                        className={cn(
                                                            status === 'active' &&
                                                                'border-emerald-500/40 text-emerald-600',
                                                            status !== 'active' &&
                                                                'text-muted-foreground',
                                                        )}
                                                    >
                                                        {t(`status.${status}`)}
                                                    </Badge>
                                                </div>
                                                <div className="mt-2 flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted-foreground">
                                                    {expiresAt ? (
                                                        <span>
                                                            {t('expiresAt', {
                                                                value: expiresAt,
                                                            })}
                                                        </span>
                                                    ) : null}
                                                    {lastSeen ? (
                                                        <span>
                                                            {t('lastSeenAt', {
                                                                value: lastSeen,
                                                            })}
                                                        </span>
                                                    ) : null}
                                                </div>
                                            </div>
                                            {!revoked ? (
                                                <Button
                                                    type="button"
                                                    size="icon-sm"
                                                    variant="ghost"
                                                    className="rounded-xl text-destructive hover:text-destructive"
                                                    onClick={() =>
                                                        setRevokeTarget(pairing)
                                                    }
                                                    aria-label={t('revoke')}
                                                    title={t('revoke')}
                                                >
                                                    <Trash2 className="size-4" />
                                                </Button>
                                            ) : null}
                                        </div>
                                    </article>
                                );
                            })
                        )}
                    </div>
                </section>

                <section className="min-h-0 overflow-y-auto px-5 py-5 sm:px-6">
                    <h3 className="text-lg font-semibold">{t('createTitle')}</h3>
                    <form onSubmit={submitForm} className="mt-4 space-y-4">
                        <label className="grid gap-1.5 text-sm">
                            <span className="font-medium">{t('name')}</span>
                            <Input
                                value={name}
                                onChange={(event) => setName(event.target.value)}
                                className="rounded-xl"
                            />
                        </label>
                        <label className="grid gap-1.5 text-sm">
                            <span className="font-medium">{t('ttlDays')}</span>
                            <Input
                                type="number"
                                min={1}
                                max={365}
                                value={ttlDays}
                                onChange={(event) =>
                                    setTTLDays(Number(event.target.value))
                                }
                                className="rounded-xl"
                            />
                        </label>
                        <Button
                            type="submit"
                            className="h-11 w-full rounded-xl"
                            disabled={createPairing.isPending}
                        >
                            <Plus className="size-4" />
                            {t('create')}
                        </Button>
                    </form>
                </section>
            </div>

            <Dialog
                open={createdPairing !== null}
                onOpenChange={(open) => !open && setCreatedPairing(null)}
            >
                <DialogContent className="rounded-2xl sm:max-w-xl">
                    <DialogHeader>
                        <DialogTitle>{t('tokenTitle')}</DialogTitle>
                        <DialogDescription>
                            {t('tokenDescription')}
                        </DialogDescription>
                    </DialogHeader>
                    <div className="flex items-center gap-2 rounded-xl border border-border/70 bg-muted/30 px-3 py-2">
                        <code className="min-w-0 flex-1 break-all text-xs">
                            {createdPairing?.token}
                        </code>
                        <CopyIconButton
                            text={createdPairing?.token ?? ''}
                            className="flex size-9 shrink-0 items-center justify-center rounded-lg text-muted-foreground hover:bg-muted hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                            copyIconClassName="size-4"
                            checkIconClassName="size-4 text-emerald-600"
                        />
                    </div>
                    <Button
                        type="button"
                        className="rounded-xl"
                        onClick={() => setCreatedPairing(null)}
                    >
                        {t('done')}
                    </Button>
                </DialogContent>
            </Dialog>

            <AlertDialog
                open={revokeTarget !== null}
                onOpenChange={(open) => !open && setRevokeTarget(null)}
            >
                <AlertDialogContent>
                    <AlertDialogHeader>
                        <AlertDialogTitle>{t('revokeConfirmTitle')}</AlertDialogTitle>
                        <AlertDialogDescription>
                            {t('revokeConfirmDescription', {
                                name: revokeTarget?.name ?? '',
                            })}
                        </AlertDialogDescription>
                    </AlertDialogHeader>
                    <AlertDialogFooter>
                        <AlertDialogCancel>{t('cancel')}</AlertDialogCancel>
                        <AlertDialogAction
                            className="bg-destructive text-white hover:bg-destructive/90"
                            onClick={confirmRevoke}
                            disabled={revokePairing.isPending}
                        >
                            {t('revoke')}
                        </AlertDialogAction>
                    </AlertDialogFooter>
                </AlertDialogContent>
            </AlertDialog>
        </>
    );
}
