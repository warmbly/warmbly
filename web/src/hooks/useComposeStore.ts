// Global compose-window state. A standalone zustand store (not an AppStore
// slice) so any surface — unibox header, contact drawer, keyboard shortcut —
// can open the composer without pulling in the whole app store, and the
// window itself mounts once in the app layout.

import { create } from "zustand";

interface ComposeStore {
    open: boolean;
    // Prefilled recipient (bare address) when opened from a contact context.
    prefillTo: string | null;
    openCompose: (to?: string) => void;
    closeCompose: () => void;
}

export const useComposeStore = create<ComposeStore>((set) => ({
    open: false,
    prefillTo: null,
    openCompose: (to) => set({ open: true, prefillTo: to?.trim() || null }),
    closeCompose: () => set({ open: false, prefillTo: null }),
}));
