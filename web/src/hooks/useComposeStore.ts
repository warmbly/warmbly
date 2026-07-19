// Global compose-window state. A standalone zustand store (not an AppStore
// slice) so any surface — unibox header, contact drawer, keyboard shortcut —
// can open the composer without pulling in the whole app store, and the
// window itself mounts once in the app layout.

import { create } from "zustand";

interface ComposeStore {
    open: boolean;
    // Docked to a small corner bar; the draft stays mounted, so nothing is
    // lost while the user browses other pages.
    minimized: boolean;
    // Prefilled recipient (bare address) when opened from a contact context.
    prefillTo: string | null;
    openCompose: (to?: string) => void;
    closeCompose: () => void;
    setMinimized: (minimized: boolean) => void;
}

export const useComposeStore = create<ComposeStore>((set, get) => ({
    open: false,
    minimized: false,
    prefillTo: null,
    openCompose: (to) => {
        // Already composing: restore the existing draft instead of clobbering
        // it with a new prefill.
        if (get().open) {
            set({ minimized: false });
            return;
        }
        set({ open: true, minimized: false, prefillTo: to?.trim() || null });
    },
    closeCompose: () => set({ open: false, minimized: false, prefillTo: null }),
    setMinimized: (minimized) => set({ minimized }),
}));
