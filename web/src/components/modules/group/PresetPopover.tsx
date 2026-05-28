'use client';

import { useState, useCallback } from 'react';
import { useTranslations } from 'next-intl';
import {
    Check,
    Pencil,
    Plus,
    Trash2,
    Copy,
    X,
    CheckCircle2,
    Layers,
} from 'lucide-react';
import {
    Popover,
    PopoverContent,
    PopoverTrigger,
} from '@/components/ui/popover';
import { Input } from '@/components/ui/input';
import { Button } from '@/components/ui/button';
import {
    Tooltip,
    TooltipContent,
    TooltipTrigger,
} from '@/components/animate-ui/components/animate/tooltip';
import {
    useGroupPresetList,
    useCreateGroupPreset,
    useCreateBlankGroupPreset,
    useCloneGroupPreset,
    useActivateGroupPreset,
    useDeleteGroupPreset,
    type Group,
    type GroupPreset,
} from '@/api/endpoints/group';
import { toast } from '@/components/common/Toast';
import { cn } from '@/lib/utils';
import { PresetEditorAutoOpener, PresetEditorContent } from './PresetEditor';
import {
    MorphingDialog,
    MorphingDialogContainer,
    MorphingDialogContent,
    MorphingDialogTrigger,
} from '@/components/ui/morphing-dialog';

interface PresetPopoverProps {
    group: Group;
}

type PendingAction =
    | { kind: 'none' }
    | { kind: 'create' }
    | { kind: 'delete'; presetID: number };

