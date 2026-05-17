'use client';

import { memo, useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useTranslations } from 'next-intl';
import { motion } from 'motion/react';
import dayjs from 'dayjs';
import relativeTime from 'dayjs/plugin/relativeTime';
import 'dayjs/locale/zh-cn';
import 'dayjs/locale/zh-tw';
import {
    ArrowUpDown,
    Check,
    CheckCircle2,
    CircleAlert,
    CircleOff,
    Clock,
    DollarSign,
    Eye,
    EyeOff,
    ExternalLink,
    Globe2,
    History,
    KeyRound,
    MessageSquare,
    MoreHorizontal,
    Plus,
    Power,
    RefreshCw,
    Search,
    Settings,
    SlidersHorizontal,
    Trash2,
    Waypoints,
    XCircle,
} from 'lucide-react';

dayjs.extend(relativeTime);

import { Bar, CartesianGrid, ComposedChart, Legend, Line, ResponsiveContainer, Tooltip as RechartsTooltip, XAxis, YAxis } from 'recharts';

const DAYJS_LOCALE_MAP: Record<'zh_hans' | 'zh_hant' | 'en', string> = {
    zh_hans: 'zh-cn',
    zh_hant: 'zh-tw',
    en: 'en',
};
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { VirtualizedGrid } from '@/components/common/VirtualizedGrid';
import {
    Dialog,
    DialogContent,
    DialogDescription,
    DialogFooter,
    DialogHeader,
    DialogTitle,
} from '@/components/ui/dialog';
import {
    MorphingDialog,
    MorphingDialogTrigger,
    MorphingDialogContainer,
    MorphingDialogContent,
    MorphingDialogTitle,
    MorphingDialogDescription,
    useMorphingDialog,
} from '@/components/ui/morphing-dialog';
import { Input } from '@/components/ui/input';
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover';
import { HoverCard, HoverCardContent, HoverCardTrigger } from '@/components/ui/hover-card';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import {
    Table,
    TableBody,
    TableCell,
    TableHead,
    TableHeader,
    TableRow,
} from '@/components/ui/table';
import { toast } from '@/components/common/Toast';
import { cn, formatCount, formatMoney } from '@/lib/utils';
import { getModelIcon } from '@/lib/model-icons';
import { useSettingStore } from '@/stores/setting';
import {
    type SiteChannelAccount,
    type SiteChannelCard,
    type SiteChannelGroup,
    type SiteSourceKeyUpdateRequest,
    type SiteModelDisableUpdateRequest,
    type SiteModelRouteType,
    type SiteModelRouteUpdateRequest,
    useCreateSiteChannelKey,
    useAddSiteManualModels,
    useDeleteSiteManualModel,
    useResetSiteChannelModelRoutes,
    useSiteChannelList,
    useUpdateSiteProjectedChannelSettings,
    useUpdateAnySiteSourceKeys,
    useUpdateSiteSourceKeys,
    useUpdateSiteChannelModelDisabled,
    useUpdateSiteChannelModelRoutes,
} from '@/api/endpoints/site-channel';
import {
    SITE_ROUTE_DISPLAY_ORDER,
    SITE_ROUTE_COLUMN_ORDER,
    getRouteSourceTone,
    getRouteTypeTone,
    isSupportedRouteType,
} from './constants';
import { translateSiteMessage } from '../site/site-message';
import {
    SITE_GROUP_FILTER_ALL,
    createGroupFilter,
    type PendingCompletionSite,
    type SiteChannelGroupFilter,
    type SiteSourceKeyFormItem,
    type SiteModelView,
    buildSiteTokenManagementUrl,
    buildSourceKeyFormItems,
    buildSourceKeyUpdatePayload,
    collectPendingCompletionSites,
    filterGroups,
    flattenAccountModels,
    formatHistoryTime,
    getErrorMessage,
    hasSourceKeyChanges,
    isMaskedTokenValue,
    isSameGroupFilter,
    platformLabel,
    routeSourceLabel,
    routeTypeLabel,
    summarizeHistory,
} from './utils';
import { useJumpStore, type JumpTarget, type PendingJump, type SiteChannelJumpTarget, isSiteChannelJumpTarget } from '@/stores/jump';
import { useEnableSiteAccount } from '@/api/endpoints/site';
import { AutoGroupType } from '@/api/endpoints/channel';
import {
    DEFAULT_SITE_CHANNEL_PANEL_PREFERENCES,
    type SiteChannelQuickFilter,
    type SiteChannelTableSort,
    type SiteChannelTableSortField,
    useSiteChannelPanelViewStore,
} from './ui-store';

type ChannelFilter = 'all' | 'enabled' | 'disabled';
type ToolbarSortField = 'name' | 'created';
type ToolbarSortOrder = 'asc' | 'desc';
type SiteChannelPendingJump = PendingJump & { target: SiteChannelJumpTarget };
type UnifiedCompletionInputState = Record<number, string>;
type UnifiedCompletionErrorState = Record<string, string>;

const SITE_PANEL_INITIAL_DISPLAY_LIMIT = 15;
const SITE_PANEL_DISPLAY_PAGE_SIZE = 30;

function makeAccountKey(siteId: number, accountId: number) {
    return `${siteId}:${accountId}`;
}

function UnifiedCompletionDialog({
    open,
    onOpenChange,
    sites,
}: {
    open: boolean;
    onOpenChange: (open: boolean) => void;
    sites: PendingCompletionSite[];
}) {
    const t = useTranslations();
    const locale = useSettingStore((state) => state.locale);
    const updateSourceKeys = useUpdateAnySiteSourceKeys();
    const [inputValues, setInputValues] = useState<UnifiedCompletionInputState>({});
    const [savingAccounts, setSavingAccounts] = useState<Record<string, boolean>>({});
    const [accountErrors, setAccountErrors] = useState<UnifiedCompletionErrorState>({});

    const totalPendingCount = useMemo(
        () => sites.reduce((sum, site) => sum + site.pending_count, 0),
        [sites],
    );

    useEffect(() => {
        if (!open) return;
        if (totalPendingCount > 0) return;
        onOpenChange(false);
    }, [open, totalPendingCount, onOpenChange]);

    useEffect(() => {
        setInputValues((current) => {
            const validIds = new Set<number>();
            for (const site of sites) {
                for (const account of site.accounts) {
                    for (const item of account.items) {
                        validIds.add(item.key_id);
                    }
                }
            }

            let changed = false;
            const next: UnifiedCompletionInputState = {};
            for (const [rawId, value] of Object.entries(current)) {
                const keyId = Number(rawId);
                if (!validIds.has(keyId)) {
                    changed = true;
                    continue;
                }
                next[keyId] = value;
            }

            return changed ? next : current;
        });

        setSavingAccounts((current) => {
            const validKeys = new Set<string>();
            for (const site of sites) {
                for (const account of site.accounts) {
                    validKeys.add(makeAccountKey(site.site_id, account.account_id));
                }
            }

            let changed = false;
            const next: Record<string, boolean> = {};
            for (const [key, value] of Object.entries(current)) {
                if (!validKeys.has(key)) {
                    changed = true;
                    continue;
                }
                next[key] = value;
            }
            return changed ? next : current;
        });

        setAccountErrors((current) => {
            const validKeys = new Set<string>();
            for (const site of sites) {
                for (const account of site.accounts) {
                    validKeys.add(makeAccountKey(site.site_id, account.account_id));
                }
            }

            let changed = false;
            const next: UnifiedCompletionErrorState = {};
            for (const [key, value] of Object.entries(current)) {
                if (!validKeys.has(key)) {
                    changed = true;
                    continue;
                }
                next[key] = value;
            }
            return changed ? next : current;
        });
    }, [sites]);

    const handleInputChange = useCallback((keyId: number, value: string) => {
        setInputValues((current) => ({
            ...current,
            [keyId]: value,
        }));
    }, []);

    const handleOpenSite = useCallback((site: PendingCompletionSite) => {
        const url = buildSiteTokenManagementUrl(site.base_url, site.platform);
        if (!url) return;
        window.open(url, '_blank', 'noopener,noreferrer');
    }, []);

    const handleSaveAccount = useCallback(async (site: PendingCompletionSite, accountId: number) => {
        const account = site.accounts.find((item) => item.account_id === accountId);
        if (!account) return;

        const accountKey = makeAccountKey(site.site_id, accountId);
        const itemsToSave = account.items.filter((item) => {
            const value = inputValues[item.key_id]?.trim() ?? '';
            return value.length > 0;
        });

        if (itemsToSave.length === 0) {
            setAccountErrors((current) => ({
                ...current,
                [accountKey]: '当前账号没有可提交的待补全 Key',
            }));
            return;
        }

        for (const item of itemsToSave) {
            const value = inputValues[item.key_id]?.trim() ?? '';
            if (!value) continue;
            if (isMaskedTokenValue(value)) {
                setAccountErrors((current) => ({
                    ...current,
                    [accountKey]: `分组「${item.group_name || item.group_key}」仍是脱敏值，必须填写完整 Key`,
                }));
                return;
            }
        }

        const groupedByGroupKey = new Map<string, typeof itemsToSave>();
        for (const item of itemsToSave) {
            const current = groupedByGroupKey.get(item.group_key) ?? [];
            current.push(item);
            groupedByGroupKey.set(item.group_key, current);
        }

        setSavingAccounts((current) => ({ ...current, [accountKey]: true }));
        setAccountErrors((current) => ({ ...current, [accountKey]: '' }));

        try {
            for (const [groupKey, groupItems] of groupedByGroupKey.entries()) {
                const payload: SiteSourceKeyUpdateRequest = {
                    group_key: groupKey,
                    keys_to_update: groupItems.map((item) => ({
                        id: item.key_id,
                        token: inputValues[item.key_id].trim(),
                        enabled: true,
                    })),
                };

                await updateSourceKeys.mutateAsync({
                    siteId: site.site_id,
                    accountId,
                    payload,
                });
            }

            setInputValues((current) => {
                const next = { ...current };
                for (const item of itemsToSave) {
                    delete next[item.key_id];
                }
                return next;
            });
            toast.success(`账号「${account.account_name}」的待补全 Key 已保存并恢复启用`);
        } catch (error) {
            setAccountErrors((current) => ({
                ...current,
                [accountKey]: translateSiteMessage(locale, getErrorMessage(error, `账号「${account.account_name}」保存失败`), t),
            }));
        } finally {
            setSavingAccounts((current) => ({ ...current, [accountKey]: false }));
        }
    }, [inputValues, locale, t, updateSourceKeys]);

    return (
        <Dialog open={open} onOpenChange={onOpenChange}>
            <DialogContent className="max-w-[min(92vw,72rem)] rounded-[2rem] p-0 sm:max-w-[min(92vw,72rem)]">
                <div className="flex max-h-[88vh] flex-col overflow-hidden">
                    <DialogHeader className="gap-3 border-b border-border/70 px-5 py-4 text-left sm:px-6">
                        <DialogTitle className="flex items-center gap-2 text-xl">
                            <KeyRound className="size-5 text-primary" />
                            统一补全 Key
                            <Badge variant="outline" className="h-6 px-2 text-[11px]">{totalPendingCount} 项</Badge>
                        </DialogTitle>
                        <DialogDescription>
                            同步到的脱敏 Key 不能直接继续投影，必须补全文明文 Key 才能恢复可用状态。
                        </DialogDescription>
                        <div className="rounded-2xl border border-amber-500/30 bg-amber-500/10 px-4 py-3 text-sm text-amber-900 dark:text-amber-100">
                            建议一个站点下每个分组只保留一个 Key，只创建自己需要分组的 Key，这样同步和投影会更干净。
                        </div>
                    </DialogHeader>

                    <div className="min-h-0 flex-1 overflow-y-auto px-5 py-5 sm:px-6">
                        <div className="space-y-4">
                            {sites.map((site) => {
                                const targetUrl = buildSiteTokenManagementUrl(site.base_url, site.platform);

                                return (
                                    <section key={site.site_id} className="rounded-3xl border border-border/70 bg-card/70 p-4">
                                        <div className="flex flex-col gap-3 border-b border-border/60 pb-4 md:flex-row md:items-start md:justify-between">
                                            <div className="min-w-0 space-y-2">
                                                <div className="flex flex-wrap items-center gap-2">
                                                    <div className="truncate text-lg font-semibold text-foreground">{site.site_name}</div>
                                                    <Badge variant="outline" className="h-6 px-2 text-[11px]">
                                                        {platformLabel(site.platform)}
                                                    </Badge>
                                                    <Badge variant="outline" className="h-6 px-2 text-[11px] border-amber-500/30 bg-amber-500/10 text-amber-800 dark:text-amber-200">
                                                        待补全 {site.pending_count}
                                                    </Badge>
                                                </div>
                                                <div className="text-xs text-muted-foreground">
                                                    站点级跳转用于直接打开该站点的令牌管理页，处理更复杂的 Key 清理或分组治理。
                                                </div>
                                            </div>
                                            <Button
                                                type="button"
                                                variant="outline"
                                                className="rounded-2xl"
                                                onClick={() => handleOpenSite(site)}
                                                disabled={!targetUrl}
                                            >
                                                <ExternalLink className="size-4" />
                                                打开令牌管理
                                            </Button>
                                        </div>

                                        <div className="mt-4 space-y-3">
                                            {site.accounts.map((account) => {
                                                const accountKey = makeAccountKey(site.site_id, account.account_id);
                                                const enteredCount = account.items.filter((item) => {
                                                    const value = inputValues[item.key_id]?.trim() ?? '';
                                                    return value.length > 0;
                                                }).length;
                                                const isSaving = Boolean(savingAccounts[accountKey]);
                                                const accountError = accountErrors[accountKey];

                                                return (
                                                    <div key={account.account_id} className="rounded-2xl border border-border/60 bg-background/70 p-4">
                                                        <div className="flex flex-col gap-3 md:flex-row md:items-start md:justify-between">
                                                            <div>
                                                                <div className="flex flex-wrap items-center gap-2">
                                                                    <div className="text-sm font-semibold text-foreground">{account.account_name}</div>
                                                                    <Badge variant="outline" className="h-5 px-1.5 text-[10px]">
                                                                        待补全 {account.items.length}
                                                                    </Badge>
                                                                    {enteredCount > 0 ? (
                                                                        <Badge variant="outline" className="h-5 px-1.5 text-[10px] border-primary/30 bg-primary/10 text-primary">
                                                                            已填写 {enteredCount}
                                                                        </Badge>
                                                                    ) : null}
                                                                </div>
                                                                <div className="mt-1 text-xs text-muted-foreground">
                                                                    仅提交当前账号内已填写完整值的待补全 Key；保存后会自动启用并重新参与投影。
                                                                </div>
                                                            </div>
                                                            <Button
                                                                type="button"
                                                                className="rounded-2xl"
                                                                onClick={() => void handleSaveAccount(site, account.account_id)}
                                                                disabled={isSaving || enteredCount === 0}
                                                            >
                                                                <RefreshCw className={cn('size-4', isSaving && 'animate-spin')} />
                                                                {isSaving ? '保存中...' : '保存本账号'}
                                                            </Button>
                                                        </div>

                                                        {accountError ? (
                                                            <div className="mt-3 rounded-2xl border border-destructive/30 bg-destructive/10 px-3 py-2 text-xs text-destructive">
                                                                {accountError}
                                                            </div>
                                                        ) : null}

                                                        <div className="mt-4 space-y-3">
                                                            {account.items.map((item) => (
                                                                <div key={item.key_id} className="rounded-2xl border border-border/60 bg-card/80 p-3">
                                                                    <div className="grid gap-3 lg:grid-cols-[minmax(0,15rem)_minmax(0,14rem)_1fr]">
                                                                        <div className="space-y-1">
                                                                            <div className="text-xs text-muted-foreground">分组</div>
                                                                            <div className="truncate text-sm font-medium text-foreground">{item.group_name || item.group_key}</div>
                                                                            <div className="text-[11px] text-muted-foreground">{item.group_key}</div>
                                                                        </div>
                                                                        <div className="space-y-1">
                                                                            <div className="text-xs text-muted-foreground">Key</div>
                                                                            <div className="truncate text-sm font-medium text-foreground">{item.key_name || `站点 Key #${item.key_id}`}</div>
                                                                            <div className="text-[11px] text-muted-foreground">当前值：{item.token_masked || item.token}</div>
                                                                        </div>
                                                                        <label className="grid gap-1.5 text-xs text-muted-foreground">
                                                                            输入完整 Key
                                                                            <Input
                                                                                value={inputValues[item.key_id] ?? ''}
                                                                                onChange={(event) => handleInputChange(item.key_id, event.target.value)}
                                                                                placeholder="填写完整明文 Key，保存后自动启用"
                                                                                disabled={isSaving}
                                                                                className="h-10 rounded-2xl"
                                                                            />
                                                                        </label>
                                                                    </div>
                                                                </div>
                                                            ))}
                                                        </div>
                                                    </div>
                                                );
                                            })}
                                        </div>
                                    </section>
                                );
                            })}
                        </div>
                    </div>

                    <DialogFooter className="border-t border-border/70 px-5 py-4 sm:px-6">
                        <Button type="button" variant="outline" className="rounded-2xl" onClick={() => onOpenChange(false)}>
                            关闭
                        </Button>
                    </DialogFooter>
                </div>
            </DialogContent>
        </Dialog>
    );
}

