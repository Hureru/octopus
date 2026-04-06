'use client';

import { useEffect, useRef, useState } from 'react';
import { useTranslations } from 'next-intl';
import { CalendarCheck2, Clock3, Globe2, RefreshCw } from 'lucide-react';
import { Input } from '@/components/ui/input';
import { Button } from '@/components/ui/button';
import { useSettingList, useSetSetting, SettingKey } from '@/api/endpoints/setting';
import { useCheckinAllSites, useSyncAllSites } from '@/api/endpoints/site';
import { toast } from '@/components/common/Toast';
import { useSettingStore } from '@/stores/setting';
import { translateSiteMessage } from '@/components/modules/site/site-message';

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

export function SettingSiteAutomation() {
    const t = useTranslations();
    const locale = useSettingStore((state) => state.locale);
    const { data: settings } = useSettingList();
    const setSetting = useSetSetting();
    const syncAllSites = useSyncAllSites();
    const checkinAllSites = useCheckinAllSites();

    const [syncInterval, setSyncInterval] = useState('');
    const [checkinInterval, setCheckinInterval] = useState('');
    const initialSyncInterval = useRef('');
    const initialCheckinInterval = useRef('');

    useEffect(() => {
        if (!settings) return;

        const siteSync = settings.find((item) => item.key === SettingKey.SiteSyncInterval);
        const siteCheckin = settings.find((item) => item.key === SettingKey.SiteCheckinInterval);

        if (siteSync) {
            queueMicrotask(() => setSyncInterval(siteSync.value));
            initialSyncInterval.current = siteSync.value;
        }
        if (siteCheckin) {
            queueMicrotask(() => setCheckinInterval(siteCheckin.value));
            initialCheckinInterval.current = siteCheckin.value;
        }
    }, [settings]);

    function handleSave(key: string, value: string, initialValue: string, onSaved: (next: string) => void) {
        if (value === initialValue) return;

        setSetting.mutate({ key, value }, {
            onSuccess: () => {
                onSaved(value);
                toast.success('已保存');
            },
            onError: (error) => {
                toast.error(translateSiteMessage(locale, getErrorMessage(error, '保存失败'), t));
            },
        });
    }

    function handleManualSync() {
        syncAllSites.mutate(undefined, {
            onSuccess: () => {
                toast.success('已触发后台站点全量同步');
            },
            onError: (error) => {
                toast.error(translateSiteMessage(locale, getErrorMessage(error, '触发同步失败'), t));
            },
        });
    }

    function handleManualCheckin() {
        checkinAllSites.mutate(undefined, {
            onSuccess: () => {
                toast.success('已触发后台站点全量签到');
            },
            onError: (error) => {
                toast.error(translateSiteMessage(locale, getErrorMessage(error, '触发签到失败'), t));
            },
        });
    }

    return (
        <div className="rounded-3xl border border-border bg-card p-6 space-y-5">
            <h2 className="text-lg font-bold text-card-foreground flex items-center gap-2">
                <Globe2 className="h-5 w-5" />
                站点自动化
            </h2>

            <div className="flex items-center justify-between gap-4">
                <div className="flex items-center gap-3">
                    <Clock3 className="h-5 w-5 text-muted-foreground" />
                    <span className="text-sm font-medium">自动同步间隔（小时）</span>
                </div>
                <Input
                    type="number"
                    value={syncInterval}
                    onChange={(event) => setSyncInterval(event.target.value)}
                    onBlur={() => handleSave(SettingKey.SiteSyncInterval, syncInterval, initialSyncInterval.current, (next) => {
                        initialSyncInterval.current = next;
                    })}
                    placeholder="请输入间隔（小时）"
                    className="w-48 rounded-xl"
                />
            </div>

            <div className="flex items-center justify-between gap-4">
                <div className="flex items-center gap-3">
                    <Clock3 className="h-5 w-5 text-muted-foreground" />
                    <span className="text-sm font-medium">自动签到间隔（小时）</span>
                </div>
                <Input
                    type="number"
                    value={checkinInterval}
                    onChange={(event) => setCheckinInterval(event.target.value)}
                    onBlur={() => handleSave(SettingKey.SiteCheckinInterval, checkinInterval, initialCheckinInterval.current, (next) => {
                        initialCheckinInterval.current = next;
                    })}
                    placeholder="请输入间隔（小时）"
                    className="w-48 rounded-xl"
                />
            </div>

            <div className="flex items-center justify-between gap-4">
                <div className="flex items-center gap-3">
                    <RefreshCw className="h-5 w-5 text-muted-foreground" />
                    <span className="text-sm font-medium">手动站点全量同步</span>
                </div>
                <Button variant="outline" size="sm" onClick={handleManualSync} disabled={syncAllSites.isPending} className="rounded-xl">
                    {syncAllSites.isPending ? '同步中...' : '立即同步'}
                </Button>
            </div>

            <div className="flex items-center justify-between gap-4">
                <div className="flex items-center gap-3">
                    <CalendarCheck2 className="h-5 w-5 text-muted-foreground" />
                    <span className="text-sm font-medium">手动站点全量签到</span>
                </div>
                <Button variant="outline" size="sm" onClick={handleManualCheckin} disabled={checkinAllSites.isPending} className="rounded-xl">
                    {checkinAllSites.isPending ? '签到中...' : '立即签到'}
                </Button>
            </div>
        </div>
    );
}
