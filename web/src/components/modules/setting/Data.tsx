'use client';

import { useMemo, useRef, useState } from 'react';
import { useTranslations } from 'next-intl';
import { AlertTriangle, Calendar, Clock, Database, Download, FileArchive, ScrollText, Trash2, Upload } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Switch } from '@/components/ui/switch';
import { toast } from '@/components/common/Toast';
import { SettingKey, useExportDB, useImportDB } from '@/api/endpoints/setting';
import { useClearLogs } from '@/api/endpoints/log';
import { SettingCard, SettingRow, SettingSection, useSettingField, useSettingToggle } from './shared';

export function SettingData() {
    const t = useTranslations('setting');

    // 历史日志与统计持久化
    const logEnabled = useSettingToggle(SettingKey.RelayLogKeepEnabled);
    const keepPeriod = useSettingField(SettingKey.RelayLogKeepPeriod);
    const statsInterval = useSettingField(SettingKey.StatsSaveInterval);
    const clearLogs = useClearLogs();

    // 备份导出/导入
    const exportDB = useExportDB();
    const importDB = useImportDB();

    const [includeStats, setIncludeStats] = useState(true);
    // 常规导出固定 JSON（可导入恢复）；含日志导出为 ZIP 流式归档，单独成按钮
    const [exportingKind, setExportingKind] = useState<'json' | 'logs' | null>(null);

    const [file, setFile] = useState<File | null>(null);
    const fileInputRef = useRef<HTMLInputElement | null>(null);

    const rowsAffected = importDB.data?.rows_affected ?? null;
    const rowsAffectedList = useMemo(() => {
        if (!rowsAffected) return [];
        return Object.entries(rowsAffected)
            .sort(([a], [b]) => a.localeCompare(b))
            .map(([k, v]) => ({ table: k, count: v }));
    }, [rowsAffected]);

    const handleClearLogs = () => {
        clearLogs.mutate(undefined, {
            onSuccess: () => toast.success(t('log.clearSuccess')),
            onError: () => toast.error(t('log.clearFailed')),
        });
    };

    const onImport = async () => {
        if (!file) {
            toast.error(t('backup.import.noFile'));
            return;
        }
        // accept 属性只约束选择器默认过滤，仍可手动选任意文件，导入前再校验一次
        if (file.type !== 'application/json' && !file.name.toLowerCase().endsWith('.json')) {
            toast.error(t('backup.import.invalidFileType'));
            if (fileInputRef.current) fileInputRef.current.value = '';
            setFile(null);
            return;
        }
        try {
            await importDB.mutateAsync(file);
            toast.success(t('backup.import.success'));
            if (fileInputRef.current) fileInputRef.current.value = '';
            setFile(null);
        } catch (e) {
            toast.error(e instanceof Error ? e.message : t('backup.import.failed'));
        }
    };

    const onExport = async (kind: 'json' | 'logs') => {
        setExportingKind(kind);
        try {
            await exportDB.mutateAsync(kind === 'logs'
                ? { include_logs: true, include_stats: includeStats, format: 'zip' }
                : { include_logs: false, include_stats: includeStats, format: 'json' });
            toast.success(t('backup.export.success'));
        } catch (e) {
            toast.error(e instanceof Error ? e.message : t('backup.export.failed'));
        } finally {
            setExportingKind(null);
        }
    };

    return (
        <SettingCard icon={Database} title={t('data.title')}>
            {/* 统计保存周期 */}
            <SettingRow icon={Clock} label={t('statsSaveInterval.label')}>
                <Input
                    type="number"
                    value={statsInterval.value}
                    onChange={(e) => statsInterval.setValue(e.target.value)}
                    onBlur={statsInterval.save}
                    placeholder={t('statsSaveInterval.placeholder')}
                    className="w-48 rounded-xl"
                />
            </SettingRow>

            {/* 历史日志 */}
            <SettingSection title={t('log.title')} />
            <SettingRow icon={ScrollText} label={t('log.enabled.label')}>
                <Switch checked={logEnabled.enabled} onCheckedChange={logEnabled.toggle} />
            </SettingRow>
            <SettingRow icon={Calendar} label={t('log.keepPeriod.label')}>
                <Input
                    type="number"
                    value={keepPeriod.value}
                    onChange={(e) => keepPeriod.setValue(e.target.value)}
                    onBlur={keepPeriod.save}
                    placeholder={t('log.keepPeriod.placeholder')}
                    className="w-48 rounded-xl"
                    disabled={!logEnabled.enabled}
                />
            </SettingRow>
            <SettingRow icon={Trash2} label={t('log.clear.label')}>
                <Button
                    variant="destructive"
                    size="sm"
                    onClick={handleClearLogs}
                    disabled={clearLogs.isPending}
                    className="rounded-xl"
                >
                    {clearLogs.isPending ? t('log.clear.clearing') : t('log.clear.button')}
                </Button>
            </SettingRow>

            {/* 备份导出 */}
            <SettingSection title={t('backup.export.title')} />
            <div className="space-y-3">
                <div className="flex items-center justify-between gap-4">
                    <div className="text-sm text-muted-foreground">{t('backup.export.includeStats')}</div>
                    <Switch checked={includeStats} onCheckedChange={setIncludeStats} />
                </div>

                <Button
                    type="button"
                    variant="outline"
                    className="w-full rounded-xl"
                    onClick={() => onExport('json')}
                    disabled={exportDB.isPending}
                >
                    <Download className="size-4" />
                    {exportingKind === 'json' ? t('backup.export.exporting') : t('backup.export.button')}
                </Button>

                {/* 含日志归档：数据量大，ZIP 流式写入，仅供留存，无法导入恢复 */}
                <Button
                    type="button"
                    variant="outline"
                    className="w-full rounded-xl"
                    onClick={() => onExport('logs')}
                    disabled={exportDB.isPending}
                >
                    <FileArchive className="size-4" />
                    {exportingKind === 'logs' ? t('backup.export.exporting') : t('backup.export.withLogsButton')}
                </Button>
                <p className="flex items-start gap-1.5 text-xs text-muted-foreground">
                    <AlertTriangle className="mt-0.5 size-3.5 shrink-0 text-destructive" />
                    {t('backup.export.withLogsWarning')}
                </p>
            </div>

            {/* 备份导入 */}
            <SettingSection title={t('backup.import.title')} />
            <div className="space-y-3">
                <Input
                    ref={fileInputRef}
                    type="file"
                    accept="application/json,.json"
                    onChange={(e) => setFile(e.target.files?.[0] ?? null)}
                    className="rounded-xl"
                />

                <Button
                    type="button"
                    variant="destructive"
                    className="w-full rounded-xl"
                    onClick={onImport}
                    disabled={importDB.isPending}
                >
                    <Upload className="size-4" />
                    {importDB.isPending ? t('backup.import.importing') : t('backup.import.button')}
                </Button>

                {rowsAffectedList.length > 0 && (
                    <div className="mt-2 space-y-1">
                        <div className="text-xs font-semibold text-card-foreground">{t('backup.import.result')}</div>
                        <div className="grid grid-cols-2 gap-1 text-xs text-muted-foreground">
                            {rowsAffectedList.map((it) => (
                                <div key={it.table} className="flex justify-between gap-2">
                                    <span className="truncate">{it.table}</span>
                                    <span className="tabular-nums">{it.count}</span>
                                </div>
                            ))}
                        </div>
                    </div>
                )}
            </div>
        </SettingCard>
    );
}