function getBaseGroupKey(groupKey: string) {
    return groupKey.split('::', 1)[0] || groupKey;
}

function makeModelKey(groupKey: string, modelName: string) {
    return `${groupKey}\u0000${modelName}`;
}

function removeKeys<T>(record: Record<string, T>, keys: string[]) {
    if (keys.length === 0) return record;
    const next = { ...record };
    let changed = false;

    for (const key of keys) {
        if (!(key in next)) continue;
        delete next[key];
        changed = true;
    }

    return changed ? next : record;
}

function addPendingKeys(current: Set<string>, keys: string[]) {
    if (keys.length === 0) return current;
    const next = new Set(current);
    for (const key of keys) {
        next.add(key);
    }
    return next;
}

function removePendingKeys(current: Set<string>, keys: string[]) {
    if (keys.length === 0) return current;
    const next = new Set(current);
    let changed = false;

    for (const key of keys) {
        if (!next.delete(key)) continue;
        changed = true;
    }

    return changed ? next : current;
}

function collectSiteSummary(card: SiteChannelCard) {
    let groupCount = 0;
    let modelCount = 0;
    let totalKeys = 0;
    let enabledKeys = 0;
    const routeCounts = new Map<SiteModelRouteType, number>();

    for (const account of card.accounts) {
        groupCount += account.group_count;
        modelCount += account.model_count;

        for (const group of account.groups) {
            totalKeys += group.key_count;
            enabledKeys += group.enabled_key_count;
        }

        for (const route of account.route_summaries) {
            routeCounts.set(route.route_type, (routeCounts.get(route.route_type) ?? 0) + route.count);
        }
    }

    return { groupCount, modelCount, totalKeys, enabledKeys, routeCounts };
}

function collectSiteRuntimeSummary(card: SiteChannelCard) {
    let successCount = 0;
    let failureCount = 0;
    let totalCost = 0;
    let lastRequestAt: number | null = null;
    let maskedPendingKeys = 0;

    for (const account of card.accounts) {
        for (const group of account.groups) {
            maskedPendingKeys += group.masked_pending_key_count;
            for (const key of group.projected_keys) {
                totalCost += key.total_cost;
                if (key.last_use_time_stamp > 0) {
                    lastRequestAt = Math.max(lastRequestAt ?? 0, key.last_use_time_stamp);
                }
            }
            for (const m of group.models) {
                const h = m.history;
                if (!h) continue;
                successCount += h.success_count;
                failureCount += h.failure_count;
                if (typeof h.last_request_at === 'number' && h.last_request_at > 0) {
                    lastRequestAt = Math.max(lastRequestAt ?? 0, h.last_request_at);
                }
            }
        }
    }

    return {
        totalRequests: successCount + failureCount,
        successCount,
        failureCount,
        totalCost,
        lastRequestAt,
        maskedPendingKeys,
    };
}

const SHORT_ROUTE_LABEL: Partial<Record<SiteModelRouteType, string>> = {
    openai_chat: 'Chat',
    openai_response: 'Response',
    openai_embedding: 'Embedding',
};

function getUnknownRouteReason(model: SiteModelView) {
    const metadata = model.route_metadata;
    if (!metadata || metadata.route_supported) return null;

    const details = [metadata.unsupported_reason];
    if (metadata.supported_endpoint_types?.length) {
        details.push(`检测到端点: ${metadata.supported_endpoint_types.join(', ')}`);
    }
    if (metadata.heuristic_endpoint_types?.length) {
        details.push(`启发式推断: ${metadata.heuristic_endpoint_types.join(', ')}`);
    }

    return details.filter((item): item is string => Boolean(item && item.trim())).join(' · ') || null;
}

function getModelLastRequestAt(model: SiteModelView) {
    return model.history?.last_request_at ?? null;
}

function getModelHistoryCount(model: SiteModelView) {
    return (model.history?.success_count ?? 0) + (model.history?.failure_count ?? 0);
}

function hasModelHistory(model: SiteModelView) {
    return getModelHistoryCount(model) > 0 || getModelLastRequestAt(model) !== null;
}

function modelNeedsAttention(model: SiteModelView) {
    return !model.has_keys || !model.projected_channel_id || !isSupportedRouteType(model.route_type);
}

function compareNullableNumber(left: number | null, right: number | null, order: 'asc' | 'desc') {
    const normalizedLeft = left ?? -1;
    const normalizedRight = right ?? -1;
    const diff = normalizedLeft - normalizedRight;
    return order === 'asc' ? diff : -diff;
}

function compareText(left: string, right: string, order: 'asc' | 'desc') {
    const diff = left.localeCompare(right);
    return order === 'asc' ? diff : -diff;
}

function matchesQuickFilters(model: SiteModelView, quickFilters: SiteChannelQuickFilter[]) {
    if (quickFilters.length === 0) return true;

    return quickFilters.every((filter) => {
        switch (filter) {
            case 'attention':
                return modelNeedsAttention(model);
            case 'with_history':
                return hasModelHistory(model);
            case 'disabled':
                return model.disabled;
            default:
                return true;
        }
    });
}

function sortModels(models: SiteModelView[], tableSort: SiteChannelTableSort) {
    return [...models].sort((left, right) => {
        switch (tableSort.field) {
            case 'group_name':
                return compareText(left.group_name || left.group_key, right.group_name || right.group_key, tableSort.order);
            case 'route_type':
                return compareText(routeTypeLabel(left.route_type), routeTypeLabel(right.route_type), tableSort.order);
            case 'last_request_at':
                return compareNullableNumber(getModelLastRequestAt(left), getModelLastRequestAt(right), tableSort.order);
            case 'model_name':
            default:
                return compareText(left.model_name, right.model_name, tableSort.order);
        }
    });
}

const QUICK_FILTER_OPTIONS: Array<{
    key: SiteChannelQuickFilter;
    label: string;
}> = [
    { key: 'attention', label: '仅未正确配置' },
    { key: 'with_history', label: '仅有请求历史' },
    { key: 'disabled', label: '仅禁用' },
];

const SITE_GROUP_FILTER_ALL_VALUE = '__site-group-all__';

function HistorySummary({ model }: { model: SiteModelView }) {
    const summary = summarizeHistory(model.history);
    const buckets = model.history?.buckets ?? [];
    const bucketSpan = model.history?.bucket_span ?? 0;

    const chartData = buckets.map((b) => {
        const total = b.success + b.failure;
        const successRate = total === 0 ? null : Math.round((b.success / total) * 100);
        return {
            time: b.time,
            success: b.success,
            failure: b.failure,
            total,
            successRate,
        };
    });

    const formatBucketTime = (time: number) => {
        const d = dayjs(time * 1000);
        if (bucketSpan >= 7 * 86400) return d.format('YY-MM-DD');
        if (bucketSpan >= 86400) return d.format('MM-DD');
        if (bucketSpan >= 3600) return d.format('MM-DD HH:mm');
        return d.format('HH:mm');
    };

    return (
        <div className="w-[24rem] space-y-3 p-4 text-left">
            <div className="space-y-1">
                <div className="flex items-center justify-between gap-2">
                    <div className="truncate text-sm font-semibold text-foreground">{model.model_name}</div>
                    <Badge variant="outline" className="h-5 px-1.5 text-[10px]">
                        {routeTypeLabel(model.route_type)}
                    </Badge>
                </div>
                <div className="text-[11px] text-muted-foreground">
                    {model.group_name || model.group_key} · 最近请求 {formatHistoryTime(model.history?.last_request_at ?? null)}
                </div>
            </div>

            <div className="grid grid-cols-3 gap-2 text-xs">
                <div className="rounded-xl border border-border/60 bg-background/70 px-2 py-2">
                    <div className="text-muted-foreground">成功</div>
                    <div className="mt-1 font-semibold text-emerald-600 dark:text-emerald-300">{summary.successCount}</div>
                </div>
                <div className="rounded-xl border border-border/60 bg-background/70 px-2 py-2">
                    <div className="text-muted-foreground">失败</div>
                    <div className="mt-1 font-semibold text-destructive">{summary.failureCount}</div>
                </div>
                <div className="rounded-xl border border-border/60 bg-background/70 px-2 py-2">
                    <div className="text-muted-foreground">成功率</div>
                    <div className="mt-1 font-semibold text-foreground">{(summary.successRate * 100).toFixed(0)}%</div>
                </div>
            </div>

            {chartData.length > 0 ? (
                <div className="rounded-xl border border-border/60 bg-background/70 p-2">
                    <div className="h-36 w-full">
                        <ResponsiveContainer width="100%" height="100%">
                            <ComposedChart data={chartData} margin={{ top: 8, right: 8, bottom: 4, left: 4 }}>
                                <defs>
                                    <linearGradient id="fillSiteChannelBar" x1="0" y1="0" x2="0" y2="1">
                                        <stop offset="5%" stopColor="var(--chart-2)" stopOpacity={0.55} />
                                        <stop offset="95%" stopColor="var(--chart-2)" stopOpacity={0.15} />
                                    </linearGradient>
                                </defs>
                                <CartesianGrid strokeDasharray="3 3" vertical={false} className="stroke-border/50" />
                                <XAxis
                                    dataKey="time"
                                    tickFormatter={formatBucketTime}
                                    tick={{ fontSize: 10 }}
                                    tickLine={false}
                                    axisLine={false}
                                    minTickGap={24}
                                />
                                <YAxis
                                    yAxisId="rate"
                                    domain={[0, 100]}
                                    tick={{ fontSize: 10 }}
                                    tickFormatter={(v) => `${v}%`}
                                    tickLine={false}
                                    axisLine={false}
                                    width={32}
                                />
                                <YAxis
                                    yAxisId="count"
                                    orientation="right"
                                    tick={{ fontSize: 10 }}
                                    tickLine={false}
                                    axisLine={false}
                                    width={24}
                                    allowDecimals={false}
                                />
                                <Legend
                                    iconType="circle"
                                    iconSize={7}
                                    height={14}
                                    wrapperStyle={{ fontSize: 10, paddingTop: 2, lineHeight: '14px' }}
                                />
                                <RechartsTooltip
                                    cursor={{ fill: 'var(--muted)', fillOpacity: 0.4 }}
                                    content={({ active, payload }) => {
                                        if (!active || !payload || payload.length === 0) return null;
                                        const point = payload[0].payload as typeof chartData[number];
                                        return (
                                            <div className="rounded-lg border border-border/70 bg-popover/95 px-3 py-2 text-[11px] shadow-md backdrop-blur">
                                                <div className="font-medium text-foreground">{formatBucketTime(point.time)}</div>
                                                <div className="mt-1 flex items-center gap-3 text-muted-foreground">
                                                    <span className="text-emerald-600 dark:text-emerald-300">成功 {point.success}</span>
                                                    <span className="text-destructive">失败 {point.failure}</span>
                                                </div>
                                                <div className="text-muted-foreground">
                                                    成功率 {point.successRate === null ? '—' : `${point.successRate}%`}
                                                </div>
                                            </div>
                                        );
                                    }}
                                />
                                <Bar
                                    yAxisId="count"
                                    dataKey="total"
                                    name="请求数"
                                    fill="url(#fillSiteChannelBar)"
                                    radius={[2, 2, 0, 0]}
                                    isAnimationActive={false}
                                />
                                <Line
                                    yAxisId="rate"
                                    type="monotone"
                                    dataKey="successRate"
                                    name="成功率"
                                    stroke="var(--chart-1)"
                                    strokeWidth={2}
                                    dot={{ r: 2, fill: 'var(--chart-1)', stroke: 'var(--chart-1)' }}
                                    activeDot={{ r: 3 }}
                                    connectNulls
                                    isAnimationActive={false}
                                />
                            </ComposedChart>
                        </ResponsiveContainer>
                    </div>
                </div>
            ) : (
                <div className="rounded-xl border border-dashed border-border/70 bg-background/50 px-3 py-4 text-center text-xs text-muted-foreground">
                    暂无请求历史
                </div>
            )}
        </div>
    );
}

function SelectionCheckbox({
    checked,
    onCheckedChange,
    disabled,
    ariaLabel,
    className,
}: {
    checked: boolean;
    onCheckedChange: (checked: boolean) => void;
    disabled?: boolean;
    ariaLabel: string;
    className?: string;
}) {
    return (
        <input
            type="checkbox"
            checked={checked}
            disabled={disabled}
            aria-label={ariaLabel}
            onChange={(event) => onCheckedChange(event.target.checked)}
            className={cn(
                'size-4 rounded border-border bg-background align-middle accent-primary disabled:cursor-not-allowed disabled:opacity-50',
                className,
            )}
        />
    );
}

function MoveRoutePopover({
    currentRouteType,
    disabled,
    buttonClassName,
    onMove,
}: {
    currentRouteType: SiteModelRouteType;
    disabled?: boolean;
    buttonClassName?: string;
    onMove: (routeType: SiteModelRouteType) => void;
}) {
    const [open, setOpen] = useState(false);

    return (
        <Popover open={open} onOpenChange={setOpen}>
            <PopoverTrigger asChild>
                <button
                    type="button"
                    disabled={disabled}
                    className={cn(
                        'rounded-lg p-1 text-muted-foreground transition hover:bg-muted hover:text-foreground disabled:cursor-not-allowed disabled:opacity-50',
                        buttonClassName,
                    )}
                >
                    <MoreHorizontal className="size-4" />
                </button>
            </PopoverTrigger>
            <PopoverContent align="end" className="w-56 rounded-2xl border border-border/70 bg-card p-2 shadow-xl">
                <div className="space-y-2">
                    <div className="px-2 pt-1 text-xs font-medium text-muted-foreground">移动至...</div>
                    <div className="grid gap-1">
                        {SITE_ROUTE_COLUMN_ORDER.map((routeType) => (
                            <button
                                key={routeType}
                                type="button"
                                disabled={disabled || routeType === currentRouteType}
                                onClick={() => {
                                    onMove(routeType);
                                    setOpen(false);
                                }}
                                className={cn(
                                    'flex items-center justify-between rounded-xl px-2 py-2 text-left text-sm transition',
                                    routeType === currentRouteType
                                        ? 'bg-muted/60 text-muted-foreground'
                                        : 'hover:bg-muted',
                                )}
                            >
                                <span>{routeTypeLabel(routeType)}</span>
                                {routeType === currentRouteType ? <Check className="size-4" /> : null}
                            </button>
                        ))}
                    </div>
                </div>
            </PopoverContent>
        </Popover>
    );
}

