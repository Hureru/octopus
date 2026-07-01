'use client';

import { useMemo, useState } from 'react';
import { useTranslations } from 'next-intl';
import { CloudUpload, Download, Eye, EyeOff, FolderSync, Key, Link, RefreshCw, User } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Switch } from '@/components/ui/switch';
import { toast } from '@/components/common/Toast';
import {
    SettingKey,
    useTestWebDAV,
    useTriggerWebDAVBackup,
    useWebDAVBackupList,
    useRestoreWebDAVBackup,
    useSettingValue,
} from '@/api/endpoints/setting';
import { SettingCard, SettingRow, SettingSection, useSettingField, useSettingToggle } from './shared';

function formatBytes(bytes: number): string {
    if (bytes < 1024) return `${bytes} B`;
    if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
    return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}

export function SettingWebDAVBackup() {
    const t = useTranslations('setting.webdavBackup');

    const url = useSettingField(SettingKey.WebDAVURL);
    const username = useSettingField(SettingKey.WebDAVUsername);
    const password = useSettingField(SettingKey.WebDAVPassword);
    const backupPath = useSettingField(SettingKey.WebDAVBackupPath);
    const interval = useSettingField(SettingKey.WebDAVBackupInterval);
    const retentionCount = useSettingField(SettingKey.WebDAVRetentionCount);
    const includeStats = useSettingToggle(SettingKey.WebDAVIncludeStats);

    const [showPassword, setShowPassword] = useState(false);

    const testWebDAV = useTestWebDAV();
    const triggerBackup = useTriggerWebDAVBackup();
    const restoreBackup = useRestoreWebDAVBackup();

    const { value: webdavUrl } = useSettingValue(SettingKey.WebDAVURL);
    const isConfigured = !!webdavUrl;

    const backupList = useWebDAVBackupList(isConfigured);

    const [restoringFile, setRestoringFile] = useState<string | null>(null);

    const backups = useMemo(() => {
        if (backupList.isPending || backupList.isError) return null;
        return backupList.data ?? [];
    }, [backupList.data, backupList.isPending, backupList.isError]);

    const handleTest = async () => {
        try {
            await testWebDAV.mutateAsync();
            toast.success(t('testSuccess'));
        } catch (e) {
            toast.error(t('testFailed'), { description: e instanceof Error ? e.message : undefined });
        }
    };

    const handleTrigger = async () => {
        try {
            await triggerBackup.mutateAsync();
            toast.success(t('triggerSuccess'));
        } catch (e) {
            toast.error(t('triggerFailed'), { description: e instanceof Error ? e.message : undefined });
        }
    };

    const handleRestore = async (filename: string) => {
        if (!confirm(t('restoreConfirm'))) return;
        setRestoringFile(filename);
        try {
            await restoreBackup.mutateAsync(filename);
            toast.success(t('restoreSuccess'));
            window.location.reload();
        } catch (e) {
            toast.error(t('restoreFailed'), { description: e instanceof Error ? e.message : undefined });
            setRestoringFile(null);
        }
    };

    return (
        <SettingCard icon={CloudUpload} title={t('title')}>
            {/* WebDAV Configuration */}
            <SettingRow icon={Link} label={t('url.label')}>
                <Input
                    value={url.value}
                    onChange={(e) => url.setValue(e.target.value)}
                    onBlur={url.save}
                    placeholder={t('url.placeholder')}
                    className="w-64 rounded-xl"
                />
            </SettingRow>

            <SettingRow icon={User} label={t('username.label')}>
                <Input
                    value={username.value}
                    onChange={(e) => username.setValue(e.target.value)}
                    onBlur={username.save}
                    placeholder={t('username.placeholder')}
                    className="w-64 rounded-xl"
                />
            </SettingRow>

            <SettingRow icon={Key} label={t('password.label')}>
                <div className="relative w-64">
                    <Input
                        type={showPassword ? 'text' : 'password'}
                        value={password.value}
                        onChange={(e) => password.setValue(e.target.value)}
                        onBlur={password.save}
                        placeholder={t('password.placeholder')}
                        className="rounded-xl pr-10"
                    />
                    <button
                        type="button"
                        onClick={() => setShowPassword(!showPassword)}
                        className="absolute right-2 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground"
                    >
                        {showPassword ? <EyeOff className="size-4" /> : <Eye className="size-4" />}
                    </button>
                </div>
            </SettingRow>

            <SettingRow icon={FolderSync} label={t('backupPath.label')}>
                <Input
                    value={backupPath.value}
                    onChange={(e) => backupPath.setValue(e.target.value)}
                    onBlur={backupPath.save}
                    placeholder={t('backupPath.placeholder')}
                    className="w-64 rounded-xl"
                />
            </SettingRow>

            <SettingSection title={t('interval.label')} />
            <div className="space-y-3">
                <Input
                    type="number"
                    value={interval.value}
                    onChange={(e) => interval.setValue(e.target.value)}
                    onBlur={interval.save}
                    placeholder={t('interval.placeholder')}
                    className="w-48 rounded-xl"
                    min={0}
                />
                <p className="text-xs text-muted-foreground">{t('interval.description')}</p>
            </div>

            <SettingRow label={t('retentionCount.label')}>
                <Input
                    type="number"
                    value={retentionCount.value}
                    onChange={(e) => retentionCount.setValue(e.target.value)}
                    onBlur={retentionCount.save}
                    placeholder={t('retentionCount.placeholder')}
                    className="w-48 rounded-xl"
                    min={1}
                />
            </SettingRow>

            <SettingRow label={t('includeStats')}>
                <Switch checked={includeStats.enabled} onCheckedChange={includeStats.toggle} />
            </SettingRow>

            {/* Actions */}
            <SettingSection title="" />
            <div className="flex gap-2">
                <Button
                    variant="outline"
                    size="sm"
                    className="rounded-xl"
                    onClick={handleTest}
                    disabled={testWebDAV.isPending || !isConfigured}
                >
                    {testWebDAV.isPending ? t('testing') : t('testConnection')}
                </Button>
                <Button
                    variant="outline"
                    size="sm"
                    className="rounded-xl"
                    onClick={handleTrigger}
                    disabled={triggerBackup.isPending || !isConfigured}
                >
                    <CloudUpload className="size-4" />
                    {triggerBackup.isPending ? t('triggering') : t('triggerBackup')}
                </Button>
            </div>

            {/* Backup List */}
            {isConfigured && (
                <>
                    <SettingSection title={t('backupList')} />
                    <div className="space-y-2">
                        <div className="flex justify-end">
                            <Button
                                variant="ghost"
                                size="sm"
                                onClick={() => backupList.refetch()}
                                disabled={backupList.isFetching}
                                className="rounded-xl"
                            >
                                <RefreshCw className={`size-4 ${backupList.isFetching ? 'animate-spin' : ''}`} />
                                {t('refresh')}
                            </Button>
                        </div>
                        {backupList.isPending ? (
                            <p className="text-sm text-muted-foreground">{t('loading')}</p>
                        ) : backupList.isError ? (
                            <p className="text-sm text-red-500">{t('loadError')}</p>
                        ) : backups && backups.length === 0 ? (
                            <p className="text-sm text-muted-foreground">{t('noBackups')}</p>
                        ) : backups ? (
                            <div className="space-y-1">
                                {backups.map((backup) => (
                                    <div
                                        key={backup.name}
                                        className="flex items-center justify-between gap-2 rounded-xl border border-border p-2.5 text-sm"
                                    >
                                        <div className="min-w-0 flex-1">
                                            <div className="truncate font-mono text-xs">{backup.name}</div>
                                            <div className="text-xs text-muted-foreground">
                                                {formatBytes(backup.size)} &middot; {new Date(backup.modified_at).toLocaleString()}
                                            </div>
                                        </div>
                                        <Button
                                            variant="outline"
                                            size="sm"
                                            className="shrink-0 rounded-xl"
                                            onClick={() => handleRestore(backup.name)}
                                            disabled={restoringFile !== null}
                                        >
                                            <Download className="size-3.5" />
                                            {restoringFile === backup.name ? t('restoring') : t('restore')}
                                        </Button>
                                    </div>
                                ))}
                            </div>
                        ) : null}
                    </div>
                </>
            )}
        </SettingCard>
    );
}
