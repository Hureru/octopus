'use client';

import { create } from 'zustand';
import { persist } from 'zustand/middleware';

export type SiteChannelQuickFilter = 'attention' | 'with_history' | 'disabled';
export type SiteChannelTableSortField = 'model_name' | 'group_name' | 'route_type' | 'last_request_at';
export type SiteChannelSortOrder = 'asc' | 'desc';

export type SiteChannelTableSort = {
    field: SiteChannelTableSortField;
    order: SiteChannelSortOrder;
};

export type SiteChannelPanelPreferences = {
    compactMode: boolean;
    quickFilters: SiteChannelQuickFilter[];
    tableSort: SiteChannelTableSort;
};

export const DEFAULT_SITE_CHANNEL_PANEL_PREFERENCES: SiteChannelPanelPreferences = {
    compactMode: true,
    quickFilters: [],
    tableSort: {
        field: 'model_name',
        order: 'asc',
    },
};

type SiteChannelPanelState = {
    panels: Record<string, SiteChannelPanelPreferences>;
    setCompactMode: (panelKey: string, compactMode: boolean) => void;
    setQuickFilters: (panelKey: string, quickFilters: SiteChannelQuickFilter[]) => void;
    setTableSort: (panelKey: string, tableSort: SiteChannelTableSort) => void;
};

function updatePanel(
    panels: Record<string, SiteChannelPanelPreferences>,
    panelKey: string,
    updater: (current: SiteChannelPanelPreferences) => SiteChannelPanelPreferences,
) {
    const current = panels[panelKey] ?? DEFAULT_SITE_CHANNEL_PANEL_PREFERENCES;

    return {
        ...panels,
        [panelKey]: updater(current),
    };
}

export const useSiteChannelPanelViewStore = create<SiteChannelPanelState>()(
    persist(
        (set) => ({
            panels: {},
            setCompactMode: (panelKey, compactMode) =>
                set((state) => ({
                    panels: updatePanel(state.panels, panelKey, (current) => ({
                        ...current,
                        compactMode,
                    })),
                })),
            setQuickFilters: (panelKey, quickFilters) =>
                set((state) => ({
                    panels: updatePanel(state.panels, panelKey, (current) => ({
                        ...current,
                        quickFilters,
                    })),
                })),
            setTableSort: (panelKey, tableSort) =>
                set((state) => ({
                    panels: updatePanel(state.panels, panelKey, (current) => ({
                        ...current,
                        tableSort,
                    })),
                })),
        }),
        {
            name: 'site-channel-panel-view-storage',
            partialize: (state) => ({
                panels: state.panels,
            }),
        },
    ),
);