const STICKY_HEAD_CELL = 'sticky top-0 z-10 bg-card';

function SiteChannelTableView({
    models,
    hasMore,
    onReachEnd,
    allVisibleSelected,
    pendingModelKeys,
    selectedModelKeys,
    compactMode,
    tableSort,
    highlightedModelKey,
    onToggleModelSelection,
    onToggleAllVisible,
    onSortChange,
    onMoveModel,
    onToggleDisabled,
    onDeleteManualModel,
    onNavigateToChannel,
    registerModelRef,
}: {
    models: SiteModelView[];
    hasMore: boolean;
    onReachEnd: () => void;
    allVisibleSelected: boolean;
    pendingModelKeys: Set<string>;
    selectedModelKeys: Set<string>;
    compactMode: boolean;
    tableSort: SiteChannelTableSort;
    highlightedModelKey: string | null;
    onToggleModelSelection: (modelKey: string, checked: boolean) => void;
    onToggleAllVisible: (checked: boolean) => void;
    onSortChange: (field: SiteChannelTableSortField) => void;
    onMoveModel: (model: SiteModelView, routeType: SiteModelRouteType) => void;
    onToggleDisabled: (model: SiteModelView) => void;
    onDeleteManualModel: (model: SiteModelView) => void;
    onNavigateToChannel: (channelId: number) => void;
    registerModelRef: (modelKey: string, node: HTMLElement | null) => void;
}) {
    const sentinelRef = useRef<HTMLDivElement | null>(null);

    useEffect(() => {
        if (!hasMore) return;
        const node = sentinelRef.current;
        if (!node) return;
        const observer = new IntersectionObserver(
            (entries) => {
                for (const entry of entries) {
                    if (entry.isIntersecting) {
                        onReachEnd();
                        break;
                    }
                }
            },
            { rootMargin: '200px', threshold: 0 },
        );
        observer.observe(node);
        return () => observer.disconnect();
    }, [hasMore, onReachEnd]);

    const renderSortHead = (field: SiteChannelTableSortField, label: string) => (
        <button
            type="button"
            onClick={() => onSortChange(field)}
            className="inline-flex items-center gap-1 text-xs font-medium text-muted-foreground transition hover:text-foreground"
        >
            <span>{label}</span>
            <ArrowUpDown className={cn('size-3.5', tableSort.field === field && 'text-foreground')} />
        </button>
    );

    return (
        <>
            <Table
                containerClassName="overflow-x-auto overflow-y-visible"
                className="min-w-[74rem]"
            >
                <TableHeader>
                    <TableRow>
                        <TableHead className={cn(STICKY_HEAD_CELL, 'w-12')}>
                            <SelectionCheckbox
                                checked={allVisibleSelected}
                                disabled={models.length === 0}
                                ariaLabel="选择当前可见模型"
                                onCheckedChange={onToggleAllVisible}
                            />
                        </TableHead>
                        <TableHead className={STICKY_HEAD_CELL}>{renderSortHead('model_name', '模型')}</TableHead>
                        <TableHead className={STICKY_HEAD_CELL}>{renderSortHead('group_name', '分组')}</TableHead>
                        <TableHead className={STICKY_HEAD_CELL}>{renderSortHead('route_type', '端点格式')}</TableHead>
                        <TableHead className={STICKY_HEAD_CELL}>来源</TableHead>
                        <TableHead className={STICKY_HEAD_CELL}>Key</TableHead>
                        <TableHead className={STICKY_HEAD_CELL}>状态</TableHead>
                        <TableHead className={STICKY_HEAD_CELL}>{renderSortHead('last_request_at', '最近请求')}</TableHead>
                        <TableHead className={STICKY_HEAD_CELL}>渠道</TableHead>
                        <TableHead className={cn(STICKY_HEAD_CELL, 'text-right')}>操作</TableHead>
                    </TableRow>
                </TableHeader>
                <TableBody>
                    {models.map((model) => {
                        const modelKey = makeModelKey(model.group_key, model.model_name);
                        const { Avatar: ModelAvatar } = getModelIcon(model.model_name);
                        const isPending = pendingModelKeys.has(modelKey);
                        const isSelected = selectedModelKeys.has(modelKey);
                        const historyCount = getModelHistoryCount(model);

                        return (
                            <TableRow
                                key={modelKey}
                                ref={(node) => registerModelRef(modelKey, node)}
                                data-state={isSelected ? 'selected' : undefined}
                                className={cn(
                                    model.disabled && 'opacity-60',
                                    isPending && 'opacity-70',
                                    highlightedModelKey === modelKey && 'ring-2 ring-primary/35 ring-inset',
                                )}
                            >
                                <TableCell className={compactMode ? 'py-2' : undefined}>
                                    <SelectionCheckbox
                                        checked={isSelected}
                                        disabled={isPending}
                                        ariaLabel={`选择模型 ${model.model_name}`}
                                        onCheckedChange={(checked) => onToggleModelSelection(modelKey, checked)}
                                    />
                                </TableCell>
                                <TableCell className={compactMode ? 'py-2' : undefined}>
                                    <div className="flex min-w-0 items-center gap-2">
                                        <ModelAvatar size={18} />
                                        <div className="min-w-0">
                                            <div className="flex min-w-0 items-center gap-1.5">
                                                <span className="truncate text-sm font-medium">{model.model_name}</span>
                                                {model.source === 'manual' ? (
                                                    <Badge variant="outline" className="h-5 px-1.5 text-[10px] border-primary/30 bg-primary/10 text-primary">自定义</Badge>
                                                ) : null}
                                            </div>
                                            {!compactMode ? (
                                                <div className="text-[11px] text-muted-foreground">
                                                    {model.manual_override ? '手动覆盖' : '自动映射'}
                                                </div>
                                            ) : null}
                                        </div>
                                    </div>
                                </TableCell>
                                <TableCell className={compactMode ? 'py-2' : undefined}>
                                    <div className="max-w-[14rem] truncate text-sm">{model.group_name || model.group_key}</div>
                                </TableCell>
                                <TableCell className={compactMode ? 'py-2' : undefined}>
                                    <div className="flex flex-wrap gap-1.5">
                                        <Badge variant="outline" className={cn('h-6 px-2 text-[11px]', getRouteTypeTone(model.route_type))}>
                                            {routeTypeLabel(model.route_type)}
                                        </Badge>
                                        {!isSupportedRouteType(model.route_type) ? (
                                            <Badge
                                                variant="outline"
                                                className="h-6 px-2 text-[11px] border-amber-500/30 bg-amber-500/10 text-amber-700 dark:text-amber-300"
                                                title={getUnknownRouteReason(model) ?? undefined}
                                            >
                                                待人工指定
                                            </Badge>
                                        ) : null}
                                    </div>
                                </TableCell>
                                <TableCell className={compactMode ? 'py-2' : undefined}>
                                    <Badge variant="outline" className={cn('h-6 px-2 text-[11px]', getRouteSourceTone(model.route_source))}>
                                        {routeSourceLabel(model.route_source)}
                                    </Badge>
                                </TableCell>
                                <TableCell className={compactMode ? 'py-2' : undefined}>
                                    <div className="text-sm">
                                        {model.enabled_key_count}/{model.key_count}
                                    </div>
                                    {!model.has_keys ? (
                                        <div className="text-[11px] text-amber-700 dark:text-amber-300">缺少 Key</div>
                                    ) : null}
                                </TableCell>
                                <TableCell className={compactMode ? 'py-2' : undefined}>
                                    <div className="flex flex-wrap gap-1.5">
                                        {model.disabled ? (
                                            <Badge variant="outline" className="h-6 px-2 text-[11px] border-destructive/30 bg-destructive/10 text-destructive">
                                                已禁用
                                            </Badge>
                                        ) : (
                                            <Badge variant="outline" className="h-6 px-2 text-[11px] border-emerald-500/30 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300">
                                                已启用
                                            </Badge>
                                        )}
                                        {modelNeedsAttention(model) ? (
                                            <Badge variant="outline" className="h-6 px-2 text-[11px] border-amber-500/30 bg-amber-500/10 text-amber-700 dark:text-amber-300">
                                                待处理
                                            </Badge>
                                        ) : null}
                                    </div>
                                </TableCell>
                                <TableCell className={compactMode ? 'py-2' : undefined}>
                                    <div className="text-sm">{formatHistoryTime(getModelLastRequestAt(model))}</div>
                                    <div className="text-[11px] text-muted-foreground">{historyCount} 次记录</div>
                                </TableCell>
                                <TableCell className={compactMode ? 'py-2' : undefined}>
                                    {model.projected_channel_id ? (
                                        <button
                                            type="button"
                                            onClick={() => onNavigateToChannel(model.projected_channel_id!)}
                                            className="inline-flex rounded-full border border-border px-2 py-1 text-xs transition hover:border-primary/30 hover:bg-primary/5"
                                        >
                                            #{model.projected_channel_id}
                                        </button>
                                    ) : (
                                        <span className="text-sm text-muted-foreground">-</span>
                                    )}
                                </TableCell>
                                <TableCell className={cn('text-right', compactMode ? 'py-2' : undefined)}>
                                    <div className="flex justify-end gap-1">
                                        <MoveRoutePopover
                                            currentRouteType={model.route_type}
                                            disabled={isPending || model.disabled}
                                            onMove={(routeType) => onMoveModel(model, routeType)}
                                        />
                                        <HoverCard>
                                            <HoverCardTrigger asChild>
                                                <button
                                                    type="button"
                                                    className="rounded-lg p-1 text-muted-foreground transition hover:bg-muted hover:text-foreground"
                                                >
                                                    <History className="size-4" />
                                                </button>
                                            </HoverCardTrigger>
                                            <HoverCardContent
                                                side="top"
                                                align="end"
                                                className="w-auto max-w-none rounded-2xl border border-border/70 bg-card p-0 shadow-xl"
                                            >
                                                <HistorySummary model={model} />
                                            </HoverCardContent>
                                        </HoverCard>
                                        {model.source === 'manual' ? (
                                            <button
                                                type="button"
                                                onClick={() => onDeleteManualModel(model)}
                                                disabled={isPending}
                                                className="rounded-lg p-1 text-muted-foreground transition hover:bg-destructive/10 hover:text-destructive disabled:opacity-50"
                                                title="删除自定义模型"
                                            >
                                                <Trash2 className="size-4" />
                                            </button>
                                        ) : null}
                                        <button
                                            type="button"
                                            onClick={() => onToggleDisabled(model)}
                                            disabled={isPending}
                                            className={cn(
                                                'rounded-lg p-1 transition',
                                                model.disabled
                                                    ? 'text-destructive hover:bg-destructive/10'
                                                    : 'text-muted-foreground hover:bg-muted hover:text-foreground',
                                            )}
                                        >
                                            <CircleOff className="size-4" />
                                        </button>
                                    </div>
                                </TableCell>
                            </TableRow>
                        );
                    })}
                </TableBody>
            </Table>
            {hasMore ? <div ref={sentinelRef} aria-hidden className="h-px" /> : null}
        </>
    );
}

