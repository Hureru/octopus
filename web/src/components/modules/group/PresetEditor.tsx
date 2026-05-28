'use client';

import { useCallback, useEffect, useMemo, useRef } from 'react';
import { useTranslations } from 'next-intl';
import {
    MorphingDialogClose,
    MorphingDialogDescription,
    MorphingDialogTitle,
    useMorphingDialog,
} from '@/components/ui/morphing-dialog';
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

interface PresetEditorContentProps {
    preset: GroupPreset;
}

/**
 * 预设编辑面板的内容部分。配合 MorphingDialog 使用，承担与 Channel/Group 卡片
 * 编辑面板一致的 morph 进入动画与视觉风格（bg-card / rounded-3xl / text-2xl）。
 */
export function PresetEditorContent({ preset }: PresetEditorContentProps) {
    const t = useTranslations('group');
    const { setIsOpen } = useMorphingDialog();
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
                    setIsOpen(false);
                },
                onError: (error) => toast.error(t('preset.toast.updateFailed'), { description: error.message }),
            },
        );
    }, [preset.id, preset.group_id, updatePreset, t, setIsOpen]);

    return (
        <>
            <MorphingDialogTitle className="shrink-0">
                <header className="mb-3 flex items-center justify-between">
                    <h2 className="text-2xl font-bold text-card-foreground truncate pr-4">
                        {t('preset.editorTitle')}
                    </h2>
                    <MorphingDialogClose
                        className="relative right-0 top-0 p-1 rounded-md text-muted-foreground hover:text-foreground hover:bg-muted transition-colors"
                        variants={{
                            initial: { opacity: 0, scale: 0.8 },
                            animate: { opacity: 1, scale: 1 },
                            exit: { opacity: 0, scale: 0.8 },
                        }}
                    />
                </header>
            </MorphingDialogTitle>
            <MorphingDialogDescription
                disableLayoutAnimation
                className="flex-1 min-h-0 overflow-hidden"
            >
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
                    onCancel={() => setIsOpen(false)}
                    onSubmit={handleSubmit}
                    nameLabel={t('preset.name')}
                />
            </MorphingDialogDescription>
        </>
    );
}

interface PresetEditorAutoOpenerProps {
    active: boolean;
    onOpened: () => void;
}

/**
 * 在 MorphingDialog 内部用 hook 主动调用 setIsOpen(true)，覆盖
 * "创建空白预设 / 克隆预设" 后需要自动打开编辑器的链路。
 */
export function PresetEditorAutoOpener({ active, onOpened }: PresetEditorAutoOpenerProps) {
    const { setIsOpen } = useMorphingDialog();
    const triggered = useRef(false);
    useEffect(() => {
        if (!active) {
            triggered.current = false;
            return;
        }
        if (triggered.current) return;
        const raf = window.requestAnimationFrame(() => {
            triggered.current = true;
            setIsOpen(true);
            onOpened();
        });
        return () => window.cancelAnimationFrame(raf);
    }, [active, setIsOpen, onOpened]);
    return null;
}
