'use client';

import { create } from 'zustand';
import { createJSONStorage, persist } from 'zustand/middleware';

export type RankSortMode = 'cost' | 'count' | 'tokens';
export type ChartPeriod = '1' | '7' | '30' | 'all';

interface HomeViewState {
    rankSortMode: RankSortMode;
    chartPeriod: ChartPeriod;
    isChannelNameHidden: boolean; // 不持久化，刷新后恢复显示
    setRankSortMode: (value: RankSortMode) => void;
    setChartPeriod: (value: ChartPeriod) => void;
    setChannelNameHidden: (value: boolean) => void;
}

export const useHomeViewStore = create<HomeViewState>()(
    persist(
        (set) => ({
            rankSortMode: 'cost',
            chartPeriod: '7',
            isChannelNameHidden: false,
            setRankSortMode: (value) => set({ rankSortMode: value }),
            setChartPeriod: (value) => set({ chartPeriod: value }),
            setChannelNameHidden: (value) => set({ isChannelNameHidden: value }),
        }),
        {
            name: 'home-view-options-storage',
            storage: createJSONStorage(() => localStorage),
            partialize: (state) => ({
                rankSortMode: state.rankSortMode,
                chartPeriod: state.chartPeriod,
            }),
        }
    )
);
