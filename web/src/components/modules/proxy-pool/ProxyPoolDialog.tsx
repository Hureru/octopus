'use client';

import { useEffect, useMemo, useRef, useState, type FormEvent } from 'react';
import { ChevronDown, ExternalLink, FlaskConical, Network, Pencil, Plus, Trash2 } from 'lucide-react';
import { useTranslations } from 'next-intl';
import {
    useCreateProxyConfiguration,
    useDeleteProxyConfiguration,
    useProxyConfigurationList,
    useProxyConfigurationReferences,
    useTestProxyConfiguration,
    useUpdateProxyConfiguration,
    type ProxyConfiguration,
    type ProxyConfigurationReference,
} from '@/api/endpoints/proxy-pool';
import { Button } from '@/components/ui/button';
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog';
import { Input } from '@/components/ui/input';
import { Badge } from '@/components/ui/badge';
import { Switch } from '@/components/ui/switch';
import { toast } from '@/components/common/Toast';
import { cn } from '@/lib/utils';
import { useJumpStore } from '@/stores/jump';
import { useProxyPoolDialogStore } from './dialog-store';

type FormState = {
    id?: number;
    name: string;
    url: string;
    enabled: boolean;
    remark: string;
};

type ProxyPoolDialogTranslator = ReturnType<typeof useTranslations>;

type ReferenceTreeNode = {
    key: string;
    reference: ProxyConfigurationReference;
    children: ProxyConfigurationReference[];
};

const emptyForm: FormState = {
    name: '',
    url: '',
    enabled: true,
    remark: '',
};

const DEFAULT_TEST_URL = 'https://api.openai.com/v1/models';

function maskProxyURL(value: string) {
    try {
        const parsed = new URL(value);
        if (parsed.password) parsed.password = '***';
        return parsed.toString();
    } catch {
        return value;
    }
}

function errorMessage(error: unknown, fallback: string) {
    if (error instanceof Error) return error.message;
    if (typeof error === 'object' && error !== null && 'message' in error) {
        const message = (error as { message?: unknown }).message;
        if (typeof message === 'string') return message;
    }
    return fallback;
}

function createFormFromProxy(proxy: ProxyConfiguration): FormState {
    return {
        id: proxy.id,
        name: proxy.name,
        url: proxy.url,
        enabled: proxy.enabled,
        remark: proxy.remark ?? '',
    };
}

function referenceTitle(reference: ProxyConfigurationReference, t: ProxyPoolDialogTranslator) {
    switch (reference.type) {
        case 'site':
            return reference.site_name || `#${reference.site_id}`;
        case 'site_account':
            return reference.site_account_name || `#${reference.site_account_id}`;
        case 'managed_channel':
            return reference.channel_name
                ? t('managedChannelTitle', { channel: reference.channel_name })
                : t('managedChannelTitle', { channel: `#${reference.channel_id}` });
        case 'channel':
            return reference.channel_name || `#${reference.channel_id}`;
        default:
            return '';
    }
}

function referenceLocation(reference: ProxyConfigurationReference, t: ProxyPoolDialogTranslator) {
    switch (reference.type) {
        case 'site':
            return reference.site_archived ? t('referenceLocations.archivedSite') : t('referenceLocations.site');
        case 'site_account':
            return reference.site_name ? t('referenceLocations.siteNamed', { name: reference.site_name }) : t('referenceLocations.siteAccount');
        case 'managed_channel':
            return reference.site_name
                ? t('referenceLocations.managedChannelUnderSite', { site: reference.site_name })
                : t('referenceLocations.managedChannel');
        case 'channel':
            return t('referenceLocations.channel');
        default:
            return '';
    }
}

function referenceTypeLabel(reference: ProxyConfigurationReference, t: ProxyPoolDialogTranslator) {
    switch (reference.type) {
        case 'site':
            return t('referenceTypes.site');
        case 'site_account':
            return t('referenceTypes.siteAccount');
        case 'managed_channel':
            return t('referenceTypes.managedChannel');
        case 'channel':
            return t('referenceTypes.channel');
        default:
            return t('referenceTypes.reference');
    }
}

function referenceNodeKey(reference: ProxyConfigurationReference) {
    return `${reference.type}:${reference.site_id ?? 0}:${reference.site_account_id ?? 0}:${reference.channel_id ?? 0}`;
}

