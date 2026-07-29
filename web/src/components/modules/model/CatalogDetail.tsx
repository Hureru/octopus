'use client';

import { useState } from 'react';
import { AlertCircle, Plus, RefreshCw, Save, Trash2 } from 'lucide-react';
import { useTranslations } from 'next-intl';
import {
    type CanonicalModel,
    type CanonicalModelUpdate,
    type ProtocolPolicy,
    type RouteCandidate,
    type RouteCandidateStatus,
    type RouteCandidateUpdate,
    type RoutingStrategy,
    useDeleteModelAlias,
    useUpdateCanonicalModel,
    useUpdateRouteCandidate,
    useUpsertModelAlias,
} from '@/api/endpoints/model-catalog';
import { toast } from '@/components/common/Toast';
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
import { Switch } from '@/components/ui/switch';
import { cn } from '@/lib/utils';
import {
    CANDIDATE_PROTOCOL_POLICIES,
    CANDIDATE_STATUSES,
    CANONICAL_PROTOCOL_POLICIES,
    ROUTING_STRATEGIES,
} from './catalog-options';
import { PricingPanel } from './PricingPanel';
import { RouteTools } from './RouteTools';

type NameMap = Map<number, string>;

function canonicalDraft(canonical: CanonicalModel): CanonicalModelUpdate {
    return {
        id: canonical.id,
        routing_strategy: canonical.routing_strategy,
        protocol_policy: canonical.protocol_policy,
        allow_lossy: canonical.allow_lossy,
        enabled: canonical.enabled,
    };
}

function candidateDraft(candidate: RouteCandidate): RouteCandidateUpdate {
    return {
        id: candidate.id,
        status: candidate.status,
        priority: candidate.priority,
        weight: candidate.weight,
        protocol_policy: candidate.protocol_policy,
        allow_lossy: candidate.allow_lossy,
    };
}

function errorMessage(error: unknown) {
    return error instanceof Error ? error.message : String(error);
}