function SiteAccountPanel({
    siteId,
    account,
    accounts,
    activeAccountId,
    onSelectAccount,
    highlightedAccountId,
    registerAccountTabRef,
    jumpRequest,
    onJumpHandled,
    onNavigateToChannel,
}: {
    siteId: number;
    account: SiteChannelAccount;
    accounts: SiteChannelAccount[];
    activeAccountId: number | null;
    onSelectAccount: (accountId: number) => void;
    highlightedAccountId: number | null;
    registerAccountTabRef: (accountId: number, node: HTMLButtonElement | null) => void;
    jumpRequest: SiteChannelPendingJump | null;
    onJumpHandled: (requestId: number) => void;
    onNavigateToChannel: (channelId: number) => void;
}) {
    const t = useTranslations();
    const locale = useSettingStore((state) => state.locale);
    const [activeFilter, setActiveFilter] = useState<SiteChannelGroupFilter>(SITE_GROUP_FILTER_ALL);
    const [pendingRouteOverrides, setPendingRouteOverrides] = useState<Record<string, SiteModelRouteType>>({});
    const [pendingDisabledOverrides, setPendingDisabledOverrides] = useState<Record<string, boolean>>({});
    const [pendingModelKeys, setPendingModelKeys] = useState<Set<string>>(new Set());
    const [selectedModelKeys, setSelectedModelKeys] = useState<Set<string>>(new Set());
    const [creatingGroup, setCreatingGroup] = useState<SiteChannelGroup | null>(null);
    const [editingProjectedGroup, setEditingProjectedGroup] = useState<SiteChannelGroup | null>(null);
    const [editingAdvancedGroup, setEditingAdvancedGroup] = useState<SiteChannelGroup | null>(null);
    const [selectedAdvancedChannelId, setSelectedAdvancedChannelId] = useState<number | null>(null);
    const [advancedForm, setAdvancedForm] = useState<Record<number, { auto_group: AutoGroupType; param_override: string }>>({});
    const [addingManualGroup, setAddingManualGroup] = useState<SiteChannelGroup | null>(null);
    const [manualModelsInput, setManualModelsInput] = useState('');
    const [manualModelRouteType, setManualModelRouteType] = useState<SiteModelRouteType>('openai_chat');
    const [sourceKeyForm, setSourceKeyForm] = useState<SiteSourceKeyFormItem[]>([]);
    const [visibleSourceKeyRows, setVisibleSourceKeyRows] = useState<Record<string, boolean>>({});
    const [quickCreateName, setQuickCreateName] = useState('');
    const [highlightedModelKey, setHighlightedModelKey] = useState<string | null>(null);
    const [modelSearchTerm, setModelSearchTerm] = useState('');
    const [bulkMoveTarget, setBulkMoveTarget] = useState<SiteModelRouteType>('openai_chat');
    const [deletingManualModelKey, setDeletingManualModelKey] = useState<string | null>(null);
    const [displayLimit, setDisplayLimit] = useState(SITE_PANEL_INITIAL_DISPLAY_LIMIT);
    const modelElementRefs = useRef<Map<string, HTMLElement>>(new Map());
    const panelKey = `${siteId}:${account.account_id}`;

    const panelPreferences = useSiteChannelPanelViewStore(
        (state) => state.panels[panelKey] ?? DEFAULT_SITE_CHANNEL_PANEL_PREFERENCES,
    );
    const setCompactMode = useSiteChannelPanelViewStore((state) => state.setCompactMode);
    const setQuickFilters = useSiteChannelPanelViewStore((state) => state.setQuickFilters);
    const setTableSort = useSiteChannelPanelViewStore((state) => state.setTableSort);

    const createKeyMutation = useCreateSiteChannelKey(siteId, account.account_id);
    const sourceKeyMutation = useUpdateSiteSourceKeys(siteId, account.account_id);
    const advancedMutation = useUpdateSiteProjectedChannelSettings(siteId, account.account_id);
    const addManualModelsMutation = useAddSiteManualModels(siteId, account.account_id);
    const deleteManualModelMutation = useDeleteSiteManualModel(siteId, account.account_id);
    const routeMutation = useUpdateSiteChannelModelRoutes(siteId, account.account_id);
    const disabledMutation = useUpdateSiteChannelModelDisabled();
    const resetMutation = useResetSiteChannelModelRoutes(siteId, account.account_id);
    const enableSiteAccount = useEnableSiteAccount();

    const translateSiteError = useCallback(
        (error: unknown, fallback: string) => translateSiteMessage(locale, getErrorMessage(error, fallback), t),
        [locale, t],
    );

    const registerModelRef = useCallback((modelKey: string, node: HTMLElement | null) => {
        if (node) {
            modelElementRefs.current.set(modelKey, node);
            return;
        }
        modelElementRefs.current.delete(modelKey);
    }, []);

    const forcedModelKey =
        jumpRequest?.target.kind === 'site-channel-model' &&
        jumpRequest.target.siteId === siteId &&
        jumpRequest.target.accountId === account.account_id
            ? makeModelKey(getBaseGroupKey(jumpRequest.target.groupKey), jumpRequest.target.modelName)
            : null;

    const visibleGroups = useMemo(
        () => filterGroups(account.groups, activeFilter),
        [account.groups, activeFilter],
    );

    const scopedModels = useMemo(() => {
        return flattenAccountModels(account, activeFilter).map((model) => {
            const modelKey = makeModelKey(model.group_key, model.model_name);
            const nextRouteType = pendingRouteOverrides[modelKey];
            const nextDisabled = pendingDisabledOverrides[modelKey];

            return {
                ...model,
                route_type: nextRouteType ?? model.route_type,
                route_source: nextRouteType ? 'manual_override' : model.route_source,
                manual_override: nextRouteType ? true : model.manual_override,
                disabled: nextDisabled ?? model.disabled,
            };
        });
    }, [account, activeFilter, pendingRouteOverrides, pendingDisabledOverrides]);

    const filteredModels = useMemo(() => {
        const normalizedSearch = modelSearchTerm.trim().toLowerCase();

        return scopedModels.filter((model) => {
            const modelKey = makeModelKey(model.group_key, model.model_name);
            if (forcedModelKey === modelKey) return true;

            const matchesSearch =
                !normalizedSearch ||
                model.model_name.toLowerCase().includes(normalizedSearch) ||
                (model.group_name || model.group_key).toLowerCase().includes(normalizedSearch);

            if (!matchesSearch) return false;

            return matchesQuickFilters(model, panelPreferences.quickFilters);
        });
    }, [scopedModels, modelSearchTerm, panelPreferences.quickFilters, forcedModelKey]);

    const visibleModels = useMemo(
        () => sortModels(filteredModels, panelPreferences.tableSort),
        [filteredModels, panelPreferences.tableSort],
    );

    const visibleModelMap = useMemo(
        () =>
            new Map(
                visibleModels.map((model) => [makeModelKey(model.group_key, model.model_name), model] as const),
            ),
        [visibleModels],
    );

    const displayedModels = useMemo(
        () => visibleModels.slice(0, displayLimit),
        [visibleModels, displayLimit],
    );

    const displayLimitResetKey = `${account.account_id}|${activeFilter.kind}|${activeFilter.kind === 'group' ? activeFilter.groupKey : ''}|${modelSearchTerm}|${panelPreferences.quickFilters.join(',')}`;
    const [prevDisplayLimitResetKey, setPrevDisplayLimitResetKey] = useState(displayLimitResetKey);
    if (prevDisplayLimitResetKey !== displayLimitResetKey) {
        setPrevDisplayLimitResetKey(displayLimitResetKey);
        setDisplayLimit(SITE_PANEL_INITIAL_DISPLAY_LIMIT);
    }

    const selectedModels = useMemo(
        () => Array.from(selectedModelKeys).map((key) => visibleModelMap.get(key)).filter((model): model is SiteModelView => !!model),
        [selectedModelKeys, visibleModelMap],
    );
    const hasPendingChanges = pendingModelKeys.size > 0 || routeMutation.isPending || disabledMutation.isPending || advancedMutation.isPending || addManualModelsMutation.isPending || deleteManualModelMutation.isPending;

    useEffect(() => {
        if (!jumpRequest || jumpRequest.target.kind !== 'site-channel-model') return;
        const target = jumpRequest.target;
        if (target.siteId !== siteId || target.accountId !== account.account_id) return;

        const targetGroupKey = getBaseGroupKey(target.groupKey);
        const targetFilter = createGroupFilter(targetGroupKey);
        if (!isSameGroupFilter(activeFilter, targetFilter)) {
            const frameId = window.requestAnimationFrame(() => {
                setActiveFilter(targetFilter);
            });
            return () => window.cancelAnimationFrame(frameId);
        }

        const modelKey = makeModelKey(targetGroupKey, target.modelName);
        const node = modelElementRefs.current.get(modelKey);
        if (!node) return;

        const timer = window.setTimeout(() => {
            node.scrollIntoView({ behavior: 'smooth', block: 'center', inline: 'nearest' });
            setHighlightedModelKey(modelKey);
            window.setTimeout(() => {
                setHighlightedModelKey((current) => (current === modelKey ? null : current));
            }, 1800);
            onJumpHandled(jumpRequest.requestId);
        }, 80);

        return () => window.clearTimeout(timer);
    }, [jumpRequest, siteId, account.account_id, activeFilter, onJumpHandled]);

    const setSelectionForKeys = useCallback((modelKeys: string[], checked: boolean) => {
        if (modelKeys.length === 0) return;

        setSelectedModelKeys((current) => {
            const next = new Set(current);
            for (const modelKey of modelKeys) {
                if (checked) {
                    next.add(modelKey);
                } else {
                    next.delete(modelKey);
                }
            }
            return next;
        });
    }, []);

    const handleToggleModelSelection = useCallback((modelKey: string, checked: boolean) => {
        setSelectionForKeys([modelKey], checked);
    }, [setSelectionForKeys]);

    const handleToggleAllVisible = useCallback((checked: boolean) => {
        setSelectionForKeys(
            visibleModels.map((model) => makeModelKey(model.group_key, model.model_name)),
            checked,
        );
    }, [visibleModels, setSelectionForKeys]);

    const handleLoadMoreModels = useCallback(() => {
        setDisplayLimit((prev) => Math.min(prev + SITE_PANEL_DISPLAY_PAGE_SIZE, visibleModels.length));
    }, [visibleModels.length]);

    const allVisibleSelected = useMemo(
        () =>
            visibleModels.length > 0 &&
            visibleModels.every((model) =>
                selectedModelKeys.has(makeModelKey(model.group_key, model.model_name)),
            ),
        [visibleModels, selectedModelKeys],
    );

    const applyRouteChange = useCallback((models: SiteModelView[], nextRouteType: SiteModelRouteType) => {
        const eligibleModels = models.filter((model) => {
            const modelKey = makeModelKey(model.group_key, model.model_name);
            return !pendingModelKeys.has(modelKey) && !model.disabled && model.route_type !== nextRouteType;
        });

        if (eligibleModels.length === 0) return;

        const modelKeys = eligibleModels.map((model) => makeModelKey(model.group_key, model.model_name));
        const payload: SiteModelRouteUpdateRequest[] = eligibleModels.map((model) => ({
            group_key: model.group_key,
            model_name: model.model_name,
            route_type: nextRouteType,
        }));

        setPendingRouteOverrides((current) => {
            const next = { ...current };
            for (const modelKey of modelKeys) {
                next[modelKey] = nextRouteType;
            }
            return next;
        });
        setPendingModelKeys((current) => addPendingKeys(current, modelKeys));

        routeMutation.mutate(payload, {
            onSuccess: () => {
                setPendingRouteOverrides((current) => removeKeys(current, modelKeys));
                toast.success(payload.length === 1 ? '模型请求端点格式已更新' : `已更新 ${payload.length} 个模型的请求端点格式`);
            },
            onError: (error) => {
                setPendingRouteOverrides((current) => removeKeys(current, modelKeys));
                toast.error(translateSiteError(error, '更新模型请求端点格式失败'));
            },
            onSettled: () => {
                setPendingModelKeys((current) => removePendingKeys(current, modelKeys));
            },
        });
    }, [pendingModelKeys, routeMutation, translateSiteError]);

    const applyDisabledChange = useCallback((models: SiteModelView[], nextDisabled: boolean) => {
        const eligibleModels = models.filter((model) => {
            const modelKey = makeModelKey(model.group_key, model.model_name);
            return !pendingModelKeys.has(modelKey) && model.disabled !== nextDisabled;
        });

        if (eligibleModels.length === 0) return;

        const modelKeys = eligibleModels.map((model) => makeModelKey(model.group_key, model.model_name));
        const payload: SiteModelDisableUpdateRequest[] = eligibleModels.map((model) => ({
            group_key: model.group_key,
            model_name: model.model_name,
            disabled: nextDisabled,
        }));

        setPendingDisabledOverrides((current) => {
            const next = { ...current };
            for (const modelKey of modelKeys) {
                next[modelKey] = nextDisabled;
            }
            return next;
        });
        setPendingModelKeys((current) => addPendingKeys(current, modelKeys));

        disabledMutation.mutate({ siteId, accountId: account.account_id, payload }, {
            onSuccess: () => {
                setPendingDisabledOverrides((current) => removeKeys(current, modelKeys));
                toast.success(payload.length === 1 ? (nextDisabled ? '模型已禁用' : '模型已启用') : `${payload.length} 个模型已${nextDisabled ? '禁用' : '启用'}`);
            },
            onError: (error) => {
                setPendingDisabledOverrides((current) => removeKeys(current, modelKeys));
                toast.error(translateSiteError(error, '更新模型禁用状态失败'));
            },
            onSettled: () => {
                setPendingModelKeys((current) => removePendingKeys(current, modelKeys));
            },
        });
    }, [pendingModelKeys, disabledMutation, siteId, account.account_id, translateSiteError]);

    const handleOpenCreateKey = (group: SiteChannelGroup) => {
        setCreatingGroup(group);
        setQuickCreateName('');
    };

    const handleCloseCreateKey = () => {
        if (createKeyMutation.isPending) return;
        setCreatingGroup(null);
        setQuickCreateName('');
    };

    const handleCreateKey = () => {
        if (!creatingGroup) return;

        createKeyMutation.mutate(
            {
                group_key: creatingGroup.group_key,
                name: quickCreateName.trim() || undefined,
            },
            {
                onSuccess: () => {
                    toast.success(`分组「${creatingGroup.group_name || creatingGroup.group_key}」已创建 Key 并完成同步`);
                    setCreatingGroup(null);
                    setQuickCreateName('');
                },
                onError: (error) => {
                    toast.error(translateSiteError(error, '快捷创建 Key 失败'));
                },
            },
        );
    };

    const handleOpenProjectedKeys = (group: SiteChannelGroup) => {
        const items = buildSourceKeyFormItems(group);
        setEditingProjectedGroup(group);
        setSourceKeyForm(items);
        setVisibleSourceKeyRows({});
    };

    const handleCloseProjectedKeys = () => {
        if (sourceKeyMutation.isPending) return;
        setEditingProjectedGroup(null);
        setSourceKeyForm([]);
        setVisibleSourceKeyRows({});
    };

    const handleOpenAdvancedSettings = (group: SiteChannelGroup) => {
        const form: Record<number, { auto_group: AutoGroupType; param_override: string }> = {};
        group.projected_channels.forEach((channel) => {
            form[channel.channel_id] = {
                auto_group: channel.auto_group,
                param_override: channel.param_override ?? '',
            };
        });
        setEditingAdvancedGroup(group);
        setSelectedAdvancedChannelId(group.projected_channels[0]?.channel_id ?? null);
        setAdvancedForm(form);
    };

    const handleCloseAdvancedSettings = () => {
        if (advancedMutation.isPending) return;
        setEditingAdvancedGroup(null);
        setSelectedAdvancedChannelId(null);
        setAdvancedForm({});
    };

    const handleOpenAddManualModels = (group: SiteChannelGroup) => {
        setAddingManualGroup(group);
        setManualModelsInput('');
        setManualModelRouteType('openai_chat');
    };

    const handleCloseAddManualModels = () => {
        if (addManualModelsMutation.isPending) return;
        setAddingManualGroup(null);
        setManualModelsInput('');
    };

    const parseManualModelNames = () => Array.from(new Set(manualModelsInput
        .split(/[\n,]+/)
        .map((item) => item.trim())
        .filter(Boolean)));

    const handleAddManualModels = () => {
        if (!addingManualGroup) return;
        const names = parseManualModelNames();
        if (names.length === 0) {
            toast.error('请填写模型名称');
            return;
        }
        const existing = new Set(addingManualGroup.models.map((model) => model.model_name));
        const duplicated = names.filter((name) => existing.has(name));
        if (duplicated.length > 0) {
            toast.error(`模型已存在：${duplicated.join(', ')}`);
            return;
        }
        addManualModelsMutation.mutate({
            group_key: addingManualGroup.group_key,
            models: names.map((name) => ({ model_name: name, route_type: manualModelRouteType })),
        }, {
            onSuccess: () => {
                toast.success(`已添加 ${names.length} 个自定义模型`);
                handleCloseAddManualModels();
            },
            onError: (error) => {
                toast.error(translateSiteError(error, '添加自定义模型失败'));
            },
        });
    };

    const handleDeleteManualModel = (model: SiteModelView) => {
        if (model.source !== 'manual') return;
        const modelKey = makeModelKey(model.group_key, model.model_name);
        if (deletingManualModelKey === modelKey) return;
        setDeletingManualModelKey(modelKey);
        deleteManualModelMutation.mutate({ group_key: model.group_key, model_name: model.model_name }, {
            onSuccess: () => toast.success('自定义模型已删除'),
            onError: (error) => toast.error(translateSiteError(error, '删除自定义模型失败')),
            onSettled: () => setDeletingManualModelKey((current) => (current === modelKey ? null : current)),
        });
    };

    const handleAdvancedAutoGroupChange = (channelId: number, value: AutoGroupType) => {
        setAdvancedForm((current) => ({
            ...current,
            [channelId]: { ...(current[channelId] ?? { auto_group: AutoGroupType.None, param_override: '' }), auto_group: value },
        }));
    };

    const handleAdvancedParamChange = (channelId: number, value: string) => {
        setAdvancedForm((current) => ({
            ...current,
            [channelId]: { ...(current[channelId] ?? { auto_group: AutoGroupType.None, param_override: '' }), param_override: value },
        }));
    };

    const validateAdvancedSettings = () => {
        for (const item of Object.values(advancedForm)) {
            const value = item.param_override.trim();
            if (!value) continue;
            try {
                const parsed = JSON.parse(value) as unknown;
                if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
                    return false;
                }
            } catch {
                return false;
            }
        }
        return true;
    };

    const selectedAdvancedChannel = editingAdvancedGroup?.projected_channels.find((channel) => channel.channel_id === selectedAdvancedChannelId)
        ?? editingAdvancedGroup?.projected_channels[0]
        ?? null;

    const handleSaveAdvancedSettings = () => {
        if (!editingAdvancedGroup) return;
        if (!validateAdvancedSettings()) {
            toast.error('参数覆盖必须是合法的 JSON 对象');
            return;
        }
        const payload = editingAdvancedGroup.projected_channels.map((channel) => ({
            channel_id: channel.channel_id,
            auto_group: advancedForm[channel.channel_id]?.auto_group ?? channel.auto_group,
            param_override: advancedForm[channel.channel_id]?.param_override?.trim() ?? '',
        }));
        advancedMutation.mutate(payload, {
            onSuccess: () => {
                toast.success('高级设置已保存');
                handleCloseAdvancedSettings();
            },
            onError: (error) => {
                toast.error(translateSiteError(error, '保存高级设置失败'));
            },
        });
    };

    const projectedKeyRowId = (item: SiteSourceKeyFormItem, index: number) => `${item.id ?? 'new'}-${index}`;

    const handleToggleProjectedKeyVisibility = (item: SiteSourceKeyFormItem, index: number) => {
        const rowId = projectedKeyRowId(item, index);
        setVisibleSourceKeyRows((current) => ({
            ...current,
            [rowId]: !current[rowId],
        }));
    };

    const handleProjectedKeyFieldChange = (index: number, patch: Partial<SiteSourceKeyFormItem>) => {
        setSourceKeyForm((current) => current.map((item, itemIndex) => itemIndex === index ? { ...item, ...patch } : item));
    };

    const handleAddProjectedKeyRow = () => {
        setSourceKeyForm((current) => ([
            ...current,
            {
                enabled: true,
                token: '',
                is_new: true,
                name: '',
                value_status: 'ready',
            },
        ]));
    };

    const handleRemoveProjectedKeyRow = (index: number) => {
        setSourceKeyForm((current) => current.filter((_, itemIndex) => itemIndex !== index));
    };

    const handleSaveProjectedKeys = () => {
        if (!editingProjectedGroup) return;
        const payload = buildSourceKeyUpdatePayload(editingProjectedGroup.group_key, editingProjectedGroup.source_keys, sourceKeyForm);
        if (!payload.keys_to_add?.length && !payload.keys_to_update?.length && !payload.keys_to_delete?.length) {
            toast.error('没有需要保存的 Key 变更');
            return;
        }
        sourceKeyMutation.mutate(payload, {
            onSuccess: () => {
                toast.success(`分组「${editingProjectedGroup.group_name || editingProjectedGroup.group_key}」的站点 Key 已更新`);
                setEditingProjectedGroup(null);
                setSourceKeyForm([]);
                setVisibleSourceKeyRows({});
            },
            onError: (error) => {
                toast.error(translateSiteError(error, '更新站点 Key 失败'));
            },
        });
    };

    const handleToggleDisabled = (model: SiteModelView) => {
        applyDisabledChange([model], !model.disabled);
    };

    const handleResetRoutes = () => {
        resetMutation.mutate(undefined, {
            onSuccess: () => {
                setPendingRouteOverrides({});
                toast.success('模型请求端点格式已重置');
            },
            onError: (error) => {
                toast.error(translateSiteError(error, '重置模型端点格式失败'));
            },
        });
    };

    const toggleQuickFilter = (filter: SiteChannelQuickFilter) => {
        const next = panelPreferences.quickFilters.includes(filter)
            ? panelPreferences.quickFilters.filter((item) => item !== filter)
            : QUICK_FILTER_OPTIONS.map((item) => item.key).filter((key) => key === filter || panelPreferences.quickFilters.includes(key));

        setQuickFilters(panelKey, next);
    };

    const handleSortChange = (field: SiteChannelTableSortField) => {
        const nextSort: SiteChannelTableSort = {
            field,
            order:
                panelPreferences.tableSort.field === field && panelPreferences.tableSort.order === 'asc'
                    ? 'desc'
                    : 'asc',
        };
        setTableSort(panelKey, nextSort);
    };

    const selectedVisibleCount = selectedModels.length;
    const activeGroupValue = activeFilter.kind === 'all' ? SITE_GROUP_FILTER_ALL_VALUE : activeFilter.groupKey;
    const activeGroup = activeFilter.kind === 'group'
        ? account.groups.find((group) => group.group_key === activeFilter.groupKey) ?? null
        : null;
    const activeGroupLabel = activeGroup ? (activeGroup.group_name || activeGroup.group_key) : '全部分组';
    const activeQuickFilterCount = panelPreferences.quickFilters.length;
    const pendingKeyGroups = useMemo(
        () => visibleGroups.filter((group) => !group.has_keys),
        [visibleGroups],
    );
    const projectedGroups = useMemo(
        () => visibleGroups.filter((group) => group.has_projected_channel),
        [visibleGroups],
    );
    const unsupportedRouteCount = useMemo(
        () => visibleModels.filter((model) => !isSupportedRouteType(model.route_type)).length,
        [visibleModels],
    );

    const handleGroupFilterChange = useCallback((value: string) => {
        setActiveFilter(value === SITE_GROUP_FILTER_ALL_VALUE ? SITE_GROUP_FILTER_ALL : createGroupFilter(value));
    }, []);

    const handleClearQuickFilters = useCallback(() => {
        setQuickFilters(panelKey, []);
    }, [panelKey, setQuickFilters]);

    const handleFocusAttention = useCallback(() => {
        if (panelPreferences.quickFilters.includes('attention')) return;
        const next = QUICK_FILTER_OPTIONS
            .map((item) => item.key)
            .filter((key) => key === 'attention' || panelPreferences.quickFilters.includes(key));
        setQuickFilters(panelKey, next);
    }, [panelKey, panelPreferences.quickFilters, setQuickFilters]);

    return (
        <div className="flex min-h-0 flex-1 flex-col gap-2.5">
            <div className="flex flex-none flex-col gap-2 rounded-2xl border border-border/70 bg-card/70 p-2.5">
                {accounts.length >= 2 ? (
                    <div className="flex items-center justify-between gap-3 border-b border-border/60 pb-2">
                        <div className="-mb-px max-w-full overflow-x-auto">
                            <div className="flex min-w-max items-baseline gap-5 px-0.5 pb-1">
                                {accounts.map((acc) => {
                                    const isActive = acc.account_id === activeAccountId;
                                    return (
                                        <button
                                            key={acc.account_id}
                                            ref={(node) => registerAccountTabRef(acc.account_id, node)}
                                            type="button"
                                            onClick={() => onSelectAccount(acc.account_id)}
                                            className={cn(
                                                'relative inline-flex items-baseline gap-1.5 pb-1 text-sm font-medium transition-colors',
                                                isActive
                                                    ? 'text-foreground'
                                                    : 'text-muted-foreground hover:text-foreground',
                                                highlightedAccountId === acc.account_id &&
                                                    'rounded-md ring-2 ring-primary/35 ring-offset-2 ring-offset-background',
                                            )}
                                        >
                                            <span className="truncate">{acc.account_name}</span>
                                            <span
                                                className={cn(
                                                    'size-1.5 shrink-0 rounded-full',
                                                    acc.enabled ? 'bg-emerald-500' : 'bg-destructive',
                                                )}
                                                aria-hidden
                                            />
                                            {isActive && (
                                                <motion.span
                                                    layoutId="site-account-tab-underline"
                                                    className="absolute -bottom-px left-0 right-0 h-0.5 rounded-full bg-primary"
                                                    transition={{ type: 'spring', stiffness: 320, damping: 30, mass: 0.8 }}
                                                />
                                            )}
                                        </button>
                                    );
                                })}
                            </div>
                        </div>

                        <button
                            type="button"
                            onClick={() =>
                                enableSiteAccount.mutate({
                                    id: account.account_id,
                                    enabled: !account.enabled,
                                })
                            }
                            disabled={enableSiteAccount.isPending}
                            className={cn(
                                'inline-flex h-7 shrink-0 cursor-pointer items-center gap-1 rounded-full border px-2.5 text-[11px] font-medium transition hover:opacity-80',
                                account.enabled
                                    ? 'border-emerald-500/30 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300'
                                    : 'border-destructive/30 bg-destructive/10 text-destructive',
                            )}
                        >
                            <Power className={cn('size-3', enableSiteAccount.isPending && 'animate-spin')} />
                            {account.enabled ? '账号启用' : '账号停用'}
                        </button>
                    </div>
                ) : null}

                <div className="flex flex-col gap-2 lg:flex-row lg:items-center">
                    <div className="flex flex-1 flex-col gap-2 sm:flex-row sm:items-center">
                        <Select value={activeGroupValue} onValueChange={handleGroupFilterChange}>
                            <SelectTrigger className="h-8 w-full rounded-2xl border-border/70 bg-background/80 sm:w-[18rem]">
                                <div className="flex min-w-0 items-center gap-2">
                                    <span className="text-xs text-muted-foreground">分组</span>
                                    <span className="truncate text-sm font-medium">{activeGroupLabel}</span>
                                </div>
                            </SelectTrigger>
                            <SelectContent align="start" className="rounded-2xl border border-border/70 bg-card">
                                <SelectItem value={SITE_GROUP_FILTER_ALL_VALUE} className="rounded-xl py-2">
                                    <div className="flex w-full min-w-0 items-center justify-between gap-3">
                                        <span className="truncate">全部分组</span>
                                        <span className="text-[11px] text-muted-foreground">{account.groups.length} 组</span>
                                    </div>
                                </SelectItem>
                                {account.groups.map((group) => (
                                    <SelectItem key={group.group_key} value={group.group_key} className="rounded-xl py-2">
                                        <div className="flex w-full min-w-0 items-start justify-between gap-3">
                                            <div className="min-w-0">
                                                <div className="truncate">{group.group_name || group.group_key}</div>
                                                <div className="text-[11px] text-muted-foreground">
                                                    {group.models.length} 模型 · Key {group.enabled_key_count}/{group.key_count}
                                                    {group.masked_pending_key_count > 0 ? ` · 待补全 ${group.masked_pending_key_count}` : ''}
                                                    {group.has_projected_channel ? ` · 投影 ${group.projected_keys.length}` : ''}
                                                </div>
                                            </div>
                                            {!group.has_keys ? (
                                                <span className="rounded-full bg-amber-500/15 px-1.5 py-0.5 text-[10px] text-amber-700 dark:text-amber-300">
                                                    待建
                                                </span>
                                            ) : group.masked_pending_key_count > 0 && group.enabled_key_count === 0 ? (
                                                <span className="rounded-full bg-amber-500/15 px-1.5 py-0.5 text-[10px] text-amber-700 dark:text-amber-300">
                                                    待补全
                                                </span>
                                            ) : null}
                                        </div>
                                    </SelectItem>
                                ))}
                            </SelectContent>
                        </Select>

                        <div className="relative min-w-0 flex-1">
                            <Search className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
                            <Input
                                value={modelSearchTerm}
                                onChange={(event) => setModelSearchTerm(event.target.value)}
                                placeholder="搜索模型名称、分组..."
                                className="h-8 rounded-2xl pl-9"
                            />
                        </div>
                    </div>

                    <div className="flex flex-wrap items-center gap-2">
                        <Button
                            type="button"
                            variant="outline"
                            className="h-8 rounded-2xl px-3"
                            onClick={() => activeGroup && handleOpenAddManualModels(activeGroup)}
                            disabled={!activeGroup}
                            title={activeGroup ? undefined : '请先选择具体分组'}
                        >
                            <Plus className="size-4" />
                            添加
                        </Button>

                        <Popover>
                            <PopoverTrigger asChild>
                                <Button type="button" variant="outline" className="h-8 rounded-2xl px-3">
                                    <SlidersHorizontal className="size-4" />
                                    {activeQuickFilterCount > 0 ? `筛选(${activeQuickFilterCount})` : '筛选'}
                                </Button>
                            </PopoverTrigger>
                            <PopoverContent align="end" className="w-60 rounded-2xl border border-border/70 bg-card p-3 shadow-xl">
                                <div className="space-y-3">
                                    <div className="text-xs font-medium text-muted-foreground">快速筛选</div>
                                    <div className="grid gap-2">
                                        {QUICK_FILTER_OPTIONS.map((option) => {
                                            const active = panelPreferences.quickFilters.includes(option.key);
                                            return (
                                                <button
                                                    key={option.key}
                                                    type="button"
                                                    onClick={() => toggleQuickFilter(option.key)}
                                                    className={cn(
                                                        'flex items-center justify-between rounded-xl border px-3 py-2 text-left text-sm transition',
                                                        active
                                                            ? 'border-primary/30 bg-primary/10 text-foreground'
                                                            : 'border-border bg-background hover:bg-muted/60',
                                                    )}
                                                >
                                                    <span>{option.label}</span>
                                                    {active ? <Check className="size-4 text-primary" /> : null}
                                                </button>
                                            );
                                        })}
                                    </div>
                                    {activeQuickFilterCount > 0 ? (
                                        <Button
                                            type="button"
                                            variant="ghost"
                                            size="sm"
                                            className="h-8 rounded-xl px-2"
                                            onClick={handleClearQuickFilters}
                                        >
                                            清空筛选
                                        </Button>
                                    ) : null}
                                </div>
                            </PopoverContent>
                        </Popover>

                        <Button
                            type="button"
                            variant="outline"
                            className="h-8 rounded-2xl px-3"
                            onClick={() => activeGroup && handleOpenAdvancedSettings(activeGroup)}
                            disabled={!activeGroup || activeGroup.projected_channels.length === 0}
                            title={!activeGroup ? '请先选择具体分组' : activeGroup.projected_channels.length === 0 ? '当前分组暂无投影渠道' : undefined}
                        >
                            <Settings className="size-4" />
                            高级
                        </Button>

                        <Popover>
                            <PopoverTrigger asChild>
                                <Button type="button" variant="outline" className="h-8 rounded-2xl px-3">
                                    <MoreHorizontal className="size-4" />
                                    更多
                                </Button>
                            </PopoverTrigger>
                            <PopoverContent align="end" className="w-64 rounded-2xl border border-border/70 bg-card p-2 shadow-xl">
                                <div className="space-y-1">
                                    <button
                                        type="button"
                                        onClick={() => setCompactMode(panelKey, !panelPreferences.compactMode)}
                                        className="flex w-full items-center justify-between rounded-xl px-3 py-2 text-left transition hover:bg-muted/60"
                                    >
                                        <div>
                                            <div className="text-sm font-medium text-foreground">紧凑模式</div>
                                            <div className="text-[11px] text-muted-foreground">压缩模型卡片和表格行高</div>
                                        </div>
                                        {panelPreferences.compactMode ? <Check className="size-4 text-primary" /> : null}
                                    </button>
                                </div>
                                <Button
                                    type="button"
                                    variant="outline"
                                    className="mt-2 h-8 w-full justify-start rounded-xl px-3"
                                    onClick={handleResetRoutes}
                                    disabled={resetMutation.isPending || hasPendingChanges}
                                >
                                    <RefreshCw className={cn('size-4', resetMutation.isPending && 'animate-spin')} />
                                    {resetMutation.isPending ? '重置中...' : '重置模型端点格式'}
                                </Button>
                            </PopoverContent>
                        </Popover>
                    </div>
                </div>

                {pendingKeyGroups.length > 0 || projectedGroups.length > 0 || unsupportedRouteCount > 0 || selectedVisibleCount > 0 ? (
                    <div className="flex min-h-8 flex-wrap items-center gap-2">
                        {pendingKeyGroups.length > 0 ? (
                            <Popover>
                                <PopoverTrigger asChild>
                                    <button
                                        type="button"
                                        className="inline-flex h-8 items-center gap-2 rounded-full border border-amber-500/30 bg-amber-500/10 px-3 text-xs font-medium text-amber-800 transition hover:bg-amber-500/15 dark:text-amber-200"
                                    >
                                        <CircleAlert className="size-3.5" />
                                        待建 Key {pendingKeyGroups.length} 组
                                    </button>
                                </PopoverTrigger>
                                <PopoverContent align="start" className="w-72 rounded-2xl border border-amber-500/30 bg-card p-3 shadow-xl">
                                    <div className="space-y-2">
                                        <div className="text-xs font-medium text-muted-foreground">未创建 Key 的分组</div>
                                        <div className="flex flex-wrap gap-2">
                                            {pendingKeyGroups.map((group) => (
                                                <Button
                                                    key={group.group_key}
                                                    type="button"
                                                    variant="outline"
                                                    size="sm"
                                                    className="rounded-full border-amber-500/30 bg-white/60 text-amber-800 hover:bg-white dark:bg-background/40 dark:text-amber-200"
                                                    onClick={() => handleOpenCreateKey(group)}
                                                    disabled={createKeyMutation.isPending}
                                                >
                                                    {group.group_name || group.group_key}
                                                    <span className="text-[10px] text-amber-700/80 dark:text-amber-200/80">
                                                        {createKeyMutation.isPending && creatingGroup?.group_key === group.group_key ? '创建中...' : '快捷创建'}
                                                    </span>
                                                </Button>
                                            ))}
                                        </div>
                                    </div>
                                </PopoverContent>
                            </Popover>
                        ) : null}

                        {visibleGroups.some((group) => group.masked_pending_key_count > 0 && group.enabled_key_count === 0) ? (
                            <button
                                type="button"
                                onClick={handleFocusAttention}
                                className="inline-flex h-8 items-center gap-2 rounded-full border border-amber-500/30 bg-amber-500/10 px-3 text-xs font-medium text-amber-800 transition hover:bg-amber-500/15 dark:text-amber-200"
                            >
                                <CircleAlert className="size-3.5" />
                                待补全文明文 Key
                            </button>
                        ) : null}

                        {projectedGroups.length > 0 ? (
                            <Popover>
                                <PopoverTrigger asChild>
                                    <button
                                        type="button"
                                        className="inline-flex h-8 items-center gap-2 rounded-full border border-border/70 bg-background/70 px-3 text-xs font-medium text-foreground transition hover:bg-muted/60"
                                    >
                                        <KeyRound className="size-3.5 text-primary" />
                                        投影 Key {projectedGroups.length} 组
                                    </button>
                                </PopoverTrigger>
                                <PopoverContent align="start" className="w-72 rounded-2xl border border-border/70 bg-card p-3 shadow-xl">
                                    <div className="space-y-2">
                                        <div className="text-xs font-medium text-muted-foreground">投影渠道 Key 管理</div>
                                        <div className="flex flex-wrap gap-2">
                                            {projectedGroups.map((group) => (
                                                <Button
                                                    key={`projected-${group.group_key}`}
                                                    type="button"
                                                    variant="outline"
                                                    size="sm"
                                                    className="rounded-full"
                                                    onClick={() => handleOpenProjectedKeys(group)}
                                                >
                                                    {group.group_name || group.group_key}
                                                    <span className="text-[10px] text-muted-foreground">{group.projected_keys.length} Keys</span>
                                                </Button>
                                            ))}
                                        </div>
                                    </div>
                                </PopoverContent>
                            </Popover>
                        ) : null}

                        {unsupportedRouteCount > 0 ? (
                            <button
                                type="button"
                                onClick={handleFocusAttention}
                                className="inline-flex h-8 items-center gap-2 rounded-full border border-amber-500/30 bg-amber-500/10 px-3 text-xs font-medium text-amber-800 transition hover:bg-amber-500/15 dark:text-amber-200"
                            >
                                <CircleAlert className="size-3.5" />
                                未识别端点 {unsupportedRouteCount}
                            </button>
                        ) : null}

                        {selectedVisibleCount > 0 ? (
                            <div className="ml-auto flex flex-wrap items-center gap-2">
                                <span className="text-xs font-medium text-foreground">已选 {selectedVisibleCount} 个</span>
                                <Select value={bulkMoveTarget} onValueChange={(value) => setBulkMoveTarget(value as SiteModelRouteType)}>
                                    <SelectTrigger className="h-7 w-[10rem] rounded-xl text-xs">
                                        <SelectValue placeholder="目标端点" />
                                    </SelectTrigger>
                                    <SelectContent className="rounded-xl">
                                        {SITE_ROUTE_COLUMN_ORDER.map((routeType) => (
                                            <SelectItem key={routeType} value={routeType}>
                                                {routeTypeLabel(routeType)}
                                            </SelectItem>
                                        ))}
                                    </SelectContent>
                                </Select>
                                <Button type="button" size="sm" className="h-7 rounded-xl px-2 text-xs" onClick={() => applyRouteChange(selectedModels, bulkMoveTarget)} disabled={hasPendingChanges}>
                                    移动
                                </Button>
                                <Button type="button" variant="outline" size="sm" className="h-7 rounded-xl px-2 text-xs" onClick={() => applyDisabledChange(selectedModels, false)} disabled={hasPendingChanges}>
                                    启用
                                </Button>
                                <Button type="button" variant="outline" size="sm" className="h-7 rounded-xl px-2 text-xs" onClick={() => applyDisabledChange(selectedModels, true)} disabled={hasPendingChanges}>
                                    停用
                                </Button>
                                <Button type="button" variant="ghost" size="sm" className="h-7 rounded-xl px-2 text-xs" onClick={() => setSelectedModelKeys(new Set())}>
                                    清空
                                </Button>
                            </div>
                        ) : null}
                    </div>
                ) : null}
            </div>

            <Dialog open={!!creatingGroup} onOpenChange={(open) => !open && handleCloseCreateKey()}>
                <DialogContent className="sm:max-w-md">
                    <DialogHeader>
                        <DialogTitle>快捷创建站点 Key</DialogTitle>
                        <DialogDescription>
                            为分组 {creatingGroup?.group_name || creatingGroup?.group_key || '-'} 在账号 {account.account_name} 下创建新 Key，并在创建后立即同步当前卡片。
                        </DialogDescription>
                    </DialogHeader>

                    <div className="space-y-3">
                        <div className="rounded-2xl border border-border/70 bg-muted/30 px-3 py-2 text-xs text-muted-foreground">
                            分组 Key：<span className="font-medium text-foreground">{creatingGroup?.group_key || '-'}</span>
                        </div>

                        <label className="grid gap-1.5 text-xs text-muted-foreground">
                            Key 名称（可选）
                            <Input
                                value={quickCreateName}
                                onChange={(event) => setQuickCreateName(event.target.value)}
                                placeholder="留空时自动生成"
                                disabled={createKeyMutation.isPending}
                                className="h-10 rounded-2xl"
                            />
                        </label>
                    </div>

                    <DialogFooter>
                        <Button
                            type="button"
                            variant="outline"
                            className="rounded-2xl"
                            onClick={handleCloseCreateKey}
                            disabled={createKeyMutation.isPending}
                        >
                            取消
                        </Button>
                        <Button
                            type="button"
                            className="rounded-2xl"
                            onClick={handleCreateKey}
                            disabled={createKeyMutation.isPending || !creatingGroup}
                        >
                            <RefreshCw className={cn('size-4', createKeyMutation.isPending && 'animate-spin')} />
                            {createKeyMutation.isPending ? '创建并同步中...' : '创建并同步 Key'}
                        </Button>
                    </DialogFooter>
                </DialogContent>
            </Dialog>

            <Dialog open={!!editingAdvancedGroup} onOpenChange={(open) => !open && handleCloseAdvancedSettings()}>
                <DialogContent className="max-h-[85vh] overflow-y-auto rounded-3xl sm:max-w-4xl">
                    <DialogHeader>
                        <DialogTitle>站点渠道高级设置</DialogTitle>
                        <DialogDescription>
                            配置分组 {editingAdvancedGroup?.group_name || editingAdvancedGroup?.group_key || '-'} 下各投影渠道的自动分组和参数覆盖。自定义 Header 请在站点设置中统一配置。
                        </DialogDescription>
                    </DialogHeader>
                    <div className="space-y-4">
                        {editingAdvancedGroup?.projected_channels.some((channel) => channel.global_override) ? (
                            <div className="rounded-2xl border border-amber-500/30 bg-amber-500/10 px-3 py-2 text-xs leading-relaxed text-amber-800 dark:text-amber-200">
                                全局投影渠道自动分组已开启，当前所有投影渠道会统一使用模糊匹配；这里保存的单独设置会在全局关闭后生效。
                            </div>
                        ) : null}

                        <div className="grid gap-4 lg:grid-cols-[16rem_1fr]">
                            <div className="space-y-2">
                                <div className="px-1 text-xs font-medium text-muted-foreground">投影渠道</div>
                                <div className="space-y-2">
                                    {editingAdvancedGroup?.projected_channels.map((channel) => {
                                        const active = selectedAdvancedChannel?.channel_id === channel.channel_id;
                                        return (
                                            <button
                                                key={channel.channel_id}
                                                type="button"
                                                onClick={() => setSelectedAdvancedChannelId(channel.channel_id)}
                                                className={cn(
                                                    'flex w-full items-center justify-between gap-3 rounded-2xl border px-3 py-3 text-left transition',
                                                    active
                                                        ? 'border-primary/30 bg-primary/10 text-foreground'
                                                        : 'border-border/60 bg-muted/10 hover:bg-muted/40',
                                                )}
                                            >
                                                <div className="min-w-0">
                                                    <div className="truncate text-sm font-medium">{routeTypeLabel(channel.route_type)}</div>
                                                    <div className="mt-0.5 truncate text-xs text-muted-foreground">#{channel.channel_id}</div>
                                                </div>
                                                {channel.global_override ? (
                                                    <Badge variant="outline" className="shrink-0 border-primary/30 bg-primary/10 px-1.5 text-[10px] text-primary">全局</Badge>
                                                ) : null}
                                            </button>
                                        );
                                    })}
                                </div>
                            </div>

                            {selectedAdvancedChannel ? (() => {
                                const channel = selectedAdvancedChannel;
                                const form = advancedForm[channel.channel_id] ?? { auto_group: channel.auto_group, param_override: channel.param_override ?? '' };
                                return (
                                    <div className="space-y-4 rounded-2xl border border-border/60 bg-muted/10 p-4">
                                        <div className="flex flex-wrap items-start justify-between gap-3">
                                            <div className="min-w-0">
                                                <div className="text-sm font-medium text-foreground">{routeTypeLabel(channel.route_type)}</div>
                                                <div className="mt-1 truncate text-xs text-muted-foreground">#{channel.channel_id} · {channel.channel_name}</div>
                                            </div>
                                            {channel.global_override ? (
                                                <Badge variant="outline" className="border-primary/30 bg-primary/10 text-primary">全局模糊匹配生效中</Badge>
                                            ) : null}
                                        </div>

                                        <div className="space-y-4">
                                            <div className="flex items-center justify-between gap-4">
                                                <span className="text-sm font-medium">自动分组</span>
                                                <Select value={String(form.auto_group)} onValueChange={(value) => handleAdvancedAutoGroupChange(channel.channel_id, Number(value) as AutoGroupType)}>
                                                    <SelectTrigger className="w-48 rounded-xl bg-background">
                                                        <SelectValue />
                                                    </SelectTrigger>
                                                    <SelectContent className="rounded-xl">
                                                        <SelectItem value={String(AutoGroupType.None)}>不自动分组</SelectItem>
                                                        <SelectItem value={String(AutoGroupType.Fuzzy)}>模糊匹配</SelectItem>
                                                        <SelectItem value={String(AutoGroupType.Exact)}>精确匹配</SelectItem>
                                                        <SelectItem value={String(AutoGroupType.Regex)}>按分组正则</SelectItem>
                                                    </SelectContent>
                                                </Select>
                                            </div>

                                            <label className="grid gap-2 text-sm">
                                                <span className="font-medium">参数覆盖 JSON</span>
                                                <textarea
                                                    value={form.param_override}
                                                    onChange={(event) => handleAdvancedParamChange(channel.channel_id, event.target.value)}
                                                    placeholder='留空表示清除；非空时必须是 JSON 对象，例如：{"temperature":0.7,"max_tokens":4096}'
                                                    className="min-h-40 rounded-xl border border-border bg-background px-3 py-2 text-sm text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                                                />
                                            </label>
                                        </div>
                                    </div>
                                );
                            })() : (
                                <div className="flex min-h-48 items-center justify-center rounded-2xl border border-dashed border-border/70 bg-muted/10 text-sm text-muted-foreground">
                                    当前分组暂无投影渠道
                                </div>
                            )}
                        </div>
                    </div>
                    <DialogFooter>
                        <Button type="button" variant="outline" className="rounded-xl" onClick={handleCloseAdvancedSettings} disabled={advancedMutation.isPending}>取消</Button>
                        <Button type="button" className="rounded-xl" onClick={handleSaveAdvancedSettings} disabled={advancedMutation.isPending || !editingAdvancedGroup}>
                            {advancedMutation.isPending ? '保存中...' : '保存'}
                        </Button>
                    </DialogFooter>
                </DialogContent>
            </Dialog>

            <Dialog open={!!addingManualGroup} onOpenChange={(open) => !open && handleCloseAddManualModels()}>
                <DialogContent className="max-h-[85vh] overflow-y-auto rounded-3xl sm:max-w-2xl">
                    <DialogHeader>
                        <DialogTitle className="flex items-center gap-2 text-2xl">
                            添加自定义模型
                        </DialogTitle>
                        <DialogDescription>
                            批量添加到分组 {addingManualGroup?.group_name || addingManualGroup?.group_key || '-'}。同组已存在的模型不能重复添加。
                        </DialogDescription>
                    </DialogHeader>
                    <div className="space-y-4">
                        <label className="grid gap-1.5 text-xs text-muted-foreground">
                            模型名称（支持换行或逗号分隔）
                            <textarea
                                value={manualModelsInput}
                                onChange={(event) => setManualModelsInput(event.target.value)}
                                placeholder={"gpt-4o\ngpt-4.1-mini"}
                                className="min-h-36 rounded-xl border border-border bg-background px-3 py-2 text-sm text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                            />
                        </label>
                        <label className="grid gap-1.5 text-xs text-muted-foreground">
                            端点格式
                            <Select value={manualModelRouteType} onValueChange={(value) => setManualModelRouteType(value as SiteModelRouteType)}>
                                <SelectTrigger className="h-10 rounded-xl bg-background"><SelectValue /></SelectTrigger>
                                <SelectContent className="rounded-xl">
                                    {SITE_ROUTE_COLUMN_ORDER.map((routeType) => (
                                        <SelectItem key={routeType} value={routeType}>{routeTypeLabel(routeType)}</SelectItem>
                                    ))}
                                </SelectContent>
                            </Select>
                        </label>
                    </div>
                    <DialogFooter>
                        <Button type="button" variant="outline" className="rounded-xl" onClick={handleCloseAddManualModels} disabled={addManualModelsMutation.isPending}>取消</Button>
                        <Button type="button" className="rounded-xl" onClick={handleAddManualModels} disabled={addManualModelsMutation.isPending || !addingManualGroup}>
                            {addManualModelsMutation.isPending ? '添加中...' : '添加'}
                        </Button>
                    </DialogFooter>
                </DialogContent>
            </Dialog>

            <Dialog open={!!editingProjectedGroup} onOpenChange={(open) => !open && handleCloseProjectedKeys()}>
                <DialogContent className="max-w-3xl">
                    <DialogHeader>
                        <DialogTitle>管理站点 Key</DialogTitle>
                        <DialogDescription>
                            分组 {editingProjectedGroup?.group_name || editingProjectedGroup?.group_key || '-'} 的站点 Key 真源会在保存后更新，并重新投影到所有托管渠道。
                        </DialogDescription>
                    </DialogHeader>

                    <div className="space-y-3">
                        <div className="rounded-2xl border border-border/70 bg-muted/30 px-3 py-2 text-xs text-muted-foreground">
                            投影渠道：{editingProjectedGroup?.projected_channel_ids.join(', ') || '-'}
                        </div>

                        <div className="max-h-[22rem] space-y-3 overflow-y-auto pr-1">
                            {sourceKeyForm.map((item, index) => (
                                <div key={projectedKeyRowId(item, index)} className="rounded-2xl border border-border/70 bg-background/80 p-3">
                                    {(() => {
                                        const rowId = projectedKeyRowId(item, index);
                                        const isVisible = item.is_new || Boolean(visibleSourceKeyRows[rowId]);

                                        return (
                                            <>
                                    <div className="flex items-center justify-between gap-2">
                                        <div className="text-xs text-muted-foreground">
                                            {item.id ? `站点 Key #${item.id}` : '新站点 Key'}
                                            {item.value_status === 'masked_pending' ? ' · 待补全' : ''}
                                        </div>
                                        <Button
                                            type="button"
                                            variant="ghost"
                                            size="sm"
                                            className="rounded-xl"
                                            onClick={() => handleRemoveProjectedKeyRow(index)}
                                            disabled={sourceKeyMutation.isPending}
                                        >
                                            删除
                                        </Button>
                                    </div>
                                    <div className="mt-3 grid gap-3 md:grid-cols-[auto,1fr,12rem]">
                                        <label className="flex items-center gap-2 text-xs text-muted-foreground">
                                            <input
                                                type="checkbox"
                                                checked={item.enabled}
                                                disabled={sourceKeyMutation.isPending}
                                                onChange={(event) => handleProjectedKeyFieldChange(index, { enabled: event.target.checked })}
                                                className="size-4 rounded border-border bg-background align-middle accent-primary"
                                            />
                                            启用
                                        </label>
                                        <label className="grid gap-1.5 text-xs text-muted-foreground">
                                            Key
                                            <div className="flex items-center gap-2">
                                                <Input
                                                    type={isVisible ? 'text' : 'password'}
                                                    value={item.token}
                                                    onChange={(event) => handleProjectedKeyFieldChange(index, { token: event.target.value })}
                                                    placeholder={item.id ? '点击眼睛查看或直接修改完整 Key' : '输入新的站点 Key'}
                                                    disabled={sourceKeyMutation.isPending}
                                                    className="h-10 rounded-2xl"
                                                />
                                                <Button
                                                    type="button"
                                                    variant="outline"
                                                    size="icon"
                                                    className="size-10 rounded-2xl shrink-0"
                                                    onClick={() => handleToggleProjectedKeyVisibility(item, index)}
                                                    disabled={sourceKeyMutation.isPending}
                                                    aria-label={isVisible ? '隐藏完整 Key' : '显示完整 Key'}
                                                    title={isVisible ? '隐藏完整 Key' : '显示完整 Key'}
                                                >
                                                    {isVisible ? <EyeOff className="size-4" /> : <Eye className="size-4" />}
                                                </Button>
                                            </div>
                                            {!isVisible && item.token_masked ? (
                                                <span className="text-[11px] text-muted-foreground">当前值：{item.token_masked}</span>
                                            ) : null}
                                        </label>
                                        <label className="grid gap-1.5 text-xs text-muted-foreground">
                                            名称
                                            <Input
                                                value={item.name}
                                                onChange={(event) => handleProjectedKeyFieldChange(index, { name: event.target.value })}
                                                placeholder="Key 名称"
                                                disabled={sourceKeyMutation.isPending}
                                                className="h-10 rounded-2xl"
                                            />
                                        </label>
                                    </div>
                                    {item.last_sync_at ? (
                                        <div className="mt-2 text-[11px] text-muted-foreground">
                                            上次同步：{new Date(item.last_sync_at).toLocaleString()}
                                        </div>
                                    ) : null}
                                            </>
                                        );
                                    })()}
                                </div>
                            ))}
                        </div>

                        <Button
                            type="button"
                            variant="outline"
                            className="rounded-2xl"
                            onClick={handleAddProjectedKeyRow}
                            disabled={sourceKeyMutation.isPending}
                        >
                            新增 Key
                        </Button>
                    </div>

                    <DialogFooter>
                        <Button
                            type="button"
                            variant="outline"
                            className="rounded-2xl"
                            onClick={handleCloseProjectedKeys}
                            disabled={sourceKeyMutation.isPending}
                        >
                            取消
                        </Button>
                        <Button
                            type="button"
                            className="rounded-2xl"
                            onClick={handleSaveProjectedKeys}
                            disabled={sourceKeyMutation.isPending || !editingProjectedGroup || !hasSourceKeyChanges(editingProjectedGroup.source_keys, sourceKeyForm)}
                        >
                            <RefreshCw className={cn('size-4', sourceKeyMutation.isPending && 'animate-spin')} />
                            {sourceKeyMutation.isPending ? '保存中...' : '保存站点 Key'}
                        </Button>
                    </DialogFooter>
                </DialogContent>
            </Dialog>

            {visibleModels.length === 0 ? (
                <div className="flex min-h-[18rem] flex-1 items-center justify-center rounded-3xl border border-dashed border-border/70 bg-muted/20 px-6 text-center text-sm text-muted-foreground">
                    当前筛选和搜索条件下没有匹配模型
                </div>
            ) : (
                <div className="flex min-h-0 flex-1 flex-col overflow-y-auto overflow-x-hidden rounded-3xl border border-border/70 bg-card/70">
                    <SiteChannelTableView
                        models={displayedModels}
                        hasMore={displayedModels.length < visibleModels.length}
                        onReachEnd={handleLoadMoreModels}
                        allVisibleSelected={allVisibleSelected}
                        pendingModelKeys={pendingModelKeys}
                        selectedModelKeys={selectedModelKeys}
                        compactMode={panelPreferences.compactMode}
                        tableSort={panelPreferences.tableSort}
                        highlightedModelKey={highlightedModelKey}
                        onToggleModelSelection={handleToggleModelSelection}
                        onToggleAllVisible={handleToggleAllVisible}
                        onSortChange={handleSortChange}
                        onMoveModel={(model, nextRouteType) => applyRouteChange([model], nextRouteType)}
                        onToggleDisabled={handleToggleDisabled}
                        onDeleteManualModel={handleDeleteManualModel}
                        onNavigateToChannel={onNavigateToChannel}
                        registerModelRef={registerModelRef}
                    />
                </div>
            )}
        </div>
    );
}

