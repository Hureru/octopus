'use client';

import { useEffect, useMemo, useState, type ReactNode } from 'react';
import { useTranslations } from 'next-intl';
import { Globe2, Plus, RefreshCw, Search, Sparkles, X } from 'lucide-react';
import { AutoGroupType } from '@/api/endpoints/channel';
import {
    type GroupAutoGroupSource,
    useGroupAutoGroupConfig,
    useRunGroupAutoGroup,
    useUpdateGroupAutoGroupConfig,
} from '@/api/endpoints/group';
import {
    MorphingDialogClose,
    MorphingDialogDescription,
    MorphingDialogTitle,
    useMorphingDialog,
} from '@/components/ui/morphing-dialog';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Badge } from '@/components/ui/badge';
import {
    Select,
    SelectContent,
    SelectItem,
    SelectTrigger,
    SelectValue,
} from '@/components/ui/select';
import { toast } from '@/components/common/Toast';
import { cn } from '@/lib/utils';

const AUTO_GROUP_VALUES = [
    AutoGroupType.None,
    AutoGroupType.Fuzzy,
    AutoGroupType.Exact,
    AutoGroupType.Regex,
] as const;

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

function sourceTitle(source: GroupAutoGroupSource) {
    if (!source.managed) return source.channel_name;
    return [source.site_name, source.site_account_name, source.site_group_name]
        .map((item) => item?.trim())
        .filter(Boolean)
        .join(' / ') || source.channel_name;
}

