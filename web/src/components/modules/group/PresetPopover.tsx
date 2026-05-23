'use client';

import { useState, useCallback } from 'react';
import { useTranslations } from 'next-intl';
import {
    Check,
    Pencil,
    Plus,
    Trash2,
    ArrowDownToLine,
    X,
    CheckCircle2,
    Layers,
    Type,
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
    useActivateGroupPreset,
    useOverwriteGroupPreset,
    useRenameGroupPreset,
    useDeleteGroupPreset,
    type Group,
    type GroupPreset,
} from '@/api/endpoints/group';
import { toast } from '@/components/common/Toast';
import { cn } from '@/lib/utils';
import { PresetEditor } from './PresetEditor';

interface PresetPopoverProps {
    group: Group;
}

type PendingAction =
    | { kind: 'none' }
    | { kind: 'create' }
    | { kind: 'rename'; presetID: number; current: string }
    | { kind: 'delete'; presetID: number }
    | { kind: 'overwrite'; presetID: number };

export function PresetPopover({ group }: PresetPopoverProps) {
    const t = useTranslations('group');
    const [open, setOpen] = useState(false);
    const [pending, setPending] = useState<PendingAction>({ kind: 'none' });
    const [nameDraft, setNameDraft] = useState('');
    const [editingPreset, setEditingPreset] = useState<GroupPreset | null>(null);

    const { data: presets = [], isLoading } = useGroupPresetList(open ? group.id : undefined);
    const createPreset = useCreateGroupPreset();
    const activatePreset = useActivateGroupPreset();
    const overwritePreset = useOverwriteGroupPreset();
    const renamePreset = useRenameGroupPreset();
    const deletePreset = useDeleteGroupPreset();

    const activeID = group.active_preset_id ?? null;

    const resetPending = useCallback(() => {
        setPending({ kind: 'none' });
        setNameDraft('');
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

    const handleOverwriteSubmit = useCallback((presetID: number) => {
        overwritePreset.mutate(
            { presetID, groupID: group.id },
            {
                onSuccess: () => {
                    toast.success(t('preset.toast.overwritten'));
                    resetPending();
                },
                onError: (e) => toast.error(t('preset.toast.overwriteFailed'), { description: e.message }),
            },
        );
    }, [overwritePreset, group.id, t, resetPending]);

    const handleRenameSubmit = useCallback((presetID: number) => {
        const name = nameDraft.trim();
        if (!name) return;
        renamePreset.mutate(
            { presetID, groupID: group.id, name },
            {
                onSuccess: () => {
                    toast.success(t('preset.toast.renamed'));
                    resetPending();
                },
                onError: (e) => toast.error(t('preset.toast.renameFailed'), { description: e.message }),
            },
        );
    }, [nameDraft, renamePreset, group.id, t, resetPending]);

    const handleDeleteSubmit = useCallback((presetID: number) => {
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

                <PopoverContent align="end" sideOffset={6} className="w-80 p-0">
                    <div className="flex items-center justify-between px-3 py-2 border-b border-border/40">
                        <span className="text-sm font-semibold">{t('preset.title')}</span>
                        {pending.kind === 'none' && (
                            <button
                                type="button"
                                onClick={() => { setPending({ kind: 'create' }); setNameDraft(''); }}
                                className="flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground transition-colors"
                            >
                                <Plus className="size-3.5" />
                                {t('preset.saveAs')}
                            </button>
                        )}
                    </div>

                    {pending.kind === 'create' && (
                        <div className="p-3 border-b border-border/40 flex items-center gap-2">
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
                                className="h-8 rounded-lg"
                            >
                                <Check className="size-3.5" />
                            </Button>
                            <button
                                type="button"
                                onClick={resetPending}
                                className="p-1 rounded-lg hover:bg-muted text-muted-foreground"
                            >
                                <X className="size-4" />
                            </button>
                        </div>
                    )}

                    <div className="max-h-80 overflow-y-auto py-1">
                        {isLoading && (
                            <div className="px-3 py-4 text-sm text-muted-foreground text-center">
                                {t('preset.loading')}
                            </div>
                        )}
                        {!isLoading && presets.length === 0 && pending.kind !== 'create' && (
                            <div className="px-3 py-4 text-sm text-muted-foreground text-center">
                                {t('preset.empty')}
                            </div>
                        )}
                        {presets.map((preset) => {
                            const isActive = activeID === preset.id;
                            const isRenamingThis = pending.kind === 'rename' && pending.presetID === preset.id;
                            const isDeletingThis = pending.kind === 'delete' && pending.presetID === preset.id;
                            const isOverwritingThis = pending.kind === 'overwrite' && pending.presetID === preset.id;

                            if (isRenamingThis) {
                                return (
                                    <div key={preset.id} className="px-3 py-2 flex items-center gap-2 bg-muted/40">
                                        <Input
                                            autoFocus
                                            value={nameDraft}
                                            onChange={(e) => setNameDraft(e.target.value)}
                                            onKeyDown={(e) => {
                                                if (e.key === 'Enter') handleRenameSubmit(preset.id);
                                                if (e.key === 'Escape') resetPending();
                                            }}
                                            className="h-7 rounded-lg text-sm"
                                        />
                                        <button
                                            type="button"
                                            onClick={() => handleRenameSubmit(preset.id)}
                                            disabled={!nameDraft.trim() || renamePreset.isPending}
                                            className="p-1 rounded-lg hover:bg-muted disabled:opacity-50"
                                        >
                                            <Check className="size-4" />
                                        </button>
                                        <button
                                            type="button"
                                            onClick={resetPending}
                                            className="p-1 rounded-lg hover:bg-muted text-muted-foreground"
                                        >
                                            <X className="size-4" />
                                        </button>
                                    </div>
                                );
                            }

                            if (isDeletingThis || isOverwritingThis) {
                                const isDel = isDeletingThis;
                                return (
                                    <div key={preset.id} className="px-3 py-2 flex items-center gap-2 bg-destructive/10">
                                        <span className="flex-1 text-xs">
                                            {isDel ? t('preset.confirmDelete', { name: preset.name }) : t('preset.confirmOverwrite', { name: preset.name })}
                                        </span>
                                        <button
                                            type="button"
                                            onClick={() => isDel ? handleDeleteSubmit(preset.id) : handleOverwriteSubmit(preset.id)}
                                            className={cn(
                                                'px-2 py-1 rounded-lg text-xs font-medium',
                                                isDel
                                                    ? 'bg-destructive text-destructive-foreground hover:bg-destructive/90'
                                                    : 'bg-primary text-primary-foreground hover:bg-primary/90',
                                            )}
                                        >
                                            {t('preset.confirm')}
                                        </button>
                                        <button
                                            type="button"
                                            onClick={resetPending}
                                            className="p-1 rounded-lg hover:bg-muted text-muted-foreground"
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
                                        'group/preset px-3 py-2 flex items-center gap-2 hover:bg-muted/40 transition-colors',
                                        isActive && 'bg-primary/5',
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
                                            <span className="text-[10px] text-primary shrink-0">
                                                {t('preset.activeBadge')}
                                            </span>
                                        )}
                                    </button>

                                    <div className="flex items-center gap-0.5 opacity-0 group-hover/preset:opacity-100 transition-opacity">
                                        {!isActive && (
                                            <Tooltip side="top" sideOffset={6} align="center">
                                                <TooltipTrigger asChild>
                                                    <button
                                                        type="button"
                                                        onClick={() => setEditingPreset(preset)}
                                                        className="p-1 rounded-md hover:bg-muted text-muted-foreground hover:text-foreground"
                                                    >
                                                        <Pencil className="size-3.5" />
                                                    </button>
                                                </TooltipTrigger>
                                                <TooltipContent>{t('preset.edit')}</TooltipContent>
                                            </Tooltip>
                                        )}
                                        <Tooltip side="top" sideOffset={6} align="center">
                                            <TooltipTrigger asChild>
                                                <button
                                                    type="button"
                                                    onClick={() => setPending({ kind: 'overwrite', presetID: preset.id })}
                                                    className="p-1 rounded-md hover:bg-muted text-muted-foreground hover:text-foreground"
                                                >
                                                    <ArrowDownToLine className="size-3.5" />
                                                </button>
                                            </TooltipTrigger>
                                            <TooltipContent>{t('preset.overwrite')}</TooltipContent>
                                        </Tooltip>
                                        <Tooltip side="top" sideOffset={6} align="center">
                                            <TooltipTrigger asChild>
                                                <button
                                                    type="button"
                                                    onClick={() => { setPending({ kind: 'rename', presetID: preset.id, current: preset.name }); setNameDraft(preset.name); }}
                                                    className="p-1 rounded-md hover:bg-muted text-muted-foreground hover:text-foreground"
                                                >
                                                    <Type className="size-3.5" />
                                                </button>
                                            </TooltipTrigger>
                                            <TooltipContent>{t('preset.rename')}</TooltipContent>
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
                </PopoverContent>
            </Popover>

            {editingPreset && (
                <PresetEditor
                    preset={editingPreset}
                    open={!!editingPreset}
                    onOpenChange={(o) => { if (!o) setEditingPreset(null); }}
                />
            )}
        </>
    );
}
