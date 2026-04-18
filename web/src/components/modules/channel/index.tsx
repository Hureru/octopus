'use client';

import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useTranslations } from 'next-intl';
import { useChannelList } from '@/api/endpoints/channel';
import { useSiteChannelList } from '@/api/endpoints/site-channel';
import { Card } from './Card';
import { useSearchStore, useToolbarViewOptionsStore } from '@/components/modules/toolbar';
import { SiteChannelCompletionAction, SiteChannelSection } from '@/components/modules/site-channel';
import { cn } from '@/lib/utils';
import { VirtualizedGrid } from '@/components/common/VirtualizedGrid';
import { AnimatePresence, motion } from 'motion/react';
import {
    isChannelJumpTarget,
    isSiteChannelJumpTarget,
    type ChannelJumpTarget,
    type PendingJump,
    useJumpStore,
} from '@/stores/jump';
import {
    Tabs,
    TabsList,
    TabsHighlight,
    TabsHighlightItem,
    TabsTrigger,
} from '@/components/animate-ui/primitives/animate/tabs';

type ChannelPendingJump = PendingJump & { target: ChannelJumpTarget };
type ChannelTab = 'site' | 'manual';

const TAB_STORAGE_KEY = 'octopus:channel-tab';

function tabFromJump(target: PendingJump['target'] | undefined): ChannelTab | null {
    if (!target) return null;
    if (isSiteChannelJumpTarget(target)) return 'site';
    if (isChannelJumpTarget(target)) return 'manual';
    return null;
}

function readInitialTab(): ChannelTab {
    const fromJump = tabFromJump(useJumpStore.getState().pending?.target);
    if (fromJump) return fromJump;

    if (typeof window === 'undefined') return 'site';
    try {
        const stored = window.sessionStorage.getItem(TAB_STORAGE_KEY);
        return stored === 'manual' ? 'manual' : 'site';
    } catch {
        return 'site';
    }
}