function sourceSubtitle(source: GroupAutoGroupSource) {
    const parts = source.managed
        ? [source.endpoint_type, `#${source.channel_id}`, source.channel_name]
        : [`#${source.channel_id}`, source.endpoint_type];
    return parts.map((item) => item?.trim()).filter(Boolean).join(' · ');
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

function SourceCard({
    source,
    action,
    mode,
    onModeChange,
    muted,
}: {
    source: GroupAutoGroupSource;
    action: ReactNode;
    mode?: AutoGroupType;
    onModeChange?: (mode: AutoGroupType) => void;
    muted?: boolean;
}) {
    const t = useTranslations('group.autoGroup');

    return (
        <div className={cn('rounded-2xl border border-border/60 bg-background p-3', muted && 'opacity-70')}>
            <div className="flex items-start justify-between gap-3">
                <div className="min-w-0">
                    <div className="flex min-w-0 flex-wrap items-center gap-1.5">
                        <span className="truncate text-sm font-medium text-foreground">{sourceTitle(source)}</span>
                        {source.managed ? <Badge variant="outline" className="px-1.5 text-[10px]">{t('source.siteGroup')}</Badge> : null}
                        {!source.enabled ? <Badge variant="outline" className="px-1.5 text-[10px] text-muted-foreground">{t('source.disabled')}</Badge> : null}
                        {source.global_override ? <Badge variant="outline" className="border-primary/30 bg-primary/10 px-1.5 text-[10px] text-primary">{t('source.globalActive')}</Badge> : null}
                    </div>
                    <div className="mt-1 truncate text-xs text-muted-foreground">{sourceSubtitle(source)}</div>
                    <div className="mt-1 text-xs text-muted-foreground">{t('source.modelCount', { count: source.model_count })}</div>
                </div>
                <div className="shrink-0">{action}</div>
            </div>

            {source.models.length > 0 ? (
                <div className="mt-2 flex flex-wrap gap-1">
                    {source.models.slice(0, 6).map((model) => (
                        <Badge key={model} variant="secondary" className="max-w-40 truncate px-1.5 text-[10px] font-normal">
                            {model}
                        </Badge>
                    ))}
                    {source.models.length > 6 ? (
                        <Badge variant="secondary" className="px-1.5 text-[10px] font-normal">+{source.models.length - 6}</Badge>
                    ) : null}
                </div>
            ) : null}

            {mode !== undefined && onModeChange ? (
                <div className="mt-3 flex items-center justify-between gap-3 border-t border-border/50 pt-3">
                    <span className="text-xs text-muted-foreground">{t('matchMode')}</span>
                    <Select value={String(mode)} onValueChange={(value) => onModeChange(Number(value) as AutoGroupType)}>
                        <SelectTrigger className="h-8 w-32 rounded-xl bg-background text-xs">
                            <SelectValue />
                        </SelectTrigger>
                        <SelectContent className="rounded-xl">
                            {AUTO_GROUP_VALUES.filter((value) => value !== AutoGroupType.None).map((value) => (
                                <SelectItem key={value} value={String(value)}>{t(`mode.${modeKey(value)}`)}</SelectItem>
                            ))}
                        </SelectContent>
                    </Select>
                </div>
            ) : null}
        </div>
    );
}

export function GroupAutoGroupDialogContent() {
    const t = useTranslations('group.autoGroup');
    const { setIsOpen } = useMorphingDialog();
    const { data: config, isLoading, error } = useGroupAutoGroupConfig();
    const updateConfig = useUpdateGroupAutoGroupConfig();
    const runAutoGroup = useRunGroupAutoGroup();
    const [keyword, setKeyword] = useState('');
    const [selectedModes, setSelectedModes] = useState<Record<number, AutoGroupType>>({});
    const [projectedGlobalMode, setProjectedGlobalMode] = useState<AutoGroupType>(AutoGroupType.None);

    useEffect(() => {
        if (!config) return;
        const next: Record<number, AutoGroupType> = {};
        for (const source of config.sources) {
            if (source.auto_group !== AutoGroupType.None) {
                next[source.channel_id] = source.auto_group;
            }
        }
        queueMicrotask(() => {
            setSelectedModes(next);
            setProjectedGlobalMode(config.projected_global_auto_group);
        });
    }, [config]);

    const normalizedKeyword = keyword.trim().toLowerCase();
    const sources = useMemo(() => config?.sources ?? [], [config?.sources]);
    const sourceById = useMemo(() => new Map(sources.map((source) => [source.channel_id, source])), [sources]);

    const availableSources = useMemo(
        () => sources.filter((source) => !(source.channel_id in selectedModes) && matchesKeyword(source, normalizedKeyword)),
        [sources, selectedModes, normalizedKeyword],
    );

    const selectedSources = useMemo(
        () => Object.keys(selectedModes)
            .map((id) => sourceById.get(Number(id)))
            .filter((source): source is GroupAutoGroupSource => !!source)
            .filter((source) => matchesKeyword(source, normalizedKeyword)),
        [selectedModes, sourceById, normalizedKeyword],
    );

    const dirtyItems = useMemo(() => {
        if (!config) return [];
        return sources
            .map((source) => ({
                channel_id: source.channel_id,
                auto_group: selectedModes[source.channel_id] ?? AutoGroupType.None,
                original: source.auto_group,
            }))
            .filter((item) => item.auto_group !== item.original)
            .map(({ channel_id, auto_group }) => ({ channel_id, auto_group }));
    }, [config, selectedModes, sources]);

    const globalDirty = !!config && projectedGlobalMode !== config.projected_global_auto_group;
    const hasChanges = globalDirty || dirtyItems.length > 0;
    const isPending = updateConfig.isPending || runAutoGroup.isPending;
    const globalModeLabel = t(`mode.${modeKey(projectedGlobalMode)}`);

    const handleAdd = (source: GroupAutoGroupSource) => {
        setSelectedModes((current) => ({
            ...current,
            [source.channel_id]: source.auto_group === AutoGroupType.None ? AutoGroupType.Fuzzy : source.auto_group,
        }));
    };

    const handleRemove = (channelId: number) => {
        setSelectedModes((current) => {
            const next = { ...current };
            delete next[channelId];
            return next;
        });
    };

    const handleSave = (runNow: boolean) => {
        if (!config) return;
        updateConfig.mutate({
            projected_global_auto_group: globalDirty ? projectedGlobalMode : undefined,
            items: dirtyItems,
            run_now: runNow,
        }, {
            onSuccess: () => {
                toast.success(runNow ? t('toast.savedAndRun') : t('toast.saved'));
                setIsOpen(false);
            },
            onError: (err) => toast.error(t('toast.saveFailed'), { description: err.message }),
        });
    };

    const handleRunAll = () => {
        runAutoGroup.mutate({}, {
            onSuccess: () => toast.success(t('toast.runSuccess')),
            onError: (err) => toast.error(t('toast.runFailed'), { description: err.message }),
        });
    };

    return (
        <div className="flex h-[calc(100vh-2rem)] w-screen max-w-full flex-col md:max-w-5xl">
            <MorphingDialogTitle className="shrink-0">
                <header className="mb-4 flex items-start justify-between gap-4">
                    <div>
                        <h2 className="flex items-center gap-2 text-2xl font-bold text-card-foreground">
                            <Sparkles className="size-5 text-primary" />
                            {t('title')}
                        </h2>
                        <p className="mt-1 text-sm text-muted-foreground">
                            {t('description')}
                        </p>
                    </div>
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
                        <div className="mb-4 rounded-2xl border border-border/60 bg-muted/20 p-4">
                            <div className="flex flex-wrap items-center justify-between gap-4">
                                <div className="flex min-w-0 items-start gap-3">
                                    <Globe2 className="mt-0.5 size-5 shrink-0 text-muted-foreground" />
                                    <div className="min-w-0">
                                        <div className="text-sm font-medium text-foreground">{t('global.title')}</div>
                                        <div className="mt-1 text-xs leading-relaxed text-muted-foreground">
                                            {t('global.description')}
                                        </div>
                                    </div>
                                </div>
                                <Select value={String(projectedGlobalMode)} onValueChange={(value) => setProjectedGlobalMode(Number(value) as AutoGroupType)} disabled={isLoading || isPending}>
                                    <SelectTrigger className="w-40 rounded-xl bg-background">
                                        <SelectValue />
                                    </SelectTrigger>
                                    <SelectContent className="rounded-xl">
                                        {AUTO_GROUP_VALUES.map((value) => (
                                            <SelectItem key={value} value={String(value)}>{t(`mode.${modeKey(value)}`)}</SelectItem>
                                        ))}
                                    </SelectContent>
                                </Select>
                            </div>
                        </div>

                        <div className="mb-3 flex flex-wrap items-center justify-between gap-3">
                            <div className="relative w-full sm:w-72">
                                <Search className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
                                <Input
                                    value={keyword}
                                    onChange={(event) => setKeyword(event.target.value)}
                                    placeholder={t('searchPlaceholder')}
                                    className="rounded-xl pl-9"
                                />
                            </div>
                            <Button type="button" variant="outline" className="rounded-xl" onClick={handleRunAll} disabled={isPending || isLoading}>
                                <RefreshCw className={cn('size-4', runAutoGroup.isPending && 'animate-spin')} />
                                {t('runAll')}
                            </Button>
                        </div>

                        <div className="grid min-h-0 flex-1 gap-4 md:grid-cols-2">
                            <section className="flex min-h-0 flex-col rounded-2xl border border-border/60 bg-muted/20">
                                <div className="flex items-center justify-between border-b border-border/50 px-4 py-3">
                                    <span className="text-sm font-medium text-foreground">{t('sections.available')}</span>
                                    <span className="text-xs text-muted-foreground">{availableSources.length}</span>
                                </div>
                                <div className="min-h-0 flex-1 space-y-2 overflow-y-auto p-3">
                                    {isLoading ? (
                                        <div className="p-6 text-center text-sm text-muted-foreground">{t('loading')}</div>
                                    ) : availableSources.length === 0 ? (
                                        <div className="p-6 text-center text-sm text-muted-foreground">{t('emptyAvailable')}</div>
                                    ) : availableSources.map((source) => (
                                        <SourceCard
                                            key={source.channel_id}
                                            source={source}
                                            action={(
                                                <Button type="button" variant="outline" size="sm" className="h-8 rounded-xl" onClick={() => handleAdd(source)}>
                                                    <Plus className="size-3.5" />
                                                    {t('add')}
                                                </Button>
                                            )}
                                        />
                                    ))}
                                </div>
                            </section>

                            <section className="flex min-h-0 flex-col rounded-2xl border border-border/60 bg-muted/20">
                                <div className="flex items-center justify-between border-b border-border/50 px-4 py-3">
                                    <span className="text-sm font-medium text-foreground">{t('sections.selected')}</span>
                                    <span className="text-xs text-muted-foreground">{Object.keys(selectedModes).length}</span>
                                </div>
                                <div className="min-h-0 flex-1 space-y-2 overflow-y-auto p-3">
                                    {selectedSources.length === 0 ? (
                                        <div className="p-6 text-center text-sm text-muted-foreground">{t('emptySelected')}</div>
                                    ) : selectedSources.map((source) => (
                                        <SourceCard
                                            key={source.channel_id}
                                            source={source}
                                            mode={selectedModes[source.channel_id] ?? AutoGroupType.Fuzzy}
                                            onModeChange={(mode) => setSelectedModes((current) => ({ ...current, [source.channel_id]: mode }))}
                                            action={(
                                                <Button type="button" variant="ghost" size="sm" className="h-8 rounded-xl text-muted-foreground hover:text-destructive" onClick={() => handleRemove(source.channel_id)}>
                                                    <X className="size-4" />
                                                </Button>
                                            )}
                                        />
                                    ))}
                                </div>
                            </section>
                        </div>

                        <div className="mt-4 rounded-2xl border border-border/60 bg-muted/20 px-4 py-3 text-xs leading-relaxed text-muted-foreground">
                            {t('notice', { mode: globalModeLabel })}
                        </div>

                        <div className="mt-4 flex shrink-0 flex-col gap-2 sm:flex-row">
                            <Button type="button" variant="secondary" className="h-11 flex-1 rounded-xl" onClick={() => setIsOpen(false)} disabled={isPending}>{t('buttons.cancel')}</Button>
                            <Button type="button" variant="outline" className="h-11 flex-1 rounded-xl" onClick={() => handleSave(false)} disabled={isPending || isLoading || !hasChanges}>
                                {updateConfig.isPending ? t('buttons.saving') : t('buttons.save')}
                            </Button>
                            <Button type="button" className="h-11 flex-1 rounded-xl" onClick={() => handleSave(true)} disabled={isPending || isLoading || (!hasChanges && Object.keys(selectedModes).length === 0 && projectedGlobalMode === AutoGroupType.None)}>
                                {updateConfig.isPending ? t('buttons.running') : t('buttons.saveAndRun')}
                            </Button>
                        </div>
                    </>
                )}
            </MorphingDialogDescription>
        </div>
    );
}
