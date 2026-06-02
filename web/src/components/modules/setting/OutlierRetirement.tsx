'use client';

import { useEffect, useState, useRef } from 'react';
import { useTranslations } from 'next-intl';
import { ShieldAlert, HelpCircle } from 'lucide-react';
import { Input } from '@/components/ui/input';
import { Switch } from '@/components/ui/switch';
import { useSettingList, useSetSetting, SettingKey } from '@/api/endpoints/setting';
import { toast } from '@/components/common/Toast';
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '@/components/animate-ui/components/animate/tooltip';

const NUMBER_FIELDS = [
    { key: SettingKey.OutlierRetireInterval, labelKey: 'interval' },
    { key: SettingKey.OutlierFailRatePct, labelKey: 'failRate' },
    { key: SettingKey.OutlierMinSamples, labelKey: 'minSamples' },
    { key: SettingKey.OutlierConsecFails, labelKey: 'consecFails' },
    { key: SettingKey.OutlierWindowMinutes, labelKey: 'windowMinutes' },
    { key: SettingKey.OutlierWindowCapacity, labelKey: 'windowCapacity' },
    { key: SettingKey.OutlierRecoverStreak, labelKey: 'recoverStreak' },
    { key: SettingKey.OutlierReapMinutes, labelKey: 'reapMinutes' },
    { key: SettingKey.OutlierCFRecoverMinutes, labelKey: 'cfRecoverMinutes' },
] as const;

export function SettingOutlierRetirement() {
    const t = useTranslations('setting');
    const { data: settings } = useSettingList();
    const setSetting = useSetSetting();

    const [enabled, setEnabled] = useState(false);
    const initialEnabled = useRef(false);
    const [values, setValues] = useState<Record<string, string>>({});
    const initialValues = useRef<Record<string, string>>({});
    const initialized = useRef(false);

    useEffect(() => {
        // 仅首次拿到数据时回填：useSettingList 每 30s 轮询、保存成功又会 invalidate，
        // 若每次刷新都重置本地状态，会覆盖用户正在编辑但尚未保存的输入。
        if (!settings || initialized.current) return;
        const en = settings.find(s => s.key === SettingKey.OutlierRetireEnabled);
        if (en) {
            const v = en.value === 'true';
            queueMicrotask(() => setEnabled(v));
            initialEnabled.current = v;
        }
        const next: Record<string, string> = {};
        for (const f of NUMBER_FIELDS) {
            const found = settings.find(s => s.key === f.key);
            if (found) next[f.key] = found.value;
        }
        queueMicrotask(() => setValues(next));
        initialValues.current = { ...next };
        initialized.current = true;
    }, [settings]);

    const handleEnabledChange = (checked: boolean) => {
        setEnabled(checked);
        setSetting.mutate(
            { key: SettingKey.OutlierRetireEnabled, value: checked ? 'true' : 'false' },
            {
                onSuccess: () => { toast.success(t('saved')); initialEnabled.current = checked; },
                onError: () => { setEnabled(initialEnabled.current); toast.error(t('saveFailed')); },
            }
        );
    };

    const handleNumberSave = (key: string) => {
        const value = values[key] ?? '';
        if (value === (initialValues.current[key] ?? '')) return;
        setSetting.mutate(
            { key, value },
            { onSuccess: () => { toast.success(t('saved')); initialValues.current[key] = value; } }
        );
    };

    return (
        <div className="rounded-3xl border border-border bg-card p-6 space-y-5">
            <h2 className="text-lg font-bold text-card-foreground flex items-center gap-2">
                <ShieldAlert className="h-5 w-5" />
                {t('outlierRetirement.title')}
                <TooltipProvider>
                    <Tooltip>
                        <TooltipTrigger asChild>
                            <HelpCircle className="size-4 text-muted-foreground cursor-help" />
                        </TooltipTrigger>
                        <TooltipContent>{t('outlierRetirement.hint')}</TooltipContent>
                    </Tooltip>
                </TooltipProvider>
            </h2>

            {/* 总开关 */}
            <div className="flex items-center justify-between gap-4">
                <span className="text-sm font-medium">{t('outlierRetirement.enabled.label')}</span>
                <Switch checked={enabled} onCheckedChange={handleEnabledChange} />
            </div>

            {/* 阈值 */}
            {NUMBER_FIELDS.map(f => (
                <div key={f.key} className="flex items-center justify-between gap-4">
                    <span className="text-sm font-medium">{t(`outlierRetirement.${f.labelKey}.label`)}</span>
                    <Input
                        type="number"
                        value={values[f.key] ?? ''}
                        onChange={(e) => setValues(prev => ({ ...prev, [f.key]: e.target.value }))}
                        onBlur={() => handleNumberSave(f.key)}
                        placeholder={t(`outlierRetirement.${f.labelKey}.placeholder`)}
                        className="w-48 rounded-xl"
                        disabled={!enabled}
                    />
                </div>
            ))}
        </div>
    );
}