export function CatalogDetail({
    canonical,
    channelNameById,
    siteNameById,
    accountNameById,
}: {
    canonical: CanonicalModel;
    channelNameById: NameMap;
    siteNameById: NameMap;
    accountNameById: NameMap;
}) {
    const t = useTranslations('model.catalog');
    const updateCanonical = useUpdateCanonicalModel();
    const upsertAlias = useUpsertModelAlias();
    const deleteAlias = useDeleteModelAlias();
    const [draftUpdates, setDraftUpdates] = useState<Partial<CanonicalModelUpdate>>({});
    const [editBaseUpdatedAt, setEditBaseUpdatedAt] = useState<string | null>(null);
    const [alias, setAlias] = useState('');
    const [selectedCandidateId, setSelectedCandidateId] = useState<number | null>(
        canonical.route_candidates[0]?.id ?? null,
    );

    const effectiveCandidateId = canonical.route_candidates.some(
        (candidate) => candidate.id === selectedCandidateId,
    )
        ? selectedCandidateId
        : canonical.route_candidates[0]?.id ?? null;
    const selectedCandidate =
        canonical.route_candidates.find((candidate) => candidate.id === effectiveCandidateId) ?? null;
    const draft = { ...canonicalDraft(canonical), ...draftUpdates };
    const draftDirty = Object.keys(draftUpdates).length > 0;
    const serverChanged =
        draftDirty &&
        editBaseUpdatedAt !== null &&
        editBaseUpdatedAt !== canonical.updated_at;

    const updateDraft = (updates: Partial<CanonicalModelUpdate>) => {
        setEditBaseUpdatedAt((current) => current ?? canonical.updated_at);
        setDraftUpdates((current) => ({ ...current, ...updates }));
    };

    const reloadCanonical = () => {
        setDraftUpdates({});
        setEditBaseUpdatedAt(null);
    };

    const saveCanonical = () => {
        updateCanonical.mutate(draft, {
            onSuccess: () => {
                setDraftUpdates({});
                setEditBaseUpdatedAt(null);
                toast.success(t('canonicalSaved'));
            },
            onError: (error) => toast.error(t('canonicalSaveFailed'), { description: errorMessage(error) }),
        });
    };

    const addAlias = () => {
        const value = alias.trim();
        if (!value) return;
        upsertAlias.mutate({ canonical_model_id: canonical.id, alias: value }, {
            onSuccess: () => {
                setAlias('');
                toast.success(t('aliasSaved'));
            },
            onError: (error) => toast.error(t('aliasSaveFailed'), { description: errorMessage(error) }),
        });
    };

    return (
        <div className="space-y-5 p-4 md:p-5">
            <header className="flex flex-col gap-3 border-b pb-4 sm:flex-row sm:items-start sm:justify-between">
                <div className="min-w-0">
                    <h2 className="truncate text-lg font-semibold">{canonical.name}</h2>
                    <p className="truncate text-xs text-muted-foreground">{canonical.normalized_name}</p>
                </div>
                <RouteTools model={canonical.name} />
            </header>

            <section className="space-y-3">
                <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
                    <label className="grid gap-1 text-xs text-muted-foreground">
                        {t('name')}
                        <Input
                            value={canonical.name}
                            readOnly
                            aria-readonly="true"
                            className="cursor-default bg-muted/30"
                        />
                    </label>
                    <label className="grid gap-1 text-xs text-muted-foreground">
                        {t('strategyLabel')}
                        <Select
                            value={draft.routing_strategy}
                            onValueChange={(value) =>
                                updateDraft({ routing_strategy: value as RoutingStrategy })
                            }
                        >
                            <SelectTrigger className="w-full"><SelectValue /></SelectTrigger>
                            <SelectContent>
                                {ROUTING_STRATEGIES.map((strategy) => (
                                    <SelectItem key={strategy} value={strategy}>{t(`strategy.${strategy}`)}</SelectItem>
                                ))}
                            </SelectContent>
                        </Select>
                    </label>
                    <label className="grid gap-1 text-xs text-muted-foreground">
                        {t('protocolPolicyLabel')}
                        <Select
                            value={draft.protocol_policy}
                            onValueChange={(value) =>
                                updateDraft({
                                    protocol_policy: value as CanonicalModel['protocol_policy'],
                                })
                            }
                        >
                            <SelectTrigger className="w-full"><SelectValue /></SelectTrigger>
                            <SelectContent>
                                {CANONICAL_PROTOCOL_POLICIES.map((policy) => (
                                    <SelectItem key={policy} value={policy}>{t(`policy.${policy}`)}</SelectItem>
                                ))}
                            </SelectContent>
                        </Select>
                    </label>
                    <div className="grid grid-cols-2 gap-2">
                        <ToggleField
                            label={t('enabled')}
                            checked={draft.enabled}
                            onCheckedChange={(enabled) => updateDraft({ enabled })}
                        />
                        <ToggleField
                            label={t('allowLossy')}
                            checked={draft.allow_lossy}
                            onCheckedChange={(allow_lossy) => updateDraft({ allow_lossy })}
                        />
                    </div>
                </div>
                <div className="flex flex-wrap items-center gap-2">
                    <Button
                        type="button"
                        size="sm"
                        onClick={saveCanonical}
                        disabled={updateCanonical.isPending || !draftDirty || serverChanged}
                    >
                        <Save />
                        {updateCanonical.isPending ? t('saving') : t('saveCanonical')}
                    </Button>
                    {canonical.manual ? <Badge variant="outline">{t('manual')}</Badge> : null}
                </div>
                {serverChanged ? (
                    <DraftConflict onReload={reloadCanonical} />
                ) : null}
            </section>

            <section className="space-y-3 border-t pt-4">
                <div className="flex items-center justify-between">
                    <h3 className="text-sm font-semibold">{t('aliases')}</h3>
                    <Badge variant="outline">{canonical.aliases.length}</Badge>
                </div>
                <div className="flex flex-wrap gap-2">
                    {canonical.aliases.map((item) => (
                        <span key={item.id} className="inline-flex min-h-9 items-center gap-1 rounded-md border pl-2 text-sm">
                            <span className="max-w-64 truncate">{item.alias}</span>
                            <button
                                type="button"
                                aria-label={t('deleteAlias', { alias: item.alias })}
                                className="grid size-9 place-items-center rounded-md text-muted-foreground hover:bg-muted hover:text-destructive"
                                onClick={() =>
                                    deleteAlias.mutate(item.id, {
                                        onError: (error) =>
                                            toast.error(t('aliasDeleteFailed'), { description: errorMessage(error) }),
                                    })
                                }
                            >
                                <Trash2 className="size-3.5" />
                            </button>
                        </span>
                    ))}
                </div>
                <div className="flex max-w-lg gap-2">
                    <Input
                        value={alias}
                        onChange={(event) => setAlias(event.target.value)}
                        onKeyDown={(event) => {
                            if (event.key === 'Enter') {
                                event.preventDefault();
                                addAlias();
                            }
                        }}
                        placeholder={t('aliasPlaceholder')}
                    />
                    <Button type="button" variant="outline" onClick={addAlias} disabled={!alias.trim() || upsertAlias.isPending}>
                        <Plus />
                        {t('addAlias')}
                    </Button>
                </div>
            </section>

            <section className="space-y-3 border-t pt-4">
                <div className="flex items-center justify-between">
                    <h3 className="text-sm font-semibold">{t('candidates')}</h3>
                    <Badge variant="outline">{canonical.route_candidates.length}</Badge>
                </div>
                <div className="grid gap-4 lg:grid-cols-[minmax(15rem,0.8fr)_minmax(18rem,1.2fr)]">
                    <div className="max-h-80 overflow-y-auto rounded-md border p-1">
                        {canonical.route_candidates.map((candidate) => (
                            <CandidateRow
                                key={candidate.id}
                                candidate={candidate}
                                channelName={channelNameById.get(candidate.channel_id)}
                                selected={candidate.id === effectiveCandidateId}
                                onSelect={() => setSelectedCandidateId(candidate.id)}
                            />
                        ))}
                        {canonical.route_candidates.length === 0 ? (
                            <div className="grid min-h-24 place-items-center text-sm text-muted-foreground">
                                {t('noCandidates')}
                            </div>
                        ) : null}
                    </div>
                    {selectedCandidate ? (
                        <CandidateEditor
                            key={selectedCandidate.id}
                            candidate={selectedCandidate}
                            channelName={channelNameById.get(selectedCandidate.channel_id)}
                            siteName={selectedCandidate.site_id ? siteNameById.get(selectedCandidate.site_id) : undefined}
                            accountName={selectedCandidate.site_account_id ? accountNameById.get(selectedCandidate.site_account_id) : undefined}
                        />
                    ) : null}
                </div>
            </section>

            {selectedCandidate ? <PricingPanel key={selectedCandidate.id} candidate={selectedCandidate} /> : null}
        </div>
    );
}

