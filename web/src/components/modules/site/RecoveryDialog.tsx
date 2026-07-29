'use client';

import { useRef, useState } from 'react';
import { useTranslations } from 'next-intl';
import { Activity, Route, ShieldCheck, XIcon } from 'lucide-react';
import type { Site, SiteAccount } from '@/api/endpoints/site';
import { useProxyConfigurationList } from '@/api/endpoints/proxy-pool';
import {
    useClearAccountProxyPreference,
    useClearSiteProxyPreference,
    useSiteOperationAttempts,
    useSiteProxyPreferences,
} from '@/api/endpoints/site-recovery';
import { toast } from '@/components/common/Toast';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/animate-ui/components/animate/tabs';
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
    Dialog,
    DialogContent,
    DialogDescription,
    DialogTitle,
} from '@/components/ui/dialog';
import { RecoveryAttempts } from './RecoveryAttempts';
import { RecoveryOverview } from './RecoveryOverview';
import { VerificationPanel } from './VerificationPanel';

type RecoveryDialogProps = {
    open: boolean;
    onOpenChange: (open: boolean) => void;
    site: Site | null;
    account: SiteAccount | null;
};

type RecoveryTab = 'overview' | 'attempts' | 'verification';
type ConfirmAction = 'clear-account-preference' | 'clear-site-preference' | null;

function errorMessage(error: unknown, fallback: string) {
    if (error instanceof Error) return error.message;
    if (typeof error === 'object' && error !== null && 'message' in error) {
        const message = (error as { message?: unknown }).message;
        if (typeof message === 'string') return message;
    }
    return fallback;
}

function stableReturnFocusTarget(activeElement: Element | null) {
    if (!(activeElement instanceof HTMLElement)) return null;
    const popover = activeElement.closest<HTMLElement>('[data-slot="popover-content"]');
    if (!popover?.id) return activeElement;
    const triggers = document.querySelectorAll<HTMLElement>('[data-slot="popover-trigger"]');
    for (const trigger of triggers) {
        if (trigger.getAttribute('aria-controls') === popover.id) return trigger;
    }
    return activeElement;
}

