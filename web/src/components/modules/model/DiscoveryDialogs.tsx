'use client';

import { useMemo, useState } from 'react';
import { useTranslations } from 'next-intl';
import { useGroupList } from '@/api/endpoints/group';
import { Button } from '@/components/ui/button';
import {
    Dialog,
    DialogContent,
    DialogDescription,
    DialogFooter,
    DialogHeader,
    DialogTitle,
} from '@/components/ui/dialog';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { cn } from '@/lib/utils';

const GROUP_SUGGESTION_LIMIT = 8;

export type MapToGroupPayload = {
    targetName: string;
    deleteEmptySourceGroups: boolean;
};

/**
 * 把选中的上游模型映射到同一个分组，例如 z-ai/glm-5.2 → glm-5.2。
 * 目标分组可以是已存在的分组，也可以直接写一个新名字。
 */
export function MapToGroupDialog({
    open,
    modelNames,
    pending,
    onOpenChange,
    onSubmit,
}: {
    open: boolean;
    modelNames: string[];
    pending: boolean;
    onOpenChange: (open: boolean) => void;
    onSubmit: (payload: MapToGroupPayload) => void;
}) {
    const t = useTranslations('model.discovery');
    const groups = useGroupList();
    const [targetName, setTargetName] = useState('');
    const [deleteEmptySourceGroups, setDeleteEmptySourceGroups] = useState(true);

    const suggestions = useMemo(() => {
        const term = targetName.trim().toLowerCase();
        const names = (groups.data ?? []).map((group) => group.name).filter(Boolean);
        const matched = term ? names.filter((name) => name.toLowerCase().includes(term)) : names;
        return matched.slice(0, GROUP_SUGGESTION_LIMIT);
    }, [groups.data, targetName]);

    const trimmedTarget = targetName.trim();

    return (
        <Dialog
            open={open}
            onOpenChange={(next) => {
                if (!next) setTargetName('');
                onOpenChange(next);
            }}
        >
            <DialogContent className="sm:max-w-lg">
                <DialogHeader>
                    <DialogTitle>{t('mapTitle')}</DialogTitle>
                    <DialogDescription>{t('mapDescription', { count: modelNames.length })}</DialogDescription>
                </DialogHeader>

                <div className="grid gap-4">
                    <div className="max-h-24 overflow-y-auto rounded-md border bg-muted/40 p-2 text-xs text-muted-foreground">
                        {modelNames.map((name) => (
                            <div key={name} className="truncate">
                                {name}
                            </div>
                        ))}
                    </div>

                    <div className="grid gap-2">
                        <Label htmlFor="map-target-group">{t('mapTargetLabel')}</Label>
                        <Input
                            id="map-target-group"
                            value={targetName}
                            autoComplete="off"
                            placeholder={t('mapTargetPlaceholder')}
                            onChange={(event) => setTargetName(event.target.value)}
                        />
                        {suggestions.length > 0 ? (
                            <div className="flex flex-wrap gap-1.5">
                                {suggestions.map((name) => (
                                    <button
                                        key={name}
                                        type="button"
                                        onClick={() => setTargetName(name)}
                                        className={cn(
                                            'min-h-7 rounded-full border px-2.5 text-xs transition-colors',
                                            name === trimmedTarget
                                                ? 'border-transparent bg-primary text-primary-foreground'
                                                : 'hover:bg-muted',
                                        )}
                                    >
                                        {name}
                                    </button>
                                ))}
                            </div>
                        ) : null}
                    </div>

                    <label className="flex items-start gap-2 text-sm text-muted-foreground">
                        <input
                            type="checkbox"
                            checked={deleteEmptySourceGroups}
                            onChange={(event) => setDeleteEmptySourceGroups(event.target.checked)}
                            className="mt-0.5 size-4 rounded border-border bg-background align-middle accent-primary"
                        />
                        <span>{t('mapDeleteSourceGroups')}</span>
                    </label>
                </div>

                <DialogFooter>
                    <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
                        {t('cancel')}
                    </Button>
                    <Button
                        type="button"
                        disabled={pending || trimmedTarget === ''}
                        onClick={() => onSubmit({ targetName: trimmedTarget, deleteEmptySourceGroups })}
                    >
                        {pending ? t('applying') : t('mapSubmit')}
                    </Button>
                </DialogFooter>
            </DialogContent>
        </Dialog>
    );
}

/**
 * 取消分组的二次确认：会删除对应分组，可能影响正在使用该模型名的客户端。
 */
export function UnprovisionDialog({
    open,
    modelNames,
    pending,
    onOpenChange,
    onSubmit,
}: {
    open: boolean;
    modelNames: string[];
    pending: boolean;
    onOpenChange: (open: boolean) => void;
    onSubmit: (deleteGroup: boolean) => void;
}) {
    const t = useTranslations('model.discovery');
    const [deleteGroup, setDeleteGroup] = useState(true);

    return (
        <Dialog open={open} onOpenChange={onOpenChange}>
            <DialogContent className="sm:max-w-lg">
                <DialogHeader>
                    <DialogTitle>{t('removeTitle')}</DialogTitle>
                    <DialogDescription>{t('removeDescription', { count: modelNames.length })}</DialogDescription>
                </DialogHeader>

                <div className="grid gap-4">
                    <div className="max-h-24 overflow-y-auto rounded-md border bg-muted/40 p-2 text-xs text-muted-foreground">
                        {modelNames.map((name) => (
                            <div key={name} className="truncate">
                                {name}
                            </div>
                        ))}
                    </div>
                    <label className="flex items-start gap-2 text-sm text-muted-foreground">
                        <input
                            type="checkbox"
                            checked={deleteGroup}
                            onChange={(event) => setDeleteGroup(event.target.checked)}
                            className="mt-0.5 size-4 rounded border-border bg-background align-middle accent-primary"
                        />
                        <span>{t('removeDeleteGroup')}</span>
                    </label>
                </div>

                <DialogFooter>
                    <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
                        {t('cancel')}
                    </Button>
                    <Button
                        type="button"
                        variant="destructive"
                        disabled={pending}
                        onClick={() => onSubmit(deleteGroup)}
                    >
                        {pending ? t('applying') : t('removeSubmit')}
                    </Button>
                </DialogFooter>
            </DialogContent>
        </Dialog>
    );
}
