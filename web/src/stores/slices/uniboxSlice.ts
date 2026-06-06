import type { StateCreator } from "zustand";
import type UniboxEmail from "@/lib/api/models/app/unibox/UniboxEmail";
import type UniboxThread from "@/lib/api/models/app/unibox/UniboxThread";

export interface UniboxSlice {
  uniboxEmails: UniboxEmail[];
  uniboxThreads: UniboxThread[];
  unseenCount: number;
  selectedThreadId: string | null;
  selectedAccountId: string | null;

  setUniboxEmails: (emails: UniboxEmail[]) => void;
  addUniboxEmail: (email: UniboxEmail) => void;
  setUniboxThreads: (threads: UniboxThread[]) => void;
  setUnseenCount: (count: number) => void;
  incrementUnseenCount: () => void;
  decrementUnseenCount: (by?: number) => void;
  setSelectedThreadId: (id: string | null) => void;
  setSelectedAccountId: (id: string | null) => void;
}

export const createUniboxSlice: StateCreator<
  UniboxSlice,
  [],
  [],
  UniboxSlice
> = (set) => ({
  uniboxEmails: [],
  uniboxThreads: [],
  unseenCount: 0,
  selectedThreadId: null,
  selectedAccountId: null,

  setUniboxEmails: (uniboxEmails) => set({ uniboxEmails }),
  // Upsert by thread: a new message in an existing conversation moves
  // that conversation to the top instead of stacking a duplicate row —
  // mirroring the thread-collapsed server list. Falls back to id when a
  // message has no thread_id.
  addUniboxEmail: (email) =>
    set((state) => {
      const key = email.thread_id || email.id;
      const rest = state.uniboxEmails.filter(
        (e) => (e.thread_id || e.id) !== key,
      );
      return { uniboxEmails: [email, ...rest] };
    }),
  setUniboxThreads: (uniboxThreads) => set({ uniboxThreads }),
  setUnseenCount: (unseenCount) => set({ unseenCount }),
  incrementUnseenCount: () =>
    set((state) => ({ unseenCount: state.unseenCount + 1 })),
  decrementUnseenCount: (by = 1) =>
    set((state) => ({ unseenCount: Math.max(0, state.unseenCount - by) })),
  setSelectedThreadId: (selectedThreadId) => set({ selectedThreadId }),
  setSelectedAccountId: (selectedAccountId) => set({ selectedAccountId }),
});
