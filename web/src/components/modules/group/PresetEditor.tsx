'use client';

import { useCallback, useMemo } from 'react';
import { useTranslations } from 'next-intl';
import { XIcon } from 'lucide-react';
import {
    Dialog,
    DialogContent,
    DialogDescription,
    DialogTitle,
} from '@/components/ui/dialog';
import { useModelChannelList } from '@/api/endpoints/model';
import {
    useUpdateGroupPreset,
    type GroupPreset,
    type GroupPresetItem,
} from '@/api/endpoints/group';
import { toast } from '@/components/common/Toast';
import { GroupEditor, type GroupEditorValues } from './Editor';
import type { SelectedMember } from './ItemList';
import { modelChannelKey } from './utils';

interface PresetEditorProps {
    preset: GroupPreset;
    open: boolean;
    onOpenChange: (open: boolean) => void;
}

/**
 * 编辑预设内容。视觉上对齐分组卡片里的 MorphingDialog 编辑面板
 * （bg-card / rounded-3xl / px-6 py-4 / text-2xl 标题）。因为是受控弹出
 * （PresetPopover 触发），用 Dialog 而非 MorphingDialog；MorphingDialog 必须有 trigger 元素。
 */
export function PresetEditor({ preset, open, onOpenChange }: PresetEditorProps) {
    const t = useTranslations('group');
    const { data: modelChannels = [] } = useModelChannelList();
    const updatePreset = useUpdateGroupPreset();

    const modelChannelByKey = useMemo(() => {
        const map = new Map<string, typeof modelChannels[number]>();
        modelChannels.forEach((mc) => {
            map.set(modelChannelKey(mc.channel_id, mc.name), mc);
        });
        return map;
    }, [modelChannels]);

    const initialMembers = useMemo<SelectedMember[]>(() => {
        return [...preset.items]
            .sort((a, b) => a.priority - b.priority)
            .map((item) => {
                const key = modelChannelKey(item.channel_id, item.model_name);
                const mc = modelChannelByKey.get(key);
                return {
                    ...mc,
                    id: key,
                    name: item.model_name,
                    enabled: mc?.enabled ?? true,
                    channel_id: item.channel_id,
                    channel_name: mc?.channel_name ?? `Channel ${item.channel_id}`,
                    weight: item.weight,
                };
            });
    }, [preset.items, modelChannelByKey]);

    const handleSubmit = useCallback((values: GroupEditorValues) => {
        const items: GroupPresetItem[] = values.members.map((m, idx) => ({
            channel_id: m.channel_id,
            model_name: m.name,
            priority: idx + 1,
            weight: m.weight ?? 1,
        }));
        updatePreset.mutate(
            {
                presetID: preset.id,
                groupID: preset.group_id,
                data: {
                    name: values.name,
                    mode: values.mode,
                    match_regex: values.match_regex,
                    first_token_time_out: values.first_token_time_out,
                    session_keep_time: values.session_keep_time,
                    retry_enabled: values.retry_enabled,
                    max_retries: values.max_retries,
                    items,
                },
            },
            {
                onSuccess: () => {
                    toast.success(t('preset.toast.updated'));
                    onOpenChange(false);
                },
                onError: (error) => toast.error(t('preset.toast.updateFailed'), { description: error.message }),
            },
        );
    }, [preset.id, preset.group_id, updatePreset, t, onOpenChange]);

    return (
        <Dialog open={open} onOpenChange={onOpenChange}>
            <DialogContent
                showCloseButton={false}
                className="w-screen max-w-full md:max-w-4xl bg-card text-card-foreground px-6 py-4 rounded-3xl h-[calc(100vh-2rem)] flex flex-col overflow-hidden gap-0 border-0 sm:max-w-4xl"
            >
                <DialogTitle asChild>
                    <header className="mb-3 flex items-center justify-between shrink-0">
                        <h2 className="text-2xl font-bold text-card-foreground truncate pr-4">
                            {t('preset.editorTitle')}
                        </h2>
                        <button
                            type="button"
                            onClick={() => onOpenChange(false)}
                            aria-label={t('preset.close')}
                            className="p-1 rounded-md text-muted-foreground hover:text-foreground hover:bg-muted transition-colors shrink-0"
                        >
                            <XIcon className="size-5" />
                        </button>
                    </header>
                </DialogTitle>
                <DialogDescription className="sr-only">
                    {t('preset.editorTitle')}
                </DialogDescription>
                <div className="flex-1 min-h-0 overflow-hidden">
                    <GroupEditor
                        key={`preset-${preset.id}`}
                        initial={{
                            name: preset.name,
                            match_regex: preset.match_regex ?? '',
                            mode: preset.mode,
                            first_token_time_out: preset.first_token_time_out ?? 0,
                            session_keep_time: preset.session_keep_time ?? 0,
                            retry_enabled: preset.retry_enabled ?? false,
                            max_retries: preset.max_retries ?? 3,
                            members: initialMembers,
                        }}
                        submitText={t('preset.save')}
                        submittingText={t('preset.saving')}
                        isSubmitting={updatePreset.isPending}
                        onCancel={() => onOpenChange(false)}
                        onSubmit={handleSubmit}
                        nameLabel={t('preset.name')}
                    />
                </div>
            </DialogContent>
        </Dialog>
    );
}
