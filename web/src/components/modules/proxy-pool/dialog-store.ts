import { create } from 'zustand';

type ProxyPoolDialogState = {
    isOpen: boolean;
    focusedProxyId: number | null;
    open: (proxyId?: number | null) => void;
    close: () => void;
    setOpen: (open: boolean) => void;
    clearFocus: () => void;
};

export const useProxyPoolDialogStore = create<ProxyPoolDialogState>((set) => ({
    isOpen: false,
    focusedProxyId: null,
    open: (proxyId) => set({ isOpen: true, focusedProxyId: typeof proxyId === 'number' ? proxyId : null }),
    close: () => set({ isOpen: false, focusedProxyId: null }),
    setOpen: (open) => set((state) => ({ isOpen: open, focusedProxyId: open ? state.focusedProxyId : null })),
    clearFocus: () => set({ focusedProxyId: null }),
}));
