'use client';

import { useEffect, useMemo, useRef, useState } from 'react';
import { useTranslations } from 'next-intl';
import { Monitor, Globe, Clock, Shield, HelpCircle, X, Activity, HeartPulse, Radio, Link } from 'lucide-react';
import { Input } from '@/components/ui/input';
import { Switch } from '@/components/ui/switch';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover';
import { useSettingList, useSetSetting, SettingKey } from '@/api/endpoints/setting';
import { toast } from '@/components/common/Toast';
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '@/components/animate-ui/components/animate/tooltip';
import type { ApiError } from '@/api/types';

export function SettingSystem() {
    const t = useTranslations('setting');
    const { data: settings } = useSettingList();
    const setSetting = useSetSetting();

    const [proxyUrl, setProxyUrl] = useState('');
    const [apiBaseUrl, setApiBaseUrl] = useState('');
    const [statsSaveInterval, setStatsSaveInterval] = useState('');
    const [corsAllowOrigins, setCorsAllowOrigins] = useState('');
    const [corsInputValue, setCorsInputValue] = useState('');
    const [responsesWSEnabled, setResponsesWSEnabled] = useState(false);
    const [responsesWSDefaultMode, setResponsesWSDefaultMode] = useState('passthrough');
    const [groupHealthEnabled, setGroupHealthEnabled] = useState(false);
    const [sseHeartbeatInterval, setSseHeartbeatInterval] = useState('');
    const [ssePreStreamHeartbeatDelay, setSsePreStreamHeartbeatDelay] = useState('');

    const initialProxyUrl = useRef('');
    const initialApiBaseUrl = useRef('');
    const initialStatsSaveInterval = useRef('');
    const initialCorsAllowOrigins = useRef('');
    const initialResponsesWSEnabled = useRef(false);
    const initialResponsesWSDefaultMode = useRef('passthrough');
    const initialGroupHealthEnabled = useRef(false);
    const initialSseHeartbeatInterval = useRef('');
    const initialSsePreStreamHeartbeatDelay = useRef('');

    useEffect(() => {
        if (settings) {
            const proxy = settings.find(s => s.key === SettingKey.ProxyURL);
            const apiBase = settings.find(s => s.key === SettingKey.ApiBaseUrl);
            const interval = settings.find(s => s.key === SettingKey.StatsSaveInterval);
            const cors = settings.find(s => s.key === SettingKey.CORSAllowOrigins);
            const responsesWS = settings.find(s => s.key === SettingKey.ResponsesWSEnabled);
            const responsesWSMode = settings.find(s => s.key === SettingKey.ResponsesWSDefaultMode);
            const groupHealth = settings.find(s => s.key === SettingKey.GroupHealthEnabled);
            const sseHeartbeat = settings.find(s => s.key === SettingKey.SSEHeartbeatInterval);
            const ssePreStreamHeartbeat = settings.find(s => s.key === SettingKey.SSEPreStreamHeartbeatDelay);
            if (proxy) {
                queueMicrotask(() => setProxyUrl(proxy.value));
                initialProxyUrl.current = proxy.value;
            }
            if (apiBase) {
                queueMicrotask(() => setApiBaseUrl(apiBase.value));
                initialApiBaseUrl.current = apiBase.value;
            }
            if (interval) {
                queueMicrotask(() => setStatsSaveInterval(interval.value));
                initialStatsSaveInterval.current = interval.value;
            }
            if (cors) {
                queueMicrotask(() => setCorsAllowOrigins(cors.value));
                initialCorsAllowOrigins.current = cors.value;
            }
            if (responsesWS) {
                const isEnabled = responsesWS.value === 'true';
                queueMicrotask(() => setResponsesWSEnabled(isEnabled));
                initialResponsesWSEnabled.current = isEnabled;
            }
            if (responsesWSMode) {
                queueMicrotask(() => setResponsesWSDefaultMode(responsesWSMode.value || 'passthrough'));
                initialResponsesWSDefaultMode.current = responsesWSMode.value || 'passthrough';
            }
            if (groupHealth) {
                const isEnabled = groupHealth.value === 'true';
                queueMicrotask(() => setGroupHealthEnabled(isEnabled));
                initialGroupHealthEnabled.current = isEnabled;
            }
            if (sseHeartbeat) {
                queueMicrotask(() => setSseHeartbeatInterval(sseHeartbeat.value));
                initialSseHeartbeatInterval.current = sseHeartbeat.value;
            }
            if (ssePreStreamHeartbeat) {
                queueMicrotask(() => setSsePreStreamHeartbeatDelay(ssePreStreamHeartbeat.value));
                initialSsePreStreamHeartbeatDelay.current = ssePreStreamHeartbeat.value;
            }
        }
    }, [settings]);

    const handleSave = (key: string, value: string, initialValue: string) => {
        if (value === initialValue) return;

        setSetting.mutate({ key, value }, {
            onSuccess: () => {
                toast.success(t('saved'));
                if (key === SettingKey.ProxyURL) {
                    initialProxyUrl.current = value;
                } else if (key === SettingKey.ApiBaseUrl) {
                    initialApiBaseUrl.current = value;
                } else if (key === SettingKey.StatsSaveInterval) {
                    initialStatsSaveInterval.current = value;
                } else if (key === SettingKey.CORSAllowOrigins) {
                    initialCorsAllowOrigins.current = value;
                } else if (key === SettingKey.ResponsesWSEnabled) {
                    initialResponsesWSEnabled.current = value === 'true';
                } else if (key === SettingKey.ResponsesWSDefaultMode) {
                    initialResponsesWSDefaultMode.current = value;
                } else if (key === SettingKey.GroupHealthEnabled) {
                    initialGroupHealthEnabled.current = value === 'true';
                } else if (key === SettingKey.SSEHeartbeatInterval) {
                    initialSseHeartbeatInterval.current = value;
                } else if (key === SettingKey.SSEPreStreamHeartbeatDelay) {
                    initialSsePreStreamHeartbeatDelay.current = value;
                }
            },
            onError: (error) => {
                const msg = (error as unknown as ApiError)?.message;
                toast.error(t('saveFailed'), { description: msg });
                if (key === SettingKey.ProxyURL) {
                    setProxyUrl(initialValue);
                } else if (key === SettingKey.ApiBaseUrl) {
                    setApiBaseUrl(initialValue);
                } else if (key === SettingKey.StatsSaveInterval) {
                    setStatsSaveInterval(initialValue);
                } else if (key === SettingKey.CORSAllowOrigins) {
                    setCorsAllowOrigins(initialValue);
                } else if (key === SettingKey.SSEHeartbeatInterval) {
                    setSseHeartbeatInterval(initialValue);
                } else if (key === SettingKey.SSEPreStreamHeartbeatDelay) {
                    setSsePreStreamHeartbeatDelay(initialValue);
                }
            }
        });
    };

    const corsAllowOriginsList = useMemo(() => {
        const value = corsAllowOrigins.trim();
        if (!value) return [];
        if (value === '*') return ['*'];
        return Array.from(new Set(
            value
                .split(/[,\n，]/)
                .map(item => item.trim())
                .filter(Boolean)
        ));
    }, [corsAllowOrigins]);

    const corsAllowOriginsDisplay = useMemo(
        () => (corsAllowOriginsList.length > 0 ? corsAllowOriginsList.join(', ') : t('corsAllowOrigins.hint')),
        [corsAllowOriginsList, t]
    );

    const saveCorsAllowOrigins = (origins: string[]) => {
        const normalizedOrigins = Array.from(new Set(
            origins
                .map(origin => origin.trim())
                .filter(Boolean)
        ));
        const normalizedValue = normalizedOrigins.includes('*') ? '*' : normalizedOrigins.join(',');
        setCorsAllowOrigins(normalizedValue);
        handleSave(SettingKey.CORSAllowOrigins, normalizedValue, initialCorsAllowOrigins.current);
    };

    const handleAddCorsOrigin = () => {
        const newOrigins = Array.from(new Set(
            corsInputValue
                .split(/[,\n，]/)
                .map(item => item.trim())
                .filter(Boolean)
        ));
        if (newOrigins.length === 0) return;

        if (newOrigins.includes('*')) {
            saveCorsAllowOrigins(['*']);
            setCorsInputValue('');
            return;
        }

        const base = corsAllowOriginsList.includes('*') ? [] : corsAllowOriginsList;
        const merged = Array.from(new Set([...base, ...newOrigins]));
        saveCorsAllowOrigins(merged);
        setCorsInputValue('');
    };

    const handleRemoveCorsOrigin = (originToRemove: string) => {
        const nextOrigins = corsAllowOriginsList.filter(origin => origin !== originToRemove);
        saveCorsAllowOrigins(nextOrigins);
    };

    const handleResponsesWSChange = (checked: boolean) => {
        setResponsesWSEnabled(checked);
        setSetting.mutate(
            { key: SettingKey.ResponsesWSEnabled, value: checked ? 'true' : 'false' },
            {
                onSuccess: () => {
                    toast.success(t('saved'));
                    initialResponsesWSEnabled.current = checked;
                },
                onError: () => setResponsesWSEnabled(initialResponsesWSEnabled.current),
            }
        );
    };

    const handleResponsesWSModeChange = (value: string) => {
        setResponsesWSDefaultMode(value);
        setSetting.mutate(
            { key: SettingKey.ResponsesWSDefaultMode, value },
            {
                onSuccess: () => {
                    toast.success(t('saved'));
                    initialResponsesWSDefaultMode.current = value;
                },
                onError: () => setResponsesWSDefaultMode(initialResponsesWSDefaultMode.current),
            }
        );
    };

    const handleGroupHealthChange = (checked: boolean) => {
        setGroupHealthEnabled(checked);
        setSetting.mutate(
            { key: SettingKey.GroupHealthEnabled, value: checked ? 'true' : 'false' },
            {
                onSuccess: () => {
                    toast.success(t('saved'));
                    initialGroupHealthEnabled.current = checked;
                },
                onError: () => setGroupHealthEnabled(initialGroupHealthEnabled.current),
            }
        );
    };

    return (
        <div className="rounded-3xl border border-border bg-card p-6 space-y-5">
            <h2 className="text-lg font-bold text-card-foreground flex items-center gap-2">
                <Monitor className="h-5 w-5" />
                {t('system')}
            </h2>

            {/* 代理地址 */}
            <div className="flex items-center justify-between gap-4">
                <div className="flex items-center gap-3">
                    <Globe className="h-5 w-5 text-muted-foreground" />
                    <span className="text-sm font-medium">{t('proxyUrl.label')}</span>
                </div>
                <Input
                    value={proxyUrl}
                    onChange={(e) => setProxyUrl(e.target.value)}
                    onBlur={() => handleSave('proxy_url', proxyUrl, initialProxyUrl.current)}
                    placeholder={t('proxyUrl.placeholder')}
                    className="w-48 rounded-xl"
                />
            </div>

            {/* 对外服务基础地址 */}
            <div className="flex items-center justify-between gap-4">
                <div className="flex items-center gap-3">
                    <Link className="h-5 w-5 text-muted-foreground" />
                    <span className="text-sm font-medium">{t('apiBaseUrl.label')}</span>
                    <TooltipProvider>
                        <Tooltip>
                            <TooltipTrigger asChild>
                                <HelpCircle className="size-4 text-muted-foreground cursor-help" />
                            </TooltipTrigger>
                            <TooltipContent>
                                {t('apiBaseUrl.description')}
                            </TooltipContent>
                        </Tooltip>
                    </TooltipProvider>
                </div>
                <Input
                    value={apiBaseUrl}
                    onChange={(e) => setApiBaseUrl(e.target.value)}
                    onBlur={() => handleSave(SettingKey.ApiBaseUrl, apiBaseUrl, initialApiBaseUrl.current)}
                    placeholder={t('apiBaseUrl.placeholder')}
                    className="w-48 rounded-xl"
                />
            </div>

            {/* 统计保存周期 */}
            <div className="flex items-center justify-between gap-4">
                <div className="flex items-center gap-3">
                    <Clock className="h-5 w-5 text-muted-foreground" />
                    <span className="text-sm font-medium">{t('statsSaveInterval.label')}</span>
                </div>
                <Input
                    type="number"
                    value={statsSaveInterval}
                    onChange={(e) => setStatsSaveInterval(e.target.value)}
                    onBlur={() => handleSave('stats_save_interval', statsSaveInterval, initialStatsSaveInterval.current)}
                    placeholder={t('statsSaveInterval.placeholder')}
                    className="w-48 rounded-xl"
                />
            </div>

            {/* CORS 跨域白名单 */}
            <div className="flex items-center justify-between gap-4">
                <div className="flex items-center gap-3">
                    <Shield className="h-5 w-5 text-muted-foreground" />
                    <span className="text-sm font-medium">{t('corsAllowOrigins.label')}</span>
                    <TooltipProvider>
                        <Tooltip>
                            <TooltipTrigger asChild>
                                <HelpCircle className="size-4 text-muted-foreground cursor-help" />
                            </TooltipTrigger>
                            <TooltipContent>
                                {t('corsAllowOrigins.hint')}
                                <br />
                                {t('corsAllowOrigins.example')}
                            </TooltipContent>
                        </Tooltip>
                    </TooltipProvider>
                </div>
                <Popover>
                    <PopoverTrigger asChild>
                        <button
                            type="button"
                            className="border-input focus-visible:border-ring focus-visible:ring-ring/50 w-48 min-h-9 rounded-xl border bg-transparent px-3 py-2 text-left text-sm shadow-xs transition-[color,box-shadow] outline-none focus-visible:ring-[3px]"
                            title={corsAllowOriginsDisplay}
                        >
                            <span className={`block overflow-hidden text-ellipsis whitespace-nowrap ${corsAllowOriginsList.length === 0 ? 'text-muted-foreground' : ''}`}>
                                {corsAllowOriginsDisplay}
                            </span>
                        </button>
                    </PopoverTrigger>
                    <PopoverContent className="w-72 space-y-2 rounded-3xl p-3 bg-card">
                        <Input
                            value={corsInputValue}
                            onChange={(e) => setCorsInputValue(e.target.value)}
                            onKeyDown={(e) => {
                                if (e.key === 'Enter') {
                                    e.preventDefault();
                                    handleAddCorsOrigin();
                                }
                            }}
                            placeholder={t('corsAllowOrigins.example')}
                            className="h-9 rounded-xl"
                            autoFocus
                        />
                        <div className="max-h-48 space-y-1 overflow-y-auto">
                            {corsAllowOriginsList.length > 0 && (
                                corsAllowOriginsList.map((origin) => (
                                    <div key={origin} className="flex items-center justify-between gap-2 rounded-xl border border-border/60 px-2 py-1">
                                        <span className="break-all text-xs leading-5">{origin}</span>
                                        <button
                                            type="button"
                                            onClick={() => handleRemoveCorsOrigin(origin)}
                                            className="text-muted-foreground transition-colors hover:text-destructive"
                                            aria-label={`remove ${origin}`}
                                        >
                                            <X className="size-4" />
                                        </button>
                                    </div>
                                ))
                            )}
                        </div>
                    </PopoverContent>
                </Popover>
            </div>

            {/* SSE 流式心跳 */}
            <div className="flex items-center justify-between gap-4">
                <div className="flex items-center gap-3">
                    <Activity className="h-5 w-5 text-muted-foreground" />
                    <span className="text-sm font-medium">{t('sseHeartbeat.label')}</span>
                    <TooltipProvider>
                        <Tooltip>
                            <TooltipTrigger asChild>
                                <HelpCircle className="size-4 text-muted-foreground cursor-help" />
                            </TooltipTrigger>
                            <TooltipContent>
                                {t('sseHeartbeat.description')}
                                <br />
                                {t('sseHeartbeat.compatibility')}
                            </TooltipContent>
                        </Tooltip>
                    </TooltipProvider>
                </div>
                <Input
                    type="number"
                    min="0"
                    value={sseHeartbeatInterval}
                    onChange={(e) => setSseHeartbeatInterval(e.target.value)}
                    onBlur={() => handleSave(SettingKey.SSEHeartbeatInterval, sseHeartbeatInterval, initialSseHeartbeatInterval.current)}
                    placeholder={t('sseHeartbeat.placeholder')}
                    className="w-48 rounded-xl"
                />
            </div>

            {/* SSE 流建立前延迟心跳 */}
            <div className="flex items-center justify-between gap-4">
                <div className="flex items-center gap-3">
                    <Activity className="h-5 w-5 text-muted-foreground" />
                    <span className="text-sm font-medium">{t('ssePreStreamHeartbeat.label')}</span>
                    <TooltipProvider>
                        <Tooltip>
                            <TooltipTrigger asChild>
                                <HelpCircle className="size-4 text-muted-foreground cursor-help" />
                            </TooltipTrigger>
                            <TooltipContent>
                                {t('ssePreStreamHeartbeat.description')}
                                <br />
                                {t('ssePreStreamHeartbeat.risk')}
                            </TooltipContent>
                        </Tooltip>
                    </TooltipProvider>
                </div>
                <Input
                    type="number"
                    min="0"
                    value={ssePreStreamHeartbeatDelay}
                    onChange={(e) => setSsePreStreamHeartbeatDelay(e.target.value)}
                    onBlur={() => handleSave(SettingKey.SSEPreStreamHeartbeatDelay, ssePreStreamHeartbeatDelay, initialSsePreStreamHeartbeatDelay.current)}
                    placeholder={t('ssePreStreamHeartbeat.placeholder')}
                    className="w-48 rounded-xl"
                />
            </div>
            <div className="flex items-center justify-between gap-4">
                <div className="flex items-center gap-3">
                    <HeartPulse className="h-5 w-5 text-muted-foreground" />
                    <span className="text-sm font-medium">{t('groupHealth.label')}</span>
                    <TooltipProvider>
                        <Tooltip>
                            <TooltipTrigger asChild>
                                <HelpCircle className="size-4 text-muted-foreground cursor-help" />
                            </TooltipTrigger>
                            <TooltipContent>
                                {t('groupHealth.description')}
                            </TooltipContent>
                        </Tooltip>
                    </TooltipProvider>
                </div>
                <Switch
                    checked={groupHealthEnabled}
                    onCheckedChange={handleGroupHealthChange}
                />
            </div>

            <div className="flex items-center justify-between gap-4">
                <div className="flex items-center gap-3">
                    <Radio className="h-5 w-5 text-muted-foreground" />
                    <span className="text-sm font-medium">{t('responsesWS.enabled.label')}</span>
                    <TooltipProvider>
                        <Tooltip>
                            <TooltipTrigger asChild>
                                <HelpCircle className="size-4 text-muted-foreground cursor-help" />
                            </TooltipTrigger>
                            <TooltipContent>
                                {t('responsesWS.enabled.description')}
                            </TooltipContent>
                        </Tooltip>
                    </TooltipProvider>
                </div>
                <Switch
                    checked={responsesWSEnabled}
                    onCheckedChange={handleResponsesWSChange}
                />
            </div>

            <div className="flex items-center justify-between gap-4">
                <div className="flex items-center gap-3">
                    <Radio className="h-5 w-5 text-muted-foreground" />
                    <span className="text-sm font-medium">{t('responsesWS.defaultMode.label')}</span>
                    <TooltipProvider>
                        <Tooltip>
                            <TooltipTrigger asChild>
                                <HelpCircle className="size-4 text-muted-foreground cursor-help" />
                            </TooltipTrigger>
                            <TooltipContent>
                                {t('responsesWS.defaultMode.description')}
                            </TooltipContent>
                        </Tooltip>
                    </TooltipProvider>
                </div>
                <Select value={responsesWSDefaultMode} onValueChange={handleResponsesWSModeChange}>
                    <SelectTrigger className="w-48 rounded-xl">
                        <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                        <SelectItem value="passthrough">{t('responsesWS.defaultMode.passthrough')}</SelectItem>
                        <SelectItem value="transform">{t('responsesWS.defaultMode.transform')}</SelectItem>
                        <SelectItem value="off">{t('responsesWS.defaultMode.off')}</SelectItem>
                    </SelectContent>
                </Select>
            </div>
        </div>
    );
}
