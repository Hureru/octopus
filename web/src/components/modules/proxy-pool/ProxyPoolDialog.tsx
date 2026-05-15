'use client';

import { useMemo, useState, type FormEvent } from 'react';
import { Network, Pencil, Plus, Trash2, FlaskConical } from 'lucide-react';
import { useTranslations } from 'next-intl';
import {
    useCreateProxyConfiguration,
    useDeleteProxyConfiguration,
    useProxyConfigurationList,
    useTestProxyConfiguration,
    useUpdateProxyConfiguration,
    type ProxyConfiguration,
} from '@/api/endpoints/proxy-pool';
import { Button } from '@/components/ui/button';
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog';
import { Input } from '@/components/ui/input';
import { Badge } from '@/components/ui/badge';
import { Switch } from '@/components/ui/switch';
import { toast } from '@/components/common/Toast';
import { cn } from '@/lib/utils';
import { useProxyPoolDialogStore } from './dialog-store';

type FormState = {
    id?: number;
    name: string;
    url: string;
    enabled: boolean;
    remark: string;
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

export function ProxyPoolHeaderAction() {
    const t = useTranslations('proxyPool');
    const open = useProxyPoolDialogStore((state) => state.open);
    return (
        <Button
            type="button"
            variant="ghost"
            size="icon"
            className="rounded-xl transition-none hover:bg-transparent text-muted-foreground hover:text-foreground"
            aria-label={t('name')}
            title={t('name')}
            onClick={open}
        >
            <Network className="size-4" />
        </Button>
    );
}

export function ProxyPoolDialog() {
    const t = useTranslations('proxyPool.dialog');
    const isOpen = useProxyPoolDialogStore((state) => state.isOpen);
    const setOpen = useProxyPoolDialogStore((state) => state.setOpen);
    const { data: proxies = [], isLoading, error } = useProxyConfigurationList();
    const createProxy = useCreateProxyConfiguration();
    const updateProxy = useUpdateProxyConfiguration();
    const deleteProxy = useDeleteProxyConfiguration();
    const testProxy = useTestProxyConfiguration();
    const [form, setForm] = useState<FormState>(emptyForm);
    const [query, setQuery] = useState('');
    const [testURL, setTestURL] = useState(DEFAULT_TEST_URL);
    const [testingKey, setTestingKey] = useState<string | null>(null);

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
                                <article key={proxy.id} className="rounded-2xl border bg-card p-4">
                                    <div className="flex items-start justify-between gap-3">
                                        <div className="min-w-0 flex-1">
                                            <div className="flex flex-wrap items-center gap-2">
                                                <h3 className="truncate font-semibold">{proxy.name}</h3>
                                                <Badge variant={proxy.enabled ? 'default' : 'secondary'}>
                                                    {proxy.enabled ? t('enabled') : t('disabled')}
                                                </Badge>
                                                <Badge variant="outline">{t('references', { count: proxy.reference_count })}</Badge>
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
        </Dialog>
    );
}
