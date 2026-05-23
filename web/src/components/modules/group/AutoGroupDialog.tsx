'use client';

import { useEffect, useMemo, useRef, useState } from 'react';
import { useTranslations } from 'next-intl';
import { AnimatePresence, motion } from 'motion/react';
import { ChevronDown, Globe2, HelpCircle, Search, WandSparkles, X } from 'lucide-react';
import { AutoGroupType } from '@/api/endpoints/channel';
import {
    type GroupAutoGroupSource,
    useGroupAutoGroupConfig,
    useUpdateGroupAutoGroupConfig,
} from '@/api/endpoints/group';
import {
    MorphingDialogClose,
    MorphingDialogDescription,
    MorphingDialogTitle,
    useMorphingDialog,
} from '@/components/ui/morphing-dialog';
import { Button } from '@/components/ui/button';
import { Badge } from '@/components/ui/badge';
import {
    Select,
    SelectContent,
    SelectItem,
    SelectTrigger,
    SelectValue,
} from '@/components/ui/select';
import { HoverCard, HoverCardContent, HoverCardTrigger } from '@/components/ui/hover-card';
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '@/components/animate-ui/components/animate/tooltip';
import { toast } from '@/components/common/Toast';
import { cn } from '@/lib/utils';

const AUTO_GROUP_VALUES = [
    AutoGroupType.None,
    AutoGroupType.Fuzzy,
    AutoGroupType.Exact,
    AutoGroupType.Regex,
] as const;
const MODEL_PREVIEW_LIMIT = 24;
const MANUAL_GROUP_KEY = '__manual__';

type SourceTreeGroup = {
    key: string;
    label: string;
    sources: GroupAutoGroupSource[];
    manual: boolean;
};

function modeKey(value: AutoGroupType) {
    switch (value) {
        case AutoGroupType.Fuzzy:
            return 'fuzzy';
        case AutoGroupType.Exact:
            return 'exact';
        case AutoGroupType.Regex:
            return 'regex';
        case AutoGroupType.None:
        default:
            return 'none';
    }
}

function matchesKeyword(source: GroupAutoGroupSource, keyword: string) {
    if (!keyword) return true;
    const haystack = [
        source.channel_name,
        source.site_name,
        source.site_account_name,
        source.site_group_name,
        source.site_group_key,
        source.endpoint_type,
        ...source.models,
    ].join('\n').toLowerCase();
    return haystack.includes(keyword);
}

function TristateCheckbox({
    state,
    onChange,
    ariaLabel,
    className,
}: {
    state: 'unchecked' | 'partial' | 'checked';
    onChange: (next: boolean) => void;
    ariaLabel: string;
    className?: string;
}) {
    const ref = useRef<HTMLInputElement>(null);
    useEffect(() => {
        if (ref.current) ref.current.indeterminate = state === 'partial';
    }, [state]);
    return (
        <input
            ref={ref}
            type="checkbox"
            checked={state === 'checked'}
            aria-label={ariaLabel}
            onClick={(event) => event.stopPropagation()}
            onChange={(event) => onChange(event.target.checked)}
            className={cn(
                'size-3.5 shrink-0 rounded border-border bg-background accent-primary',
                className,
            )}
        />
    );
}

function ModelPreview({ source }: { source: GroupAutoGroupSource }) {
    const t = useTranslations('group.autoGroup');
    const models = source.models.slice(0, MODEL_PREVIEW_LIMIT);
    const extraCount = Math.max(0, source.models.length - MODEL_PREVIEW_LIMIT);

    return (
        <HoverCard openDelay={120} closeDelay={150}>
            <HoverCardTrigger asChild>
                <button
                    type="button"
                    className="h-5 rounded-md px-1.5 text-[10px] tabular-nums text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
                    aria-label={t('source.modelCount', { count: source.model_count })}
                >
                    {source.model_count}
                </button>
            </HoverCardTrigger>
            <HoverCardContent side="top" align="center" sideOffset={8} className="w-72 rounded-xl p-3">
                <div className="mb-2 text-xs font-medium text-foreground">
                    {t('source.modelCount', { count: source.model_count })}
                </div>
                {models.length > 0 ? (
                    <div className="flex max-h-56 flex-wrap gap-1 overflow-y-auto">
                        {models.map((model) => (
                            <Badge key={model} variant="secondary" className="max-w-64 truncate px-1.5 text-[10px] font-normal">
                                {model}
                            </Badge>
                        ))}
                        {extraCount > 0 ? (
                            <Badge variant="secondary" className="px-1.5 text-[10px] font-normal">
                                {t('source.moreModels', { count: extraCount })}
                            </Badge>
                        ) : null}
                    </div>
                ) : (
                    <div className="text-xs text-muted-foreground">{t('source.noModels')}</div>
                )}
            </HoverCardContent>
        </HoverCard>
    );
}