function SiteChannelDialog({
    card,
    jumpRequest,
    onJumpHandled,
    onNavigateToSite,
    onNavigateToSiteAccount,
    onNavigateToChannel,
}: {
    card: SiteChannelCard;
    jumpRequest: SiteChannelPendingJump | null;
    onJumpHandled: (requestId: number) => void;
    onNavigateToSite: () => void;
    onNavigateToSiteAccount: (accountId: number) => void;
    onNavigateToChannel: (channelId: number) => void;
}) {
    const { setIsOpen } = useMorphingDialog();
    const [activeAccountId, setActiveAccountId] = useState<number | null>(card.accounts[0]?.account_id ?? null);
    const [highlightedAccountId, setHighlightedAccountId] = useState<number | null>(null);
    const handledJumpRequestRef = useRef<number | null>(null);
    // Defer mounting the heavy SiteAccountPanel by one frame so the morph
    // animation can start immediately. The panel pulls in recharts, dnd, ~16
    // useStates and ~14 useMemos; rendering it synchronously while the FLIP
    // animation tries to measure layout is the main cause of the perceived
    // "click → wait → animate" delay (especially on top/middle cards).
    const [panelReady, setPanelReady] = useState(false);
    const accountTabRefs = useRef<Map<number, HTMLButtonElement>>(new Map());

    useEffect(() => {
        // Two-frame defer: frame 1 lets the morph animation start + first paint,
        // frame 2 actually mounts the panel content.
        let raf2 = 0;
        const raf1 = window.requestAnimationFrame(() => {
            raf2 = window.requestAnimationFrame(() => setPanelReady(true));
        });
        return () => {
            window.cancelAnimationFrame(raf1);
            if (raf2) window.cancelAnimationFrame(raf2);
        };
    }, []);

    const closeAndNavigate = useCallback((navigate: () => void) => {
        setIsOpen(false);
        window.requestAnimationFrame(() => {
            navigate();
        });
    }, [setIsOpen]);

    const handleOpenSiteBaseUrl = useCallback(() => {
        if (!card.base_url) return;
        window.open(card.base_url, '_blank', 'noopener,noreferrer');
    }, [card.base_url]);

    const resolvedAccount =
        card.accounts.find((account) => account.account_id === activeAccountId) ??
        card.accounts[0] ??
        null;

    const enableSiteAccount = useEnableSiteAccount();

    const setAccountTabRef = useCallback((accountId: number, node: HTMLButtonElement | null) => {
        if (node) {
            accountTabRefs.current.set(accountId, node);
            return;
        }
        accountTabRefs.current.delete(accountId);
    }, []);

    useEffect(() => {
        if (!jumpRequest) return;
        if (jumpRequest.target.siteId !== card.site_id) return;
        if (handledJumpRequestRef.current === jumpRequest.requestId) return;
        const target = jumpRequest.target;
        if (target.kind === 'site-channel-card') return;

        if (activeAccountId !== target.accountId) {
            const frameId = window.requestAnimationFrame(() => {
                setActiveAccountId(target.accountId);
            });
            return () => window.cancelAnimationFrame(frameId);
        }

        const node = accountTabRefs.current.get(target.accountId);
        const frameId = window.requestAnimationFrame(() => {
            if (node) {
                node.scrollIntoView({ behavior: 'smooth', block: 'nearest', inline: 'center' });
                setHighlightedAccountId(target.accountId);
                window.setTimeout(() => {
                    setHighlightedAccountId((current) =>
                        current === target.accountId ? null : current,
                    );
                }, 1800);
            }

            if (target.kind === 'site-channel-account') {
                handledJumpRequestRef.current = jumpRequest.requestId;
                onJumpHandled(jumpRequest.requestId);
            }
        });

        return () => window.cancelAnimationFrame(frameId);
    }, [jumpRequest, card.site_id, activeAccountId, onJumpHandled]);

    return (
        <div className="flex h-[88vh] flex-col overflow-hidden">
            <header className="flex flex-none items-center gap-2 border-b border-border/70 px-5 py-3 text-left sm:px-6">
                <MorphingDialogDescription className="sr-only">
                    站点渠道管理面板
                </MorphingDialogDescription>

                <MorphingDialogTitle className="flex min-w-0 flex-1 flex-wrap items-center gap-2 text-lg font-semibold sm:text-xl">
                    <span className="truncate">{card.site_name}</span>
                    <Badge variant="outline" className="h-6 px-2 text-[11px]">
                        {platformLabel(card.platform)}
                    </Badge>
                    <Badge
                        variant="outline"
                        className={cn(
                            'h-6 px-2 text-[11px]',
                            card.enabled
                                ? 'border-emerald-500/30 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300'
                                : 'border-destructive/30 bg-destructive/10 text-destructive',
                        )}
                    >
                        {card.enabled ? '站点启用' : '站点停用'}
                    </Badge>
                    {resolvedAccount && card.accounts.length <= 1 ? (
                        <>
                            <span className="text-sm font-normal text-muted-foreground">
                                · {resolvedAccount.account_name}
                            </span>
                            <button
                                type="button"
                                onClick={() =>
                                    enableSiteAccount.mutate({
                                        id: resolvedAccount.account_id,
                                        enabled: !resolvedAccount.enabled,
                                    })
                                }
                                disabled={enableSiteAccount.isPending}
                                className={cn(
                                    'inline-flex h-6 cursor-pointer items-center gap-1 rounded-full border px-2 text-[11px] font-medium transition hover:opacity-80',
                                    resolvedAccount.enabled
                                        ? 'border-emerald-500/30 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300'
                                        : 'border-destructive/30 bg-destructive/10 text-destructive',
                                )}
                            >
                                <Power className={cn('size-3', enableSiteAccount.isPending && 'animate-spin')} />
                                {resolvedAccount.enabled ? '账号启用' : '账号停用'}
                            </button>
                        </>
                    ) : null}
                </MorphingDialogTitle>

                <div className="flex flex-none items-center gap-1">
                    <Button
                        type="button"
                        variant="outline"
                        size="icon"
                        className="size-8 rounded-xl"
                        onClick={handleOpenSiteBaseUrl}
                        disabled={!card.base_url}
                        aria-label="打开站点"
                        title="打开站点"
                    >
                        <ExternalLink className="size-4" />
                    </Button>
                    <Button
                        type="button"
                        variant="outline"
                        size="icon"
                        className="size-8 rounded-xl"
                        onClick={() => closeAndNavigate(onNavigateToSite)}
                        aria-label="站点页"
                        title="站点页"
                    >
                        <Globe2 className="size-4" />
                    </Button>
                    {resolvedAccount ? (
                        <Button
                            type="button"
                            variant="outline"
                            size="icon"
                            className="size-8 rounded-xl"
                            onClick={() => closeAndNavigate(() => onNavigateToSiteAccount(resolvedAccount.account_id))}
                            aria-label="站点页账号"
                            title="站点页账号"
                        >
                            <Waypoints className="size-4" />
                        </Button>
                    ) : null}
                </div>
            </header>

            <div className="flex min-h-0 flex-1 flex-col overflow-hidden px-5 py-3 sm:px-6">
                {resolvedAccount ? (
                    panelReady ? (
                        <SiteAccountPanel
                            key={resolvedAccount.account_id}
                            siteId={card.site_id}
                            account={resolvedAccount}
                            accounts={card.accounts}
                            activeAccountId={activeAccountId}
                            onSelectAccount={setActiveAccountId}
                            highlightedAccountId={highlightedAccountId}
                            registerAccountTabRef={setAccountTabRef}
                            jumpRequest={jumpRequest}
                            onJumpHandled={onJumpHandled}
                            onNavigateToChannel={(channelId) => closeAndNavigate(() => onNavigateToChannel(channelId))}
                        />
                    ) : (
                        <div className="min-h-0 flex-1 overflow-y-auto">
                            <SiteAccountPanelSkeleton />
                        </div>
                    )
                ) : (
                    <div className="flex min-h-[16rem] flex-1 items-center justify-center rounded-3xl border border-dashed border-border/70 bg-muted/20 text-sm text-muted-foreground">
                        当前站点没有可管理的账号
                    </div>
                )}
            </div>
        </div>
    );
}

