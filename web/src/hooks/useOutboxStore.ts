// Pending "undo send" queue. Instant sends are queued a few seconds out
// server-side; each one lands here so the header pill can count down and
// cancel it. A standalone zustand store (mirroring useComposeStore) so the
// send sites, the header pill, and ThreadView can all reach it without the
// app store. No persistence: a reload just means the emails send.

import { create } from "zustand";
import type { ComposeDraft } from "@/lib/api/client/app/unibox/composeDrafts";

// Everything needed to reopen the thread's reply composer exactly as it
// was when a cancelled reply is handed back.
export interface OutboxReplyPayload {
    threadId: string;
    messageId: string;
    mode: "reply" | "forward";
    to: string[];
    cc: string[];
    bcc: string[];
    subject: string;
    body: string;
}

export interface OutboxEntry {
    taskId: string;
    /** Epoch ms when the backend will actually send. */
    scheduledAt: number;
    kind: "compose" | "reply";
    to: string[];
    subject: string;
    /** Compose only: draft seed so cancel restores the composer via openDraft. */
    seed?: ComposeDraft;
    threadId?: string;
    /** Reply only: payload for reopening the thread's reply composer. */
    reply?: OutboxReplyPayload;
}

interface OutboxStore {
    entries: OutboxEntry[];
    // A cancelled reply waiting for its thread to be open; ThreadView
    // consumes it and reopens the reply composer with the payload.
    pendingReplyRestore: OutboxReplyPayload | null;
    add: (entry: OutboxEntry) => void;
    remove: (taskId: string) => void;
    setReplyRestore: (restore: OutboxReplyPayload | null) => void;
}

export const useOutboxStore = create<OutboxStore>((set) => ({
    entries: [],
    pendingReplyRestore: null,
    add: (entry) =>
        set((s) => ({
            entries: [...s.entries.filter((e) => e.taskId !== entry.taskId), entry],
        })),
    remove: (taskId) =>
        set((s) => ({ entries: s.entries.filter((e) => e.taskId !== taskId) })),
    setReplyRestore: (restore) => set({ pendingReplyRestore: restore }),
}));

// The server response carries the real fire time, but a skewed client clock
// would make the countdown start at 0; fall back to the user's window.
export function resolveSendAt(serverIso: unknown, undoSeconds: number): number {
    const fallback = Date.now() + (undoSeconds || 30) * 1000;
    if (typeof serverIso !== "string" && !(serverIso instanceof Date)) return fallback;
    const at = new Date(serverIso).getTime();
    if (!Number.isFinite(at) || at <= Date.now()) return fallback;
    return at;
}
