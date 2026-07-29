'use client';

import { useMemo, useState } from 'react';
import { AlertCircle, Plus, ShieldCheck } from 'lucide-react';
import { useTranslations } from 'next-intl';
import { useChannelList } from '@/api/endpoints/channel';
import {
    type HeaderPolicy,
    useHeaderPolicies,
} from '@/api/endpoints/header-policy';
import { useModelCatalog } from '@/api/endpoints/model-catalog';
import { useSiteList } from '@/api/endpoints/site';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { cn } from '@/lib/utils';
import { HeaderPolicyEditor } from './HeaderPolicyEditor';
import { HeaderPolicyPreview } from './HeaderPolicyPreview';
import type { ScopeTargets } from './header-policy-options';

export function HeaderPolicies() {
    const t = useTranslations('model.headerPolicy');
    const policies = useHeaderPolicies();
    const channels = useChannelList();
    const sites = useSiteList();
    const catalog = useModelCatalog();
    const [selectedId, setSelectedId] = useState<number | 'new' | null>(null);
    const loadError = policies.error ?? channels.error ?? sites.error ?? catalog.error;

    const targets = useMemo<ScopeTargets>(() => {
        const channelTargets = (channels.data ?? []).map((item) => ({
            id: item.raw.id,
            label: item.raw.name,
        }));
        const siteTargets = (sites.data ?? []).map((site) => ({ id: site.id, label: site.name }));
        const accountTargets = (sites.data ?? []).flatMap((site) =>
            site.accounts.map((account) => ({
                id: account.id,
                label: `${site.name} / ${account.name}`,
            })),
        );
        const canonicalTargets = (catalog.data ?? []).map((item) => ({
            id: item.id,
            label: item.name,
        }));
        const candidateTargets = (catalog.data ?? []).flatMap((item) =>
            item.route_candidates.map((candidate) => ({
                id: candidate.id,
                label: `${item.name} / ${candidate.upstream_model_name} (#${candidate.channel_id})`,
            })),
        );
        return {
            global: [{ id: 0, label: t('scope.global') }],
            site: siteTargets,
            site_account: accountTargets,
            channel: channelTargets,
            canonical_model: canonicalTargets,
            route_candidate: candidateTargets,
        };
    }, [catalog.data, channels.data, sites.data, t]);

    const selected =
        typeof selectedId === 'number'
            ? (policies.data ?? []).find((policy) => policy.id === selectedId) ?? null
            : null;
    const defaultSelected = selectedId === null ? policies.data?.[0] ?? null : selected;
    const editingNew = selectedId === 'new';

    return (
        <div className="grid h-full min-h-0 grid-rows-[minmax(12rem,2fr)_minmax(0,3fr)] gap-3 md:grid-cols-[18rem_minmax(0,1fr)] md:grid-rows-1 md:gap-4">
            <aside
                aria-label={t('title')}
                className="flex min-h-0 flex-col overflow-hidden rounded-lg border bg-card"
            >
                <div className="flex items-center justify-between gap-2 border-b px-3 py-2">
                    <div>
                        <h2 className="text-sm font-semibold">{t('title')}</h2>
                        <p className="text-xs tabular-nums text-muted-foreground">
                            {t('policyCount', { count: policies.data?.length ?? 0 })}
                        </p>
                    </div>
                    <Button
                        type="button"
                        variant="ghost"
                        size="icon"
                        aria-label={t('newPolicy')}
                        title={t('newPolicy')}
                        onClick={() => setSelectedId('new')}
                    >
                        <Plus />
                    </Button>
                </div>
                <div className="min-h-0 flex-1 overflow-y-auto p-1.5">
                    {loadError ? (
                        <div role="alert" className="m-2 rounded-md border border-destructive/30 bg-destructive/10 p-3 text-sm text-destructive">
                            <AlertCircle className="mb-2 size-4" />
                            <p>{t('loadFailed', { message: loadError instanceof Error ? loadError.message : String(loadError) })}</p>
                            <Button
                                type="button"
                                variant="outline"
                                size="sm"
                                className="mt-3"
                                onClick={() => void Promise.all([
                                    policies.refetch(),
                                    channels.refetch(),
                                    sites.refetch(),
                                    catalog.refetch(),
                                ])}
                            >
                                {t('retry')}
                            </Button>
                        </div>
                    ) : null}
                    {(policies.data ?? []).map((policy) => (
                        <PolicyListItem
                            key={policy.id}
                            policy={policy}
                            targetLabel={policyTargetLabel(policy, targets, t)}
                            selected={(selectedId === null ? policies.data?.[0]?.id : selectedId) === policy.id}
                            onSelect={() => setSelectedId(policy.id)}
                        />
                    ))}
                    {editingNew ? (
                        <div className="mb-1 min-h-12 rounded-md bg-primary/10 px-3 py-2 ring-1 ring-primary/40">
                            <span className="block text-sm font-medium">{t('newPolicy')}</span>
                        </div>
                    ) : null}
                    {!policies.isLoading && !policies.error && (policies.data ?? []).length === 0 && !editingNew ? (
                        <div className="grid min-h-40 place-items-center px-4 text-center text-sm text-muted-foreground">
                            <div>
                                <ShieldCheck className="mx-auto mb-2 size-5" />
                                {t('empty')}
                            </div>
                        </div>
                    ) : null}
                </div>
            </aside>

            <section
                aria-label={t('editPolicy')}
                className="min-h-0 overflow-y-auto rounded-lg border bg-card"
            >
                <div className="space-y-6 p-4 md:p-5">
                    <HeaderPolicyEditor
                        key={editingNew ? 'new' : defaultSelected?.id ?? 'empty'}
                        policy={editingNew ? null : defaultSelected}
                        targets={targets}
                        onSaved={(policy) => setSelectedId(policy.id)}
                        onDeleted={() => setSelectedId(null)}
                    />
                    <HeaderPolicyPreview targets={targets} />
                </div>
            </section>
        </div>
    );
}

function policyTargetLabel(
    policy: HeaderPolicy,
    targets: ScopeTargets,
    t: ReturnType<typeof useTranslations>,
) {
    if (policy.scope === 'global') return t('scope.global');
    const target = targets[policy.scope].find((item) => item.id === policy.scope_id);
    return target?.label ?? `${t(`scope.${policy.scope}`)} #${policy.scope_id}`;
}

function PolicyListItem({
    policy,
    targetLabel,
    selected,
    onSelect,
}: {
    policy: HeaderPolicy;
    targetLabel: string;
    selected: boolean;
    onSelect: () => void;
}) {
    const t = useTranslations('model.headerPolicy');
    return (
        <button
            type="button"
            aria-pressed={selected}
            onClick={onSelect}
            className={cn(
                'mb-1 min-h-12 w-full rounded-md px-3 py-2 text-left transition-colors',
                selected ? 'bg-primary text-primary-foreground' : 'hover:bg-muted',
            )}
        >
            <span className="flex min-w-0 items-center gap-2">
                <span className="min-w-0 flex-1 truncate text-sm font-medium">
                    {policy.name || targetLabel}
                </span>
                {!policy.enabled ? <Badge variant="outline">{t('disabled')}</Badge> : null}
            </span>
            <span
                title={targetLabel}
                className={cn(
                    'block truncate text-xs',
                    selected ? 'text-primary-foreground/75' : 'text-muted-foreground',
                )}
            >
                {t(`scope.${policy.scope}`)} · {targetLabel} · {t('version', { version: policy.version })}
            </span>
        </button>
    );
}