function ToggleField({
    label,
    checked,
    onCheckedChange,
}: {
    label: string;
    checked: boolean;
    onCheckedChange: (checked: boolean) => void;
}) {
    return (
        <label className="flex min-h-14 items-center justify-between gap-2 rounded-md border px-3 text-xs text-muted-foreground">
            {label}
            <Switch checked={checked} onCheckedChange={onCheckedChange} />
        </label>
    );
}

function CandidateRow({
    candidate,
    channelName,
    selected,
    onSelect,
}: {
    candidate: RouteCandidate;
    channelName?: string;
    selected: boolean;
    onSelect: () => void;
}) {
    const t = useTranslations('model.catalog');
    return (
        <button
            type="button"
            aria-pressed={selected}
            onClick={onSelect}
            className={cn(
                'mb-1 min-h-14 w-full rounded-md px-3 py-2 text-left transition-colors',
                selected ? 'bg-primary/10 text-foreground ring-1 ring-primary/40' : 'hover:bg-muted',
            )}
        >
            <span className="flex min-w-0 items-center gap-2">
                <span className="min-w-0 flex-1 truncate text-sm font-medium">{candidate.upstream_model_name}</span>
                <Badge variant="outline">{t(`status.${candidate.status}`)}</Badge>
            </span>
            <span className="mt-1 block truncate text-xs text-muted-foreground">
                {channelName ?? `#${candidate.channel_id}`} · P{candidate.priority} · W{candidate.weight}
            </span>
        </button>
    );
}

