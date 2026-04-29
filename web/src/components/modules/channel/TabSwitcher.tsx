'use client';

import { useMemo } from 'react';
import { useTranslations } from 'next-intl';
import { motion } from 'motion/react';
import { useChannelList } from '@/api/endpoints/channel';
import { useSiteChannelList } from '@/api/endpoints/site-channel';
import { SiteChannelCompletionAction } from '@/components/modules/site-channel';
import { cn } from '@/lib/utils';
import { useChannelTabStore, type ChannelTab } from './tab-store';

const TABS: { value: ChannelTab; key: 'site' | 'manual' }[] = [
    { value: 'site', key: 'site' },
    { value: 'manual', key: 'manual' },
];

type Props = { className?: string };

export function ChannelTabSwitcher({ className }: Props) {
    const t = useTranslations('channel.tabs');
    const activeTab = useChannelTabStore((s) => s.activeTab);
    const setActiveTab = useChannelTabStore((s) => s.setActiveTab);
    const { data: channelsData } = useChannelList();
    const { data: siteChannelsData } = useSiteChannelList();

    const counts = useMemo(
        () => ({
            site: (siteChannelsData ?? []).filter((card) => card.account_count > 0).length,
            manual: (channelsData ?? []).filter((c) => !c.raw.managed).length,
        }),
        [channelsData, siteChannelsData],
    );

    return (
        <div className={cn('flex items-baseline gap-5', className)}>
            {TABS.map(({ value, key }) => {
                const active = activeTab === value;
                return (
                    <button
                        key={value}
                        type="button"
                        onClick={() => setActiveTab(value)}
                        className={cn(
                            'relative inline-flex items-baseline gap-1.5 pb-1 text-sm font-medium transition-colors',
                            active ? 'text-foreground' : 'text-muted-foreground hover:text-foreground',
                        )}
                    >
                        <span>{t(key)}</span>
                        <span
                            className={cn(
                                'text-xs tabular-nums transition-colors',
                                active ? 'text-primary font-semibold' : 'text-muted-foreground',
                            )}
                        >
                            {counts[value]}
                        </span>
                        {active && (
                            <motion.span
                                layoutId="channel-tab-underline"
                                className="absolute -bottom-px left-0 right-0 h-0.5 rounded-full bg-primary"
                                transition={{ type: 'spring', stiffness: 320, damping: 30, mass: 0.8 }}
                            />
                        )}
                    </button>
                );
            })}
        </div>
    );
}

export function ChannelHeaderActions() {
    const activeTab = useChannelTabStore((s) => s.activeTab);
    if (activeTab !== 'site') return null;
    return <SiteChannelCompletionAction />;
}