export function PresetPopover({ group }: PresetPopoverProps) {
    const t = useTranslations('group');
    const [open, setOpen] = useState(false);
    const [pending, setPending] = useState<PendingAction>({ kind: 'none' });
    const [nameDraft, setNameDraft] = useState('');
    const [pendingEditId, setPendingEditId] = useState<number | null>(null);

    const { data: presets = [], isLoading } = useGroupPresetList(open ? group.id : undefined);
    const createPreset = useCreateGroupPreset();
    const createBlankPreset = useCreateBlankGroupPreset();
    const clonePreset = useCloneGroupPreset();
    const activatePreset = useActivateGroupPreset();
    const deletePreset = useDeleteGroupPreset();

    const activeID = group.active_preset_id ?? null;

    const resetPending = useCallback(() => {
        setPending({ kind: 'none' });
        setNameDraft('');
        setPendingEditId(null);
    }, []);

    const handleCreateSubmit = useCallback(() => {
        if (!group.id) return;
        const name = nameDraft.trim();
        if (!name) return;
        createPreset.mutate(
            { groupID: group.id, name },
            {
                onSuccess: () => {
                    toast.success(t('preset.toast.created'));
                    resetPending();
                },
                onError: (e) => toast.error(t('preset.toast.createFailed'), { description: e.message }),
            },
        );
    }, [group.id, nameDraft, createPreset, t, resetPending]);

    const handleCreateBlank = useCallback(() => {
        if (!group.id) return;
        const name = `${t('preset.autoNamePrefix')} ${presets.length + 1}`;
        createBlankPreset.mutate(
            { groupID: group.id, name },
            {
                onSuccess: (preset) => {
                    toast.success(t('preset.toast.created'));
                    setPendingEditId(preset.id);
                },
                onError: (e) => toast.error(t('preset.toast.createBlankFailed'), { description: e.message }),
            },
        );
    }, [group.id, presets.length, createBlankPreset, t]);

    const handleActivate = useCallback((presetID: number) => {
        if (!group.id || presetID === activeID) return;
        activatePreset.mutate(
            { presetID, groupID: group.id },
            {
                onSuccess: () => toast.success(t('preset.toast.activated')),
                onError: (e) => toast.error(t('preset.toast.activateFailed'), { description: e.message }),
            },
        );
    }, [group.id, activeID, activatePreset, t]);

    const handleClone = useCallback((preset: GroupPreset) => {
        if (!group.id) return;
        const name = `${preset.name} ${t('preset.cloneSuffix')}`;
        clonePreset.mutate(
            { presetID: preset.id, groupID: group.id, name },
            {
                onSuccess: (cloned) => {
                    toast.success(t('preset.toast.cloned'));
                    setPendingEditId(cloned.id);
                },
                onError: (e) => toast.error(t('preset.toast.cloneFailed'), { description: e.message }),
            },
        );
    }, [group.id, clonePreset, t]);

    const handleDeleteSubmit = useCallback((presetID: number) => {
        if (deletePreset.isPending) return;
        deletePreset.mutate(
            { presetID, groupID: group.id },
            {
                onSuccess: () => {
                    toast.success(t('preset.toast.deleted'));
                    resetPending();
                },
                onError: (e) => toast.error(t('preset.toast.deleteFailed'), { description: e.message }),
            },
        );
    }, [deletePreset, group.id, t, resetPending]);

    const hasPresets = presets.length > 0;

    return (
        <>
            <Popover open={open} onOpenChange={(o) => { setOpen(o); if (!o) resetPending(); }}>
                <PopoverTrigger asChild>
                    <button
                        type="button"
                        className="p-1.5 rounded-lg transition-colors hover:bg-muted text-muted-foreground hover:text-foreground"
                    >
                        <Tooltip side="top" sideOffset={10} align="center">
                            <TooltipTrigger asChild>
                                <Layers className="size-4" />
                            </TooltipTrigger>
                            <TooltipContent>{t('preset.title')}</TooltipContent>
                        </Tooltip>
                    </button>
                </PopoverTrigger>

                <PopoverContent
                    align="end"
                    side="bottom"
                    sideOffset={8}
                    className="w-[20rem] rounded-2xl border border-border/60 bg-card p-3 shadow-xl"
                >
                    <div className="flex flex-col gap-3">
                        <div className="flex items-center justify-between">
                            <p className="text-sm font-semibold">{t('preset.title')}</p>
                            {pending.kind === 'none' && (
                                hasPresets ? (
                                    <button
                                        type="button"
                                        onClick={handleCreateBlank}
                                        disabled={createBlankPreset.isPending}
                                        className="flex items-center gap-1 text-xs text-muted-foreground transition-colors hover:text-foreground disabled:opacity-50"
                                    >
                                        <Plus className="size-3.5" />
                                        {t('preset.createBlank')}
                                    </button>
                                ) : (
                                    <button
                                        type="button"
                                        onClick={() => { setPending({ kind: 'create' }); setNameDraft(''); }}
                                        className="flex items-center gap-1 text-xs text-muted-foreground transition-colors hover:text-foreground"
                                    >
                                        <Plus className="size-3.5" />
                                        {t('preset.saveCurrent')}
                                    </button>
                                )
                            )}
                        </div>

                        {pending.kind === 'create' && (
                            <div className="flex items-center gap-1.5">
                                <Input
                                    autoFocus
                                    value={nameDraft}
                                    onChange={(e) => setNameDraft(e.target.value)}
                                    onKeyDown={(e) => {
                                        if (e.key === 'Enter') handleCreateSubmit();
                                        if (e.key === 'Escape') resetPending();
                                    }}
                                    placeholder={t('preset.namePlaceholder')}
                                    className="h-8 rounded-lg text-sm"
                                />
                                <Button
                                    type="button"
                                    size="sm"
                                    onClick={handleCreateSubmit}
                                    disabled={!nameDraft.trim() || createPreset.isPending}
                                    className="h-8 rounded-lg shrink-0"
                                >
                                    <Check className="size-3.5" />
                                </Button>
                                <button
                                    type="button"
                                    onClick={resetPending}
                                    className="p-1 rounded-lg hover:bg-muted text-muted-foreground shrink-0"
                                >
                                    <X className="size-4" />
                                </button>
                            </div>
                        )}

                        <div className="max-h-72 overflow-y-auto -mx-1 px-1 flex flex-col gap-0.5">
                            {isLoading && (
                                <div className="py-6 text-xs text-muted-foreground text-center">
                                    {t('preset.loading')}
                                </div>
                            )}
                            {!isLoading && presets.length === 0 && pending.kind !== 'create' && (
                                <div className="py-6 text-xs text-muted-foreground text-center">
                                    {t('preset.empty')}
                                </div>
                            )}
                            {presets.map((preset) => {
                                const isActive = activeID === preset.id;
                                const isDeletingThis = pending.kind === 'delete' && pending.presetID === preset.id;

                                if (isDeletingThis) {
                                    return (
                                        <div
                                            key={preset.id}
                                            className="flex items-center gap-2 rounded-lg bg-destructive/10 px-2.5 py-2 border border-destructive/20"
                                        >
                                            <span className="flex-1 text-xs text-foreground">
                                                {t('preset.confirmDelete', { name: preset.name })}
                                            </span>
                                            <button
                                                type="button"
                                                onClick={() => handleDeleteSubmit(preset.id)}
                                                disabled={deletePreset.isPending}
                                                className="h-6 rounded-md bg-destructive px-2 text-xs font-medium text-destructive-foreground hover:bg-destructive/90 transition-colors shrink-0 disabled:opacity-50 disabled:cursor-not-allowed"
                                            >
                                                {t('preset.confirm')}
                                            </button>
                                            <button
                                                type="button"
                                                onClick={resetPending}
                                                className="p-1 rounded-md hover:bg-muted text-muted-foreground shrink-0"
                                            >
                                                <X className="size-4" />
                                            </button>
                                        </div>
                                    );
                                }

                                return (
                                    <div
                                        key={preset.id}
                                        className={cn(
                                            'group/preset flex items-center gap-2 rounded-lg px-2.5 py-2 transition-colors',
                                            isActive ? 'bg-primary/5' : 'hover:bg-muted/60',
                                        )}
                                    >
                                        <button
                                            type="button"
                                            onClick={() => handleActivate(preset.id)}
                                            disabled={isActive || activatePreset.isPending}
                                            className="flex-1 flex items-center gap-2 min-w-0 text-left"
                                        >
                                            {isActive ? (
                                                <CheckCircle2 className="size-3.5 shrink-0 text-primary" />
                                            ) : (
                                                <span className="size-3.5 shrink-0 rounded-full border border-border" />
                                            )}
                                            <span className={cn('text-sm truncate', isActive && 'font-medium')}>{preset.name}</span>
                                            {isActive && (
                                                <span className="text-[10px] uppercase tracking-wide text-primary shrink-0">
                                                    {t('preset.activeBadge')}
                                                </span>
                                            )}
                                        </button>

                                        <div className="flex items-center gap-0.5 opacity-0 group-hover/preset:opacity-100 transition-opacity">
                                            <MorphingDialog key={`preset-edit-${preset.id}`}>
                                                <MorphingDialogTrigger
                                                    aria-label={t('preset.edit')}
                                                    className="p-1 rounded-md hover:bg-muted text-muted-foreground hover:text-foreground"
                                                >
                                                    <Tooltip side="top" sideOffset={6} align="center">
                                                        <TooltipTrigger asChild>
                                                            <Pencil className="size-3.5" />
                                                        </TooltipTrigger>
                                                        <TooltipContent>{t('preset.edit')}</TooltipContent>
                                                    </Tooltip>
                                                </MorphingDialogTrigger>
                                                <PresetEditorAutoOpener
                                                    active={pendingEditId === preset.id}
                                                    onOpened={() => setPendingEditId(null)}
                                                />
                                                <MorphingDialogContainer>
                                                    <MorphingDialogContent className="relative w-screen max-w-full md:max-w-4xl bg-card text-card-foreground px-6 py-4 rounded-3xl h-[calc(100vh-2rem)] flex flex-col overflow-hidden">
                                                        <PresetEditorContent preset={preset} />
                                                    </MorphingDialogContent>
                                                </MorphingDialogContainer>
                                            </MorphingDialog>
                                            <Tooltip side="top" sideOffset={6} align="center">
                                                <TooltipTrigger asChild>
                                                    <button
                                                        type="button"
                                                        onClick={() => handleClone(preset)}
                                                        disabled={clonePreset.isPending}
                                                        className="p-1 rounded-md hover:bg-muted text-muted-foreground hover:text-foreground disabled:opacity-50"
                                                    >
                                                        <Copy className="size-3.5" />
                                                    </button>
                                                </TooltipTrigger>
                                                <TooltipContent>{t('preset.clone')}</TooltipContent>
                                            </Tooltip>
                                            {!isActive && (
                                                <Tooltip side="top" sideOffset={6} align="center">
                                                    <TooltipTrigger asChild>
                                                        <button
                                                            type="button"
                                                            onClick={() => setPending({ kind: 'delete', presetID: preset.id })}
                                                            className="p-1 rounded-md hover:bg-destructive/10 text-muted-foreground hover:text-destructive"
                                                        >
                                                            <Trash2 className="size-3.5" />
                                                        </button>
                                                    </TooltipTrigger>
                                                    <TooltipContent>{t('preset.delete')}</TooltipContent>
                                                </Tooltip>
                                            )}
                                        </div>
                                    </div>
                                );
                            })}
                        </div>
                    </div>
                </PopoverContent>
            </Popover>
        </>
    );
}