function ChannelRow({
    source,
    mode,
    overridden,
    globalMode,
    selected,
    onSelectedChange,
    onModeChange,
}: {
    source: GroupAutoGroupSource;
    mode: AutoGroupType;
    overridden: boolean;
    globalMode: AutoGroupType;
    selected: boolean;
    onSelectedChange: (next: boolean) => void;
    onModeChange: (mode: AutoGroupType) => void;
}) {
    const t = useTranslations('group.autoGroup');
    const configured = mode !== AutoGroupType.None;

    return (
        <div
            className={cn(
                'mx-2 mb-1 flex h-8 items-center gap-2 rounded-lg border px-2 text-left transition-colors',
                selected
                    ? 'border-primary/40 bg-primary/5 hover:bg-primary/10'
                    : 'border-border/40 bg-background hover:bg-muted/60',
            )}
        >
            <TristateCheckbox
                state={selected ? 'checked' : 'unchecked'}
                onChange={onSelectedChange}
                ariaLabel={source.channel_name}
            />
            <span
                className={cn(
                    'min-w-0 flex-1 truncate text-xs',
                    configured ? 'font-medium text-foreground' : 'text-muted-foreground',
                )}
            >
                {source.channel_name}
            </span>
            {!source.enabled ? (
                <Badge variant="outline" className="h-5 px-1.5 text-[10px] text-muted-foreground">
                    {t('source.disabled')}
                </Badge>
            ) : null}
            {overridden ? (
                <TooltipProvider>
                    <Tooltip>
                        <TooltipTrigger asChild>
                            <Badge
                                variant="outline"
                                className="h-5 cursor-help gap-1 border-primary/30 bg-primary/10 px-1.5 text-[10px] text-primary"
                            >
                                <Globe2 className="size-3" />
                                {t(`mode.${modeKey(globalMode)}`)}
                            </Badge>
                        </TooltipTrigger>
                        <TooltipContent className="max-w-xs">{t('source.followingGlobalTip')}</TooltipContent>
                    </Tooltip>
                </TooltipProvider>
            ) : null}
            <ModelPreview source={source} />
            <Select value={String(mode)} onValueChange={(value) => onModeChange(Number(value) as AutoGroupType)}>
                <SelectTrigger
                    size="sm"
                    className={cn(
                        '!h-6 w-auto min-w-18 justify-end rounded-md border-transparent bg-transparent px-1 py-0 text-xs shadow-none hover:text-primary focus-visible:border-transparent focus-visible:ring-0 dark:bg-transparent dark:hover:bg-transparent',
                        configured ? 'font-medium text-foreground' : 'text-muted-foreground',
                        overridden ? 'opacity-60' : '',
                    )}
                >
                    <SelectValue />
                </SelectTrigger>
                <SelectContent className="rounded-xl">
                    {AUTO_GROUP_VALUES.map((value) => (
                        <SelectItem key={value} value={String(value)}>
                            {t(`mode.${modeKey(value)}`)}
                        </SelectItem>
                    ))}
                </SelectContent>
            </Select>
        </div>
    );
}