function SiteAccountPanelSkeleton() {
    // Lightweight skeleton shown for one frame while the morph animation starts.
    // Keeps the dialog body roughly the same height so morph layout doesn't jump
    // when SiteAccountPanel mounts. Pure CSS, no framer-motion / recharts / dnd.
    return (
        <div className="space-y-4">
            <div className="flex flex-wrap gap-2">
                <div className="h-9 w-32 animate-pulse rounded-2xl bg-muted/50" />
                <div className="h-9 w-24 animate-pulse rounded-2xl bg-muted/50" />
                <div className="h-9 w-28 animate-pulse rounded-2xl bg-muted/50" />
                <div className="ml-auto h-9 w-44 animate-pulse rounded-2xl bg-muted/50" />
            </div>
            <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
                {Array.from({ length: 4 }).map((_, idx) => (
                    <div key={idx} className="h-40 animate-pulse rounded-3xl border border-border/70 bg-muted/40" />
                ))}
            </div>
            <div className="h-72 animate-pulse rounded-3xl border border-border/70 bg-muted/40" />
        </div>
    );
}

function SiteCardImpl({
    card,
    layout,
    jumpRequest,
    highlighted,
    registerCardRef,
    onJumpHandled,
    requestJump,
}: {
    card: SiteChannelCard;
    layout: 'grid' | 'list';
    jumpRequest: SiteChannelPendingJump | null;
    highlighted: boolean;
    registerCardRef: (siteId: number, node: HTMLDivElement | null) => void;
    onJumpHandled: (requestId: number) => void;
    requestJump: (target: JumpTarget) => void;
}) {
    const summary = useMemo(() => collectSiteSummary(card), [card]);
    const runtime = useMemo(() => collectSiteRuntimeSummary(card), [card]);
    const tCard = useTranslations('siteChannel.card');
    const tMetrics = useTranslations('siteChannel.card.metrics');
    const locale = useSettingStore((s) => s.locale);
    const lastUsedText = runtime.lastRequestAt
        ? dayjs(runtime.lastRequestAt * 1000).locale(DAYJS_LOCALE_MAP[locale]).fromNow()
        : null;
    const totalRequestsFmt = formatCount(runtime.totalRequests).formatted;
    const successFmt = formatCount(runtime.successCount).formatted;
    const failureFmt = formatCount(runtime.failureCount).formatted;
    const costFmt = formatMoney(runtime.totalCost).formatted;

    // Stable navigation callbacks. Building these inside SiteCard (rather than
    // inside SiteChannelGrid.renderCard) keeps SiteCard's prop identity stable
    // across grid re-renders, so memo() can actually skip work for cards whose
    // own data didn't change.
    const onNavigateToSite = useCallback(
        () => requestJump({ kind: 'site-card', siteId: card.site_id }),
        [requestJump, card.site_id],
    );
    const onNavigateToSiteAccount = useCallback(
        (accountId: number) => requestJump({ kind: 'site-account', siteId: card.site_id, accountId }),
        [requestJump, card.site_id],
    );
    const onNavigateToChannel = useCallback(
        (channelId: number) => requestJump({ kind: 'channel-card', channelId }),
        [requestJump],
    );

    return (
        <MorphingDialog>
            <div
                ref={(node) => registerCardRef(card.site_id, node)}
                className={cn(
                    'h-full rounded-[1.75rem] transition-all',
                    highlighted && 'ring-2 ring-primary/35 ring-offset-2 ring-offset-background',
                )}
            >
                <MorphingDialogTrigger className="h-full w-full">
                    <article
                        className="flex h-full w-full flex-col gap-4 rounded-3xl border border-border/70 bg-card p-4 text-left transition hover:border-primary/20 hover:bg-card/90"
                    >
                        <header className="flex items-center justify-between gap-3">
                            <div className="flex min-w-0 flex-1 items-center gap-2">
                                <span
                                    className={cn(
                                        'inline-block size-2 shrink-0 rounded-full',
                                        card.enabled
                                            ? 'bg-emerald-500'
                                            : 'bg-destructive',
                                    )}
                                    title={tCard(card.enabled ? 'statusEnabled' : 'statusDisabled')}
                                />
                                <div className="truncate text-lg font-bold">{card.site_name}</div>
                            </div>
                            <div className="flex shrink-0 flex-wrap items-center justify-end gap-2">
                                <Badge variant="outline" className="h-6 px-2 text-[11px]">
                                    {platformLabel(card.platform)}
                                </Badge>
                                {runtime.maskedPendingKeys > 0 ? (
                                    <Badge variant="outline" className="h-6 border-amber-500/30 bg-amber-500/10 px-2 text-[11px] text-amber-700 dark:text-amber-300">
                                        {tCard('maskedPending', { n: runtime.maskedPendingKeys })}
                                    </Badge>
                                ) : null}
                            </div>
                        </header>

                        <dl className={cn('grid gap-2', layout === 'list' ? 'grid-cols-5' : 'grid-cols-2')}>
                            {layout === 'list' ? (
                                <div className="rounded-2xl border border-border/70 bg-background/80 p-2">
                                    <dt className="mb-1 flex items-center gap-1 text-xs text-muted-foreground">
                                        <MessageSquare className="size-3.5 text-primary" />
                                        {tMetrics('totalRequests')}
                                    </dt>
                                    <dd className="text-sm font-semibold tabular-nums">
                                        {totalRequestsFmt.value}
                                        {totalRequestsFmt.unit && (
                                            <span className="ml-1 text-xs font-normal text-muted-foreground">{totalRequestsFmt.unit}</span>
                                        )}
                                    </dd>
                                </div>
                            ) : null}
                            <div className="rounded-2xl border border-border/70 bg-background/80 p-2">
                                <dt className="mb-1 flex items-center gap-1 text-xs text-muted-foreground">
                                    <CheckCircle2 className="size-3.5 text-emerald-500" />
                                    {tMetrics('successRequests')}
                                </dt>
                                <dd className="text-sm font-semibold tabular-nums">
                                    {successFmt.value}
                                    {successFmt.unit && (
                                        <span className="ml-1 text-xs font-normal text-muted-foreground">{successFmt.unit}</span>
                                    )}
                                </dd>
                            </div>
                            <div className="rounded-2xl border border-border/70 bg-background/80 p-2">
                                <dt className="mb-1 flex items-center gap-1 text-xs text-muted-foreground">
                                    <XCircle className="size-3.5 text-destructive" />
                                    {tMetrics('failedRequests')}
                                </dt>
                                <dd className="text-sm font-semibold tabular-nums">
                                    {failureFmt.value}
                                    {failureFmt.unit && (
                                        <span className="ml-1 text-xs font-normal text-muted-foreground">{failureFmt.unit}</span>
                                    )}
                                </dd>
                            </div>
                            <div className="rounded-2xl border border-border/70 bg-background/80 p-2">
                                <dt className="mb-1 flex items-center gap-1 text-xs text-muted-foreground">
                                    <DollarSign className="size-3.5 text-primary" />
                                    {tMetrics('totalCost')}
                                </dt>
                                <dd className="text-sm font-semibold tabular-nums">
                                    {costFmt.value}
                                    {costFmt.unit && (
                                        <span className="ml-1 text-xs font-normal text-muted-foreground">{costFmt.unit}</span>
                                    )}
                                </dd>
                            </div>
                            <div className="rounded-2xl border border-border/70 bg-background/80 p-2">
                                <dt className="mb-1 flex items-center gap-1 text-xs text-muted-foreground">
                                    <Clock className="size-3.5 text-primary" />
                                    {tMetrics('lastRequestAt')}
                                </dt>
                                <dd className="text-sm font-semibold tabular-nums">
                                    {lastUsedText ?? <span className="text-muted-foreground">—</span>}
                                </dd>
                            </div>
                        </dl>

                        {summary.routeCounts.size > 0 ? (
                            <div className="flex flex-1 flex-wrap content-center gap-2">
                                {SITE_ROUTE_DISPLAY_ORDER.filter((routeType) => (summary.routeCounts.get(routeType) ?? 0) > 0).map((routeType) => (
                                    <Badge key={routeType} variant="outline" className={cn('h-6 shrink-0 px-2 text-[11px]', getRouteTypeTone(routeType))}>
                                        {SHORT_ROUTE_LABEL[routeType] ?? routeTypeLabel(routeType)}
                                        <span className="ml-1">{summary.routeCounts.get(routeType)}</span>
                                    </Badge>
                                ))}
                            </div>
                        ) : (
                            <div className="flex flex-1 items-center justify-center text-xs text-muted-foreground">{tCard('noRouteDistribution')}</div>
                        )}
                    </article>
                </MorphingDialogTrigger>
            </div>

            <MorphingDialogContainer>
                <MorphingDialogContent className="max-w-[min(96vw,92rem)] w-[min(96vw,92rem)] overflow-hidden rounded-[2rem] bg-background max-h-[90vh]">
                    <SiteChannelDialog
                        card={card}
                        jumpRequest={jumpRequest?.target.siteId === card.site_id ? jumpRequest : null}
                        onJumpHandled={() => {}}
                        onNavigateToSite={onNavigateToSite}
                        onNavigateToSiteAccount={onNavigateToSiteAccount}
                        onNavigateToChannel={onNavigateToChannel}
                    />
                </MorphingDialogContent>
            </MorphingDialogContainer>

            <SiteCardJumpWatcher jumpRequest={jumpRequest} siteId={card.site_id} onJumpHandled={onJumpHandled} />
        </MorphingDialog>
    );
}

