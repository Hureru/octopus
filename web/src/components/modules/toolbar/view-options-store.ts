import { create } from 'zustand';
import { persist } from 'zustand/middleware';

export type ToolbarLayout = 'grid' | 'list';
export type ToolbarSortOrder = 'asc' | 'desc';
export type ToolbarSortField = 'default' | 'name' | 'created' | 'balance';
export type ToolbarSortablePage = 'site' | 'channel' | 'group';
export const TOOLBAR_PAGES = ['site', 'channel', 'group', 'model', 'log'] as const;
export type ToolbarPage = (typeof TOOLBAR_PAGES)[number];
export type LogDateRange = { start?: number; end?: number };
export type LogKeywordMode = 'default' | 'prefix' | 'exact' | 'contains';
export type LogKeywordScope = 'default' | 'content';

interface ToolbarViewOptionsState {
    layouts: Partial<Record<ToolbarPage, ToolbarLayout>>;
    sortFields: Partial<Record<ToolbarSortablePage, ToolbarSortField>>;
    sortOrders: Partial<Record<ToolbarPage, ToolbarSortOrder>>;
    logDateRange: LogDateRange;
    logChannelIds: number[];
    logKeywordMode: LogKeywordMode;
    logKeywordScope: LogKeywordScope;

    getLayout: (item: ToolbarPage) => ToolbarLayout;
    setLayout: (item: ToolbarPage, value: ToolbarLayout) => void;

    getSortField: (item: ToolbarSortablePage) => ToolbarSortField;
    setSortConfig: (
        item: ToolbarSortablePage,
        field: ToolbarSortField,
        order: ToolbarSortOrder
    ) => void;

    getSortOrder: (item: ToolbarPage) => ToolbarSortOrder;
    setSortOrder: (item: ToolbarPage, value: ToolbarSortOrder) => void;

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
            logDateRange: {},
            logChannelIds: [],
            logKeywordMode: 'default',
            logKeywordScope: 'default',

            getLayout: (item) => get().layouts[item] || 'grid',
            setLayout: (item, value) => {
                set((state) => ({ layouts: { ...state.layouts, [item]: value } }));
            },

            getSortField: (item) => {
                const field = get().sortFields[item];
                if (item === 'site') {
                    return field === 'balance' || field === 'name' ? field : 'default';
                }
                return field === 'created' ? 'created' : 'name';
            },
            setSortConfig: (item, field, order) => {
                set((state) => ({
                    sortFields: { ...state.sortFields, [item]: field },
                    sortOrders: { ...state.sortOrders, [item]: order },
                }));
            },

            getSortOrder: (item) => {
                if (item === 'site' && get().getSortField('site') === 'default') {
                    return 'asc';
                }
                return get().sortOrders[item] === 'desc' ? 'desc' : 'asc';
            },
            setSortOrder: (item, value) => {
                set((state) => ({ sortOrders: { ...state.sortOrders, [item]: value } }));
            },

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
                logDateRange: state.logDateRange,
                logChannelIds: state.logChannelIds,
                logKeywordMode: state.logKeywordMode,
                logKeywordScope: state.logKeywordScope,
            }),
        }
    )
);
