'use client';

import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useChannelList } from '@/api/endpoints/channel';
import { Card } from './Card';
import { useSearchStore, useToolbarViewOptionsStore } from '@/components/modules/toolbar';
import { VirtualizedGrid } from '@/components/common/VirtualizedGrid';
import { SiteChannelSection } from '@/components/modules/site-channel';
import { cn } from '@/lib/utils';
import { isChannelJumpTarget, type ChannelJumpTarget, type PendingJump, useJumpStore } from '@/stores/jump';

type ChannelPendingJump = PendingJump & { target: ChannelJumpTarget };

export function Channel() {
    const { data: channelsData, isLoading, error } = useChannelList();
    const pendingJump = useJumpStore((state) => state.pending);
    const clearPending = useJumpStore((state) => state.clearPending);
    const pageKey = 'channel' as const;
    const searchTerm = useSearchStore((s) => s.getSearchTerm(pageKey));
    const layout = useToolbarViewOptionsStore((s) => s.getLayout(pageKey));
    const sortField = useToolbarViewOptionsStore((s) => s.getSortField(pageKey));
    const sortOrder = useToolbarViewOptionsStore((s) => s.getSortOrder(pageKey));
    const filter = useToolbarViewOptionsStore((s) => s.channelFilter);
    const [highlightedChannelId, setHighlightedChannelId] = useState<number | null>(null);
    const channelCardRefs = useRef<Map<number, HTMLDivElement>>(new Map());

    const pendingChannelJump = pendingJump && isChannelJumpTarget(pendingJump.target)
        ? pendingJump as ChannelPendingJump
        : null;
    const targetedChannelId = pendingChannelJump?.target.channelId ?? null;

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

        const channelId = pendingChannelJump.target.channelId;
        const node = channelCardRefs.current.get(channelId);
        if (!node) return;

        const timer = window.setTimeout(() => {
            node.scrollIntoView({ behavior: 'smooth', block: 'center' });
            flashChannelCard(channelId);
            clearPending(pendingChannelJump.requestId);
        }, 80);

        return () => window.clearTimeout(timer);
    }, [pendingChannelJump, clearPending, flashChannelCard, visibleManualChannels.length, targetedManagedChannel]);

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

    const header = (
        <div className="space-y-6 pb-4">
            <SiteChannelSection
                searchTerm={searchTerm}
                filter={filter}
                sortField={sortField}
                sortOrder={sortOrder}
                layout={layout}
            />

            {targetedManagedChannel ? (
                <section className="space-y-3 px-1">
                    <div>
                        <div className="text-sm font-semibold">定位的托管渠道</div>
                        <div className="text-xs text-muted-foreground">
                            这个渠道由站点账号投影生成，默认不会出现在普通渠道列表中。
                        </div>
                    </div>
                    {renderChannelCard(targetedManagedChannel)}
                </section>
            ) : null}

            <section className="space-y-2 px-1">
                <div className="text-sm font-semibold">普通渠道</div>
                <div className="text-xs text-muted-foreground">
                    手动创建的渠道继续在这里管理，站点投影渠道已统一收敛到上方站点渠道卡片。
                </div>
            </section>
        </div>
    );

    const footer = isLoading ? (
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
        <VirtualizedGrid
            items={visibleManualChannels}
            layout={layout}
            columns={{ default: 1, md: 2, lg: 3 }}
            estimateItemHeight={216}
            header={header}
            footer={footer}
            getItemKey={(item) => `channel-${item.raw.id}`}
            renderItem={renderChannelCard}
        />
    );
}
