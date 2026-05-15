import { create } from 'zustand';

type ProxyPoolDialogState = {
    isOpen: boolean;
    open: () => void;
    close: () => void;
    setOpen: (open: boolean) => void;
};

export const useProxyPoolDialogStore = create<ProxyPoolDialogState>((set) => ({
    isOpen: false,
    open: () => set({ isOpen: true }),
    close: () => set({ isOpen: false }),
    setOpen: (open) => set({ isOpen: open }),
}));
