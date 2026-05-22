import { create } from 'zustand';
import { persist } from 'zustand/middleware';

export type ToolbarLayout = 'grid' | 'list';
export type ToolbarSortOrder = 'asc' | 'desc';
export type ToolbarSortField = 'name' | 'created';
export type ToolbarCreatedSortablePage = 'channel' | 'group';
export const TOOLBAR_PAGES = ['site', 'channel', 'group', 'model', 'log'] as const;
export type ToolbarPage = (typeof TOOLBAR_PAGES)[number];
export type ChannelFilter = 'all' | 'enabled' | 'disabled';
export type GroupFilter = 'all' | 'with-members' | 'empty';
export type ModelFilter = 'all' | 'priced' | 'free';
export type SiteFilter = 'all' | 'abnormal' | 'enabled' | 'disabled' | 'pinned';
export type LogDateRange = { start?: number; end?: number };
export type LogKeywordMode = 'default' | 'prefix' | 'exact' | 'contains';
export type LogKeywordScope = 'default' | 'content';

interface ToolbarViewOptionsState {
    layouts: Partial<Record<ToolbarPage, ToolbarLayout>>;
    sortFields: Partial<Record<ToolbarCreatedSortablePage, ToolbarSortField>>;
    sortOrders: Partial<Record<ToolbarPage, ToolbarSortOrder>>;
    siteFilter: SiteFilter;
    channelFilter: ChannelFilter;
    groupFilter: GroupFilter;
    modelFilter: ModelFilter;
    logDateRange: LogDateRange;
    logChannelIds: number[];
    logKeywordMode: LogKeywordMode;
    logKeywordScope: LogKeywordScope;

    getLayout: (item: ToolbarPage) => ToolbarLayout;
    setLayout: (item: ToolbarPage, value: ToolbarLayout) => void;

    getSortField: (item: ToolbarCreatedSortablePage) => ToolbarSortField;
    setSortConfig: (
        item: ToolbarCreatedSortablePage,
        field: ToolbarSortField,
        order: ToolbarSortOrder
    ) => void;

    getSortOrder: (item: ToolbarPage) => ToolbarSortOrder;
    setSortOrder: (item: ToolbarPage, value: ToolbarSortOrder) => void;

    setSiteFilter: (value: SiteFilter) => void;
    setChannelFilter: (value: ChannelFilter) => void;
    setGroupFilter: (value: GroupFilter) => void;
    setModelFilter: (value: ModelFilter) => void;
    setLogDateRange: (value: LogDateRange) => void;
    setLogChannelIds: (value: number[]) => void;
    setLogKeywordMode: (value: LogKeywordMode) => void;
    setLogKeywordScope: (value: LogKeywordScope) => void;
}

export const useToolbarViewOptionsStore = create<ToolbarViewOptionsState>()(
    persist(
        (set, get) => ({
            layouts: {},
            sortFields: {},
            sortOrders: {},
            siteFilter: 'all',
            channelFilter: 'all',
            groupFilter: 'all',
            modelFilter: 'all',
            logDateRange: {},
            logChannelIds: [],
            logKeywordMode: 'default',
            logKeywordScope: 'default',

            getLayout: (item) => get().layouts[item] || 'grid',
            setLayout: (item, value) => {
                set((state) => ({ layouts: { ...state.layouts, [item]: value } }));
            },

            getSortField: (item) => get().sortFields[item] || 'name',
            setSortConfig: (item, field, order) => {
                set((state) => ({
                    sortFields: { ...state.sortFields, [item]: field },
                    sortOrders: { ...state.sortOrders, [item]: order },
                }));
            },

            getSortOrder: (item) => (get().sortOrders[item] === 'desc' ? 'desc' : 'asc'),
            setSortOrder: (item, value) => {
                set((state) => ({ sortOrders: { ...state.sortOrders, [item]: value } }));
            },

            setSiteFilter: (value) => set({ siteFilter: value }),
            setChannelFilter: (value) => set({ channelFilter: value }),
            setGroupFilter: (value) => set({ groupFilter: value }),
            setModelFilter: (value) => set({ modelFilter: value }),
            setLogDateRange: (value) => set({ logDateRange: value }),
            setLogChannelIds: (value) => set({ logChannelIds: value }),
            setLogKeywordMode: (value) => set({ logKeywordMode: value }),
            setLogKeywordScope: (value) => set({ logKeywordScope: value }),
        }),
        {
            name: 'toolbar-view-options-storage',
            partialize: (state) => ({
                layouts: state.layouts,
                sortFields: state.sortFields,
                sortOrders: state.sortOrders,
                siteFilter: state.siteFilter,
                channelFilter: state.channelFilter,
                groupFilter: state.groupFilter,
                modelFilter: state.modelFilter,
                logDateRange: state.logDateRange,
                logChannelIds: state.logChannelIds,
                logKeywordMode: state.logKeywordMode,
                logKeywordScope: state.logKeywordScope,
            }),
        }
    )
);