function buildReferenceTree(references: ProxyConfigurationReference[]) {
    const roots: ReferenceTreeNode[] = [];
    const rootMap = new Map<string, ReferenceTreeNode>();

    for (const reference of references) {
        if (reference.type === 'managed_channel') continue;
        const key = referenceNodeKey(reference);
        const node = rootMap.get(key) ?? { key, reference, children: [] };
        node.reference = reference;
        rootMap.set(key, node);
        if (!roots.includes(node)) roots.push(node);
    }

    const siteAccountRoots = roots.filter((node) => node.reference.type === 'site_account');
    const siteRoots = roots.filter((node) => node.reference.type === 'site');

    for (const reference of references) {
        if (reference.type !== 'managed_channel') continue;
        const accountParent = siteAccountRoots.find((node) =>
            (node.reference.site_account_id ?? 0) > 0 && node.reference.site_account_id === reference.site_account_id,
        );
        const siteParent = siteRoots.find((node) =>
            (node.reference.site_id ?? 0) > 0 && node.reference.site_id === reference.site_id,
        );
        const parent = accountParent ?? siteParent;
        if (parent) {
            parent.children.push(reference);
            continue;
        }

        const key = `derived:${referenceNodeKey(reference)}`;
        roots.push({ key, reference, children: [] });
    }

    return roots;
}

