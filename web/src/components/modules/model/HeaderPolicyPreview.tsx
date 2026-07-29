'use client';

import { useMemo, useState } from 'react';
import { Eye } from 'lucide-react';
import { useTranslations } from 'next-intl';
import { useHeaderPolicyPreview } from '@/api/endpoints/header-policy';
import { Badge } from '@/components/ui/badge';
import {
    Select,
    SelectContent,
    SelectItem,
    SelectTrigger,
    SelectValue,
} from '@/components/ui/select';
import type { ScopeTargets } from './header-policy-options';

export function HeaderPolicyPreview({ targets }: { targets: ScopeTargets }) {
    const t = useTranslations('model.headerPolicy');
    const [channelId, setChannelId] = useState(0);
    const [canonicalModelId, setCanonicalModelId] = useState(0);
    const [routeCandidateId, setRouteCandidateId] = useState(0);
    const preview = useHeaderPolicyPreview({
        channelId: channelId || undefined,
        canonicalModelId: canonicalModelId || undefined,
        routeCandidateId: routeCandidateId || undefined,
    });
    const selectedCandidate = useMemo(
        () => targets.route_candidate.find((item) => item.id === routeCandidateId),
        [routeCandidateId, targets.route_candidate],
    );

    return (
        <section className="space-y-4 border-t pt-5">
            <div className="flex items-center gap-2">
                <Eye className="size-4 text-muted-foreground" />
                <h2 className="text-sm font-semibold">{t('preview')}</h2>
            </div>
            <div className="grid gap-3 sm:grid-cols-3">
                <PreviewSelect
                    label={t('scope.channel')}
                    value={channelId}
                    targets={targets.channel}
                    onChange={setChannelId}
                    noneLabel={t('notSelected')}
                />
                <PreviewSelect
                    label={t('scope.canonical_model')}
                    value={canonicalModelId}
                    targets={targets.canonical_model}
                    onChange={setCanonicalModelId}
                    noneLabel={t('notSelected')}
                />
                <PreviewSelect
                    label={t('scope.route_candidate')}
                    value={routeCandidateId}
                    targets={targets.route_candidate}
                    onChange={setRouteCandidateId}
                    noneLabel={t('notSelected')}
                />
            </div>
            {selectedCandidate ? (
                <p className="truncate text-xs text-muted-foreground">{selectedCandidate.label}</p>
            ) : null}
            {preview.data ? (
                <div className="space-y-3">
                    <div className="flex flex-wrap gap-2">
                        <Badge variant={preview.data.forward_client_headers ? 'default' : 'outline'}>
                            {t('forwardClientHeaders')}: {preview.data.forward_client_headers ? t('yes') : t('no')}
                        </Badge>
                        {preview.data.user_agent ? <Badge variant="outline">UA: {preview.data.user_agent}</Badge> : null}
                    </div>
                    <dl className="grid gap-3 text-sm lg:grid-cols-3">
                        <PreviewList title={t('allowedClientHeaders')} values={preview.data.allowed_client_headers} />
                        <PreviewList
                            title={t('setHeaders')}
                            values={preview.data.set_headers.map((header) => `${header.header_key}: ${header.header_value}`)}
                        />
                        <PreviewList title={t('unsetHeaders')} values={preview.data.unset_headers} />
                    </dl>
                    <div className="divide-y rounded-md border">
                        {preview.data.trace.map((trace) => (
                            <div key={trace.policy_id} className="flex flex-wrap items-center gap-2 px-3 py-2 text-xs">
                                <Badge variant="outline">{t(`scope.${trace.scope}`)} #{trace.scope_id}</Badge>
                                <span className="font-medium">
                                    {trace.policy_name || `${t('policyId')} #${trace.policy_id}`}
                                </span>
                                <span className="text-muted-foreground">
                                    #{trace.policy_id} · {t('version', { version: trace.policy_version })}
                                </span>
                                {(trace.applied_keys ?? []).map((key) => <Badge key={`set:${key}`} variant="secondary">+ {key}</Badge>)}
                                {(trace.unset_keys ?? []).map((key) => <Badge key={`unset:${key}`} variant="outline">- {key}</Badge>)}
                            </div>
                        ))}
                    </div>
                </div>
            ) : (
                <p className="text-sm text-muted-foreground">{preview.isLoading ? t('loading') : t('previewUnavailable')}</p>
            )}
        </section>
    );
}

function PreviewSelect({
    label,
    value,
    targets,
    onChange,
    noneLabel,
}: {
    label: string;
    value: number;
    targets: Array<{ id: number; label: string }>;
    onChange: (value: number) => void;
    noneLabel: string;
}) {
    return (
        <label className="grid gap-1 text-xs text-muted-foreground">
            {label}
            <Select value={String(value)} onValueChange={(next) => onChange(Number(next))}>
                <SelectTrigger className="w-full"><SelectValue /></SelectTrigger>
                <SelectContent>
                    <SelectItem value="0">{noneLabel}</SelectItem>
                    {targets.map((target) => (
                        <SelectItem key={target.id} value={String(target.id)}>{target.label}</SelectItem>
                    ))}
                </SelectContent>
            </Select>
        </label>
    );
}

function PreviewList({ title, values }: { title: string; values: string[] }) {
    return (
        <div className="min-w-0 rounded-md border px-3 py-2">
            <dt className="text-xs font-medium text-muted-foreground">{title}</dt>
            <dd className="mt-2 space-y-1">
                {values.length > 0
                    ? values.map((value) => <div key={value} className="break-all text-xs">{value}</div>)
                    : <span className="text-xs text-muted-foreground">-</span>}
            </dd>
        </div>
    );
}
