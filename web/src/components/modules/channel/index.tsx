'use client';

import { useMemo } from 'react';
import { useChannelList } from '@/api/endpoints/channel';
import { Card } from './Card';
import { useSearchStore, useToolbarViewOptionsStore } from '@/components/modules/toolbar';
import { VirtualizedGrid } from '@/components/common/VirtualizedGrid';
import { SiteChannelSection } from '@/components/modules/site-channel';
import { cn } from '@/lib/utils';

export function Channel() {
    const { data: channelsData, isLoading, error } = useChannelList();
    const pageKey = 'channel' as const;
    const searchTerm = useSearchStore((s) => s.getSearchTerm(pageKey));
    const layout = useToolbarViewOptionsStore((s) => s.getLayout(pageKey));
    const sortField = useToolbarViewOptionsStore((s) => s.getSortField(pageKey));
    const sortOrder = useToolbarViewOptionsStore((s) => s.getSortOrder(pageKey));
    const filter = useToolbarViewOptionsStore((s) => s.channelFilter);

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
        const byName = !term ? sortedChannels : sortedChannels.filter((c) => c.raw.name.toLowerCase().includes(term));

        if (filter === 'enabled') return byName.filter((c) => c.raw.enabled);
        if (filter === 'disabled') return byName.filter((c) => !c.raw.enabled);

        return byName;
    }, [sortedChannels, searchTerm, filter]);

    const visibleManualChannels = useMemo(
        () => visibleChannels.filter((channel) => !channel.raw.managed),
        [visibleChannels],
    );

    const header = (
        <div className="space-y-6 pb-4">
            <SiteChannelSection
                searchTerm={searchTerm}
                filter={filter}
                sortField={sortField}
                sortOrder={sortOrder}
                layout={layout}
            />

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
    ) : visibleManualChannels.length === 0 ? (
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
            renderItem={(item) => <Card channel={item.raw} stats={item.formatted} layout={layout} />}
        />
    );
}
