'use client';

import { useCallback, useEffect, useRef, useState } from 'react';
import { useTranslations } from 'next-intl';
import { HelpCircle, type LucideIcon } from 'lucide-react';
import { useSettingList, useSetSetting } from '@/api/endpoints/setting';
import { toast } from '@/components/common/Toast';
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '@/components/animate-ui/components/animate/tooltip';
import type { ApiError } from '@/api/types';

// 文本/数字设置项的本地编辑状态。
// 仅首次拿到数据时回填：useSettingList 每 30s 轮询、保存成功又会 invalidate，
// 反复回填会覆盖正在编辑但尚未保存的输入。
// 回填经 queueMicrotask 延迟：lint 规则 react-hooks/set-state-in-effect 禁止 effect 内同步 setState。
// mirrorKeys 用于一个输入同时写多个 key（需传模块级常量保持引用稳定），回显以 key 为准。
export function useSettingField(key: string, mirrorKeys?: readonly string[]) {
    const t = useTranslations('setting');
    const { data: settings } = useSettingList();
    const setSetting = useSetSetting();

    const [value, setValue] = useState('');
    const initial = useRef('');
    const initialized = useRef(false);

    useEffect(() => {
        if (!settings || initialized.current) return;
        const found = settings.find((s) => s.key === key);
        if (found) {
            queueMicrotask(() => setValue(found.value));
            initial.current = found.value;
        }
        initialized.current = true;
    }, [settings, key]);

    const commit = useCallback(async (next: string) => {
        setValue(next);
        if (next === initial.current) return;
        try {
            await Promise.all(
                [key, ...(mirrorKeys ?? [])].map((k) => setSetting.mutateAsync({ key: k, value: next }))
            );
            toast.success(t('saved'));
            initial.current = next;
        } catch (error) {
            toast.error(t('saveFailed'), { description: (error as ApiError)?.message });
            setValue(initial.current);
        }
    }, [key, mirrorKeys, setSetting, t]);

    const save = useCallback(() => commit(value), [commit, value]);

    return { value, setValue, save, commit };
}

// 开关型设置项：切换立即保存，失败回滚。
export function useSettingToggle(key: string) {
    const t = useTranslations('setting');
    const { data: settings } = useSettingList();
    const setSetting = useSetSetting();

    const [enabled, setEnabled] = useState(false);
    const initial = useRef(false);
    const initialized = useRef(false);

    useEffect(() => {
        if (!settings || initialized.current) return;
        const found = settings.find((s) => s.key === key);
        if (found) {
            const v = found.value === 'true';
            queueMicrotask(() => setEnabled(v));
            initial.current = v;
        }
        initialized.current = true;
    }, [settings, key]);

    const toggle = useCallback((checked: boolean) => {
        setEnabled(checked);
        setSetting.mutate(
            { key, value: checked ? 'true' : 'false' },
            {
                onSuccess: () => {
                    toast.success(t('saved'));
                    initial.current = checked;
                },
                onError: () => {
                    setEnabled(initial.current);
                    toast.error(t('saveFailed'));
                },
            }
        );
    }, [key, setSetting, t]);

    return { enabled, toggle };
}

export function SettingHelpTip({ children }: { children: React.ReactNode }) {
    return (
        <TooltipProvider>
            <Tooltip>
                <TooltipTrigger asChild>
                    <HelpCircle className="size-4 text-muted-foreground cursor-help" />
                </TooltipTrigger>
                {/* 限宽让长描述自动换行；内层覆盖组件自带的 text-balance——
                    balance 会把各行收窄至近似等宽，导致盒子右侧留白 */}
                <TooltipContent className="max-w-xs">
                    <span className="block text-wrap">{children}</span>
                </TooltipContent>
            </Tooltip>
        </TooltipProvider>
    );
}

export function SettingCard({ icon: Icon, title, tooltip, children }: {
    icon: LucideIcon;
    title: string;
    tooltip?: React.ReactNode;
    children: React.ReactNode;
}) {
    return (
        <div className="rounded-3xl border border-border bg-card p-6 space-y-5">
            <h2 className="text-lg font-bold text-card-foreground flex items-center gap-2">
                <Icon className="h-5 w-5" />
                {title}
                {tooltip && <SettingHelpTip>{tooltip}</SettingHelpTip>}
            </h2>
            {children}
        </div>
    );
}

export function SettingRow({ icon: Icon, label, tooltip, children }: {
    icon?: LucideIcon;
    label: string;
    tooltip?: React.ReactNode;
    children: React.ReactNode;
}) {
    return (
        <div className="flex items-center justify-between gap-4">
            <div className="flex items-center gap-3">
                {Icon && <Icon className="h-5 w-5 text-muted-foreground" />}
                <span className="text-sm font-medium">{label}</span>
                {tooltip && <SettingHelpTip>{tooltip}</SettingHelpTip>}
            </div>
            {children}
        </div>
    );
}

// 合并卡片内的小节标题：上边框分隔 + 标题 + 可选说明
export function SettingSection({ title, tooltip }: { title: string; tooltip?: React.ReactNode }) {
    return (
        <div className="flex items-center gap-2 border-t border-border pt-4 text-sm font-semibold text-card-foreground">
            {title}
            {tooltip && <SettingHelpTip>{tooltip}</SettingHelpTip>}
        </div>
    );
}
