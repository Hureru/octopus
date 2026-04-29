import { create } from 'zustand';
import {
    isChannelJumpTarget,
    isSiteChannelJumpTarget,
    type JumpTarget,
    useJumpStore,
} from '@/stores/jump';

export type ChannelTab = 'site' | 'manual';

const STORAGE_KEY = 'octopus:channel-tab';

function tabFromJump(target: JumpTarget | undefined): ChannelTab | null {
    if (!target) return null;
    if (isSiteChannelJumpTarget(target)) return 'site';
    if (isChannelJumpTarget(target)) return 'manual';
    return null;
}

function readInitialTab(): ChannelTab {
    const fromJump = tabFromJump(useJumpStore.getState().pending?.target);
    if (fromJump) return fromJump;

    if (typeof window === 'undefined') return 'site';
    try {
        const stored = window.sessionStorage.getItem(STORAGE_KEY);
        return stored === 'manual' ? 'manual' : 'site';
    } catch {
        return 'site';
    }
}

type ChannelTabState = {
    activeTab: ChannelTab;
    setActiveTab: (tab: ChannelTab) => void;
};

export const useChannelTabStore = create<ChannelTabState>((set) => ({
    activeTab: readInitialTab(),
    setActiveTab: (tab) => {
        set({ activeTab: tab });
        if (typeof window !== 'undefined') {
            try {
                window.sessionStorage.setItem(STORAGE_KEY, tab);
            } catch {
                // ignore (private mode, disabled storage)
            }
        }
    },
}));

if (typeof window !== 'undefined') {
    useJumpStore.subscribe((state, prevState) => {
        const pending = state.pending;
        if (!pending) return;
        if (pending.requestId === prevState.pending?.requestId) return;
        const nextTab = tabFromJump(pending.target);
        if (nextTab) useChannelTabStore.getState().setActiveTab(nextTab);
    });
}