export function Channel() {
    const t = useTranslations('channel.tabs');
    const { data: channelsData, isLoading, error } = useChannelList();
    const { data: siteChannelsData } = useSiteChannelList();
    const pendingJump = useJumpStore((state) => state.pending);
    const clearPending = useJumpStore((state) => state.clearPending);
    const pageKey = 'channel' as const;
    const searchTerm = useSearchStore((s) => s.getSearchTerm(pageKey));
    const layout = useToolbarViewOptionsStore((s) => s.getLayout(pageKey));
    const sortField = useToolbarViewOptionsStore((s) => s.getSortField(pageKey));
    const sortOrder = useToolbarViewOptionsStore((s) => s.getSortOrder(pageKey));
    const filter = useToolbarViewOptionsStore((s) => s.channelFilter);
    const [highlightedChannelId, setHighlightedChannelId] = useState<number | null>(null);
    const [activeTab, setActiveTab] = useState<ChannelTab>(readInitialTab);
    const channelCardRefs = useRef<Map<number, HTMLDivElement>>(new Map());

    const pendingChannelJump = pendingJump && isChannelJumpTarget(pendingJump.target)
        ? pendingJump as ChannelPendingJump
        : null;
    const targetedChannelId = pendingChannelJump?.target.channelId ?? null;

    useEffect(() => {
        try {
            window.sessionStorage.setItem(TAB_STORAGE_KEY, activeTab);
        } catch {
            // ignore (private mode, disabled storage)
        }
    }, [activeTab]);

    useEffect(() => {
        const unsubscribe = useJumpStore.subscribe((state, prevState) => {
            const pending = state.pending;
            if (!pending) return;
            if (pending.requestId === prevState.pending?.requestId) return;
            const nextTab = tabFromJump(pending.target);
            if (nextTab) setActiveTab(nextTab);
        });
        return unsubscribe;
    }, []);

    const setChannelCardRef = useCallback((channelId: number, node: HTMLDivElement | null) => {
        const refs = channelCardRefs.current;
        if (node) {
            refs.set(channelId, node);
            return;
        }
        refs.delete(channelId);
    }, []);

    const flashChannelCard = useCallback((channelId: number) => {
        setHighlightedChannelId(channelId);
        window.setTimeout(() => {
            setHighlightedChannelId((current) => (current === channelId ? null : current));
        }, 1800);
    }, []);

    const sortedChannels = useMemo(() => {
        if (!channelsData) return [];
        return [...channelsData].sort((a, b) => {
            const diff = sortField === 'name'
                ? a.raw.name.localeCompare(b.raw.name)
                : a.raw.id - b.raw.id;
            return sortOrder === 'asc' ? diff : -diff;
        });
    }, [channelsData, sortField, sortOrder]);

    const visibleChannels = useMemo(() => {
        const term = searchTerm.toLowerCase().trim();

        return sortedChannels.filter((channel) => {
            if (channel.raw.id === targetedChannelId) {
                return true;
            }

            if (term && !channel.raw.name.toLowerCase().includes(term)) {
                return false;
            }

            if (filter === 'enabled') return channel.raw.enabled;
            if (filter === 'disabled') return !channel.raw.enabled;

            return true;
        });
    }, [sortedChannels, searchTerm, filter, targetedChannelId]);

    const visibleManualChannels = useMemo(
        () => visibleChannels.filter((channel) => !channel.raw.managed),
        [visibleChannels],
    );

    const targetedManagedChannel = useMemo(
        () => visibleChannels.find((channel) => channel.raw.id === targetedChannelId && channel.raw.managed) ?? null,
        [visibleChannels, targetedChannelId],
    );

    useEffect(() => {
        if (!pendingChannelJump) return;
        if (activeTab !== 'manual') return;

        const channelId = pendingChannelJump.target.channelId;
        const node = channelCardRefs.current.get(channelId);
        if (!node) return;

        const timer = window.setTimeout(() => {
            node.scrollIntoView({ behavior: 'smooth', block: 'center' });
            flashChannelCard(channelId);
            clearPending(pendingChannelJump.requestId);
        }, 80);

        return () => window.clearTimeout(timer);
    }, [pendingChannelJump, clearPending, flashChannelCard, visibleManualChannels.length, targetedManagedChannel, activeTab]);

    const renderChannelCard = useCallback((item: NonNullable<typeof channelsData>[number]) => (
        <div
            ref={(node) => setChannelCardRef(item.raw.id, node)}
            className={cn(
                'rounded-[1.75rem] transition-all',
                highlightedChannelId === item.raw.id && 'ring-2 ring-primary/35 ring-offset-2 ring-offset-background',
            )}
        >
            <Card channel={item.raw} stats={item.formatted} layout={layout} />
        </div>
    ), [highlightedChannelId, layout, setChannelCardRef]);

    const siteCount = useMemo(
        () => (siteChannelsData ?? []).filter((card) => card.account_count > 0).length,
        [siteChannelsData],
    );
    const manualCount = visibleManualChannels.length;

    const manualColumnCompute = useCallback((width: number) => {
        if (layout === 'list') return 1;
        const MIN_CARD_WIDTH = 320;
        const GUTTER = 16;
        const cols = Math.floor((width + GUTTER) / (MIN_CARD_WIDTH + GUTTER));
        return Math.max(1, Math.min(6, cols));
    }, [layout]);

    const triggerClass = 'flex h-10 items-center justify-center gap-2 px-5 rounded-xl text-sm font-medium transition-colors data-[state=active]:text-foreground data-[state=inactive]:text-muted-foreground data-[state=inactive]:hover:text-foreground';

    const manualHeader = targetedManagedChannel ? (
        <section className="space-y-3 px-1 pb-4">
            <div>
                <div className="text-sm font-semibold">定位的托管渠道</div>
                <div className="text-xs text-muted-foreground">
                    这个渠道由站点账号投影生成，默认不会出现在普通渠道列表中。
                </div>
            </div>
            {renderChannelCard(targetedManagedChannel)}
        </section>
    ) : undefined;

    const manualFooter = isLoading ? (
        <div className={cn('grid gap-4', layout === 'list' ? 'grid-cols-1' : 'md:grid-cols-2 lg:grid-cols-3')}>
            {Array.from({ length: layout === 'list' ? 2 : 3 }).map((_, index) => (
                <div key={index} className="h-56 animate-pulse rounded-3xl border border-border/70 bg-muted/40" />
            ))}
        </div>
    ) : error ? (
        <div className="rounded-3xl border border-destructive/30 bg-destructive/10 px-4 py-6 text-sm text-destructive">
            普通渠道加载失败：{error.message}
        </div>
    ) : visibleManualChannels.length === 0 && !targetedManagedChannel ? (
        <div className="rounded-3xl border border-border/70 bg-card/70 px-4 py-8 text-center text-sm text-muted-foreground">
            当前筛选下没有普通渠道
        </div>
    ) : null;

    return (
        <div className="flex h-full min-h-0 flex-col gap-4">
            <div className="flex items-center gap-3">
                <Tabs
                    value={activeTab}
                    onValueChange={(value) => setActiveTab(value as ChannelTab)}
                >
                    <TabsList className="flex w-fit p-1 bg-muted rounded-2xl">
                        <TabsHighlight
                            className="rounded-xl bg-background shadow-md ring-1 ring-border"
                            transition={{ type: 'spring', stiffness: 320, damping: 30, mass: 0.8 }}
                        >
                            <TabsHighlightItem value="site">
                                <TabsTrigger value="site" className={triggerClass}>
                                    {t('site')}
                                    <span
                                        className={cn(
                                            'text-xs tabular-nums transition-colors',
                                            activeTab === 'site'
                                                ? 'text-primary font-semibold'
                                                : 'text-muted-foreground',
                                        )}
                                    >
                                        {siteCount}
                                    </span>
                                </TabsTrigger>
                            </TabsHighlightItem>
                            <TabsHighlightItem value="manual">
                                <TabsTrigger value="manual" className={triggerClass}>
                                    {t('manual')}
                                    <span
                                        className={cn(
                                            'text-xs tabular-nums transition-colors',
                                            activeTab === 'manual'
                                                ? 'text-primary font-semibold'
                                                : 'text-muted-foreground',
                                        )}
                                    >
                                        {manualCount}
                                    </span>
                                </TabsTrigger>
                            </TabsHighlightItem>
                        </TabsHighlight>
                    </TabsList>
                </Tabs>
                <div className="ml-auto">
                    <SiteChannelCompletionAction />
                </div>
            </div>

            <div className="relative flex-1 min-h-0">
                <AnimatePresence mode="wait" initial={false}>
                    <motion.div
                        key={activeTab}
                        initial={{ opacity: 0, y: 6 }}
                        animate={{ opacity: 1, y: 0 }}
                        exit={{ opacity: 0, y: -4 }}
                        transition={{ duration: 0.18, ease: [0.4, 0, 0.2, 1] }}
                        className="absolute inset-0 flex flex-col min-h-0"
                    >
                        {activeTab === 'site' ? (
                            <div className="h-full overflow-y-auto overscroll-contain rounded-t-3xl">
                                <SiteChannelSection
                                    searchTerm={searchTerm}
                                    filter={filter}
                                    sortField={sortField}
                                    sortOrder={sortOrder}
                                    layout={layout}
                                />
                            </div>
                        ) : (
                            <VirtualizedGrid
                                items={visibleManualChannels}
                                layout={layout}
                                columns={manualColumnCompute}
                                estimateItemHeight={216}
                                header={manualHeader}
                                footer={manualFooter}
                                getItemKey={(item) => `channel-${item.raw.id}`}
                                renderItem={renderChannelCard}
                            />
                        )}
                    </motion.div>
                </AnimatePresence>
            </div>
        </div>
    );
}