function CandidateEditor({
    candidate,
    channelName,
    siteName,
    accountName,
}: {
    candidate: RouteCandidate;
    channelName?: string;
    siteName?: string;
    accountName?: string;
}) {
    const t = useTranslations('model.catalog');
    const updateCandidate = useUpdateRouteCandidate();
    const [draftUpdates, setDraftUpdates] = useState<Partial<RouteCandidateUpdate>>({});
    const [editBaseUpdatedAt, setEditBaseUpdatedAt] = useState<string | null>(null);
    const draft = { ...candidateDraft(candidate), ...draftUpdates };
    const draftDirty = Object.keys(draftUpdates).length > 0;
    const serverChanged =
        draftDirty &&
        editBaseUpdatedAt !== null &&
        editBaseUpdatedAt !== candidate.updated_at;
    const lossyMode = draft.allow_lossy == null ? 'inherit' : draft.allow_lossy ? 'allow' : 'deny';

    const updateDraft = (updates: Partial<RouteCandidateUpdate>) => {
        setEditBaseUpdatedAt((current) => current ?? candidate.updated_at);
        setDraftUpdates((current) => ({ ...current, ...updates }));
    };

    const reloadCandidate = () => {
        setDraftUpdates({});
        setEditBaseUpdatedAt(null);
    };

    const save = () => {
        updateCandidate.mutate(draft, {
            onSuccess: () => {
                setDraftUpdates({});
                setEditBaseUpdatedAt(null);
                toast.success(t('candidateSaved'));
            },
            onError: (error) => toast.error(t('candidateSaveFailed'), { description: errorMessage(error) }),
        });
    };

    return (
        <div className="space-y-3">
            <div>
                <h4 className="truncate text-sm font-semibold">{candidate.upstream_model_name}</h4>
                <p className="break-words text-xs text-muted-foreground">
                    {channelName ?? `#${candidate.channel_id}`}
                    {siteName ? ` · ${siteName}` : ''}
                    {accountName ? ` / ${accountName}` : ''}
                    {candidate.site_group_key ? ` · ${candidate.site_group_key}` : ''}
                </p>
            </div>
            <div className="grid gap-3 sm:grid-cols-2">
                <label className="grid gap-1 text-xs text-muted-foreground">
                    {t('statusLabel')}
                    <Select
                        value={draft.status}
                        onValueChange={(value) => updateDraft({ status: value as RouteCandidateStatus })}
                    >
                        <SelectTrigger className="w-full"><SelectValue /></SelectTrigger>
                        <SelectContent>
                            {CANDIDATE_STATUSES.map((status) => (
                                <SelectItem key={status} value={status}>{t(`status.${status}`)}</SelectItem>
                            ))}
                        </SelectContent>
                    </Select>
                </label>
                <label className="grid gap-1 text-xs text-muted-foreground">
                    {t('protocolPolicyLabel')}
                    <Select
                        value={draft.protocol_policy}
                        onValueChange={(value) => updateDraft({ protocol_policy: value as ProtocolPolicy })}
                    >
                        <SelectTrigger className="w-full"><SelectValue /></SelectTrigger>
                        <SelectContent>
                            {CANDIDATE_PROTOCOL_POLICIES.map((policy) => (
                                <SelectItem key={policy} value={policy}>{t(`policy.${policy}`)}</SelectItem>
                            ))}
                        </SelectContent>
                    </Select>
                </label>
                <label className="grid gap-1 text-xs text-muted-foreground">
                    {t('priority')}
                    <Input
                        type="number"
                        value={draft.priority}
                        onChange={(event) => updateDraft({ priority: Number(event.target.value) || 0 })}
                    />
                </label>
                <label className="grid gap-1 text-xs text-muted-foreground">
                    {t('weight')}
                    <Input
                        type="number"
                        min="0"
                        value={draft.weight}
                        onChange={(event) => updateDraft({ weight: Math.max(0, Number(event.target.value) || 0) })}
                    />
                </label>
                <label className="grid gap-1 text-xs text-muted-foreground sm:col-span-2">
                    {t('allowLossy')}
                    <Select
                        value={lossyMode}
                        onValueChange={(value) =>
                            updateDraft({
                                allow_lossy: value === 'inherit' ? null : value === 'allow',
                            })
                        }
                    >
                        <SelectTrigger className="w-full"><SelectValue /></SelectTrigger>
                        <SelectContent>
                            <SelectItem value="inherit">{t('inherit')}</SelectItem>
                            <SelectItem value="allow">{t('allow')}</SelectItem>
                            <SelectItem value="deny">{t('deny')}</SelectItem>
                        </SelectContent>
                    </Select>
                </label>
            </div>
            <div className="flex flex-wrap items-center gap-2">
                <Button
                    type="button"
                    size="sm"
                    onClick={save}
                    disabled={updateCandidate.isPending || !draftDirty || serverChanged}
                >
                    <Save />
                    {updateCandidate.isPending ? t('saving') : t('saveCandidate')}
                </Button>
                {candidate.manual ? <Badge variant="outline">{t('manual')}</Badge> : null}
                <Badge variant="outline">#{candidate.id}</Badge>
            </div>
            {serverChanged ? <DraftConflict onReload={reloadCandidate} /> : null}
        </div>
    );
}

function DraftConflict({ onReload }: { onReload: () => void }) {
    const t = useTranslations('model.catalog');
    return (
        <div role="alert" className="flex flex-wrap items-center gap-2 rounded-md border border-amber-500/40 bg-amber-500/10 p-3 text-sm">
            <AlertCircle className="size-4 shrink-0 text-amber-600" />
            <span className="min-w-48 flex-1">{t('serverChanged')}</span>
            <Button type="button" size="sm" variant="outline" onClick={onReload}>
                <RefreshCw className="size-4" />
                {t('reloadServerVersion')}
            </Button>
        </div>
    );
}
