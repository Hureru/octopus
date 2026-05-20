import { create } from 'zustand';

interface LogUIState {
    refreshRequestId: number;
    requestRefresh: () => void;
}

export const useLogUIStore = create<LogUIState>((set) => ({
    refreshRequestId: 0,
    requestRefresh: () => set((state) => ({ refreshRequestId: state.refreshRequestId + 1 })),
}));
