'use client';

import { useState, type FormEvent } from 'react';
import { useTranslations } from 'next-intl';
import {
    Eye,
    EyeOff,
    Pencil,
    Plus,
    RefreshCw,
    ServerCog,
    Trash2,
} from 'lucide-react';
import {
    useClashControllerList,
    useClashControllerState,
    useDeleteClashController,
    useSwitchClashControllerNode,
    useUpsertClashController,
    type ClashController,
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
import { Switch } from '@/components/ui/switch';
import { cn } from '@/lib/utils';

type ClashControllerPanelProps = {
    enabled: boolean;
};

type ClashForm = {
    id?: number;
    name: string;
    api_url: string;
    proxy_url: string;
    group_name: string;
    secret: string;
    enabled: boolean;
};

const EMPTY_FORM: ClashForm = {
    name: '',
    api_url: '',
    proxy_url: '',
    group_name: '',
    secret: '',
    enabled: true,
};

function formFromController(controller: ClashController): ClashForm {
    return {
        id: controller.id,
        name: controller.name,
        api_url: controller.api_url,
        proxy_url: controller.proxy_url,
        group_name: controller.group_name,
        secret: '',
        enabled: controller.enabled,
    };
}

function errorMessage(error: unknown, fallback: string) {
    if (error instanceof Error) return error.message;
    if (typeof error === 'object' && error !== null && 'message' in error) {
        const message = (error as { message?: unknown }).message;
        if (typeof message === 'string') return message;
    }
    return fallback;
}

export function ClashControllerPanel({ enabled }: ClashControllerPanelProps) {
    const t = useTranslations('proxyPool.clash');
    const controllersQuery = useClashControllerList(enabled);
    const upsertController = useUpsertClashController();
    const deleteController = useDeleteClashController();
    const switchNode = useSwitchClashControllerNode();
    const [form, setForm] = useState<ClashForm>(EMPTY_FORM);
    const [showSecret, setShowSecret] = useState(false);
    const [selectedControllerId, setSelectedControllerId] = useState<number | null>(
        null,
    );
    const [selectedNode, setSelectedNode] = useState('');
    const [deleteTarget, setDeleteTarget] = useState<ClashController | null>(null);
    const stateQuery = useClashControllerState(
        selectedControllerId,
        enabled && selectedControllerId !== null,
    );

    const controllers = controllersQuery.data ?? [];
    const editing = typeof form.id === 'number';
    const selectedController =
        controllers.find((item) => item.id === selectedControllerId) ?? null;
    const activeNodeSelection =
        stateQuery.data?.all.includes(selectedNode)
            ? selectedNode
            : stateQuery.data?.now ?? '';

    function resetForm() {
        setForm(EMPTY_FORM);
        setShowSecret(false);
    }

    async function submitForm(event: FormEvent<HTMLFormElement>) {
        event.preventDefault();
        const payload = {
            id: form.id,
            name: form.name.trim(),
            api_url: form.api_url.trim(),
            proxy_url: form.proxy_url.trim(),
            group_name: form.group_name.trim(),
            secret: form.secret.trim() || undefined,
            enabled: form.enabled,
        };
        if (
            !payload.name ||
            !payload.api_url ||
            !payload.proxy_url ||
            !payload.group_name
        ) {
            toast.error(t('required'));
            return;
        }
        try {
            const saved = await upsertController.mutateAsync(payload);
            toast.success(editing ? t('updated') : t('created'));
            setSelectedControllerId(saved.id);
            resetForm();
        } catch (error) {
            toast.error(errorMessage(error, t('operationFailed')));
        }
    }

    async function confirmDelete() {
        if (!deleteTarget) return;
        try {
            await deleteController.mutateAsync(deleteTarget.id);
            if (selectedControllerId === deleteTarget.id) {
                setSelectedControllerId(null);
            }
            if (form.id === deleteTarget.id) {
                resetForm();
            }
            setDeleteTarget(null);
            toast.success(t('deleted'));
        } catch (error) {
            toast.error(errorMessage(error, t('operationFailed')));
        }
    }

    async function handleSwitchNode() {
        if (!selectedControllerId || !activeNodeSelection) return;
        try {
            await switchNode.mutateAsync({
                id: selectedControllerId,
                node: activeNodeSelection,
            });
            toast.success(t('switched', { node: activeNodeSelection }));
        } catch (error) {
            toast.error(errorMessage(error, t('operationFailed')));
        }
    }

    return (
        <>
            <div className="grid h-full min-h-0 grid-cols-1 overflow-hidden md:grid-cols-[1.05fr_0.95fr]">
                <section className="flex min-h-0 flex-col border-b md:border-b-0 md:border-r">
                    <div className="shrink-0 px-5 pb-3 pt-5 sm:px-6">
                        <div className="flex items-center gap-2">
                            <ServerCog className="size-5" />
                            <h3 className="text-lg font-semibold">{t('title')}</h3>
                        </div>
                    </div>
                    <div className="min-h-0 flex-1 divide-y divide-border/60 overflow-y-auto border-y border-border/60 px-5 sm:px-6">
                        {controllersQuery.isLoading ? (
                            <div className="py-6 text-sm text-muted-foreground">
                                {t('loading')}
                            </div>
                        ) : controllersQuery.error ? (
                            <div className="py-5 text-sm text-destructive">
                                {errorMessage(
                                    controllersQuery.error,
                                    t('operationFailed'),
                                )}
                            </div>
                        ) : controllers.length === 0 ? (
                            <div className="py-10 text-center text-sm text-muted-foreground">
                                {t('empty')}
                            </div>
                        ) : (
                            controllers.map((controller) => (
                                <article key={controller.id} className="py-4">
                                    <div className="flex items-start justify-between gap-3">
                                        <button
                                            type="button"
                                            className="min-w-0 flex-1 text-left focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                                            onClick={() =>
                                                {
                                                    setSelectedControllerId(
                                                        controller.id,
                                                    );
                                                    setSelectedNode('');
                                                }
                                            }
                                        >
                                            <div className="flex flex-wrap items-center gap-2">
                                                <span className="truncate text-sm font-semibold">
                                                    {controller.name}
                                                </span>
                                                <Badge
                                                    variant={
                                                        controller.enabled
                                                            ? 'default'
                                                            : 'secondary'
                                                    }
                                                >
                                                    {controller.enabled
                                                        ? t('enabled')
                                                        : t('disabled')}
                                                </Badge>
                                            </div>
                                            <div className="mt-1 break-all font-mono text-xs text-muted-foreground">
                                                {controller.api_url}
                                            </div>
                                            <div className="mt-2 flex flex-wrap gap-x-3 gap-y-1 text-xs text-muted-foreground">
                                                <span>{controller.group_name}</span>
                                                <span>{controller.proxy_url}</span>
                                            </div>
                                        </button>
                                        <div className="flex shrink-0 gap-1">
                                            <Button
                                                type="button"
                                                size="icon-sm"
                                                variant="ghost"
                                                className="rounded-xl"
                                                onClick={() => {
                                                    setSelectedControllerId(
                                                        controller.id,
                                                    );
                                                    setSelectedNode('');
                                                    stateQuery.refetch();
                                                }}
                                                aria-label={t('loadNodes')}
                                                title={t('loadNodes')}
                                            >
                                                <RefreshCw className="size-4" />
                                            </Button>
                                            <Button
                                                type="button"
                                                size="icon-sm"
                                                variant="ghost"
                                                className="rounded-xl"
                                                onClick={() =>
                                                    setForm(
                                                        formFromController(
                                                            controller,
                                                        ),
                                                    )
                                                }
                                                aria-label={t('edit')}
                                                title={t('edit')}
                                            >
                                                <Pencil className="size-4" />
                                            </Button>
                                            <Button
                                                type="button"
                                                size="icon-sm"
                                                variant="ghost"
                                                className="rounded-xl text-destructive hover:text-destructive"
                                                onClick={() =>
                                                    setDeleteTarget(controller)
                                                }
                                                aria-label={t('delete')}
                                                title={t('delete')}
                                            >
                                                <Trash2 className="size-4" />
                                            </Button>
                                        </div>
                                    </div>
                                </article>
                            ))
                        )}
                    </div>

                    <div className="shrink-0 space-y-3 px-5 py-4 sm:px-6">
                        <div className="flex items-center justify-between gap-3">
                            <div>
                                <div className="text-sm font-semibold">
                                    {selectedController?.name ?? t('nodeState')}
                                </div>
                                {stateQuery.data?.now ? (
                                    <div className="text-xs text-muted-foreground">
                                        {t('currentNode', {
                                            node: stateQuery.data.now,
                                        })}
                                    </div>
                                ) : null}
                            </div>
                            <Button
                                type="button"
                                size="icon-sm"
                                variant="outline"
                                className="rounded-xl"
                                onClick={() => stateQuery.refetch()}
                                disabled={
                                    !selectedControllerId || stateQuery.isFetching
                                }
                                aria-label={t('refresh')}
                                title={t('refresh')}
                            >
                                <RefreshCw
                                    className={cn(
                                        'size-4',
                                        stateQuery.isFetching && 'animate-spin',
                                    )}
                                />
                            </Button>
                        </div>
                        {stateQuery.error ? (
                            <div className="text-sm text-destructive">
                                {errorMessage(
                                    stateQuery.error,
                                    t('operationFailed'),
                                )}
                            </div>
                        ) : null}
                        <div className="flex gap-2">
                            <Select
                                value={activeNodeSelection}
                                onValueChange={setSelectedNode}
                                disabled={!stateQuery.data?.all.length}
                            >
                                <SelectTrigger className="min-w-0 flex-1 rounded-xl" aria-label={t('selectNode')}>
                                    <SelectValue placeholder={t('selectNode')} />
                                </SelectTrigger>
                                <SelectContent>
                                    {(stateQuery.data?.all ?? []).map((node) => (
                                        <SelectItem key={node} value={node}>
                                            {node}
                                        </SelectItem>
                                    ))}
                                </SelectContent>
                            </Select>
                            <Button
                                type="button"
                                className="rounded-xl"
                                onClick={handleSwitchNode}
                                disabled={
                                    !selectedControllerId ||
                                    !activeNodeSelection ||
                                    switchNode.isPending ||
                                    activeNodeSelection === stateQuery.data?.now
                                }
                            >
                                {t('switch')}
                            </Button>
                        </div>
                    </div>
                </section>

                <section className="min-h-0 overflow-y-auto px-5 py-5 sm:px-6">
                    <div className="mb-4 flex items-center justify-between gap-3">
                        <div>
                            <h3 className="text-lg font-semibold">
                                {editing ? t('editTitle') : t('createTitle')}
                            </h3>
                        </div>
                        <Button
                            type="button"
                            size="sm"
                            variant="outline"
                            className="rounded-xl"
                            onClick={resetForm}
                        >
                            <Plus className="size-4" />
                            {t('new')}
                        </Button>
                    </div>

                    <form onSubmit={submitForm} className="space-y-4">
                        <label className="grid gap-1.5 text-sm">
                            <span className="font-medium">{t('name')}</span>
                            <Input
                                value={form.name}
                                onChange={(event) =>
                                    setForm({ ...form, name: event.target.value })
                                }
                                className="rounded-xl"
                            />
                        </label>
                        <label className="grid gap-1.5 text-sm">
                            <span className="font-medium">{t('apiUrl')}</span>
                            <Input
                                value={form.api_url}
                                onChange={(event) =>
                                    setForm({
                                        ...form,
                                        api_url: event.target.value,
                                    })
                                }
                                placeholder="http://127.0.0.1:9090"
                                className="rounded-xl"
                            />
                        </label>
                        <label className="grid gap-1.5 text-sm">
                            <span className="font-medium">{t('proxyUrl')}</span>
                            <Input
                                value={form.proxy_url}
                                onChange={(event) =>
                                    setForm({
                                        ...form,
                                        proxy_url: event.target.value,
                                    })
                                }
                                placeholder="http://127.0.0.1:7890"
                                className="rounded-xl"
                            />
                        </label>
                        <label className="grid gap-1.5 text-sm">
                            <span className="font-medium">{t('groupName')}</span>
                            <Input
                                value={form.group_name}
                                onChange={(event) =>
                                    setForm({
                                        ...form,
                                        group_name: event.target.value,
                                    })
                                }
                                className="rounded-xl"
                            />
                        </label>
                        <label className="grid gap-1.5 text-sm">
                            <span className="font-medium">{t('secret')}</span>
                            <div className="relative">
                                <Input
                                    type={showSecret ? 'text' : 'password'}
                                    value={form.secret}
                                    onChange={(event) =>
                                        setForm({
                                            ...form,
                                            secret: event.target.value,
                                        })
                                    }
                                    autoComplete="off"
                                    placeholder={
                                        editing
                                            ? t('secretKeep')
                                            : t('secretOptional')
                                    }
                                    className="rounded-xl pr-10"
                                />
                                <button
                                    type="button"
                                    onClick={() =>
                                        setShowSecret((value) => !value)
                                    }
                                    className="absolute inset-y-0 right-0 flex w-10 items-center justify-center rounded-r-xl text-muted-foreground hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                                    aria-label={
                                        showSecret
                                            ? t('hideSecret')
                                            : t('showSecret')
                                    }
                                >
                                    {showSecret ? (
                                        <EyeOff className="size-4" />
                                    ) : (
                                        <Eye className="size-4" />
                                    )}
                                </button>
                            </div>
                        </label>
                        <label className="flex items-center justify-between rounded-xl border border-border/60 bg-muted/20 px-4 py-3">
                            <span className="text-sm font-medium">
                                {t('enabled')}
                            </span>
                            <Switch
                                checked={form.enabled}
                                onCheckedChange={(value) =>
                                    setForm({ ...form, enabled: value })
                                }
                            />
                        </label>
                        <Button
                            type="submit"
                            className="h-11 w-full rounded-xl"
                            disabled={upsertController.isPending}
                        >
                            {editing ? t('save') : t('create')}
                        </Button>
                    </form>
                </section>
            </div>

            <AlertDialog
                open={deleteTarget !== null}
                onOpenChange={(open) => !open && setDeleteTarget(null)}
            >
                <AlertDialogContent>
                    <AlertDialogHeader>
                        <AlertDialogTitle>{t('deleteConfirmTitle')}</AlertDialogTitle>
                        <AlertDialogDescription>
                            {t('deleteConfirmDescription', {
                                name: deleteTarget?.name ?? '',
                            })}
                        </AlertDialogDescription>
                    </AlertDialogHeader>
                    <AlertDialogFooter>
                        <AlertDialogCancel>{t('cancel')}</AlertDialogCancel>
                        <AlertDialogAction
                            className="bg-destructive text-white hover:bg-destructive/90"
                            onClick={confirmDelete}
                            disabled={deleteController.isPending}
                        >
                            {t('delete')}
                        </AlertDialogAction>
                    </AlertDialogFooter>
                </AlertDialogContent>
            </AlertDialog>
        </>
    );
}
