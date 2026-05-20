import { create } from 'zustand';

interface LogUIState {
    refreshRequestId: number;
    isRefreshing: boolean;
    requestRefresh: () => void;
    setRefreshing: (value: boolean) => void;
}

export const useLogUIStore = create<LogUIState>((set) => ({
    refreshRequestId: 0,
    isRefreshing: false,
    requestRefresh: () => set((state) => ({ refreshRequestId: state.refreshRequestId + 1 })),
    setRefreshing: (value) => set({ isRefreshing: value }),
}));
