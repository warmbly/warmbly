// Global compose-window state. A standalone zustand store (not an AppStore
// slice) so any surface — unibox header, contact drawer, keyboard shortcut,
// the drafts list — can open the composer without pulling in the whole app
// store, and the window itself mounts once in the app layout.

import { create } from "zustand";
import type { ComposeDraft } from "@/lib/api/client/app/unibox/composeDrafts";

interface ComposeStore {
    open: boolean;
    // Docked to a small corner bar; the draft stays mounted, so nothing is
    // lost while the user browses other pages.
    minimized: boolean;
    // Prefilled recipient (bare address) when opened from a contact context.
    prefillTo: string | null;
    // Saved draft to resume, when opened from the drafts list.
    seed: ComposeDraft | null;
    // Bumped whenever a fresh compose session starts; the window keys its
    // inner state on this so reopening or switching drafts resets cleanly.
    session: number;
    openCompose: (to?: string) => void;
    openDraft: (draft: ComposeDraft) => void;
    closeCompose: () => void;
    setMinimized: (minimized: boolean) => void;
}

export const useComposeStore = create<ComposeStore>((set, get) => ({
    open: false,
    minimized: false,
    prefillTo: null,
    seed: null,
    session: 0,
    openCompose: (to) => {
        // Already composing: restore the existing draft instead of clobbering
        // it with a new prefill (the current one autosaves anyway).
        if (get().open) {
            set({ minimized: false });
            return;
        }
        set((s) => ({
            open: true,
            minimized: false,
            prefillTo: to?.trim() || null,
            seed: null,
            session: s.session + 1,
        }));
    },
    // Resuming a saved draft always loads it, even mid-compose: the current
    // window state is autosaved, so nothing is lost by switching.
    openDraft: (draft) =>
        set((s) => ({
            open: true,
            minimized: false,
            prefillTo: null,
            seed: draft,
            session: s.session + 1,
        })),
    closeCompose: () => set({ open: false, minimized: false, prefillTo: null, seed: null }),
    setMinimized: (minimized) => set({ minimized }),
}));