export function RecoveryDialog({
    open,
    onOpenChange,
    site,
    account,
}: RecoveryDialogProps) {
    const t = useTranslations('siteRecovery');
    const [tab, setTab] = useState<RecoveryTab>('overview');
    const [confirmAction, setConfirmAction] = useState<ConfirmAction>(null);
    const preferencesQuery = useSiteProxyPreferences(account?.id ?? null, open);
    const attemptsQuery = useSiteOperationAttempts(account?.id ?? null, open);
    const proxiesQuery = useProxyConfigurationList();
    const clearAccountPreference = useClearAccountProxyPreference();
    const clearSitePreference = useClearSiteProxyPreference();
    const returnFocusRef = useRef<HTMLElement | null>(null);

    if (!site || !account) {
        return null;
    }
    const currentSite = site;
    const currentAccount = account;

    async function runConfirmedAction() {
        if (!confirmAction) return;
        try {
            if (confirmAction === 'clear-account-preference') {
                await clearAccountPreference.mutateAsync(currentAccount.id);
                toast.success(t('overview.accountCleared'));
            } else {
                await clearSitePreference.mutateAsync(currentSite.id);
                toast.success(t('overview.siteCleared'));
            }
            setConfirmAction(null);
        } catch (error) {
            toast.error(errorMessage(error, t('operationFailed')));
        }
    }

    return (
        <>
            <Dialog open={open} onOpenChange={onOpenChange}>
                <DialogContent
                    showCloseButton={false}
                    onOpenAutoFocus={() => {
                        returnFocusRef.current = stableReturnFocusTarget(document.activeElement);
                    }}
                    onCloseAutoFocus={(event) => {
                        const target = returnFocusRef.current;
                        returnFocusRef.current = null;
                        if (!target?.isConnected) return;
                        event.preventDefault();
                        target.focus();
                    }}
                    className="flex h-[min(90dvh,52rem)] w-screen max-w-full flex-col gap-0 overflow-hidden rounded-2xl border bg-card p-0 text-card-foreground sm:max-w-4xl"
                >
                    <header className="flex shrink-0 items-start justify-between gap-4 border-b border-border/60 px-5 py-4 sm:px-6">
                        <div className="min-w-0">
                            <div className="flex items-center gap-2">
                                <ShieldCheck className="size-5 text-primary" />
                                <DialogTitle className="truncate text-xl font-bold">
                                    {t('title')}
                                </DialogTitle>
                            </div>
                            <DialogDescription className="mt-1 truncate text-sm text-muted-foreground">
                                {currentSite.name} / {currentAccount.name}
                            </DialogDescription>
                        </div>
                        <button
                            type="button"
                            onClick={() => onOpenChange(false)}
                            aria-label={t('close')}
                            title={t('close')}
                            className="flex size-10 shrink-0 items-center justify-center rounded-lg text-muted-foreground transition-colors hover:bg-muted hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                        >
                            <XIcon className="size-5" />
                        </button>
                    </header>

                    <Tabs
                        value={tab}
                        onValueChange={(value) => setTab(value as RecoveryTab)}
                        className="flex min-h-0 flex-1"
                    >
                        <div className="shrink-0 overflow-x-auto border-b border-border/60 px-5 py-3 sm:px-6">
                            <TabsList className="w-max min-w-full">
                                <TabsTrigger value="overview">
                                    <Route className="size-4" />
                                    {t('tabs.overview')}
                                </TabsTrigger>
                                <TabsTrigger value="attempts">
                                    <Activity className="size-4" />
                                    {t('tabs.attempts')}
                                </TabsTrigger>
                                <TabsTrigger value="verification">
                                    <ShieldCheck className="size-4" />
                                    {t('tabs.verification')}
                                </TabsTrigger>
                            </TabsList>
                        </div>

                        <TabsContent
                            value="overview"
                            className="min-h-0 flex-1 overflow-y-auto px-5 py-5 sm:px-6"
                        >
                            {tab === 'overview' ? (
                                <RecoveryOverview
                                    site={currentSite}
                                    account={currentAccount}
                                    preferences={preferencesQuery.data ?? []}
                                    proxies={proxiesQuery.data ?? []}
                                    isLoading={preferencesQuery.isLoading}
                                    error={preferencesQuery.error}
                                    onClearAccountPreference={() =>
                                        setConfirmAction(
                                            'clear-account-preference',
                                        )
                                    }
                                    onClearSitePreference={() =>
                                        setConfirmAction('clear-site-preference')
                                    }
                                    clearingAccount={
                                        clearAccountPreference.isPending
                                    }
                                    clearingSite={clearSitePreference.isPending}
                                />
                            ) : null}
                        </TabsContent>
                        <TabsContent
                            value="attempts"
                            className="min-h-0 flex-1 overflow-y-auto px-5 py-5 sm:px-6"
                        >
                            {tab === 'attempts' ? (
                                <RecoveryAttempts
                                    attempts={attemptsQuery.data ?? []}
                                    isLoading={attemptsQuery.isLoading}
                                    error={attemptsQuery.error}
                                />
                            ) : null}
                        </TabsContent>
                        <TabsContent
                            value="verification"
                            className="min-h-0 flex-1 overflow-y-auto px-5 py-5 sm:px-6"
                        >
                            {tab === 'verification' ? (
                                <VerificationPanel
                                    open={open}
                                    account={currentAccount}
                                />
                            ) : null}
                        </TabsContent>
                    </Tabs>
                </DialogContent>
            </Dialog>

            <AlertDialog
                open={confirmAction !== null}
                onOpenChange={(nextOpen) => !nextOpen && setConfirmAction(null)}
            >
                <AlertDialogContent>
                    <AlertDialogHeader>
                        <AlertDialogTitle>
                            {confirmAction === 'clear-site-preference'
                                ? t('overview.clearSiteConfirmTitle')
                                : t('overview.clearAccountConfirmTitle')}
                        </AlertDialogTitle>
                        <AlertDialogDescription>
                            {t('overview.clearConfirmDescription')}
                        </AlertDialogDescription>
                    </AlertDialogHeader>
                    <AlertDialogFooter>
                        <AlertDialogCancel>{t('cancel')}</AlertDialogCancel>
                        <AlertDialogAction
                            className="bg-destructive text-white hover:bg-destructive/90"
                            onClick={runConfirmedAction}
                            disabled={
                                clearAccountPreference.isPending ||
                                clearSitePreference.isPending
                            }
                        >
                            {t('confirm')}
                        </AlertDialogAction>
                    </AlertDialogFooter>
                </AlertDialogContent>
            </AlertDialog>
        </>
    );
}
