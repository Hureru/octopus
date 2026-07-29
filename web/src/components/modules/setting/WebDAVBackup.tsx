'use client';

import { useMemo, useRef, useState } from 'react';
import { useTranslations } from 'next-intl';
import { CloudUpload, Download, Eye, EyeOff, FolderSync, Key, Link, RefreshCw, User } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Switch } from '@/components/ui/switch';
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
import { toast } from '@/components/common/Toast';
import {
    type DBImportResult,
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
    const [restoreCandidate, setRestoreCandidate] = useState<string | null>(null);
    const [restoreResult, setRestoreResult] = useState<DBImportResult | null>(null);
    const restoreTriggerRef = useRef<HTMLButtonElement | null>(null);

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

    const restoreRows = useMemo(() => {
        if (!restoreResult) return [];
        return Object.entries(restoreResult.rows_affected)
            .filter(([, count]) => count > 0)
            .sort(([left], [right]) => left.localeCompare(right));
    }, [restoreResult]);

    const handleRestore = async () => {
        const filename = restoreCandidate;
        if (!filename) return;
        setRestoringFile(filename);
        try {
            const result = await restoreBackup.mutateAsync(filename);
            setRestoreResult(result);
            setRestoreCandidate(null);
            toast.success(t('restoreSuccess'));
        } catch (e) {
            toast.error(t('restoreFailed'), { description: e instanceof Error ? e.message : undefined });
        } finally {
            setRestoringFile(null);
        }
    };

    return (
        <SettingCard icon={CloudUpload} title={t('title')}>
            {/* WebDAV Configuration */}
            <SettingRow icon={Link} label={t('url.label')} htmlFor="webdav-url">
                <Input
                    id="webdav-url"
                    value={url.value}
                    onChange={(e) => url.setValue(e.target.value)}
                    onBlur={url.save}
                    placeholder={t('url.placeholder')}
                    className="w-64 rounded-xl"
                />
            </SettingRow>

            <SettingRow icon={User} label={t('username.label')} htmlFor="webdav-username">
                <Input
                    id="webdav-username"
                    value={username.value}
                    onChange={(e) => username.setValue(e.target.value)}
                    onBlur={username.save}
                    placeholder={t('username.placeholder')}
                    className="w-64 rounded-xl"
                />
            </SettingRow>

            <SettingRow icon={Key} label={t('password.label')} htmlFor="webdav-password">
                <div className="relative w-64">
                    <Input
                        id="webdav-password"
                        type={showPassword ? 'text' : 'password'}
                        value={password.value}
                        onChange={(e) => password.setValue(e.target.value)}
                        onBlur={password.save}
                        placeholder={t('password.placeholder')}
                        className="rounded-xl pr-10"
                    />
                    <button
                        type="button"
                        aria-label={showPassword ? t('password.hide') : t('password.show')}
                        aria-pressed={showPassword}
                        title={showPassword ? t('password.hide') : t('password.show')}
                        onClick={() => setShowPassword(!showPassword)}
                        className="absolute right-2 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground"
                    >
                        {showPassword ? <EyeOff className="size-4" /> : <Eye className="size-4" />}
                    </button>
                </div>
            </SettingRow>

            <SettingRow icon={FolderSync} label={t('backupPath.label')} htmlFor="webdav-backup-path">
                <Input
                    id="webdav-backup-path"
                    value={backupPath.value}
                    onChange={(e) => backupPath.setValue(e.target.value)}
                    onBlur={backupPath.save}
                    placeholder={t('backupPath.placeholder')}
                    className="w-64 rounded-xl"
                />
            </SettingRow>

			<SettingRow label={t('interval.label')} htmlFor="webdav-backup-interval">
				<div className="space-y-2">
					<Input
						id="webdav-backup-interval"
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
			</SettingRow>

            <SettingRow label={t('retentionCount.label')} htmlFor="webdav-retention-count">
                <Input
                    id="webdav-retention-count"
                    type="number"
                    value={retentionCount.value}
                    onChange={(e) => retentionCount.setValue(e.target.value)}
                    onBlur={retentionCount.save}
                    placeholder={t('retentionCount.placeholder')}
                    className="w-48 rounded-xl"
                    min={1}
                />
            </SettingRow>

            <SettingRow label={t('includeStats')} htmlFor="webdav-include-stats">
                <Switch
                    id="webdav-include-stats"
                    checked={includeStats.enabled}
                    onCheckedChange={includeStats.toggle}
                />
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
                            <div className="flex items-center justify-between gap-3 text-sm text-red-500">
                                <span>
                                    {t('loadError')}: {backupList.error instanceof Error ? backupList.error.message : t('unknownError')}
                                </span>
                                <Button variant="outline" size="sm" onClick={() => backupList.refetch()}>
                                    <RefreshCw className="size-4" />
                                    {t('retry')}
                                </Button>
                            </div>
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
                                            onClick={(event) => {
                                                restoreTriggerRef.current = event.currentTarget;
                                                setRestoreResult(null);
                                                setRestoreCandidate(backup.name);
                                            }}
                                            disabled={restoringFile !== null}
                                        >
                                            <Download className="size-3.5" />
                                            {restoringFile === backup.name ? t('restoring') : t('restore')}
                                        </Button>
                                    </div>
                                ))}
                            </div>
                        ) : null}

                        {restoreResult ? (
                            <div className="space-y-3 border-t border-border pt-4" role="status">
                                <div className="flex items-center justify-between gap-3">
                                    <h3 className="text-sm font-semibold">{t('restoreResult')}</h3>
                                    <Button variant="outline" size="sm" onClick={() => window.location.reload()}>
                                        {t('reload')}
                                    </Button>
                                </div>
                                {restoreRows.length > 0 ? (
                                    <div className="grid grid-cols-2 gap-x-4 gap-y-1 text-xs text-muted-foreground sm:grid-cols-3">
                                        {restoreRows.map(([table, count]) => (
                                            <div key={table} className="flex justify-between gap-2">
                                                <span className="truncate">{table}</span>
                                                <span className="tabular-nums">{count}</span>
                                            </div>
                                        ))}
                                    </div>
                                ) : (
                                    <p className="text-xs text-muted-foreground">{t('restoreNoChanges')}</p>
                                )}
                                {restoreResult.warnings?.map((warning) => (
                                    <p key={`${warning.code}:${warning.resource_type}:${warning.resource_name}`} className="text-xs text-amber-700 dark:text-amber-300">
                                        {warning.resource_name}: {warning.message}
                                    </p>
                                ))}
                            </div>
                        ) : null}
                    </div>
                </>
            )}

            <AlertDialog
                open={restoreCandidate !== null}
                onOpenChange={(open) => {
                    if (!open && !restoreBackup.isPending) setRestoreCandidate(null);
                }}
            >
                <AlertDialogContent
                    onCloseAutoFocus={(event) => {
                        const target = restoreTriggerRef.current;
                        restoreTriggerRef.current = null;
                        if (!target?.isConnected) return;
                        event.preventDefault();
                        target.focus();
                    }}
                >
                    <AlertDialogHeader>
                        <AlertDialogTitle>{t('restore')}</AlertDialogTitle>
                        <AlertDialogDescription>
                            {t('restoreConfirm')}
                            {restoreCandidate ? ` ${restoreCandidate}` : ''}
                        </AlertDialogDescription>
                    </AlertDialogHeader>
                    <AlertDialogFooter>
                        <AlertDialogCancel disabled={restoreBackup.isPending}>{t('cancel')}</AlertDialogCancel>
                        <AlertDialogAction
                            disabled={restoreBackup.isPending}
                            onClick={(event) => {
                                event.preventDefault();
                                void handleRestore();
                            }}
                        >
                            {restoreBackup.isPending ? t('restoring') : t('restore')}
                        </AlertDialogAction>
                    </AlertDialogFooter>
                </AlertDialogContent>
            </AlertDialog>
        </SettingCard>
    );
}
