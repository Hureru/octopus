'use client';

import { create } from 'zustand';
import { persist } from 'zustand/middleware';

export type SiteChannelViewMode = 'board' | 'table';
export type SiteChannelQuickFilter = 'attention' | 'with_history' | 'disabled';
export type SiteChannelTableSortField = 'model_name' | 'group_name' | 'route_type' | 'last_request_at';
export type SiteChannelSortOrder = 'asc' | 'desc';

export type SiteChannelTableSort = {
    field: SiteChannelTableSortField;
    order: SiteChannelSortOrder;
};

export type SiteChannelPanelPreferences = {
    viewMode: SiteChannelViewMode;
    compactMode: boolean;
    collapseEmptyColumns: boolean;
    quickFilters: SiteChannelQuickFilter[];
    tableSort: SiteChannelTableSort;
};

export const DEFAULT_SITE_CHANNEL_PANEL_PREFERENCES: SiteChannelPanelPreferences = {
    viewMode: 'table',
    compactMode: false,
    collapseEmptyColumns: true,
    quickFilters: [],
    tableSort: {
        field: 'model_name',
        order: 'asc',
    },
};

type SiteChannelPanelState = {
    panels: Record<string, SiteChannelPanelPreferences>;
    setViewMode: (panelKey: string, viewMode: SiteChannelViewMode) => void;
    setCompactMode: (panelKey: string, compactMode: boolean) => void;
    setCollapseEmptyColumns: (panelKey: string, collapseEmptyColumns: boolean) => void;
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
            setViewMode: (panelKey, viewMode) =>
                set((state) => ({
                    panels: updatePanel(state.panels, panelKey, (current) => ({
                        ...current,
                        viewMode,
                    })),
                })),
            setCompactMode: (panelKey, compactMode) =>
                set((state) => ({
                    panels: updatePanel(state.panels, panelKey, (current) => ({
                        ...current,
                        compactMode,
                    })),
                })),
            setCollapseEmptyColumns: (panelKey, collapseEmptyColumns) =>
                set((state) => ({
                    panels: updatePanel(state.panels, panelKey, (current) => ({
                        ...current,
                        collapseEmptyColumns,
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
