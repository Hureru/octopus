'use client';

import { create } from 'zustand';
import { persist } from 'zustand/middleware';
import type { UsageDimension, UsageMetricScope } from '@/api/endpoints/log-analytics';

export type LogView = 'analytics' | 'detail';

function browserTimezone() {
    try {
        return Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC';
    } catch {
        return 'UTC';
    }
}

interface LogAnalyticsState {
    view: LogView;
    scope: UsageMetricScope;
    dimension: UsageDimension;
    timezone: string;
    siteIds: number[];
    siteAccountIds: number[];
    apiKeyIds: number[];
    requestModels: string[];
    actualModels: string[];
    canonicalModels: string[];
    setView: (value: LogView) => void;
    setScope: (value: UsageMetricScope) => void;
    setDimension: (value: UsageDimension) => void;
    setTimezone: (value: string) => void;
    setSiteIds: (value: number[]) => void;
    setSiteAccountIds: (value: number[]) => void;
    setAPIKeyIds: (value: number[]) => void;
    setRequestModels: (value: string[]) => void;
    setActualModels: (value: string[]) => void;
    setCanonicalModels: (value: string[]) => void;
    clearBusinessFilters: () => void;
}

export const useLogAnalyticsStore = create<LogAnalyticsState>()(
    persist(
        (set) => ({
            view: 'analytics',
            scope: 'request',
            dimension: 'site',
            timezone: browserTimezone(),
            siteIds: [],
            siteAccountIds: [],
            apiKeyIds: [],
            requestModels: [],
            actualModels: [],
            canonicalModels: [],
            setView: (view) => set({ view }),
            setScope: (scope) => set({ scope }),
            setDimension: (dimension) => set({ dimension }),
            setTimezone: (timezone) => set({ timezone }),
            setSiteIds: (siteIds) => set({ siteIds, siteAccountIds: [] }),
            setSiteAccountIds: (siteAccountIds) => set({ siteAccountIds }),
            setAPIKeyIds: (apiKeyIds) => set({ apiKeyIds }),
            setRequestModels: (requestModels) => set({ requestModels }),
            setActualModels: (actualModels) => set({ actualModels }),
            setCanonicalModels: (canonicalModels) => set({ canonicalModels }),
            clearBusinessFilters: () =>
                set({
                    siteIds: [],
                    siteAccountIds: [],
                    apiKeyIds: [],
                    requestModels: [],
                    actualModels: [],
                    canonicalModels: [],
                }),
        }),
        {
            name: 'log-analytics-options',
        },
    ),
);
