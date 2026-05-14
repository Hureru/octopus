'use client';

import { Activity } from './activity';
import { StatsChart } from './chart';
import { GroupHealthSummaryStrip } from './group-health-summary-strip';
import { Rank } from './rank';
import { PageWrapper } from '@/components/common/PageWrapper';

export function Home() {
    return (
        <PageWrapper className="h-full min-h-0 overflow-y-auto overscroll-contain space-y-6 pb-24 md:pb-4 rounded-t-3xl">
            <StatsChart />
            <GroupHealthSummaryStrip />
            <Activity />
            <Rank />
        </PageWrapper>
    );
}
