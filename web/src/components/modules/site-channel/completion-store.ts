import { create } from 'zustand';

type CompletionState = {
    pendingCount: number;
    setPendingCount: (count: number) => void;
    dialogOpen: boolean;
    openDialog: () => void;
    setDialogOpen: (open: boolean) => void;
};

export const useCompletionStore = create<CompletionState>((set) => ({
    pendingCount: 0,
    setPendingCount: (count) => set({ pendingCount: count }),
    dialogOpen: false,
    openDialog: () => set({ dialogOpen: true }),
    setDialogOpen: (open) => set({ dialogOpen: open }),
}));
