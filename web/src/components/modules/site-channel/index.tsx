'use client';

import { useMemo, useState } from 'react';
import {
    DragDropContext,
    Draggable,
    Droppable,
    type DropResult,
} from '@hello-pangea/dnd';
import {
    CircleAlert,
    CircleOff,
    FolderTree,
    GripVertical,
    History,
    KeyRound,
    RefreshCw,
    Server,
    Sparkles,
    UserRound,
} from 'lucide-react';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import {
    Dialog,
    DialogContent,
    DialogDescription,
    DialogHeader,
    DialogTitle,
} from '@/components/ui/dialog';
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/animate-ui/components/animate/tooltip';
import { toast } from '@/components/common/Toast';
import { cn } from '@/lib/utils';
import { getModelIcon } from '@/lib/model-icons';
import {
    type SiteChannelAccount,
    type SiteChannelCard,
    type SiteModelDisableUpdateRequest,
    type SiteModelRouteType,
    type SiteModelRouteUpdateRequest,
    useResetSiteChannelModelRoutes,
    useSiteChannelList,
    useUpdateSiteChannelModelDisabled,
    useUpdateSiteChannelModelRoutes,
} from '@/api/endpoints/site-channel';
import {
    ROUTE_COLUMN_KEY_PREFIX,
    SITE_ROUTE_COLUMN_ORDER,
    getRouteSourceTone,
    getRouteTypeTone,
} from './constants';
import {
    SITE_GROUP_FILTER_ALL,
    SITE_GROUP_FILTER_MISSING_KEYS,
    createGroupFilter,
    type SiteChannelGroupFilter,
    type SiteModelView,
    countAccountKeys,
    filterGroups,
    flattenAccountModels,
    formatHistoryTime,
    getErrorMessage,
    groupFilterCount,
    isSameGroupFilter,
    platformLabel,
    routeSourceLabel,
    routeTypeLabel,
    summarizeHistory,
} from './utils';

type ChannelFilter = 'all' | 'enabled' | 'disabled';
type ToolbarSortField = 'name' | 'created';
type ToolbarSortOrder = 'asc' | 'desc';

function makeModelKey(groupKey: string, modelName: string) {
    return `${groupKey}\u0000${modelName}`;
}

function removeKey<T>(record: Record<string, T>, key: string) {
    if (!(key in record)) return record;
    const next = { ...record };
    delete next[key];
    return next;
}

function addPendingKey(current: Set<string>, key: string) {
    const next = new Set(current);
    next.add(key);
    return next;
}

