'use client';

import { useMemo, useState } from 'react';
import { Plus, Save, Trash2, X } from 'lucide-react';
import { useTranslations } from 'next-intl';
import {
    type HeaderPolicy,
    type HeaderPolicyScope,
    type HeaderPolicyUpsertInput,
    useDeleteHeaderPolicy,
    useHeaderPolicyRegistry,
    useUpsertHeaderPolicy,
    useUpsertUserAgentProfile,
    useUserAgentProfiles,
} from '@/api/endpoints/header-policy';
import type { CustomHeader } from '@/api/endpoints/channel';
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
import { HEADER_POLICY_SCOPES, type ScopeTargets } from './header-policy-options';

type TriState = 'inherit' | 'yes' | 'no';
type UserAgentMode = 'inherit' | 'clear' | 'custom';
type AllowedMode = 'inherit' | 'none' | 'custom';

function emptyHeader(): CustomHeader {
    return { header_key: '', header_value: '' };
}

function errorMessage(error: unknown) {
    return error instanceof Error ? error.message : String(error);
}

function splitHeaderNames(value: string) {
    return [...new Set(value.split(/[\n,]/).map((item) => item.trim()).filter(Boolean))];
}

export function HeaderPolicyEditor({
    policy,
    targets,
    onSaved,
    onDeleted,
}: {
    policy: HeaderPolicy | null;
    targets: ScopeTargets;
    onSaved: (policy: HeaderPolicy) => void;
    onDeleted: () => void;
}) {
    const t = useTranslations('model.headerPolicy');
    const registry = useHeaderPolicyRegistry();
    const profiles = useUserAgentProfiles();
    const upsert = useUpsertHeaderPolicy();
    const remove = useDeleteHeaderPolicy();
    const upsertProfile = useUpsertUserAgentProfile();
    const [name, setName] = useState(policy?.name ?? '');
    const [scope, setScope] = useState<HeaderPolicyScope>(policy?.scope ?? 'global');
    const [scopeId, setScopeId] = useState(policy?.scope_id ?? 0);
    const [enabled, setEnabled] = useState(policy?.enabled ?? true);
    const [forwardMode, setForwardMode] = useState<TriState>(
        policy?.forward_client_headers == null ? 'inherit' : policy.forward_client_headers ? 'yes' : 'no',
    );
    const [userAgentMode, setUserAgentMode] = useState<UserAgentMode>(
        policy?.user_agent == null ? 'inherit' : policy.user_agent === '' ? 'clear' : 'custom',
    );
    const [userAgent, setUserAgent] = useState(policy?.user_agent ?? '');
    const [allowedMode, setAllowedMode] = useState<AllowedMode>(
        policy?.allowed_client_headers == null
            ? 'inherit'
            : policy.allowed_client_headers.length === 0
                ? 'none'
                : 'custom',
    );
    const [allowedHeaders, setAllowedHeaders] = useState(
        policy?.allowed_client_headers?.join('\n') ?? '',
    );
    const [unsetHeaders, setUnsetHeaders] = useState(policy?.unset_headers.join('\n') ?? '');
    const [setHeaders, setSetHeaders] = useState<CustomHeader[]>(
        policy?.set_headers.length ? policy.set_headers : [emptyHeader()],
    );
    const [profileName, setProfileName] = useState('');
    const [profileValue, setProfileValue] = useState('');

    const protectedRows = useMemo(() => {
        const exact = new Set((registry.data?.protected_headers ?? []).map((header) => header.toLowerCase()));
        const prefixes = (registry.data?.protected_prefixes ?? []).map((prefix) => prefix.toLowerCase());
        return new Set(
            setHeaders.flatMap((header, index) => {
                const key = header.header_key.trim().toLowerCase();
                return key && (exact.has(key) || prefixes.some((prefix) => key.startsWith(prefix)))
                    ? [index]
                    : [];
            }),
        );
    }, [registry.data, setHeaders]);

    const changeScope = (next: HeaderPolicyScope) => {
        setScope(next);
        setScopeId(next === 'global' ? 0 : targets[next][0]?.id ?? 0);
    };

    const save = () => {
        if (!name.trim()) {
            toast.error(t('nameRequired'));
            return;
        }
        if (scope !== 'global' && scopeId <= 0) {
            toast.error(t('targetRequired'));
            return;
        }
        if (protectedRows.size > 0) {
            toast.error(t('protectedHeader'));
            return;
        }
        const payload: HeaderPolicyUpsertInput = {
            id: policy?.id ?? 0,
            name: name.trim(),
            scope,
            scope_id: scope === 'global' ? 0 : scopeId,
            enabled,
            forward_client_headers:
                forwardMode === 'inherit' ? null : forwardMode === 'yes',
            user_agent:
                userAgentMode === 'inherit' ? null : userAgentMode === 'clear' ? '' : userAgent,
            set_headers: setHeaders
                .map((header) => ({
                    header_key: header.header_key.trim(),
                    header_value: header.header_value,
                }))
                .filter((header) => header.header_key),
            unset_headers: splitHeaderNames(unsetHeaders),
            allowed_client_headers:
                allowedMode === 'inherit'
                    ? null
                    : allowedMode === 'none'
                        ? []
                        : splitHeaderNames(allowedHeaders),
        };
        upsert.mutate(payload, {
            onSuccess: (saved) => {
                toast.success(t('saved'));
                onSaved({
                    ...saved,
                    set_headers: saved.set_headers ?? [],
                    unset_headers: saved.unset_headers ?? [],
                    allowed_client_headers: saved.allowed_client_headers ?? null,
                });
            },
            onError: (error) => toast.error(t('saveFailed'), { description: errorMessage(error) }),
        });
    };

    const deletePolicy = () => {
        if (!policy?.id) return;
        remove.mutate(policy.id, {
            onSuccess: () => {
                toast.success(t('deleted'));
                onDeleted();
            },
            onError: (error) => toast.error(t('deleteFailed'), { description: errorMessage(error) }),
        });
    };

    const saveProfile = () => {
        if (!profileName.trim() || !profileValue.trim()) {
            toast.error(t('profileRequired'));
            return;
        }
        upsertProfile.mutate({ id: 0, name: profileName.trim(), value: profileValue }, {
            onSuccess: () => {
                setProfileName('');
                setProfileValue('');
                toast.success(t('profileSaved'));
            },
            onError: (error) => toast.error(t('profileSaveFailed'), { description: errorMessage(error) }),
        });
    };

    return (
        <section className="space-y-4">
            <header className="flex flex-wrap items-center justify-between gap-3 border-b pb-4">
                <div>
                    <h2 className="text-lg font-semibold">
                        {policy ? policy.name : t('newPolicy')}
                    </h2>
                    {policy ? (
                        <p className="text-xs text-muted-foreground">
                            #{policy.id} · {t('version', { version: policy.version })}
                        </p>
                    ) : null}
                </div>
                <label className="flex min-h-11 items-center gap-2 text-sm">
                    {t('enabled')}
                    <Switch checked={enabled} onCheckedChange={setEnabled} />
                </label>
            </header>

            <label className="grid gap-1 text-xs text-muted-foreground">
                {t('nameLabel')}
                <Input
                    value={name}
                    onChange={(event) => setName(event.target.value)}
                    placeholder={t('namePlaceholder')}
                    maxLength={191}
                />
            </label>

            <div className="grid gap-3 sm:grid-cols-2">
                <label className="grid gap-1 text-xs text-muted-foreground">
                    {t('scopeLabel')}
                    <Select value={scope} onValueChange={(value) => changeScope(value as HeaderPolicyScope)}>
                        <SelectTrigger className="w-full"><SelectValue /></SelectTrigger>
                        <SelectContent>
                            {HEADER_POLICY_SCOPES.map((item) => (
                                <SelectItem key={item} value={item}>{t(`scope.${item}`)}</SelectItem>
                            ))}
                        </SelectContent>
                    </Select>
                </label>
                <label className="grid gap-1 text-xs text-muted-foreground">
                    {t('targetLabel')}
                    <Select
                        value={String(scopeId)}
                        onValueChange={(value) => setScopeId(Number(value))}
                        disabled={scope === 'global' || targets[scope].length === 0}
                    >
                        <SelectTrigger className="w-full"><SelectValue placeholder={t('selectTarget')} /></SelectTrigger>
                        <SelectContent>
                            {targets[scope].map((target) => (
                                <SelectItem key={target.id} value={String(target.id)}>{target.label}</SelectItem>
                            ))}
                        </SelectContent>
                    </Select>
                </label>
                <label className="grid gap-1 text-xs text-muted-foreground">
                    {t('forwardClientHeaders')}
                    <TriStateSelect value={forwardMode} onChange={setForwardMode} t={t} />
                </label>
                <label className="grid gap-1 text-xs text-muted-foreground">
                    {t('allowedHeadersMode')}
                    <Select value={allowedMode} onValueChange={(value) => setAllowedMode(value as AllowedMode)}>
                        <SelectTrigger className="w-full"><SelectValue /></SelectTrigger>
                        <SelectContent>
                            <SelectItem value="inherit">{t('inherit')}</SelectItem>
                            <SelectItem value="none">{t('denyAll')}</SelectItem>
                            <SelectItem value="custom">{t('custom')}</SelectItem>
                        </SelectContent>
                    </Select>
                </label>
            </div>

            {allowedMode === 'custom' ? (
                <label className="grid gap-1 text-xs text-muted-foreground">
                    {t('allowedClientHeaders')}
                    <textarea
                        value={allowedHeaders}
                        onChange={(event) => setAllowedHeaders(event.target.value)}
                        className="min-h-24 resize-y rounded-md border bg-background px-3 py-2 text-sm text-foreground outline-none focus-visible:ring-2 focus-visible:ring-ring"
                    />
                </label>
            ) : null}

            <div className="grid gap-3 sm:grid-cols-[12rem_minmax(0,1fr)]">
                <label className="grid gap-1 text-xs text-muted-foreground">
                    {t('userAgentMode')}
                    <Select value={userAgentMode} onValueChange={(value) => setUserAgentMode(value as UserAgentMode)}>
                        <SelectTrigger className="w-full"><SelectValue /></SelectTrigger>
                        <SelectContent>
                            <SelectItem value="inherit">{t('inherit')}</SelectItem>
                            <SelectItem value="clear">{t('clear')}</SelectItem>
                            <SelectItem value="custom">{t('custom')}</SelectItem>
                        </SelectContent>
                    </Select>
                </label>
                {userAgentMode === 'custom' ? (
                    <label className="grid gap-1 text-xs text-muted-foreground">
                        User-Agent
                        <Input value={userAgent} onChange={(event) => setUserAgent(event.target.value)} />
                    </label>
                ) : null}
            </div>

            {userAgentMode === 'custom' && (profiles.data ?? []).length > 0 ? (
                <div className="flex flex-wrap gap-1.5">
                    {(profiles.data ?? []).map((profile) => (
                        <button
                            key={profile.id}
                            type="button"
                            onClick={() => setUserAgent(profile.value)}
                            className="min-h-9 rounded-md border px-2 text-xs hover:bg-muted"
                        >
                            {profile.name}
                            {profile.built_in ? <Badge variant="outline" className="ml-1">{t('builtIn')}</Badge> : null}
                        </button>
                    ))}
                </div>
            ) : null}

            <div className="space-y-2 border-t pt-4">
                <div className="flex items-center justify-between">
                    <h3 className="text-sm font-semibold">{t('setHeaders')}</h3>
                    <Button
                        type="button"
                        variant="ghost"
                        size="sm"
                        onClick={() => setSetHeaders((current) => [...current, emptyHeader()])}
                    >
                        <Plus />
                        {t('addHeader')}
                    </Button>
                </div>
                {setHeaders.map((header, index) => (
                    <div key={index} className="grid gap-2 sm:grid-cols-[minmax(8rem,0.8fr)_minmax(12rem,1.2fr)_2.25rem]">
                        <Input
                            aria-label={t('headerName')}
                            aria-invalid={protectedRows.has(index)}
                            value={header.header_key}
                            onChange={(event) =>
                                setSetHeaders((current) =>
                                    current.map((item, itemIndex) =>
                                        itemIndex === index ? { ...item, header_key: event.target.value } : item,
                                    ),
                                )
                            }
                            placeholder="OpenAI-Beta"
                        />
                        <Input
                            aria-label={t('headerValue')}
                            value={header.header_value}
                            onChange={(event) =>
                                setSetHeaders((current) =>
                                    current.map((item, itemIndex) =>
                                        itemIndex === index ? { ...item, header_value: event.target.value } : item,
                                    ),
                                )
                            }
                        />
                        <Button
                            type="button"
                            variant="ghost"
                            size="icon"
                            aria-label={t('removeHeader')}
                            onClick={() =>
                                setSetHeaders((current) =>
                                    current.length === 1 ? [emptyHeader()] : current.filter((_, itemIndex) => itemIndex !== index),
                                )
                            }
                        >
                            <X />
                        </Button>
                        {protectedRows.has(index) ? (
                            <p className="text-xs text-destructive sm:col-span-3">{t('protectedHeader')}</p>
                        ) : null}
                    </div>
                ))}
            </div>

            <label className="grid gap-1 border-t pt-4 text-xs text-muted-foreground">
                {t('unsetHeaders')}
                <textarea
                    value={unsetHeaders}
                    onChange={(event) => setUnsetHeaders(event.target.value)}
                    className="min-h-20 resize-y rounded-md border bg-background px-3 py-2 text-sm text-foreground outline-none focus-visible:ring-2 focus-visible:ring-ring"
                />
            </label>

            <div className="flex flex-wrap gap-2 border-t pt-4">
                <Button type="button" onClick={save} disabled={upsert.isPending || protectedRows.size > 0}>
                    <Save />
                    {upsert.isPending ? t('saving') : t('save')}
                </Button>
                {policy?.id ? (
                    <Button type="button" variant="destructive" onClick={deletePolicy} disabled={remove.isPending}>
                        <Trash2 />
                        {t('delete')}
                    </Button>
                ) : null}
            </div>

            <div className="space-y-3 border-t pt-4">
                <h3 className="text-sm font-semibold">{t('userAgentProfiles')}</h3>
                <div className="grid gap-2 sm:grid-cols-[10rem_minmax(14rem,1fr)_auto]">
                    <Input
                        aria-label={t('profileName')}
                        value={profileName}
                        onChange={(event) => setProfileName(event.target.value)}
                        placeholder={t('profileName')}
                    />
                    <Input
                        aria-label="User-Agent"
                        value={profileValue}
                        onChange={(event) => setProfileValue(event.target.value)}
                        placeholder="Mozilla/5.0 ..."
                    />
                    <Button type="button" variant="outline" onClick={saveProfile} disabled={upsertProfile.isPending}>
                        {t('saveProfile')}
                    </Button>
                </div>
            </div>
        </section>
    );
}

function TriStateSelect({
    value,
    onChange,
    t,
}: {
    value: TriState;
    onChange: (value: TriState) => void;
    t: ReturnType<typeof useTranslations>;
}) {
    return (
        <Select value={value} onValueChange={(next) => onChange(next as TriState)}>
            <SelectTrigger className="w-full"><SelectValue /></SelectTrigger>
            <SelectContent>
                <SelectItem value="inherit">{t('inherit')}</SelectItem>
                <SelectItem value="yes">{t('yes')}</SelectItem>
                <SelectItem value="no">{t('no')}</SelectItem>
            </SelectContent>
        </Select>
    );
}