const SiteCard = memo(SiteCardImpl);

function SiteCardJumpWatcher({
    jumpRequest,
    siteId,
    onJumpHandled,
}: {
    jumpRequest: SiteChannelPendingJump | null;
    siteId: number;
    onJumpHandled: (requestId: number) => void;
}) {
    const { isOpen, setIsOpen } = useMorphingDialog();
    const handledRequestRef = useRef<number | null>(null);
    const openedRequestRef = useRef<number | null>(null);
    const onJumpHandledRef = useRef(onJumpHandled);

    useEffect(() => {
        onJumpHandledRef.current = onJumpHandled;
    }, [onJumpHandled]);

    useEffect(() => {
        if (!jumpRequest) {
            handledRequestRef.current = null;
            return;
        }
        if (jumpRequest.target.siteId !== siteId) return;
        if (jumpRequest.target.kind === 'site-channel-card') return;
        if (isOpen) return;
        if (handledRequestRef.current === jumpRequest.requestId) return;

        const requestId = jumpRequest.requestId;
        const frameId = window.requestAnimationFrame(() => {
            handledRequestRef.current = requestId;
            openedRequestRef.current = requestId;
            setIsOpen(true);
        });
        return () => window.cancelAnimationFrame(frameId);
    }, [jumpRequest, siteId, isOpen, setIsOpen]);

    useEffect(() => {
        if (isOpen) return;
        const openedRequestId = openedRequestRef.current;
        if (openedRequestId === null) return;

        const timer = window.setTimeout(() => {
            if (openedRequestRef.current !== openedRequestId) return;
            openedRequestRef.current = null;
            onJumpHandledRef.current(openedRequestId);
        }, 260);

        return () => window.clearTimeout(timer);
    }, [isOpen]);

    useEffect(() => {
        return () => {
            const openedRequestId = openedRequestRef.current;
            if (openedRequestId === null) return;
            openedRequestRef.current = null;
            onJumpHandledRef.current(openedRequestId);
        };
    }, []);

    return null;
}

