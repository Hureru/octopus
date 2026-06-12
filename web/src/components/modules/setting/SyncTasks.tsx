'use client';

import { useTranslations } from 'next-intl';
import { CalendarCheck2, CalendarSync, DollarSign, Globe2, RefreshCw, type LucideIcon } from 'lucide-react';
import { Input } from '@/components/ui/input';
import { Button } from '@/components/ui/button';
import { SettingKey } from '@/api/endpoints/setting';
import { useLastSyncTime, useSyncChannel } from '@/api/endpoints/channel';
import { useLastUpdateTime, useUpdateModelPrice } from '@/api/endpoints/model';
import { useCheckinAllSites, useSiteLastCheckinTime, useSiteLastSyncTime, useSyncAllSites } from '@/api/endpoints/site';
import { toast } from '@/components/common/Toast';
import { useSettingStore } from '@/stores/setting';
import { translateSiteMessage } from '@/components/modules/site/site-message';
import { SettingCard, useSettingField } from './shared';

function getErrorMessage(error: unknown, fallback: string) {
    if (error instanceof Error && error.message.trim()) {
        return error.message;
    }
    if (error && typeof error === 'object' && 'message' in error) {
        const message = (error as { message?: unknown }).message;
        if (typeof message === 'string' && message.trim()) {
            return message;
        }
    }
    return fallback;
}

// 每行一个定时任务：自动执行间隔（小时）+ 手动触发，可选展示上次执行时间
function TaskRow({ icon: Icon, label, settingKey, last, running, runLabel, pendingLabel, onRun }: {
    icon: LucideIcon;
    label: string;
    settingKey: string;
    last?: string;
    running: boolean;
    runLabel: string;
    pendingLabel: string;
    onRun: () => void;
}) {
    const t = useTranslations('setting');
    const field = useSettingField(settingKey);

    return (
        <div className="flex items-center justify-between gap-4">
            <div className="flex min-w-0 flex-col gap-1">
                <div className="flex items-center gap-3">
                    <Icon className="h-5 w-5 shrink-0 text-muted-foreground" />
                    <span className="text-sm font-medium">{label}</span>
                </div>
                {last !== undefined && (
                    <span className="ml-8 text-xs text-muted-foreground">
                        {t('syncTasks.last')}: {last}
                    </span>
                )}
            </div>
            <div className="flex shrink-0 items-center gap-2">
                <Input
                    type="number"
                    min="0"
                    value={field.value}
                    onChange={(e) => field.setValue(e.target.value)}
                    onBlur={field.save}
                    placeholder={t('syncTasks.intervalPlaceholder')}
                    className="w-28 rounded-xl"
                />
                <Button variant="outline" size="sm" onClick={onRun} disabled={running} className="rounded-xl">
                    {running ? pendingLabel : runLabel}
                </Button>
            </div>
        </div>
    );
}

export function SettingSyncTasks() {
    const t = useTranslations('setting');
    const tAll = useTranslations();
    const locale = useSettingStore((state) => state.locale);

    const syncChannel = useSyncChannel();
    const { data: lastSyncTime } = useLastSyncTime();
    const updatePrice = useUpdateModelPrice();
    const { data: lastUpdateTime } = useLastUpdateTime();
    const syncAllSites = useSyncAllSites();
    const checkinAllSites = useCheckinAllSites();
    const { data: lastSiteSyncTime } = useSiteLastSyncTime();
    const { data: lastSiteCheckinTime } = useSiteLastCheckinTime();

    const formatTime = (timeStr: string | undefined) => {
        if (!timeStr) return t('syncTasks.never');
        const date = new Date(timeStr);
        if (Number.isNaN(date.getTime())) return t('syncTasks.never');
        if (date.getFullYear() === 1) return t('syncTasks.never');
        return date.toLocaleString();
    };

    return (
        <SettingCard icon={CalendarSync} title={t('syncTasks.title')}>
            {/* 渠道同步 */}
            <TaskRow
                icon={RefreshCw}
                label={t('syncTasks.llmSync.label')}
                settingKey={SettingKey.SyncLLMInterval}
                last={formatTime(lastSyncTime)}
                running={syncChannel.isPending}
                runLabel={t('syncTasks.llmSync.button')}
                pendingLabel={t('syncTasks.llmSync.pending')}
                onRun={() => syncChannel.mutate(undefined, {
                    onSuccess: () => toast.success(t('syncTasks.llmSync.success')),
                    onError: () => toast.error(t('syncTasks.llmSync.failed')),
                })}
            />

            {/* 模型价格更新 */}
            <TaskRow
                icon={DollarSign}
                label={t('syncTasks.llmPrice.label')}
                settingKey={SettingKey.ModelInfoUpdateInterval}
                last={formatTime(lastUpdateTime)}
                running={updatePrice.isPending}
                runLabel={t('syncTasks.llmPrice.button')}
                pendingLabel={t('syncTasks.llmPrice.pending')}
                onRun={() => updatePrice.mutate(undefined, {
                    onSuccess: () => toast.success(t('syncTasks.llmPrice.success')),
                    onError: () => toast.error(t('syncTasks.llmPrice.failed')),
                })}
            />

            {/* 站点全量同步 */}
            <TaskRow
                icon={Globe2}
                label={t('syncTasks.siteSync.label')}
                settingKey={SettingKey.SiteSyncInterval}
                last={formatTime(lastSiteSyncTime)}
                running={syncAllSites.isPending}
                runLabel={t('syncTasks.siteSync.button')}
                pendingLabel={t('syncTasks.siteSync.pending')}
                onRun={() => syncAllSites.mutate(undefined, {
                    onSuccess: () => toast.success(t('syncTasks.siteSync.success')),
                    onError: (error) => toast.error(translateSiteMessage(locale, getErrorMessage(error, t('syncTasks.siteSync.failed')), tAll)),
                })}
            />

            {/* 站点全量签到 */}
            <TaskRow
                icon={CalendarCheck2}
                label={t('syncTasks.siteCheckin.label')}
                settingKey={SettingKey.SiteCheckinInterval}
                last={formatTime(lastSiteCheckinTime)}
                running={checkinAllSites.isPending}
                runLabel={t('syncTasks.siteCheckin.button')}
                pendingLabel={t('syncTasks.siteCheckin.pending')}
                onRun={() => checkinAllSites.mutate(undefined, {
                    onSuccess: () => toast.success(t('syncTasks.siteCheckin.success')),
                    onError: (error) => toast.error(translateSiteMessage(locale, getErrorMessage(error, t('syncTasks.siteCheckin.failed')), tAll)),
                })}
            />
        </SettingCard>
    );
}
