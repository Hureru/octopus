'use client';

import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useTranslations } from 'next-intl';
import {
    DragDropContext,
    Draggable,
    Droppable,
    type DropResult,
} from '@hello-pangea/dnd';
import {
    ArrowUpDown,
    Check,
    CircleAlert,
    CircleOff,
    Eye,
    EyeOff,
    ExternalLink,
    FolderTree,
    Globe2,
    GripVertical,
    History,
    KeyRound,
    LayoutGrid,
    List,
    MoreHorizontal,
    Power,
    RefreshCw,
    Search,
    SlidersHorizontal,
    Sparkles,
    UserRound,
    Waypoints,
} from 'lucide-react';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import {
    Dialog,
    DialogContent,
    DialogDescription,
    DialogFooter,
    DialogHeader,
    DialogTitle,
} from '@/components/ui/dialog';
import { Input } from '@/components/ui/input';
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import {
    Table,
    TableBody,
    TableCell,
    TableHead,
    TableHeader,
    TableRow,
} from '@/components/ui/table';
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/animate-ui/components/animate/tooltip';
import { toast } from '@/components/common/Toast';
import { cn } from '@/lib/utils';
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
    useResetSiteChannelModelRoutes,
    useSiteChannelList,
    useUpdateAnySiteSourceKeys,
    useUpdateSiteSourceKeys,
    useUpdateSiteChannelModelDisabled,
    useUpdateSiteChannelModelRoutes,
} from '@/api/endpoints/site-channel';
import {
    ROUTE_COLUMN_KEY_PREFIX,
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
import { useJumpStore, type PendingJump, type SiteChannelJumpTarget, isSiteChannelJumpTarget } from '@/stores/jump';
import { useEnableSiteAccount } from '@/api/endpoints/site';
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

function findModel(account: SiteChannelAccount, modelKey: string) {
    for (const group of account.groups) {
        for (const model of group.models) {
            if (makeModelKey(group.group_key, model.model_name) === modelKey) {
                return { group, model };
            }
        }
    }

    return null;
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

function canAssignRouteType(routeType: SiteModelRouteType) {
    return isSupportedRouteType(routeType);
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

    return (
        <div className="w-[22rem] space-y-3 p-4 text-left">
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

            {model.history?.recent?.length ? (
                <div className="space-y-2">
                    {model.history.recent.map((item, index) => (
                        <div
                            key={`${item.time}-${item.channel_id}-${item.request_model}-${item.actual_model}-${index}`}
                            className="rounded-xl border border-border/60 bg-background/80 px-3 py-2"
                        >
                            <div className="flex items-center justify-between gap-2 text-[11px]">
                                <span className={cn('font-medium', item.success ? 'text-emerald-600 dark:text-emerald-300' : 'text-destructive')}>
                                    {item.success ? '成功' : '失败'}
                                </span>
                                <span className="text-muted-foreground">{formatHistoryTime(item.time)}</span>
                            </div>
                            <div className="mt-1 flex flex-wrap items-center gap-1 text-[10px] text-muted-foreground">
                                <Badge variant="outline" className="h-5 px-1.5 text-[10px]">
                                    {routeTypeLabel(item.route_type)}
                                </Badge>
                                <Badge variant="outline" className="h-5 px-1.5 text-[10px]">
                                    #{item.channel_id}
                                </Badge>
                                <span className="truncate">{item.channel_name}</span>
                            </div>
                            <div className="mt-2 text-[11px] text-foreground">
                                <span className="font-medium">{item.request_model || '-'}</span>
                                <span className="mx-1 text-muted-foreground">→</span>
                                <span>{item.actual_model || item.request_model || '-'}</span>
                            </div>
                            {item.error ? (
                                <div className="mt-1 line-clamp-2 text-[10px] text-destructive">{item.error}</div>
                            ) : null}
                        </div>
                    ))}
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

function RouteColumn({
    routeType,
    models,
    pendingModelKeys,
    selectedModelKeys,
    compactMode,
    canCollapseEmpty,
    onToggleModelSelection,
    onToggleColumnSelection,
    onMoveModel,
    onToggleDisabled,
    onNavigateToChannel,
    registerModelRef,
    highlightedModelKey,
    onCollapseEmpty,
}: {
    routeType: SiteModelRouteType;
    models: SiteModelView[];
    pendingModelKeys: Set<string>;
    selectedModelKeys: Set<string>;
    compactMode: boolean;
    canCollapseEmpty: boolean;
    onToggleModelSelection: (modelKey: string, checked: boolean) => void;
    onToggleColumnSelection: (modelKeys: string[], checked: boolean) => void;
    onMoveModel: (model: SiteModelView, routeType: SiteModelRouteType) => void;
    onToggleDisabled: (model: SiteModelView) => void;
    onNavigateToChannel: (channelId: number) => void;
    registerModelRef: (modelKey: string, node: HTMLElement | null) => void;
    highlightedModelKey: string | null;
    onCollapseEmpty?: () => void;
}) {
    const modelKeys = models.map((model) => makeModelKey(model.group_key, model.model_name));
    const allSelected = modelKeys.length > 0 && modelKeys.every((key) => selectedModelKeys.has(key));

    return (
        <Droppable droppableId={routeType}>
            {(provided, snapshot) => (
                <div
                    ref={provided.innerRef}
                    {...provided.droppableProps}
                    className={cn(
                        'flex min-h-[28rem] w-[18rem] flex-col rounded-3xl border border-border/70 bg-card/70 p-3 transition',
                        snapshot.isDraggingOver && 'border-primary/40 bg-primary/5',
                    )}
                >
                    <div className="mb-3 flex items-start justify-between gap-2">
                        <div>
                            <div className="text-sm font-semibold">{routeTypeLabel(routeType)}</div>
                            <div className="text-xs text-muted-foreground">
                                {models.length === 0
                                    ? canAssignRouteType(routeType)
                                        ? '空列，可拖拽模型到此'
                                        : '空列，仅展示未识别端点'
                                    : `${models.length} 个模型`}
                            </div>
                        </div>
                        <div className="flex items-center gap-2">
                            {models.length === 0 && canCollapseEmpty && onCollapseEmpty ? (
                                <button
                                    type="button"
                                    className="rounded-lg px-2 py-1 text-[11px] text-muted-foreground transition hover:bg-muted hover:text-foreground"
                                    onClick={onCollapseEmpty}
                                >
                                    收起
                                </button>
                            ) : null}
                            <SelectionCheckbox
                                checked={allSelected}
                                disabled={models.length === 0}
                                ariaLabel={`选择 ${routeTypeLabel(routeType)} 列全部模型`}
                                onCheckedChange={(checked) => onToggleColumnSelection(modelKeys, checked)}
                            />
                            <Badge variant="outline" className={cn('h-6 px-2 text-[10px]', getRouteTypeTone(routeType))}>
                                {routeTypeLabel(routeType)}
                            </Badge>
                        </div>
                    </div>

                    <div className="flex min-h-0 flex-1 flex-col gap-2">
                        {models.map((model, index) => {
                            const modelKey = makeModelKey(model.group_key, model.model_name);
                            const { Avatar: ModelAvatar } = getModelIcon(model.model_name);
                            const isPending = pendingModelKeys.has(modelKey);
                            const isSelected = selectedModelKeys.has(modelKey);
                            const unknownRouteReason = getUnknownRouteReason(model);

                            return (
                                <Draggable
                                    key={modelKey}
                                    draggableId={modelKey}
                                    index={index}
                                    isDragDisabled={model.disabled || isPending}
                                >
                                    {(draggableProvided, draggableSnapshot) => (
                                        <div
                                            ref={(node) => {
                                                draggableProvided.innerRef(node);
                                                registerModelRef(modelKey, node);
                                            }}
                                            {...draggableProvided.draggableProps}
                                            style={draggableProvided.draggableProps.style}
                                            data-model-key={modelKey}
                                            className={cn(
                                                'rounded-2xl border border-border/70 bg-background/95 shadow-xs transition',
                                                compactMode ? 'p-2.5' : 'p-3',
                                                model.disabled && 'opacity-60 grayscale',
                                                isPending && 'opacity-70',
                                                isSelected && 'border-primary/30 bg-primary/5',
                                                draggableSnapshot.isDragging && 'shadow-lg ring-2 ring-primary/30',
                                                highlightedModelKey === modelKey && 'ring-2 ring-primary/35 ring-offset-2 ring-offset-background',
                                            )}
                                        >
                                            <div className="flex items-start gap-2.5">
                                                <SelectionCheckbox
                                                    checked={isSelected}
                                                    disabled={isPending}
                                                    ariaLabel={`选择模型 ${model.model_name}`}
                                                    onCheckedChange={(checked) => onToggleModelSelection(modelKey, checked)}
                                                    className="mt-1"
                                                />
                                                <div
                                                    {...draggableProvided.dragHandleProps}
                                                    className={cn(
                                                        'mt-0.5 rounded-lg p-1 text-muted-foreground transition',
                                                        model.disabled || isPending
                                                            ? 'cursor-not-allowed opacity-50'
                                                            : 'cursor-grab hover:bg-muted hover:text-foreground active:cursor-grabbing',
                                                    )}
                                                >
                                                    <GripVertical className="size-4" />
                                                </div>
                                                <span className="mt-0.5 shrink-0">
                                                    <ModelAvatar size={18} />
                                                </span>
                                                <div className="min-w-0 flex-1 space-y-2">
                                                    <div className="flex items-start justify-between gap-2">
                                                        <div className="min-w-0">
                                                            <div className="truncate text-sm font-semibold">{model.model_name}</div>
                                                            <div className="mt-0.5 text-[11px] text-muted-foreground">
                                                                {model.group_name || model.group_key}
                                                                {compactMode ? '' : ` · Key ${model.enabled_key_count}/${model.key_count}`}
                                                            </div>
                                                        </div>
                                                        <div className="flex items-center gap-1">
                                                            <MoveRoutePopover
                                                                currentRouteType={model.route_type}
                                                                disabled={isPending || model.disabled}
                                                                onMove={(nextRouteType) => onMoveModel(model, nextRouteType)}
                                                            />
                                                            <Tooltip side="top" align="end">
                                                                <TooltipTrigger asChild>
                                                                    <button
                                                                        type="button"
                                                                        className="rounded-lg p-1 text-muted-foreground transition hover:bg-muted hover:text-foreground"
                                                                    >
                                                                        <History className="size-4" />
                                                                    </button>
                                                                </TooltipTrigger>
                                                                <TooltipContent className="max-w-none rounded-2xl border border-border/70 bg-card p-0 shadow-xl">
                                                                    <HistorySummary model={model} />
                                                                </TooltipContent>
                                                            </Tooltip>
                                                            <Tooltip side="top" align="end">
                                                                <TooltipTrigger asChild>
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
                                                                </TooltipTrigger>
                                                                <TooltipContent>{model.disabled ? '启用模型' : '禁用模型'}</TooltipContent>
                                                            </Tooltip>
                                                        </div>
                                                    </div>
                                                    <div className="flex flex-wrap gap-1.5">
                                                        <Badge variant="outline" className={cn('h-5 px-1.5 text-[10px]', getRouteSourceTone(model.route_source))}>
                                                            {routeSourceLabel(model.route_source)}
                                                        </Badge>
                                                        {!isSupportedRouteType(model.route_type) ? (
                                                            <Badge
                                                                variant="outline"
                                                                className="h-5 px-1.5 text-[10px] border-amber-500/30 bg-amber-500/10 text-amber-700 dark:text-amber-300"
                                                                title={unknownRouteReason ?? undefined}
                                                            >
                                                                待人工指定
                                                            </Badge>
                                                        ) : null}
                                                        {!model.has_keys ? (
                                                            <Badge
                                                                variant="outline"
                                                                className="h-5 px-1.5 text-[10px] border-amber-500/30 bg-amber-500/10 text-amber-700 dark:text-amber-300"
                                                            >
                                                                缺少 Key
                                                            </Badge>
                                                        ) : null}
                                                        {!compactMode && model.projected_channel_id ? (
                                                            <button
                                                                type="button"
                                                                onClick={() => onNavigateToChannel(model.projected_channel_id!)}
                                                                className="inline-flex"
                                                            >
                                                                <Badge variant="outline" className="h-5 px-1.5 text-[10px] hover:border-primary/30 hover:bg-primary/5">
                                                                    渠道 #{model.projected_channel_id}
                                                                </Badge>
                                                            </button>
                                                        ) : null}
                                                        {compactMode && model.disabled ? (
                                                            <Badge
                                                                variant="outline"
                                                                className="h-5 px-1.5 text-[10px] border-destructive/30 bg-destructive/10 text-destructive"
                                                            >
                                                                已禁用
                                                            </Badge>
                                                        ) : null}
                                                    </div>
                                                </div>
                                            </div>
                                        </div>
                                    )}
                                </Draggable>
                            );
                        })}

                        {provided.placeholder}

                        {models.length === 0 ? (
                            <div className="flex min-h-[16rem] flex-1 items-center justify-center rounded-2xl border border-dashed border-border/70 bg-background/50 px-4 py-8 text-center">
                                <div className="space-y-2">
                                    <div className="text-sm font-medium text-foreground">当前列暂无模型</div>
                                    <div className="text-xs text-muted-foreground">
                                        {canAssignRouteType(routeType)
                                            ? '可直接拖拽模型到此，或使用“移动至...”切换端点格式。'
                                            : '未识别端点仅用于展示自动识别结果，不能直接作为目标端点。'}
                                    </div>
                                </div>
                            </div>
                        ) : null}
                    </div>
                </div>
            )}
        </Droppable>
    );
}

function EmptyRouteDropCapsule({
    routeType,
    onExpand,
}: {
    routeType: SiteModelRouteType;
    onExpand: () => void;
}) {
    const droppable = canAssignRouteType(routeType);

    if (!droppable) {
        return (
            <div className="flex min-w-[14rem] items-center justify-between gap-3 rounded-2xl border border-dashed border-border/70 bg-muted/20 px-3 py-2.5">
                <div className="min-w-0">
                    <div className="flex items-center gap-2">
                        <Badge variant="outline" className={cn('h-5 px-1.5 text-[10px]', getRouteTypeTone(routeType))}>
                            0
                        </Badge>
                        <span className="text-sm font-medium">{routeTypeLabel(routeType)}</span>
                    </div>
                    <div className="mt-1 text-[11px] text-muted-foreground">当前无模型，仅作为状态汇总</div>
                </div>
                <Button type="button" variant="outline" size="sm" className="rounded-xl" onClick={onExpand}>
                    展开
                </Button>
            </div>
        );
    }

    return (
        <Droppable droppableId={routeType}>
            {(provided, snapshot) => (
                <div
                    ref={provided.innerRef}
                    {...provided.droppableProps}
                    className={cn(
                        'flex min-w-[14rem] items-center justify-between gap-3 rounded-2xl border border-dashed border-border/70 bg-muted/20 px-3 py-2.5 transition',
                        snapshot.isDraggingOver && 'border-primary/40 bg-primary/10 shadow-sm',
                    )}
                >
                    <div className="min-w-0">
                        <div className="flex items-center gap-2">
                            <Badge variant="outline" className={cn('h-5 px-1.5 text-[10px]', getRouteTypeTone(routeType))}>
                                0
                            </Badge>
                            <span className="text-sm font-medium">{routeTypeLabel(routeType)}</span>
                        </div>
                        <div className="mt-1 text-[11px] text-muted-foreground">拖拽模型到这里，或先展开空列</div>
                    </div>
                    <Button type="button" variant="outline" size="sm" className="rounded-xl" onClick={onExpand}>
                        展开
                    </Button>
                    {provided.placeholder}
                </div>
            )}
        </Droppable>
    );
}

function SiteChannelTableView({
    models,
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
    onNavigateToChannel,
    registerModelRef,
}: {
    models: SiteModelView[];
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
    onNavigateToChannel: (channelId: number) => void;
    registerModelRef: (modelKey: string, node: HTMLElement | null) => void;
}) {
    const allVisibleSelected = models.length > 0 && models.every((model) => selectedModelKeys.has(makeModelKey(model.group_key, model.model_name)));

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
        <div className="overflow-hidden rounded-3xl border border-border/70 bg-card/70">
            <Table className="min-w-[74rem]">
                <TableHeader>
                    <TableRow>
                        <TableHead className="w-12">
                            <SelectionCheckbox
                                checked={allVisibleSelected}
                                disabled={models.length === 0}
                                ariaLabel="选择当前可见模型"
                                onCheckedChange={onToggleAllVisible}
                            />
                        </TableHead>
                        <TableHead>{renderSortHead('model_name', '模型')}</TableHead>
                        <TableHead>{renderSortHead('group_name', '分组')}</TableHead>
                        <TableHead>{renderSortHead('route_type', '端点格式')}</TableHead>
                        <TableHead>来源</TableHead>
                        <TableHead>Key</TableHead>
                        <TableHead>状态</TableHead>
                        <TableHead>{renderSortHead('last_request_at', '最近请求')}</TableHead>
                        <TableHead>渠道</TableHead>
                        <TableHead className="text-right">操作</TableHead>
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
                                            <div className="truncate text-sm font-medium">{model.model_name}</div>
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
                                        <Tooltip side="top" align="end">
                                            <TooltipTrigger asChild>
                                                <button
                                                    type="button"
                                                    className="rounded-lg p-1 text-muted-foreground transition hover:bg-muted hover:text-foreground"
                                                >
                                                    <History className="size-4" />
                                                </button>
                                            </TooltipTrigger>
                                            <TooltipContent className="max-w-none rounded-2xl border border-border/70 bg-card p-0 shadow-xl">
                                                <HistorySummary model={model} />
                                            </TooltipContent>
                                        </Tooltip>
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
        </div>
    );
}

function SiteAccountPanel({
    siteId,
    account,
    jumpRequest,
    onJumpHandled,
    onNavigateToChannel,
}: {
    siteId: number;
    account: SiteChannelAccount;
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
    const [sourceKeyForm, setSourceKeyForm] = useState<SiteSourceKeyFormItem[]>([]);
    const [visibleSourceKeyRows, setVisibleSourceKeyRows] = useState<Record<string, boolean>>({});
    const [quickCreateName, setQuickCreateName] = useState('');
    const [highlightedModelKey, setHighlightedModelKey] = useState<string | null>(null);
    const [modelSearchTerm, setModelSearchTerm] = useState('');
    const [bulkMoveTarget, setBulkMoveTarget] = useState<SiteModelRouteType>('openai_chat');
    const [expandedEmptyColumns, setExpandedEmptyColumns] = useState<Set<SiteModelRouteType>>(new Set());
    const modelElementRefs = useRef<Map<string, HTMLElement>>(new Map());
    const panelKey = `${siteId}:${account.account_id}`;

    const panelPreferences = useSiteChannelPanelViewStore(
        (state) => state.panels[panelKey] ?? DEFAULT_SITE_CHANNEL_PANEL_PREFERENCES,
    );
    const setViewMode = useSiteChannelPanelViewStore((state) => state.setViewMode);
    const setCompactMode = useSiteChannelPanelViewStore((state) => state.setCompactMode);
    const setCollapseEmptyColumns = useSiteChannelPanelViewStore((state) => state.setCollapseEmptyColumns);
    const setQuickFilters = useSiteChannelPanelViewStore((state) => state.setQuickFilters);
    const setTableSort = useSiteChannelPanelViewStore((state) => state.setTableSort);

    const createKeyMutation = useCreateSiteChannelKey(siteId, account.account_id);
    const sourceKeyMutation = useUpdateSiteSourceKeys(siteId, account.account_id);
    const routeMutation = useUpdateSiteChannelModelRoutes(siteId, account.account_id);
    const disabledMutation = useUpdateSiteChannelModelDisabled();
    const resetMutation = useResetSiteChannelModelRoutes(siteId, account.account_id);

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

    const groupedModels = useMemo(() => {
        const next: Record<SiteModelRouteType, SiteModelView[]> = {
            unknown: [],
            openai_chat: [],
            openai_response: [],
            anthropic: [],
            gemini: [],
            volcengine: [],
            openai_embedding: [],
        };

        for (const model of visibleModels) {
            next[model.route_type].push(model);
        }

        return next;
    }, [visibleModels]);

    const emptyRouteTypes = useMemo(
        () => SITE_ROUTE_DISPLAY_ORDER.filter((routeType) => groupedModels[routeType].length === 0),
        [groupedModels],
    );

    const shouldCollapseEmptyColumns = panelPreferences.collapseEmptyColumns && visibleModels.length > 0;
    const effectiveExpandedEmptyColumns = useMemo(
        () => new Set(
            shouldCollapseEmptyColumns
                ? Array.from(expandedEmptyColumns).filter((routeType) => groupedModels[routeType].length === 0)
                : [],
        ),
        [expandedEmptyColumns, groupedModels, shouldCollapseEmptyColumns],
    );

    const visibleBoardColumns = useMemo(
        () => SITE_ROUTE_DISPLAY_ORDER.filter((routeType) => {
            if (groupedModels[routeType].length > 0) return true;
            return !panelPreferences.collapseEmptyColumns || effectiveExpandedEmptyColumns.has(routeType);
        }),
        [groupedModels, panelPreferences.collapseEmptyColumns, effectiveExpandedEmptyColumns],
    );

    const collapsedEmptyColumns = useMemo(
        () => {
            if (!panelPreferences.collapseEmptyColumns) return [] as SiteModelRouteType[];
            return emptyRouteTypes.filter((routeType) => !effectiveExpandedEmptyColumns.has(routeType));
        },
        [panelPreferences.collapseEmptyColumns, emptyRouteTypes, effectiveExpandedEmptyColumns],
    );
    const selectedModels = useMemo(
        () => Array.from(selectedModelKeys).map((key) => visibleModelMap.get(key)).filter((model): model is SiteModelView => !!model),
        [selectedModelKeys, visibleModelMap],
    );
    const hasPendingChanges = pendingModelKeys.size > 0 || routeMutation.isPending || disabledMutation.isPending;

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
                toast.error(getErrorMessage(error, '更新模型请求端点格式失败'));
            },
            onSettled: () => {
                setPendingModelKeys((current) => removePendingKeys(current, modelKeys));
            },
        });
    }, [pendingModelKeys, routeMutation]);

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
                toast.error(getErrorMessage(error, '更新模型禁用状态失败'));
            },
            onSettled: () => {
                setPendingModelKeys((current) => removePendingKeys(current, modelKeys));
            },
        });
    }, [pendingModelKeys, disabledMutation, siteId, account.account_id]);

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
                    toast.error(translateSiteMessage(locale, getErrorMessage(error, '快捷创建 Key 失败'), t));
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
                toast.error(translateSiteMessage(locale, getErrorMessage(error, '更新站点 Key 失败'), t));
            },
        });
    };

    const handleRouteDrop = (result: DropResult) => {
        const { destination, draggableId } = result;
        if (!destination) return;

        const nextRouteType = destination.droppableId as SiteModelRouteType;
        if (!SITE_ROUTE_COLUMN_ORDER.includes(nextRouteType)) return;

        const target = findModel(account, draggableId);
        if (!target) return;

        const currentRouteType = pendingRouteOverrides[draggableId] ?? target.model.route_type;
        const currentDisabled = pendingDisabledOverrides[draggableId] ?? target.model.disabled;

        if (currentDisabled || currentRouteType === nextRouteType) return;

        const visibleTarget = visibleModelMap.get(draggableId);
        if (visibleTarget) {
            applyRouteChange([visibleTarget], nextRouteType);
            return;
        }

        applyRouteChange(
            [
                {
                    ...target.model,
                    group_key: target.group.group_key,
                    group_name: target.group.group_name,
                    key_count: target.group.key_count,
                    enabled_key_count: target.group.enabled_key_count,
                    has_keys: target.group.has_keys,
                    has_projected_channel: target.group.has_projected_channel,
                    projected_channel_ids: target.group.projected_channel_ids,
                },
            ],
            nextRouteType,
        );
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
                toast.error(getErrorMessage(error, '重置模型端点格式失败'));
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

    const expandEmptyColumn = useCallback((routeType: SiteModelRouteType) => {
        setExpandedEmptyColumns((current) => {
            if (current.has(routeType)) return current;
            const next = new Set(current);
            next.add(routeType);
            return next;
        });
    }, []);

    const collapseEmptyColumn = useCallback((routeType: SiteModelRouteType) => {
        setExpandedEmptyColumns((current) => {
            if (!current.has(routeType)) return current;
            const next = new Set(current);
            next.delete(routeType);
            return next;
        });
    }, []);

    return (
        <div className="space-y-3">
            <div className="flex flex-col gap-3 rounded-3xl border border-border/70 bg-card/70 p-3">
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
                        <div className="inline-flex rounded-2xl border border-border/70 bg-background/80 p-1">
                            <button
                                type="button"
                                onClick={() => setViewMode(panelKey, 'board')}
                                className={cn(
                                    'inline-flex h-6 items-center gap-1.5 rounded-xl px-2.5 text-xs font-medium transition',
                                    panelPreferences.viewMode === 'board'
                                        ? 'bg-primary text-primary-foreground'
                                        : 'text-muted-foreground hover:text-foreground',
                                )}
                            >
                                <LayoutGrid className="size-3.5" />
                                看板
                            </button>
                            <button
                                type="button"
                                onClick={() => setViewMode(panelKey, 'table')}
                                className={cn(
                                    'inline-flex h-6 items-center gap-1.5 rounded-xl px-2.5 text-xs font-medium transition',
                                    panelPreferences.viewMode === 'table'
                                        ? 'bg-primary text-primary-foreground'
                                        : 'text-muted-foreground hover:text-foreground',
                                )}
                            >
                                <List className="size-3.5" />
                                列表
                            </button>
                        </div>
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
                                    <button
                                        type="button"
                                        onClick={() => setCollapseEmptyColumns(panelKey, !panelPreferences.collapseEmptyColumns)}
                                        className="flex w-full items-center justify-between rounded-xl px-3 py-2 text-left transition hover:bg-muted/60"
                                    >
                                        <div>
                                            <div className="text-sm font-medium text-foreground">收起空列</div>
                                            <div className="text-[11px] text-muted-foreground">默认只展示有模型的端点列</div>
                                        </div>
                                        {panelPreferences.collapseEmptyColumns ? <Check className="size-4 text-primary" /> : null}
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
                    <div className="flex flex-wrap items-center justify-between gap-2">
                        <div className="flex flex-wrap gap-2">
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
                        </div>

                        {selectedVisibleCount > 0 ? (
                            <div className="flex flex-wrap items-center gap-2">
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
                <div className="flex min-h-[18rem] items-center justify-center rounded-3xl border border-dashed border-border/70 bg-muted/20 px-6 text-center text-sm text-muted-foreground">
                    当前筛选和搜索条件下没有匹配模型
                </div>
            ) : panelPreferences.viewMode === 'board' ? (
                <div className="overflow-x-auto pb-2">
                    <DragDropContext onDragEnd={handleRouteDrop}>
                        <div className="min-w-max space-y-3">
                            {collapsedEmptyColumns.length > 0 ? (
                                <div className="flex flex-wrap gap-2 rounded-3xl border border-border/70 bg-background/60 p-3">
                                    {collapsedEmptyColumns.map((routeType) => (
                                        <EmptyRouteDropCapsule
                                            key={`${ROUTE_COLUMN_KEY_PREFIX}-collapsed-${routeType}`}
                                            routeType={routeType}
                                            onExpand={() => expandEmptyColumn(routeType)}
                                        />
                                    ))}
                                </div>
                            ) : null}
                            <div className="flex min-w-max gap-4">
                                {visibleBoardColumns.map((routeType) => (
                                <RouteColumn
                                    key={`${ROUTE_COLUMN_KEY_PREFIX}-${routeType}`}
                                    routeType={routeType}
                                    models={groupedModels[routeType]}
                                    pendingModelKeys={pendingModelKeys}
                                    selectedModelKeys={selectedModelKeys}
                                    compactMode={panelPreferences.compactMode}
                                    canCollapseEmpty={shouldCollapseEmptyColumns}
                                    onToggleModelSelection={handleToggleModelSelection}
                                    onToggleColumnSelection={setSelectionForKeys}
                                    onMoveModel={(model, nextRouteType) => applyRouteChange([model], nextRouteType)}
                                    onToggleDisabled={handleToggleDisabled}
                                    onNavigateToChannel={onNavigateToChannel}
                                    registerModelRef={registerModelRef}
                                    highlightedModelKey={highlightedModelKey}
                                    onCollapseEmpty={
                                        groupedModels[routeType].length === 0 && shouldCollapseEmptyColumns
                                            ? () => collapseEmptyColumn(routeType)
                                            : undefined
                                    }
                                />
                                ))}
                            </div>
                        </div>
                    </DragDropContext>
                </div>
            ) : (
                <div className="overflow-x-auto pb-2">
                    <SiteChannelTableView
                        models={visibleModels}
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
    const [activeAccountId, setActiveAccountId] = useState<number | null>(card.accounts[0]?.account_id ?? null);
    const [highlightedAccountId, setHighlightedAccountId] = useState<number | null>(null);
    const accountTabRefs = useRef<Map<number, HTMLButtonElement>>(new Map());

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
        const target = jumpRequest.target;
        if (target.kind === 'site-channel-card') return;

        if (activeAccountId !== target.accountId) {
            const frameId = window.requestAnimationFrame(() => {
                setActiveAccountId(target.accountId);
            });
            return () => window.cancelAnimationFrame(frameId);
        }

        const node = accountTabRefs.current.get(target.accountId);
        if (!node) return;

        const timer = window.setTimeout(() => {
            node.scrollIntoView({ behavior: 'smooth', block: 'nearest', inline: 'center' });
            setHighlightedAccountId(target.accountId);
            window.setTimeout(() => {
                setHighlightedAccountId((current) =>
                    current === target.accountId ? null : current,
                );
            }, 1800);

            if (target.kind === 'site-channel-account') {
                onJumpHandled(jumpRequest.requestId);
            }
        }, 80);

        return () => window.clearTimeout(timer);
    }, [jumpRequest, card.site_id, activeAccountId, onJumpHandled]);

    return (
        <div className="flex max-h-[88vh] flex-col overflow-hidden">
            <DialogHeader className="gap-3 border-b border-border/70 px-5 py-3.5 text-left sm:px-6">
                <DialogDescription className="sr-only">
                    站点渠道管理面板
                </DialogDescription>

                <div className="flex flex-col gap-3 xl:flex-row xl:items-start xl:justify-between">
                    <div className="min-w-0 space-y-2">
                        <DialogTitle className="flex flex-wrap items-center gap-2 text-xl sm:text-2xl">
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
                            {resolvedAccount ? (
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
                            ) : null}
                        </DialogTitle>
                        {resolvedAccount ? (
                            <div className="text-sm text-muted-foreground">
                                当前账号：{resolvedAccount.account_name} · {resolvedAccount.model_count} 模型 / {resolvedAccount.group_count} 分组
                            </div>
                        ) : null}
                    </div>

                    <div className="max-w-full overflow-x-auto pb-1">
                        <div className="flex min-w-max gap-2">
                            {card.accounts.map((account) => (
                                <button
                                    key={account.account_id}
                                    ref={(node) => setAccountTabRef(account.account_id, node)}
                                    type="button"
                                    onClick={() => setActiveAccountId(account.account_id)}
                                    className={cn(
                                        'min-w-[9.75rem] rounded-2xl border px-3 py-2 text-left transition',
                                        resolvedAccount?.account_id === account.account_id
                                            ? 'border-primary/30 bg-primary text-primary-foreground'
                                            : 'border-border bg-background hover:bg-muted/50',
                                        highlightedAccountId === account.account_id &&
                                            'ring-2 ring-primary/35 ring-offset-2 ring-offset-background',
                                    )}
                                >
                                    <div className="flex items-start justify-between gap-2">
                                        <div className="min-w-0">
                                            <div className="truncate text-sm font-medium">{account.account_name}</div>
                                            <div
                                                className={cn(
                                                    'truncate text-[11px]',
                                                    resolvedAccount?.account_id === account.account_id
                                                        ? 'text-primary-foreground/80'
                                                        : 'text-muted-foreground',
                                                )}
                                            >
                                                {account.model_count} 模型 / {account.group_count} 分组
                                            </div>
                                        </div>
                                        <span
                                            className={cn(
                                                'mt-1 size-2.5 shrink-0 rounded-full',
                                                account.enabled ? 'bg-emerald-500' : 'bg-destructive',
                                            )}
                                            aria-hidden="true"
                                        />
                                    </div>
                                </button>
                            ))}
                        </div>
                    </div>
                </div>

                <div className="overflow-x-auto pb-1">
                    <div className="flex min-w-max gap-2">
                        <Button
                            type="button"
                            variant="outline"
                            size="sm"
                            className="h-8 rounded-2xl px-3"
                            onClick={handleOpenSiteBaseUrl}
                            disabled={!card.base_url}
                        >
                            <ExternalLink className="size-4" />
                            打开站点
                        </Button>
                        <Button
                            type="button"
                            variant="outline"
                            size="sm"
                            className="h-8 rounded-2xl px-3"
                            onClick={onNavigateToSite}
                        >
                            <Globe2 className="size-4" />
                            站点页
                        </Button>
                        {resolvedAccount ? (
                            <Button
                                type="button"
                                variant="outline"
                                size="sm"
                                className="h-8 rounded-2xl px-3"
                                onClick={() => onNavigateToSiteAccount(resolvedAccount.account_id)}
                            >
                                <Waypoints className="size-4" />
                                站点页账号
                            </Button>
                        ) : null}
                    </div>
                </div>
            </DialogHeader>

            <div className="flex min-h-0 flex-1 flex-col overflow-hidden px-6 py-5">
                {resolvedAccount ? (
                    <div className="min-h-0 flex-1 overflow-y-auto pr-1">
                        <SiteAccountPanel
                            key={resolvedAccount.account_id}
                            siteId={card.site_id}
                            account={resolvedAccount}
                            jumpRequest={jumpRequest}
                            onJumpHandled={onJumpHandled}
                            onNavigateToChannel={onNavigateToChannel}
                        />
                    </div>
                ) : (
                    <div className="flex min-h-[16rem] items-center justify-center rounded-3xl border border-dashed border-border/70 bg-muted/20 text-sm text-muted-foreground">
                        当前站点没有可管理的账号
                    </div>
                )}
            </div>
        </div>
    );
}

function SiteCard({
    card,
    layout,
    jumpRequest,
    highlighted,
    registerCardRef,
    onJumpHandled,
    onNavigateToSite,
    onNavigateToSiteAccount,
    onNavigateToChannel,
}: {
    card: SiteChannelCard;
    layout: 'grid' | 'list';
    jumpRequest: SiteChannelPendingJump | null;
    highlighted: boolean;
    registerCardRef: (siteId: number, node: HTMLDivElement | null) => void;
    onJumpHandled: (requestId: number) => void;
    onNavigateToSite: () => void;
    onNavigateToSiteAccount: (accountId: number) => void;
    onNavigateToChannel: (channelId: number) => void;
}) {
    const [open, setOpen] = useState(false);
    const summary = useMemo(() => collectSiteSummary(card), [card]);
    const isListLayout = layout === 'list';

    const closeDialogAndNavigate = useCallback((navigate: () => void) => {
        setOpen(false);
        window.requestAnimationFrame(() => {
            navigate();
        });
    }, []);

    useEffect(() => {
        if (!jumpRequest) return;
        if (jumpRequest.target.siteId !== card.site_id) return;
        if (jumpRequest.target.kind === 'site-channel-card' || open) return;

        const frameId = window.requestAnimationFrame(() => {
            setOpen(true);
        });
        return () => window.cancelAnimationFrame(frameId);
    }, [jumpRequest, card.site_id, open]);

    return (
        <>
            <div
                ref={(node) => registerCardRef(card.site_id, node)}
                className={cn(
                    'rounded-[1.75rem] transition-all',
                    highlighted && 'ring-2 ring-primary/35 ring-offset-2 ring-offset-background',
                )}
            >
                <article
                    className={cn(
                        'flex h-full flex-col gap-4 rounded-3xl border border-border/70 bg-card p-4 transition hover:border-primary/20 hover:bg-card/90',
                        isListLayout && 'md:flex-row md:items-stretch',
                    )}
                    style={{
                        contentVisibility: 'auto',
                        containIntrinsicSize: isListLayout ? '200px' : '260px',
                    }}
                >
                    <div
                        role="button"
                        tabIndex={0}
                        onClick={() => setOpen(true)}
                        onKeyDown={(event) => {
                            if (event.key === 'Enter' || event.key === ' ') {
                                event.preventDefault();
                                setOpen(true);
                            }
                        }}
                        className={cn('flex min-w-0 flex-1 flex-col gap-4 text-left outline-none', isListLayout && 'md:max-w-[24rem]')}
                    >
                        <header className="flex items-start justify-between gap-3">
                            <div className="min-w-0">
                                <div className="flex items-center gap-2">
                                    <span
                                        className={cn(
                                            'inline-block size-2 shrink-0 rounded-full',
                                            card.enabled
                                                ? 'bg-emerald-500'
                                                : 'bg-destructive',
                                        )}
                                        title={card.enabled ? '启用' : '停用'}
                                    />
                                    <div className="truncate text-lg font-bold">{card.site_name}</div>
                                </div>
                                <div className="mt-1 flex flex-wrap gap-2">
                                    <Badge variant="outline" className="h-6 px-2 text-[11px]">
                                        {platformLabel(card.platform)}
                                    </Badge>
                                </div>
                            </div>
                        </header>

                        <div className="flex flex-wrap gap-x-4 gap-y-1 text-sm text-muted-foreground">
                            <span className="flex items-center gap-1"><FolderTree className="size-3.5" />{summary.groupCount} 分组</span>
                            <span className="flex items-center gap-1"><Sparkles className="size-3.5" />{summary.modelCount} 模型</span>
                            <span className="flex items-center gap-1"><KeyRound className="size-3.5" />{summary.enabledKeys}/{summary.totalKeys} Key</span>
                            <span className="flex items-center gap-1"><UserRound className="size-3.5" />{card.account_count} 账号</span>
                        </div>

                        <div className={cn('space-y-2', isListLayout ? 'md:min-w-[18rem] md:max-w-[22rem]' : '')}>
                        <div className="text-xs font-medium text-muted-foreground">端点格式分布</div>
                        <div className="flex flex-wrap gap-2">
                                {SITE_ROUTE_DISPLAY_ORDER.filter((routeType) => (summary.routeCounts.get(routeType) ?? 0) > 0).map((routeType) => (
                                    <Badge key={routeType} variant="outline" className={cn('h-6 px-2 text-[11px]', getRouteTypeTone(routeType))}>
                                        {routeTypeLabel(routeType)}
                                        <span className="ml-1">{summary.routeCounts.get(routeType)}</span>
                                    </Badge>
                                ))}
                                {summary.routeCounts.size === 0 ? (
                                    <span className="text-xs text-muted-foreground">暂无模型分布</span>
                                ) : null}
                            </div>
                        </div>
                    </div>

                </article>
            </div>

            <Dialog open={open} onOpenChange={setOpen}>
                {open ? (
                    <DialogContent className="max-w-[min(96vw,92rem)] w-[min(96vw,92rem)] rounded-[2rem] p-0 sm:max-w-[min(96vw,92rem)]">
                        <SiteChannelDialog
                            card={card}
                            jumpRequest={jumpRequest?.target.siteId === card.site_id ? jumpRequest : null}
                            onJumpHandled={onJumpHandled}
                            onNavigateToSite={() => closeDialogAndNavigate(onNavigateToSite)}
                            onNavigateToSiteAccount={(accountId) =>
                                closeDialogAndNavigate(() => onNavigateToSiteAccount(accountId))
                            }
                            onNavigateToChannel={(channelId) => {
                                closeDialogAndNavigate(() => onNavigateToChannel(channelId));
                            }}
                        />
                    </DialogContent>
                ) : null}
            </Dialog>
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
    const [completionDialogOpen, setCompletionDialogOpen] = useState(false);
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
                const diff = sortField === 'name'
                    ? a.site_name.localeCompare(b.site_name)
                    : a.site_id - b.site_id;
                return sortOrder === 'asc' ? diff : -diff;
            });
    }, [data, searchTerm, filter, sortField, sortOrder, forcedSiteId]);

    const pendingCompletionSites = useMemo(
        () => collectPendingCompletionSites(data ?? []),
        [data],
    );
    const totalPendingCompletionCount = useMemo(
        () => pendingCompletionSites.reduce((sum, site) => sum + site.pending_count, 0),
        [pendingCompletionSites],
    );
    const effectiveCompletionDialogOpen = completionDialogOpen && totalPendingCompletionCount > 0;

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
            <section className="space-y-3">
                <div className="px-1">
                    <div className="text-sm font-semibold">站点渠道</div>
                    <div className="text-xs text-muted-foreground">正在加载站点投影的渠道聚合视图</div>
                </div>
                <div className={cn('grid gap-4', layout === 'list' ? 'grid-cols-1' : 'md:grid-cols-2 xl:grid-cols-3')}>
                    {Array.from({ length: layout === 'list' ? 2 : 3 }).map((_, index) => (
                        <div key={index} className="h-56 animate-pulse rounded-3xl border border-border/70 bg-muted/40" />
                    ))}
                </div>
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

    return (
        <>
            <section className="space-y-3">
                <div className="flex flex-col gap-3 px-1 sm:flex-row sm:items-start sm:justify-between">
                    <div>
                        <div className="text-sm font-semibold">站点渠道</div>
                        <div className="text-xs text-muted-foreground">
                            统一查看站点投影的分组、模型端点格式、请求历史和模型禁用状态
                        </div>
                    </div>
                    {totalPendingCompletionCount > 0 ? (
                        <Button
                            type="button"
                            variant="outline"
                            className="h-9 rounded-2xl px-3"
                            onClick={() => setCompletionDialogOpen(true)}
                        >
                            <KeyRound className="size-4 text-primary" />
                            统一补全 Key
                            <Badge variant="outline" className="h-5 px-1.5 text-[10px]">
                                {totalPendingCompletionCount}
                            </Badge>
                        </Button>
                    ) : null}
                </div>

                <div className={cn('grid gap-4', layout === 'list' ? 'grid-cols-1' : 'md:grid-cols-2 xl:grid-cols-3')}>
                    {cards.map((card) => (
                        <SiteCard
                            key={card.site_id}
                            card={card}
                            layout={layout}
                            jumpRequest={pendingSiteChannelJump?.target.siteId === card.site_id ? pendingSiteChannelJump : null}
                            highlighted={highlightedSiteId === card.site_id}
                            registerCardRef={registerCardRef}
                            onJumpHandled={clearPending}
                            onNavigateToSite={() => requestJump({ kind: 'site-card', siteId: card.site_id })}
                            onNavigateToSiteAccount={(accountId) =>
                                requestJump({ kind: 'site-account', siteId: card.site_id, accountId })
                            }
                            onNavigateToChannel={(channelId) =>
                                requestJump({ kind: 'channel-card', channelId })
                            }
                        />
                    ))}
                </div>
            </section>

            <UnifiedCompletionDialog
                open={effectiveCompletionDialogOpen}
                onOpenChange={setCompletionDialogOpen}
                sites={pendingCompletionSites}
            />
        </>
    );
}