export function SiteChannelCompletionAction() {
    const { data } = useSiteChannelList();
    const [completionDialogOpen, setCompletionDialogOpen] = useState(false);

    const pendingCompletionSites = useMemo(
        () => collectPendingCompletionSites(data ?? []),
        [data],
    );
    const totalPendingCompletionCount = useMemo(
        () => pendingCompletionSites.reduce((sum, site) => sum + site.pending_count, 0),
        [pendingCompletionSites],
    );
    const effectiveCompletionDialogOpen = completionDialogOpen && totalPendingCompletionCount > 0;

    if (totalPendingCompletionCount === 0) return null;

    return (
        <>
            <Button
                type="button"
                variant="outline"
                className="h-10 rounded-2xl px-3"
                onClick={() => setCompletionDialogOpen(true)}
            >
                <KeyRound className="size-4 text-primary" />
                统一补全 Key
                <Badge variant="outline" className="h-5 px-1.5 text-[10px]">
                    {totalPendingCompletionCount}
                </Badge>
            </Button>
            <UnifiedCompletionDialog
                open={effectiveCompletionDialogOpen}
                onOpenChange={setCompletionDialogOpen}
                sites={pendingCompletionSites}
            />
        </>
    );
}

export function SiteChannelSection({
    searchTerm,
    filter,
    sortField,
    sortOrder,
    layout,
}: {
    searchTerm: string;
    filter: ChannelFilter;
    sortField: ToolbarSortField;
    sortOrder: ToolbarSortOrder;
    layout: 'grid' | 'list';
}) {
    const t = useTranslations();
    const locale = useSettingStore((state) => state.locale);
    const { data, isLoading, error } = useSiteChannelList();
    const pendingJump = useJumpStore((state) => state.pending);
    const clearPending = useJumpStore((state) => state.clearPending);
    const requestJump = useJumpStore((state) => state.requestJump);
    const [highlightedSiteId, setHighlightedSiteId] = useState<number | null>(null);
    const siteCardRefs = useRef<Map<number, HTMLDivElement>>(new Map());

    const pendingSiteChannelJump = pendingJump && isSiteChannelJumpTarget(pendingJump.target)
        ? pendingJump as SiteChannelPendingJump
        : null;
    const forcedSiteId = pendingSiteChannelJump?.target.siteId ?? null;

    const registerCardRef = useCallback((siteId: number, node: HTMLDivElement | null) => {
        if (node) {
            siteCardRefs.current.set(siteId, node);
            return;
        }
        siteCardRefs.current.delete(siteId);
    }, []);

    const cards = useMemo(() => {
        const term = searchTerm.toLowerCase().trim();
        return (data ?? [])
            .filter((card) => card.account_count > 0)
            .filter((card) => {
                if (card.site_id === forcedSiteId) return true;
                if (!term) return true;

                const accountNames = card.accounts.map((account) => account.account_name.toLowerCase());
                return card.site_name.toLowerCase().includes(term) || accountNames.some((name) => name.includes(term));
            })
            .filter((card) => {
                if (card.site_id === forcedSiteId) return true;
                if (filter === 'enabled') return card.enabled;
                if (filter === 'disabled') return !card.enabled;
                return true;
            })
            .sort((a, b) => {
                // Pin the jump target to the top so the virtualized list keeps
                // it mounted in the initial overscan window. Without this, the
                // jump-to-card useEffect below would no-op when the target is
                // outside the rendered window (registerCardRef never fires for
                // off-screen items, so siteCardRefs.get() returns null).
                if (forcedSiteId !== null) {
                    if (a.site_id === forcedSiteId) return -1;
                    if (b.site_id === forcedSiteId) return 1;
                }
                const diff = sortField === 'name'
                    ? a.site_name.localeCompare(b.site_name)
                    : a.site_id - b.site_id;
                return sortOrder === 'asc' ? diff : -diff;
            });
    }, [data, searchTerm, filter, sortField, sortOrder, forcedSiteId]);

    useEffect(() => {
        if (!pendingSiteChannelJump) return;
        const node = siteCardRefs.current.get(pendingSiteChannelJump.target.siteId);
        if (!node) return;

        const timer = window.setTimeout(() => {
            node.scrollIntoView({ behavior: 'smooth', block: 'center' });
            setHighlightedSiteId(pendingSiteChannelJump.target.siteId);
            window.setTimeout(() => {
                setHighlightedSiteId((current) =>
                    current === pendingSiteChannelJump.target.siteId ? null : current,
                );
            }, 1800);

            if (pendingSiteChannelJump.target.kind === 'site-channel-card') {
                clearPending(pendingSiteChannelJump.requestId);
            }
        }, 80);

        return () => window.clearTimeout(timer);
    }, [pendingSiteChannelJump, clearPending, cards.length]);

    if (isLoading) {
        return (
            <section className={cn('grid gap-4', layout === 'list' ? 'grid-cols-1' : 'md:grid-cols-2 xl:grid-cols-3')}>
                {Array.from({ length: layout === 'list' ? 2 : 3 }).map((_, index) => (
                    <div key={index} className="h-56 animate-pulse rounded-3xl border border-border/70 bg-muted/40" />
                ))}
            </section>
        );
    }

    if (error) {
        return (
            <section className="rounded-3xl border border-destructive/30 bg-destructive/10 px-4 py-3 text-sm text-destructive">
                站点渠道加载失败：{translateSiteMessage(locale, error.message, t)}
            </section>
        );
    }

    if (cards.length === 0) {
        return null;
    }

    return <SiteChannelGrid
        cards={cards}
        layout={layout}
        pendingSiteChannelJump={pendingSiteChannelJump}
        highlightedSiteId={highlightedSiteId}
        registerCardRef={registerCardRef}
        clearPending={clearPending}
        requestJump={requestJump}
    />;
}

function SiteChannelGrid({
    cards,
    layout,
    pendingSiteChannelJump,
    highlightedSiteId,
    registerCardRef,
    clearPending,
    requestJump,
}: {
    cards: SiteChannelCard[];
    layout: 'grid' | 'list';
    pendingSiteChannelJump: SiteChannelPendingJump | null;
    highlightedSiteId: number | null;
    registerCardRef: (siteId: number, node: HTMLDivElement | null) => void;
    clearPending: (requestId?: number) => void;
    requestJump: (target: JumpTarget) => void;
}) {
    const columnCompute = useCallback((width: number) => {
        if (layout === 'list') return 1;
        const MIN_CARD_WIDTH = 320;
        const GUTTER = 16;
        const cols = Math.floor((width + GUTTER) / (MIN_CARD_WIDTH + GUTTER));
        return Math.max(1, Math.min(6, cols));
    }, [layout]);

    const renderCard = useCallback((card: SiteChannelCard) => (
        <SiteCard
            key={card.site_id}
            card={card}
            layout={layout}
            jumpRequest={pendingSiteChannelJump?.target.siteId === card.site_id ? pendingSiteChannelJump : null}
            highlighted={highlightedSiteId === card.site_id}
            registerCardRef={registerCardRef}
            onJumpHandled={clearPending}
            requestJump={requestJump}
        />
    ), [layout, pendingSiteChannelJump, highlightedSiteId, registerCardRef, clearPending, requestJump]);

    return (
        <VirtualizedGrid
            items={cards}
            layout={layout}
            columns={columnCompute}
            estimateItemHeight={240}
            getItemKey={(card) => `site-channel-${card.site_id}`}
            renderItem={renderCard}
        />
    );
}
