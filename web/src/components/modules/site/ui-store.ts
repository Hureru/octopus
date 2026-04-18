'use client';

import { create } from 'zustand';

type SiteUIHandlers = {
    openCreateDialog: () => void;
    openImportDialog: () => void;
    openArchivedDialog: () => void;
    syncAll: () => void;
    checkinAll: () => void;
};

interface SiteUIState {
    handlers: SiteUIHandlers;
    setHandlers: (handlers: Partial<SiteUIHandlers>) => void;
    resetHandlers: () => void;
    requestOpenCreateDialog: () => void;
    requestOpenImportDialog: () => void;
    requestOpenArchivedDialog: () => void;
    requestSyncAll: () => void;
    requestCheckinAll: () => void;
}

const noop = () => {};

const defaultHandlers: SiteUIHandlers = {
    openCreateDialog: noop,
    openImportDialog: noop,
    openArchivedDialog: noop,
    syncAll: noop,
    checkinAll: noop,
};

export const useSiteUIStore = create<SiteUIState>((set, get) => ({
    handlers: defaultHandlers,
    setHandlers: (handlers) =>
        set((state) => ({
            handlers: { ...state.handlers, ...handlers },
        })),
    resetHandlers: () => set({ handlers: defaultHandlers }),
    requestOpenCreateDialog: () => get().handlers.openCreateDialog(),
    requestOpenImportDialog: () => get().handlers.openImportDialog(),
    requestOpenArchivedDialog: () => get().handlers.openArchivedDialog(),
    requestSyncAll: () => get().handlers.syncAll(),
    requestCheckinAll: () => get().handlers.checkinAll(),
}));