function removePendingKey(current: Set<string>, key: string) {
    if (!current.has(key)) return current;
    const next = new Set(current);
    next.delete(key);
    return next;
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
                    {model.history.recent.map((item) => (
                        <div
                            key={`${item.time}-${item.channel_id}-${item.request_model}-${item.actual_model}`}
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

function RouteColumn({
    routeType,
    models,
    pendingModelKeys,
    onToggleDisabled,
}: {
    routeType: SiteModelRouteType;
    models: SiteModelView[];
    pendingModelKeys: Set<string>;
    onToggleDisabled: (model: SiteModelView) => void;
}) {
    return (
        <Droppable droppableId={routeType}>
            {(provided, snapshot) => (
                <div
                    ref={provided.innerRef}
                    {...provided.droppableProps}
                    className={cn(
                        'flex min-h-[30rem] w-[18.5rem] flex-col rounded-3xl border border-border/70 bg-card/70 p-3 transition',
                        snapshot.isDraggingOver && 'border-primary/40 bg-primary/5',
                    )}
                >
                    <div className="mb-3 flex items-center justify-between gap-2">
                        <div>
                            <div className="text-sm font-semibold">{routeTypeLabel(routeType)}</div>
                            <div className="text-xs text-muted-foreground">{models.length} 个模型</div>
                        </div>
                        <Badge variant="outline" className={cn('h-6 px-2 text-[10px]', getRouteTypeTone(routeType))}>
                            {routeTypeLabel(routeType)}
                        </Badge>
                    </div>

                    <div className="flex min-h-0 flex-1 flex-col gap-2">
                        {models.map((model, index) => {
                            const modelKey = makeModelKey(model.group_key, model.model_name);
                            const { Avatar: ModelAvatar } = getModelIcon(model.model_name);
                            const isPending = pendingModelKeys.has(modelKey);

                            return (
                                <Draggable
                                    key={modelKey}
                                    draggableId={modelKey}
                                    index={index}
                                    isDragDisabled={model.disabled || isPending}
                                >
                                    {(draggableProvided, snapshot) => (
                                        <div
                                            ref={draggableProvided.innerRef}
                                            {...draggableProvided.draggableProps}
                                            style={draggableProvided.draggableProps.style}
                                            className={cn(
                                                'rounded-2xl border border-border/70 bg-background/95 p-3 shadow-xs transition',
                                                model.disabled && 'opacity-60 grayscale',
                                                isPending && 'opacity-70',
                                                snapshot.isDragging && 'shadow-lg ring-2 ring-primary/30',
                                            )}
                                        >
                                            <div className="flex items-start gap-3">
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
                                                                {model.group_name || model.group_key} · Key {model.enabled_key_count}/{model.key_count}
                                                            </div>
                                                        </div>
                                                        <div className="flex items-center gap-1">
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
                                                        {!model.has_keys ? (
                                                            <Badge
                                                                variant="outline"
                                                                className="h-5 px-1.5 text-[10px] border-amber-500/30 bg-amber-500/10 text-amber-700 dark:text-amber-300"
                                                            >
                                                                缺少 Key
                                                            </Badge>
                                                        ) : null}
                                                        {model.projected_channel_id ? (
                                                            <Badge variant="outline" className="h-5 px-1.5 text-[10px]">
                                                                渠道 #{model.projected_channel_id}
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
                            <div className="flex flex-1 items-center justify-center rounded-2xl border border-dashed border-border/70 bg-background/50 px-4 py-8 text-center text-sm text-muted-foreground">
                                当前筛选下没有模型
                            </div>
                        ) : null}
                    </div>
                </div>
            )}
        </Droppable>
    );
}

function SiteAccountPanel({ siteId, account }: { siteId: number; account: SiteChannelAccount }) {
    const [activeFilter, setActiveFilter] = useState<SiteChannelGroupFilter>(SITE_GROUP_FILTER_ALL);
    const [pendingRouteOverrides, setPendingRouteOverrides] = useState<Record<string, SiteModelRouteType>>({});
    const [pendingDisabledOverrides, setPendingDisabledOverrides] = useState<Record<string, boolean>>({});
    const [pendingModelKeys, setPendingModelKeys] = useState<Set<string>>(new Set());

    const routeMutation = useUpdateSiteChannelModelRoutes(siteId, account.account_id);
    const disabledMutation = useUpdateSiteChannelModelDisabled(siteId, account.account_id);
    const resetMutation = useResetSiteChannelModelRoutes(siteId, account.account_id);

    const visibleGroups = useMemo(
        () => filterGroups(account.groups, activeFilter),
        [account.groups, activeFilter],
    );

    const visibleModels = useMemo(() => {
        return flattenAccountModels(account, activeFilter)
            .map((model) => {
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
            })
            .sort((a, b) => {
                const byGroup = (a.group_name || a.group_key).localeCompare(b.group_name || b.group_key);
                if (byGroup !== 0) return byGroup;
                return a.model_name.localeCompare(b.model_name);
            });
    }, [account, activeFilter, pendingRouteOverrides, pendingDisabledOverrides]);

    const groupedModels = useMemo(() => {
        const next: Record<SiteModelRouteType, SiteModelView[]> = {
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

    const accountKeys = useMemo(() => countAccountKeys(account), [account]);
    const missingKeyGroups = useMemo(
        () => account.groups.filter((group) => !group.has_keys),
        [account.groups],
    );
    const hasPendingChanges = pendingModelKeys.size > 0 || routeMutation.isPending || disabledMutation.isPending;

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

        setPendingRouteOverrides((current) => ({
            ...current,
            [draggableId]: nextRouteType,
        }));
        setPendingModelKeys((current) => addPendingKey(current, draggableId));

        const payload: SiteModelRouteUpdateRequest[] = [
            {
                group_key: target.group.group_key,
                model_name: target.model.model_name,
                route_type: nextRouteType,
            },
        ];

        routeMutation.mutate(payload, {
            onSuccess: () => {
                setPendingRouteOverrides((current) => removeKey(current, draggableId));
                toast.success('模型请求端点格式已更新');
            },
            onError: (error) => {
                setPendingRouteOverrides((current) => removeKey(current, draggableId));
                toast.error(getErrorMessage(error, '更新模型请求端点格式失败'));
            },
            onSettled: () => {
                setPendingModelKeys((current) => removePendingKey(current, draggableId));
            },
        });
    };

    const handleToggleDisabled = (model: SiteModelView) => {
        const modelKey = makeModelKey(model.group_key, model.model_name);
        const target = findModel(account, modelKey);
        if (!target || pendingModelKeys.has(modelKey)) return;

        const currentDisabled = pendingDisabledOverrides[modelKey] ?? target.model.disabled;
        const nextDisabled = !currentDisabled;

        setPendingDisabledOverrides((current) => ({
            ...current,
            [modelKey]: nextDisabled,
        }));
        setPendingModelKeys((current) => addPendingKey(current, modelKey));

        const payload: SiteModelDisableUpdateRequest[] = [
            {
                group_key: target.group.group_key,
                model_name: target.model.model_name,
                disabled: nextDisabled,
            },
        ];

        disabledMutation.mutate(payload, {
            onSuccess: () => {
                setPendingDisabledOverrides((current) => removeKey(current, modelKey));
                toast.success(nextDisabled ? '模型已禁用' : '模型已启用');
            },
            onError: (error) => {
                setPendingDisabledOverrides((current) => removeKey(current, modelKey));
                toast.error(getErrorMessage(error, '更新模型禁用状态失败'));
            },
            onSettled: () => {
                setPendingModelKeys((current) => removePendingKey(current, modelKey));
            },
        });
    };

    const handleResetRoutes = () => {
        resetMutation.mutate(undefined, {
            onSuccess: () => {
                setPendingRouteOverrides({});
                toast.success('模型请求端点格式已重置');
            },
            onError: (error) => {
                toast.error(getErrorMessage(error, '重置模型请求端点格式失败'));
            },
        });
    };

    return (
        <div className="space-y-4">
            <div className="grid gap-3 md:grid-cols-4">
                <div className="rounded-2xl border border-border/70 bg-background/80 p-4">
                    <div className="flex items-center gap-2 text-xs text-muted-foreground">
                        <FolderTree className="size-4" />
                        分组
                    </div>
                    <div className="mt-2 text-2xl font-semibold">{account.groups.length}</div>
                </div>
                <div className="rounded-2xl border border-border/70 bg-background/80 p-4">
                    <div className="flex items-center gap-2 text-xs text-muted-foreground">
                        <Sparkles className="size-4" />
                        模型
                    </div>
                    <div className="mt-2 text-2xl font-semibold">{account.model_count}</div>
                </div>
                <div className="rounded-2xl border border-border/70 bg-background/80 p-4">
                    <div className="flex items-center gap-2 text-xs text-muted-foreground">
                        <KeyRound className="size-4" />
                        Key
                    </div>
                    <div className="mt-2 text-2xl font-semibold">
                        {accountKeys.enabled}/{accountKeys.total}
                    </div>
                </div>
                <div className="rounded-2xl border border-border/70 bg-background/80 p-4">
                    <div className="flex items-center gap-2 text-xs text-muted-foreground">
                        <Server className="size-4" />
                        状态
                    </div>
                    <div className="mt-2 flex flex-wrap gap-2">
                        <Badge
                            variant="outline"
                            className={cn(
                                'h-6 px-2 text-[11px]',
                                account.enabled
                                    ? 'border-emerald-500/30 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300'
                                    : 'border-destructive/30 bg-destructive/10 text-destructive',
                            )}
                        >
                            {account.enabled ? '账号启用' : '账号停用'}
                        </Badge>
                        <Badge
                            variant="outline"
                            className={cn(
                                'h-6 px-2 text-[11px]',
                                account.auto_sync
                                    ? 'border-blue-500/30 bg-blue-500/10 text-blue-700 dark:text-blue-300'
                                    : 'border-border bg-muted/40 text-muted-foreground',
                            )}
                        >
                            {account.auto_sync ? '自动同步' : '手动同步'}
                        </Badge>
                    </div>
                </div>
            </div>

            <div className="flex flex-col gap-3 rounded-3xl border border-border/70 bg-card/70 p-4">
                <div className="flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between">
                    <div className="space-y-1">
                        <div className="flex items-center gap-2 text-sm font-semibold">
                            <FolderTree className="size-4 text-primary" />
                            分组筛选
                        </div>
                        <div className="text-xs text-muted-foreground">
                            当前筛选下显示 {groupFilterCount(account.groups, activeFilter)} 个模型
                        </div>
                    </div>
                    <Button
                        type="button"
                        variant="outline"
                        className="rounded-2xl"
                        onClick={handleResetRoutes}
                        disabled={resetMutation.isPending || hasPendingChanges}
                    >
                        <RefreshCw className={cn('size-4', resetMutation.isPending && 'animate-spin')} />
                        {resetMutation.isPending ? '重置中...' : '重置模型端点格式'}
                    </Button>
                </div>

                <div className="overflow-x-auto pb-1">
                    <div className="flex min-w-max gap-2">
                        <button
                            type="button"
                            onClick={() => setActiveFilter(SITE_GROUP_FILTER_ALL)}
                            className={cn(
                                'inline-flex items-center gap-2 rounded-full border px-3 py-1.5 text-xs transition',
                                isSameGroupFilter(activeFilter, SITE_GROUP_FILTER_ALL)
                                    ? 'border-primary/30 bg-primary text-primary-foreground'
                                    : 'border-border bg-background hover:bg-muted/50',
                            )}
                        >
                            全部分组
                            <span>{account.groups.length}</span>
                        </button>
                        <button
                            type="button"
                            onClick={() => setActiveFilter(SITE_GROUP_FILTER_MISSING_KEYS)}
                            className={cn(
                                'inline-flex items-center gap-2 rounded-full border px-3 py-1.5 text-xs transition',
                                isSameGroupFilter(activeFilter, SITE_GROUP_FILTER_MISSING_KEYS)
                                    ? 'border-amber-500/30 bg-amber-500 text-white'
                                    : 'border-amber-500/20 bg-amber-500/10 text-amber-700 hover:bg-amber-500/15 dark:text-amber-300',
                            )}
                        >
                            未创建 Key
                            <span>{missingKeyGroups.length}</span>
                        </button>
                        {account.groups.map((group) => (
                            <button
                                key={group.group_key}
                                type="button"
                                onClick={() => setActiveFilter(createGroupFilter(group.group_key))}
                                className={cn(
                                    'inline-flex items-center gap-2 rounded-full border px-3 py-1.5 text-xs transition',
                                    isSameGroupFilter(activeFilter, createGroupFilter(group.group_key))
                                        ? 'border-primary/30 bg-primary text-primary-foreground'
                                        : 'border-border bg-background hover:bg-muted/50',
                                )}
                            >
                                <span>{group.group_name || group.group_key}</span>
                                <span>{group.models.length}</span>
                                <span className="text-[10px] opacity-80">Key {group.enabled_key_count}/{group.key_count}</span>
                                {!group.has_keys ? (
                                    <span className="rounded-full bg-amber-500/15 px-1.5 py-0.5 text-[10px] text-amber-700 dark:text-amber-300">
                                        待建
                                    </span>
                                ) : null}
                            </button>
                        ))}
                    </div>
                </div>

                {visibleGroups.some((group) => !group.has_keys) ? (
                    <div className="rounded-2xl border border-amber-500/30 bg-amber-500/10 p-3">
                        <div className="flex items-center gap-2 text-sm font-medium text-amber-800 dark:text-amber-200">
                            <CircleAlert className="size-4" />
                            当前筛选中存在未创建 Key 的分组
                        </div>
                        <div className="mt-3 flex flex-wrap gap-2">
                            {visibleGroups
                                .filter((group) => !group.has_keys)
                                .map((group) => (
                                    <Button
                                        key={group.group_key}
                                        type="button"
                                        variant="outline"
                                        size="sm"
                                        className="rounded-full border-amber-500/30 bg-white/60 text-amber-800 hover:bg-white dark:bg-background/40 dark:text-amber-200"
                                        onClick={() => {
                                            toast.info(`分组「${group.group_name || group.group_key}」的快捷创建 Key 功能暂未接入`);
                                        }}
                                    >
                                        {group.group_name || group.group_key}
                                        <span className="text-[10px] text-amber-700/80 dark:text-amber-200/80">占位</span>
                                    </Button>
                                ))}
                        </div>
                    </div>
                ) : null}
            </div>

            <div className="overflow-x-auto pb-2">
                <DragDropContext onDragEnd={handleRouteDrop}>
                    <div className="grid min-w-max grid-cols-6 gap-4">
                        {SITE_ROUTE_COLUMN_ORDER.map((routeType) => (
                            <RouteColumn
                                key={`${ROUTE_COLUMN_KEY_PREFIX}-${routeType}`}
                                routeType={routeType}
                                models={groupedModels[routeType]}
                                pendingModelKeys={pendingModelKeys}
                                onToggleDisabled={handleToggleDisabled}
                            />
                        ))}
                    </div>
                </DragDropContext>
            </div>
        </div>
    );
}

function SiteChannelDialog({ card }: { card: SiteChannelCard }) {
    const [activeAccountId, setActiveAccountId] = useState<number | null>(card.accounts[0]?.account_id ?? null);

    const resolvedAccount =
        card.accounts.find((account) => account.account_id === activeAccountId) ??
        card.accounts[0] ??
        null;

    return (
        <div className="flex max-h-[88vh] flex-col overflow-hidden">
            <DialogHeader className="border-b border-border/70 px-6 py-5 text-left">
                <DialogTitle className="flex flex-wrap items-center gap-2 text-2xl">
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
                </DialogTitle>
                <DialogDescription>
                    站点渠道 · 统一管理分组、模型端点格式、请求历史和模型禁用
                </DialogDescription>
            </DialogHeader>

            <div className="flex min-h-0 flex-1 flex-col gap-4 overflow-hidden px-6 py-5">
                <div className="overflow-x-auto pb-1">
                    <div className="flex min-w-max gap-2">
                        {card.accounts.map((account) => (
                            <button
                                key={account.account_id}
                                type="button"
                                onClick={() => setActiveAccountId(account.account_id)}
                                className={cn(
                                    'min-w-[14rem] rounded-2xl border px-4 py-3 text-left transition',
                                    resolvedAccount?.account_id === account.account_id
                                        ? 'border-primary/30 bg-primary text-primary-foreground'
                                        : 'border-border bg-background hover:bg-muted/50',
                                )}
                            >
                                <div className="flex items-center justify-between gap-3">
                                    <div className="min-w-0">
                                        <div className="truncate text-sm font-medium">{account.account_name}</div>
                                        <div
                                            className={cn(
                                                'text-[11px]',
                                                resolvedAccount?.account_id === account.account_id
                                                    ? 'text-primary-foreground/80'
                                                    : 'text-muted-foreground',
                                            )}
                                        >
                                            {account.model_count} 模型 · {account.group_count} 分组
                                        </div>
                                    </div>
                                    <Badge
                                        variant="outline"
                                        className={cn(
                                            'h-5 px-1.5 text-[10px]',
                                            account.enabled
                                                ? 'border-emerald-500/30 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300'
                                                : 'border-destructive/30 bg-destructive/10 text-destructive',
                                        )}
                                    >
                                        {account.enabled ? '启用' : '停用'}
                                    </Badge>
                                </div>
                            </button>
                        ))}
                    </div>
                </div>

                {resolvedAccount ? (
                    <div className="min-h-0 flex-1 overflow-y-auto pr-1">
                        <SiteAccountPanel key={resolvedAccount.account_id} siteId={card.site_id} account={resolvedAccount} />
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
}: {
    card: SiteChannelCard;
    layout: 'grid' | 'list';
}) {
    const [open, setOpen] = useState(false);
    const summary = useMemo(() => collectSiteSummary(card), [card]);
    const isListLayout = layout === 'list';

    return (
        <>
            <button
                type="button"
                onClick={() => setOpen(true)}
                className="w-full text-left"
            >
                <article
                    className={cn(
                        'flex h-full flex-col gap-4 rounded-3xl border border-border/70 bg-card p-4 transition hover:border-primary/20 hover:bg-card/90',
                        isListLayout && 'md:flex-row md:items-stretch',
                    )}
                    style={{
                        contentVisibility: 'auto',
                        containIntrinsicSize: isListLayout ? '260px' : '340px',
                    }}
                >
                    <div className={cn('flex min-w-0 flex-1 flex-col gap-4', isListLayout && 'md:max-w-[24rem]')}>
                        <header className="flex items-start justify-between gap-3">
                            <div className="min-w-0">
                                <div className="truncate text-lg font-bold">{card.site_name}</div>
                                <div className="mt-1 flex flex-wrap gap-2">
                                    <Badge variant="outline" className="h-6 px-2 text-[11px]">
                                        {platformLabel(card.platform)}
                                    </Badge>
                                    <Badge
                                        variant="outline"
                                        className="h-6 px-2 text-[11px] border-primary/20 bg-primary/10 text-primary"
                                    >
                                        站点渠道
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
                                        {card.enabled ? '启用' : '停用'}
                                    </Badge>
                                </div>
                            </div>
                            <div className="rounded-2xl border border-border/60 bg-background/70 px-3 py-2 text-right">
                                <div className="text-[10px] text-muted-foreground">账号</div>
                                <div className="text-lg font-semibold">{card.account_count}</div>
                            </div>
                        </header>

                        <dl className="grid grid-cols-2 gap-3">
                            <div className="rounded-2xl border border-border/60 bg-background/70 p-3">
                                <dt className="flex items-center gap-1 text-xs text-muted-foreground">
                                    <FolderTree className="size-3.5" />
                                    分组
                                </dt>
                                <dd className="mt-2 text-lg font-semibold">{summary.groupCount}</dd>
                            </div>
                            <div className="rounded-2xl border border-border/60 bg-background/70 p-3">
                                <dt className="flex items-center gap-1 text-xs text-muted-foreground">
                                    <Sparkles className="size-3.5" />
                                    模型
                                </dt>
                                <dd className="mt-2 text-lg font-semibold">{summary.modelCount}</dd>
                            </div>
                            <div className="rounded-2xl border border-border/60 bg-background/70 p-3">
                                <dt className="flex items-center gap-1 text-xs text-muted-foreground">
                                    <KeyRound className="size-3.5" />
                                    Key
                                </dt>
                                <dd className="mt-2 text-lg font-semibold">
                                    {summary.enabledKeys}/{summary.totalKeys}
                                </dd>
                            </div>
                            <div className="rounded-2xl border border-border/60 bg-background/70 p-3">
                                <dt className="flex items-center gap-1 text-xs text-muted-foreground">
                                    <UserRound className="size-3.5" />
                                    账号
                                </dt>
                                <dd className="mt-2 truncate text-sm font-medium text-foreground">
                                    {card.accounts.map((account) => account.account_name).join(' · ')}
                                </dd>
                            </div>
                        </dl>
                    </div>

                    <div className={cn('space-y-2', isListLayout ? 'md:min-w-[18rem] md:max-w-[22rem]' : '')}>
                        <div className="text-xs font-medium text-muted-foreground">端点格式分布</div>
                        <div className="flex flex-wrap gap-2">
                            {SITE_ROUTE_COLUMN_ORDER.filter((routeType) => (summary.routeCounts.get(routeType) ?? 0) > 0).map((routeType) => (
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
                </article>
            </button>

            <Dialog open={open} onOpenChange={setOpen}>
                {open ? (
                    <DialogContent className="max-w-[min(96vw,92rem)] w-[min(96vw,92rem)] rounded-[2rem] p-0 sm:max-w-[min(96vw,92rem)]">
                        <SiteChannelDialog card={card} />
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
    const { data, isLoading, error } = useSiteChannelList();

    const cards = useMemo(() => {
        const term = searchTerm.toLowerCase().trim();
        return (data ?? [])
            .filter((card) => card.account_count > 0)
            .filter((card) => {
                if (!term) return true;

                const accountNames = card.accounts.map((account) => account.account_name.toLowerCase());
                return card.site_name.toLowerCase().includes(term) || accountNames.some((name) => name.includes(term));
            })
            .filter((card) => {
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
    }, [data, searchTerm, filter, sortField, sortOrder]);

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
                站点渠道加载失败：{error.message}
            </section>
        );
    }

    if (cards.length === 0) {
        return null;
    }

    return (
        <section className="space-y-3">
            <div className="px-1">
                <div className="text-sm font-semibold">站点渠道</div>
                <div className="text-xs text-muted-foreground">
                    统一查看站点投影的分组、模型端点格式、请求历史和模型禁用状态
                </div>
            </div>

            <div className={cn('grid gap-4', layout === 'list' ? 'grid-cols-1' : 'md:grid-cols-2 xl:grid-cols-3')}>
                {cards.map((card) => (
                    <SiteCard key={card.site_id} card={card} layout={layout} />
                ))}
            </div>
        </section>
    );
}