export function ProxyPoolDialog() {
    const t = useTranslations('proxyPool.dialog');
    const isOpen = useProxyPoolDialogStore((state) => state.isOpen);
    const focusedProxyId = useProxyPoolDialogStore((state) => state.focusedProxyId);
    const setOpen = useProxyPoolDialogStore((state) => state.setOpen);
    const clearFocus = useProxyPoolDialogStore((state) => state.clearFocus);
    const requestJump = useJumpStore((state) => state.requestJump);
    const { data: proxies = [], isLoading, error } = useProxyConfigurationList();
    const createProxy = useCreateProxyConfiguration();
    const updateProxy = useUpdateProxyConfiguration();
    const deleteProxy = useDeleteProxyConfiguration();
    const testProxy = useTestProxyConfiguration();
    const [form, setForm] = useState<FormState>(emptyForm);
    const [query, setQuery] = useState('');
    const [testURL, setTestURL] = useState(DEFAULT_TEST_URL);
    const [testingKey, setTestingKey] = useState<string | null>(null);
    const [referencesProxy, setReferencesProxy] = useState<ProxyConfiguration | null>(null);
    const [expandedReferenceKeys, setExpandedReferenceKeys] = useState<Set<string>>(() => new Set());
    const focusedProxyRefs = useRef<Map<number, HTMLElement>>(new Map());
    const { data: references = [], isLoading: referencesLoading, error: referencesError } = useProxyConfigurationReferences(
        referencesProxy?.id ?? null,
        isOpen && !!referencesProxy,
    );

    const referenceTree = useMemo(() => buildReferenceTree(references), [references]);

    const filteredProxies = useMemo(() => {
        const term = query.trim().toLowerCase();
        if (!term) return proxies;
        return proxies.filter((item) =>
            item.name.toLowerCase().includes(term) ||
            item.url.toLowerCase().includes(term) ||
            (item.remark ?? '').toLowerCase().includes(term)
        );
    }, [proxies, query]);

    const editing = typeof form.id === 'number';

    function resetForm() {
        setForm(emptyForm);
    }

    function setProxyArticleRef(proxyId: number, node: HTMLElement | null) {
        if (node) {
            focusedProxyRefs.current.set(proxyId, node);
            return;
        }
        focusedProxyRefs.current.delete(proxyId);
    }

    function openReferences(proxy: ProxyConfiguration) {
        setReferencesProxy(proxy);
        setExpandedReferenceKeys(new Set());
    }

    function toggleReferenceExpanded(key: string) {
        setExpandedReferenceKeys((current) => {
            const next = new Set(current);
            if (next.has(key)) next.delete(key);
            else next.add(key);
            return next;
        });
    }

    function jumpToReference(reference: ProxyConfigurationReference) {
        setReferencesProxy(null);
        setOpen(false);
        switch (reference.type) {
            case 'site':
                if (reference.site_id) requestJump({ kind: 'site-card', siteId: reference.site_id });
                return;
            case 'site_account':
                if (reference.site_id && reference.site_account_id) {
                    requestJump({ kind: 'site-account', siteId: reference.site_id, accountId: reference.site_account_id });
                }
                return;
            case 'managed_channel':
                if (reference.site_id) {
                    requestJump(
                        reference.site_account_id
                            ? { kind: 'site-channel-account', siteId: reference.site_id, accountId: reference.site_account_id }
                            : { kind: 'site-channel-card', siteId: reference.site_id },
                    );
                }
                return;
            case 'channel':
                if (reference.channel_id) requestJump({ kind: 'channel-card', channelId: reference.channel_id });
                return;
            default:
                return;
        }
    }

    function submitForm(event: FormEvent<HTMLFormElement>) {
        event.preventDefault();
        const payload = {
            name: form.name.trim(),
            url: form.url.trim(),
            enabled: form.enabled,
            remark: form.remark.trim(),
        };
        if (!payload.name || !payload.url) {
            toast.error(t('formRequired'));
            return;
        }
        if (editing && form.id) {
            updateProxy.mutate({ id: form.id, ...payload }, {
                onSuccess: () => {
                    toast.success(t('updated'));
                    resetForm();
                },
                onError: (err) => toast.error(errorMessage(err, t('operationFailed'))),
            });
            return;
        }
        createProxy.mutate(payload, {
            onSuccess: () => {
                toast.success(t('created'));
                resetForm();
            },
            onError: (err) => toast.error(errorMessage(err, t('operationFailed'))),
        });
    }

    function handleDelete(proxy: ProxyConfiguration) {
        if (proxy.reference_count > 0) {
            toast.error(t('deleteReferenced'));
            return;
        }
        deleteProxy.mutate(proxy.id, {
            onSuccess: () => {
                toast.success(t('deleted'));
                if (form.id === proxy.id) resetForm();
            },
            onError: (err) => toast.error(errorMessage(err, t('operationFailed'))),
        });
    }

    function handleTest(proxy?: ProxyConfiguration) {
        const key = proxy ? `saved-${proxy.id}` : 'draft';
        setTestingKey(key);
        testProxy.mutate(
            proxy && proxy.enabled
                ? { proxy_config_id: proxy.id, url: testURL.trim() || DEFAULT_TEST_URL }
                : { proxy_url: proxy?.url ?? form.url.trim(), url: testURL.trim() || DEFAULT_TEST_URL },
            {
                onSuccess: (result) => {
                    if (result.success) {
                        toast.success(t('testSuccess', { statusCode: result.status_code, durationMs: result.duration_ms }));
                    } else {
                        toast.error(t('testFailed'), { description: result.message });
                    }
                },
                onError: (err) => toast.error(errorMessage(err, t('operationFailed'))),
                onSettled: () => setTestingKey(null),
            }
        );
    }

    useEffect(() => {
        if (!isOpen || !focusedProxyId) return;
        const node = focusedProxyRefs.current.get(focusedProxyId);
        if (!node) return;
        const timer = window.setTimeout(() => {
            node.scrollIntoView({ behavior: 'smooth', block: 'center' });
            clearFocus();
        }, 80);
        return () => window.clearTimeout(timer);
    }, [isOpen, focusedProxyId, filteredProxies.length, clearFocus]);

    return (
        <Dialog open={isOpen} onOpenChange={setOpen}>
            <DialogContent className="max-h-[90vh] overflow-hidden rounded-3xl p-0 sm:max-w-5xl">
                <div className="grid max-h-[90vh] min-h-[620px] grid-cols-1 overflow-hidden md:grid-cols-[1.1fr_0.9fr]">
                    <section className="flex min-h-0 flex-col border-b md:border-b-0 md:border-r">
                        <DialogHeader className="shrink-0 p-6 pb-3">
                            <DialogTitle className="flex items-center gap-2 text-2xl">
                                <Network className="size-5" />
                                {t('title')}
                            </DialogTitle>
                            <DialogDescription>{t('description')}</DialogDescription>
                        </DialogHeader>
                        <div className="shrink-0 px-6 pb-3">
                            <Input
                                value={query}
                                onChange={(event) => setQuery(event.target.value)}
                                placeholder={t('searchPlaceholder')}
                                className="rounded-xl"
                            />
                        </div>
                        <div className="min-h-0 flex-1 space-y-2 overflow-y-auto px-6 pb-6">
                            {isLoading ? (
                                <div className="rounded-2xl border bg-muted/30 p-4 text-sm text-muted-foreground">{t('loading')}</div>
                            ) : error ? (
                                <div className="rounded-2xl border border-destructive/30 bg-destructive/10 p-4 text-sm text-destructive">{t('loadFailed', { message: errorMessage(error, t('operationFailed')) })}</div>
                            ) : filteredProxies.length === 0 ? (
                                <div className="rounded-2xl border bg-muted/30 p-8 text-center text-sm text-muted-foreground">{t('empty')}</div>
                            ) : filteredProxies.map((proxy) => (
                                <article
                                    key={proxy.id}
                                    ref={(node) => setProxyArticleRef(proxy.id, node)}
                                    className={cn(
                                        "rounded-2xl border bg-card p-4 transition-colors",
                                        focusedProxyId === proxy.id && "ring-2 ring-primary/35 ring-offset-2 ring-offset-background",
                                    )}
                                >
                                    <div className="flex items-start justify-between gap-3">
                                        <div className="min-w-0 flex-1">
                                            <div className="flex flex-wrap items-center gap-2">
                                                <h3 className="truncate font-semibold">{proxy.name}</h3>
                                                <Badge variant={proxy.enabled ? 'default' : 'secondary'}>
                                                    {proxy.enabled ? t('enabled') : t('disabled')}
                                                </Badge>
                                                <button type="button" onClick={() => openReferences(proxy)} className="rounded-full" title={t('referencesTitle')}>
                                                    <Badge variant="outline" className="cursor-pointer hover:bg-accent hover:text-accent-foreground">
                                                        <ExternalLink className="size-3" />
                                                        {t('references', { count: proxy.reference_count })}
                                                    </Badge>
                                                </button>
                                            </div>
                                            <div className="mt-1 truncate font-mono text-xs text-muted-foreground" title={maskProxyURL(proxy.url)}>
                                                {maskProxyURL(proxy.url)}
                                            </div>
                                            {proxy.remark ? <p className="mt-2 text-xs text-muted-foreground">{proxy.remark}</p> : null}
                                        </div>
                                        <div className="flex shrink-0 items-center gap-1">
                                            <Button type="button" variant="ghost" size="icon-sm" className="rounded-xl" onClick={() => handleTest(proxy)} disabled={testingKey === `saved-${proxy.id}` || !proxy.enabled} title={proxy.enabled ? t('test') : t('disabled')}>
                                                <FlaskConical className={cn('size-4', testingKey === `saved-${proxy.id}` && 'animate-pulse')} />
                                            </Button>
                                            <Button type="button" variant="ghost" size="icon-sm" className="rounded-xl" onClick={() => setForm(createFormFromProxy(proxy))} title={t('edit')}>
                                                <Pencil className="size-4" />
                                            </Button>
                                            <Button type="button" variant="ghost" size="icon-sm" className="rounded-xl text-destructive hover:text-destructive" onClick={() => handleDelete(proxy)} disabled={deleteProxy.isPending || proxy.reference_count > 0} title={proxy.reference_count > 0 ? t('deleteBlocked') : t('delete')}>
                                                <Trash2 className="size-4" />
                                            </Button>
                                        </div>
                                    </div>
                                </article>
                            ))}
                        </div>
                    </section>

                    <section className="flex min-h-0 flex-col overflow-y-auto p-6">
                        <div className="mb-4 flex items-center justify-between gap-3">
                            <div>
                                <h3 className="text-lg font-semibold">{editing ? t('formTitleEdit') : t('formTitleCreate')}</h3>
                                <p className="text-sm text-muted-foreground">{t('formDescription')}</p>
                            </div>
                            <Button type="button" variant="outline" size="sm" className="rounded-xl" onClick={resetForm}>
                                <Plus className="size-4" />
                                {t('new')}
                            </Button>
                        </div>

                        <form onSubmit={submitForm} className="space-y-4">
                            <div className="space-y-2">
                                <label className="text-sm font-medium">{t('name')}</label>
                                <Input value={form.name} onChange={(event) => setForm({ ...form, name: event.target.value })} className="rounded-xl" required />
                            </div>
                            <div className="space-y-2">
                                <label className="text-sm font-medium">{t('url')}</label>
                                <Input value={form.url} onChange={(event) => setForm({ ...form, url: event.target.value })} placeholder="socks5://127.0.0.1:1080" className="rounded-xl" required />
                            </div>
                            <div className="space-y-2">
                                <label className="text-sm font-medium">{t('remark')}</label>
                                <Input value={form.remark} onChange={(event) => setForm({ ...form, remark: event.target.value })} className="rounded-xl" />
                            </div>
                            <label className="flex items-center justify-between rounded-xl border bg-muted/20 px-4 py-3">
                                <span className="text-sm font-medium">{t('enabled')}</span>
                                <Switch checked={form.enabled} onCheckedChange={(enabled) => setForm({ ...form, enabled })} />
                            </label>

                            <div className="space-y-2 rounded-2xl border bg-muted/20 p-4">
                                <label className="text-sm font-medium">{t('testUrl')}</label>
                                <Input value={testURL} onChange={(event) => setTestURL(event.target.value)} className="rounded-xl" />
                                <Button type="button" variant="outline" className="w-full rounded-xl" onClick={() => handleTest()} disabled={!form.url.trim() || testingKey === 'draft'}>
                                    <FlaskConical className={cn('size-4', testingKey === 'draft' && 'animate-pulse')} />
                                    {t('testDraft')}
                                </Button>
                            </div>

                            <Button type="submit" className="w-full rounded-2xl h-11" disabled={createProxy.isPending || updateProxy.isPending}>
                                {editing ? t('submitEdit') : t('submitCreate')}
                            </Button>
                        </form>
                    </section>
                </div>
            </DialogContent>
            <Dialog open={!!referencesProxy} onOpenChange={(open) => !open && setReferencesProxy(null)}>
                <DialogContent className="max-h-[85vh] overflow-hidden rounded-3xl sm:max-w-2xl">
                    <DialogHeader>
                        <DialogTitle>{t('referencesTitle')}</DialogTitle>
                        <DialogDescription>
                            {referencesProxy ? t('referencesDescription', { name: referencesProxy.name }) : null}
                        </DialogDescription>
                    </DialogHeader>
                    <div className="min-h-0 space-y-2 overflow-y-auto pr-1">
                        {referencesLoading ? (
                            <div className="rounded-2xl border bg-muted/30 p-4 text-sm text-muted-foreground">{t('loading')}</div>
                        ) : referencesError ? (
                            <div className="rounded-2xl border border-destructive/30 bg-destructive/10 p-4 text-sm text-destructive">
                                {t('loadFailed', { message: errorMessage(referencesError, t('operationFailed')) })}
                            </div>
                        ) : references.length === 0 ? (
                            <div className="rounded-2xl border bg-muted/30 p-8 text-center text-sm text-muted-foreground">
                                {t('referencesEmpty')}
                            </div>
                        ) : referenceTree.map((node) => {
                            const expanded = expandedReferenceKeys.has(node.key);
                            return (
                                <div key={node.key} className="rounded-2xl border bg-card p-3">
                                    <div className="flex items-center justify-between gap-3">
                                        <div className="flex min-w-0 items-center gap-2">
                                            {node.children.length > 0 ? (
                                                <button
                                                    type="button"
                                                    className="shrink-0 rounded-lg p-1 text-muted-foreground transition hover:bg-muted hover:text-foreground"
                                                    onClick={() => toggleReferenceExpanded(node.key)}
                                                    title={expanded ? t('collapseReference') : t('expandReference')}
                                                >
                                                    <ChevronDown className={cn('size-4 transition-transform', !expanded && '-rotate-90')} />
                                                </button>
                                            ) : (
                                                <span className="w-6 shrink-0" />
                                            )}
                                            <div className="min-w-0">
                                                <div className="flex items-center gap-2">
                                                    <Badge variant="outline">{referenceTypeLabel(node.reference, t)}</Badge>
                                                    <span className="truncate text-sm font-medium">{referenceTitle(node.reference, t)}</span>
                                                    {node.children.length > 0 ? (
                                                        <Badge variant="secondary" className="text-[10px]">
                                                            {t('derivedReferences', { count: node.children.length })}
                                                        </Badge>
                                                    ) : null}
                                                </div>
                                                <div className="mt-1 truncate text-xs text-muted-foreground">{referenceLocation(node.reference, t)}</div>
                                            </div>
                                        </div>
                                        <Button type="button" variant="ghost" size="icon-sm" className="shrink-0 rounded-xl" onClick={() => jumpToReference(node.reference)} title={t('jumpToReference')}>
                                            <ExternalLink className="size-4" />
                                        </Button>
                                    </div>
                                    {expanded && node.children.length > 0 ? (
                                        <div className="mt-3 space-y-2 border-l border-dashed border-border/80 pl-6">
                                            {node.children.map((child, childIndex) => (
                                                <div key={`${referenceNodeKey(child)}:${childIndex}`} className="flex items-center justify-between gap-3 rounded-xl bg-muted/20 px-3 py-2">
                                                    <div className="min-w-0">
                                                        <div className="flex items-center gap-2">
                                                            <Badge variant="outline" className="text-[10px]">{referenceTypeLabel(child, t)}</Badge>
                                                            <span className="truncate text-xs font-medium">{referenceTitle(child, t)}</span>
                                                        </div>
                                                        <div className="mt-1 truncate text-[11px] text-muted-foreground">{referenceLocation(child, t)}</div>
                                                    </div>
                                                    <Button type="button" variant="ghost" size="icon-sm" className="shrink-0 rounded-xl" onClick={() => jumpToReference(child)} title={t('jumpToReference')}>
                                                        <ExternalLink className="size-4" />
                                                    </Button>
                                                </div>
                                            ))}
                                        </div>
                                    ) : null}
                                </div>
                            );
                        })}
                    </div>
                </DialogContent>
            </Dialog>
        </Dialog>
    );
}