export function GroupAutoGroupDialogContent() {
    const t = useTranslations('group.autoGroup');
    const { setIsOpen } = useMorphingDialog();
    const { data: config, isLoading, error } = useGroupAutoGroupConfig();
    const updateConfig = useUpdateGroupAutoGroupConfig();
    const [keyword, setKeyword] = useState('');
    const [modes, setModes] = useState<Record<number, AutoGroupType>>({});
    const [projectedGlobalMode, setProjectedGlobalMode] = useState<AutoGroupType>(AutoGroupType.None);
    const [expanded, setExpanded] = useState<Set<string>>(new Set());
    const [selection, setSelection] = useState<Set<number>>(new Set());

    useEffect(() => {
        if (!config) return;
        const next: Record<number, AutoGroupType> = {};
        for (const source of config.sources) {
            next[source.channel_id] = source.auto_group;
        }
        queueMicrotask(() => {
            setModes(next);
            setProjectedGlobalMode(config.projected_global_auto_group);
        });
    }, [config]);

    const normalizedKeyword = keyword.trim().toLowerCase();
    const sources = useMemo(() => config?.sources ?? [], [config?.sources]);

    const groups = useMemo<SourceTreeGroup[]>(() => {
        const siteBuckets = new Map<string, SourceTreeGroup>();
        const manualSources: GroupAutoGroupSource[] = [];

        for (const source of sources) {
            if (!matchesKeyword(source, normalizedKeyword)) continue;
            if (!source.managed) {
                manualSources.push(source);
                continue;
            }
            const key = source.site_id ? `site:${source.site_id}` : `site:${source.site_name || 'unknown'}`;
            const existing = siteBuckets.get(key);
            if (existing) {
                existing.sources.push(source);
                continue;
            }
            siteBuckets.set(key, {
                key,
                label: source.site_name || t('source.unknownSite'),
                sources: [source],
                manual: false,
            });
        }

        const list = Array.from(siteBuckets.values()).sort((a, b) => a.label.localeCompare(b.label));
        if (manualSources.length > 0) {
            list.push({ key: MANUAL_GROUP_KEY, label: t('source.manualGroup'), sources: manualSources, manual: true });
        }
        for (const group of list) {
            group.sources.sort((a, b) => a.channel_name.localeCompare(b.channel_name));
        }
        return list;
    }, [sources, normalizedKeyword, t]);

    const configuredCount = useMemo(
        () => sources.filter((s) => (modes[s.channel_id] ?? AutoGroupType.None) !== AutoGroupType.None).length,
        [sources, modes],
    );

    const dirtyItems = useMemo(() => {
        if (!config) return [];
        return sources
            .map((source) => ({
                channel_id: source.channel_id,
                auto_group: modes[source.channel_id] ?? AutoGroupType.None,
                original: source.auto_group,
            }))
            .filter((item) => item.auto_group !== item.original)
            .map(({ channel_id, auto_group }) => ({ channel_id, auto_group }));
    }, [config, modes, sources]);

    const globalDirty = !!config && projectedGlobalMode !== config.projected_global_auto_group;
    const globalModeEnabled = projectedGlobalMode !== AutoGroupType.None;
    const shouldRunAfterSave = useMemo(() => {
        if (!config) return false;
        if (globalDirty && projectedGlobalMode !== AutoGroupType.None) return true;
        return dirtyItems.some((item) => item.auto_group !== AutoGroupType.None);
    }, [config, dirtyItems, globalDirty, projectedGlobalMode]);
    const hasChanges = globalDirty || dirtyItems.length > 0;
    const isPending = updateConfig.isPending;

    const toggleCollapsed = (key: string) => {
        setExpanded((current) => {
            const next = new Set(current);
            if (next.has(key)) next.delete(key);
            else next.add(key);
            return next;
        });
    };

    const toggleSelected = (channelId: number, next: boolean) => {
        setSelection((current) => {
            const updated = new Set(current);
            if (next) updated.add(channelId);
            else updated.delete(channelId);
            return updated;
        });
    };

    const setGroupSelection = (group: SourceTreeGroup, next: boolean) => {
        setSelection((current) => {
            const updated = new Set(current);
            for (const source of group.sources) {
                if (next) updated.add(source.channel_id);
                else updated.delete(source.channel_id);
            }
            return updated;
        });
    };

    const applyBulkMode = (mode: AutoGroupType) => {
        if (selection.size === 0) return;
        setModes((current) => {
            const updated = { ...current };
            for (const id of selection) updated[id] = mode;
            return updated;
        });
        setSelection(new Set());
    };

    const clearSelection = () => setSelection(new Set());

    const handleSave = () => {
        if (!config) return;
        updateConfig.mutate(
            {
                projected_global_auto_group: globalDirty ? projectedGlobalMode : undefined,
                items: dirtyItems,
                run_now: shouldRunAfterSave,
            },
            {
                onSuccess: () => {
                    toast.success(shouldRunAfterSave ? t('toast.savedAndRun') : t('toast.saved'));
                    setIsOpen(false);
                },
                onError: (err) => toast.error(t('toast.saveFailed'), { description: err.message }),
            },
        );
    };

    return (
        <div className="flex h-[calc(100vh-2rem)] min-h-0 w-screen max-w-full flex-col overflow-hidden md:max-w-2xl">
            <MorphingDialogTitle className="shrink-0">
                <header className="mb-3 flex items-center justify-between gap-4">
                    <h2 className="flex items-center gap-2 text-2xl font-bold text-card-foreground">
                        <WandSparkles className="size-5 text-primary" />
                        {t('title')}
                    </h2>
                    <MorphingDialogClose className="relative right-0 top-0" />
                </header>
            </MorphingDialogTitle>

            <MorphingDialogDescription className="flex min-h-0 flex-1 flex-col overflow-hidden">
                {error ? (
                    <div className="rounded-2xl border border-destructive/30 bg-destructive/10 p-4 text-sm text-destructive">
                        {t('loadFailed', { message: error.message })}
                    </div>
                ) : (
                    <>
                        <div className="mb-3 shrink-0 rounded-xl border border-border/50 bg-muted/30 px-3 py-2">
                            <div className="flex flex-wrap items-center justify-between gap-3">
                                <div className="flex min-w-0 items-center gap-2">
                                    <Globe2 className="size-4 shrink-0 text-muted-foreground" />
                                    <span className="text-sm font-medium text-foreground">{t('global.title')}</span>
                                    <TooltipProvider>
                                        <Tooltip>
                                            <TooltipTrigger asChild>
                                                <HelpCircle className="size-4 cursor-help text-muted-foreground" />
                                            </TooltipTrigger>
                                            <TooltipContent className="max-w-xs">
                                                {t('global.description')}
                                            </TooltipContent>
                                        </Tooltip>
                                    </TooltipProvider>
                                </div>
                                <Select
                                    value={String(projectedGlobalMode)}
                                    onValueChange={(value) => setProjectedGlobalMode(Number(value) as AutoGroupType)}
                                    disabled={isLoading || isPending}
                                >
                                    <SelectTrigger className="h-8 w-36 rounded-xl bg-background text-xs">
                                        <SelectValue />
                                    </SelectTrigger>
                                    <SelectContent className="rounded-xl">
                                        {AUTO_GROUP_VALUES.map((value) => (
                                            <SelectItem key={value} value={String(value)}>
                                                {t(`mode.${modeKey(value)}`)}
                                            </SelectItem>
                                        ))}
                                    </SelectContent>
                                </Select>
                            </div>
                        </div>

                        <section className="flex min-h-0 flex-1 flex-col overflow-hidden rounded-xl border border-border/50 bg-muted/30">
                            <div className="flex h-10 shrink-0 items-center gap-2 border-b border-border/30 bg-muted/50 px-3 py-2">
                                <span className="min-w-0 truncate text-sm font-medium text-foreground">
                                    {t('sections.channels')}
                                </span>
                                {configuredCount > 0 ? (
                                    <Badge variant="outline" className="h-5 border-primary/30 bg-primary/10 px-1.5 text-[10px] text-primary">
                                        {configuredCount}
                                    </Badge>
                                ) : null}
                                <div className="ml-auto flex items-center gap-2">
                                    <div className="relative w-40 sm:w-48">
                                        <Search className="pointer-events-none absolute left-2 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" />
                                        <input
                                            value={keyword}
                                            onChange={(event) => setKeyword(event.target.value)}
                                            placeholder={t('searchPlaceholder')}
                                            className="h-6 w-full rounded-lg border border-border/60 bg-background/70 pl-7 pr-2 text-xs shadow-none outline-none focus-visible:ring-1 focus-visible:ring-ring"
                                        />
                                    </div>
                                </div>
                            </div>
                            <AnimatePresence initial={false}>
                                {selection.size > 0 ? (
                                    <motion.div
                                        key="bulk-bar"
                                        initial={{ height: 0, opacity: 0 }}
                                        animate={{ height: 'auto', opacity: 1 }}
                                        exit={{ height: 0, opacity: 0 }}
                                        transition={{ duration: 0.22, ease: 'easeOut' }}
                                        className="overflow-hidden"
                                    >
                                        <div className="flex h-10 items-center gap-2 border-b border-primary/20 bg-primary/5 px-3 text-xs">
                                            <span className="font-medium text-primary">
                                                {t('bulk.selected', { count: selection.size })}
                                            </span>
                                            <Select onValueChange={(value) => applyBulkMode(Number(value) as AutoGroupType)}>
                                                <SelectTrigger
                                                    size="sm"
                                                    className="!h-7 ml-auto w-36 rounded-lg border-primary/30 bg-background text-xs"
                                                >
                                                    <SelectValue placeholder={t('bulk.placeholder')} />
                                                </SelectTrigger>
                                                <SelectContent className="rounded-xl">
                                                    {AUTO_GROUP_VALUES.map((value) => (
                                                        <SelectItem key={value} value={String(value)}>
                                                            {t(`mode.${modeKey(value)}`)}
                                                        </SelectItem>
                                                    ))}
                                                </SelectContent>
                                            </Select>
                                            <button
                                                type="button"
                                                onClick={clearSelection}
                                                className="flex size-6 shrink-0 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
                                                aria-label={t('bulk.clear')}
                                            >
                                                <X className="size-3.5" />
                                            </button>
                                        </div>
                                    </motion.div>
                                ) : null}
                            </AnimatePresence>
                            <div className="min-h-0 flex-1 overflow-y-auto rounded-b-xl">
                                {isLoading ? (
                                    <div className="p-6 text-center text-sm text-muted-foreground">{t('loading')}</div>
                                ) : groups.length === 0 ? (
                                    <div className="p-6 text-center text-sm text-muted-foreground">{t('emptyChannels')}</div>
                                ) : (
                                    groups.map((group) => {
                                        const isExpanded = expanded.has(group.key) || !!normalizedKeyword;
                                        const groupConfigured = group.sources.filter(
                                            (s) => (modes[s.channel_id] ?? AutoGroupType.None) !== AutoGroupType.None,
                                        ).length;
                                        const groupSelected = group.sources.filter((s) => selection.has(s.channel_id)).length;
                                        const groupState: 'unchecked' | 'partial' | 'checked' =
                                            groupSelected === 0
                                                ? 'unchecked'
                                                : groupSelected === group.sources.length
                                                    ? 'checked'
                                                    : 'partial';
                                        return (
                                            <div key={group.key} className="border-b border-border/40 last:border-b-0">
                                                <div className="mx-2 my-1 flex h-8 w-[calc(100%-1rem)] items-center gap-2 rounded-lg bg-muted px-2 transition-colors hover:bg-muted/80">
                                                    <TristateCheckbox
                                                        state={groupState}
                                                        onChange={(next) => setGroupSelection(group, next)}
                                                        ariaLabel={group.label}
                                                    />
                                                    <button
                                                        type="button"
                                                        onClick={() => toggleCollapsed(group.key)}
                                                        className="flex min-w-0 flex-1 items-center gap-2 text-left"
                                                    >
                                                        <ChevronDown
                                                            className={cn(
                                                                'size-3.5 shrink-0 text-muted-foreground transition-transform',
                                                                isExpanded ? '' : '-rotate-90',
                                                            )}
                                                        />
                                                        <span className="min-w-0 flex-1 truncate text-xs font-semibold text-foreground">
                                                            {group.label}
                                                        </span>
                                                        {groupConfigured > 0 ? (
                                                            <span className="text-[10px] tabular-nums text-primary">
                                                                {groupConfigured}/{group.sources.length}
                                                            </span>
                                                        ) : (
                                                            <span className="text-[10px] tabular-nums text-muted-foreground">
                                                                {group.sources.length}
                                                            </span>
                                                        )}
                                                    </button>
                                                </div>
                                                <AnimatePresence initial={false}>
                                                    {isExpanded ? (
                                                        <motion.div
                                                            key="content"
                                                            initial={{ height: 0, opacity: 0 }}
                                                            animate={{ height: 'auto', opacity: 1 }}
                                                            exit={{ height: 0, opacity: 0 }}
                                                            transition={{ duration: 0.22, ease: 'easeOut' }}
                                                            className="overflow-hidden"
                                                        >
                                                            <div className="flex flex-col">
                                                                {group.sources.map((source) => (
                                                                    <ChannelRow
                                                                        key={source.channel_id}
                                                                        source={source}
                                                                        mode={modes[source.channel_id] ?? AutoGroupType.None}
                                                                        overridden={globalModeEnabled && source.managed}
                                                                        globalMode={projectedGlobalMode}
                                                                        selected={selection.has(source.channel_id)}
                                                                        onSelectedChange={(next) =>
                                                                            toggleSelected(source.channel_id, next)
                                                                        }
                                                                        onModeChange={(mode) =>
                                                                            setModes((current) => ({
                                                                                ...current,
                                                                                [source.channel_id]: mode,
                                                                            }))
                                                                        }
                                                                    />
                                                                ))}
                                                            </div>
                                                        </motion.div>
                                                    ) : null}
                                                </AnimatePresence>
                                            </div>
                                        );
                                    })
                                )}
                            </div>
                        </section>

                        <div className="mt-4 flex shrink-0 flex-col gap-2 sm:flex-row">
                            <Button
                                type="button"
                                variant="secondary"
                                className="h-11 flex-1 rounded-xl"
                                onClick={() => setIsOpen(false)}
                                disabled={isPending}
                            >
                                {t('buttons.cancel')}
                            </Button>
                            <Button
                                type="button"
                                className="h-11 flex-1 rounded-xl"
                                onClick={handleSave}
                                disabled={isPending || isLoading || !hasChanges}
                            >
                                {updateConfig.isPending ? t('buttons.saving') : t('buttons.save')}
                            </Button>
                        </div>
                    </>
                )}
            </MorphingDialogDescription>
        </div>
    );
}
