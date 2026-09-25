// The actions a conversation carries, wherever it is shown.
//
// The thread header, the list row and the selection bar offer the same set, so
// the copy, the undo toast and the cache handling live here once rather than
// three times. Everything takes a SET of thread ids: one row is the
// one-element case, and a selection is one round trip rather than one per row.
//
// Addressing by thread rather than by message id is deliberate. A list row
// knows its conversation and not the ids inside it, and filing part of a
// conversation leaves it in the view it was filed out of, which is exactly
// what "Archive does nothing" looked like.

import React from "react";
import toast from "react-hot-toast";
import { useQueryClient } from "@tanstack/react-query";

import useMarkSeen from "@/lib/api/hooks/app/unibox/useMarkSeen";
import useMoveFolder from "@/lib/api/hooks/app/unibox/useMoveFolder";
import moveFolderRequest, {
    type FilableFolder,
} from "@/lib/api/client/app/unibox/moveFolder";
import { removeThreadsFromLists } from "@/lib/api/hooks/app/unibox/listCache";
import {
    snoozeThreads,
    unsnoozeThreads,
} from "@/lib/api/client/app/unibox/snoozeThread";
import { SNOOZE_MAX_MS } from "@/lib/unibox/snooze";

// Filing copy, per destination. "Deleted" is deliberately not said anywhere:
// the message is moved to Trash here and still sits in the mail client.
const FILE_COPY: Record<FilableFolder, { done: string; failed: string }> = {
    archive: { done: "Archived", failed: "Couldn't archive" },
    trash: { done: "Moved to Trash", failed: "Couldn't move to Trash" },
    inbox: { done: "Moved to Inbox", failed: "Couldn't move to Inbox" },
};

function plural(n: number, one: string): string {
    return n === 1 ? one : `${n.toLocaleString()} conversations`;
}

export interface ConversationActions {
    /** Archive / Trash / Move to inbox. Optional ids cover a loaded thread. */
    file: (threadIds: string[], folder: FilableFolder, ids?: string[]) => Promise<void>;
    setSeen: (threadIds: string[], seen: boolean) => void;
    snooze: (threadIds: string[], until: Date) => Promise<void>;
    unsnooze: (threadIds: string[]) => Promise<void>;
    filing: boolean;
}

export function useConversationActions(): ConversationActions {
    const queryClient = useQueryClient();
    const moveFolder = useMoveFolder();
    const markSeen = useMarkSeen();

    // One click and the conversations are gone from the list, so the way back
    // belongs on screen. The undo calls the endpoint directly rather than the
    // mutation: whatever offered it may have unmounted by the time it is
    // clicked, and react-query drops an unmounted observer's callbacks.
    const offerUndo = React.useCallback(
        (message: string, threadIds: string[]) => {
            toast((t) => (
                <span className="flex items-center gap-3 text-[12.5px] text-slate-700">
                    {message}
                    <button
                        type="button"
                        onClick={() => {
                            toast.dismiss(t.id);
                            moveFolderRequest({ threadIds, folder: "inbox" })
                                .then(() => {
                                    queryClient.invalidateQueries({ queryKey: ["unibox"] });
                                    toast.success("Moved back to Inbox");
                                })
                                .catch(() => toast.error("Couldn't undo"));
                        }}
                        className="h-6 px-2 rounded-md border border-slate-200 hover:border-slate-300 text-[11.5px] font-medium text-sky-700 hover:bg-sky-50 transition-colors"
                    >
                        Undo
                    </button>
                </span>
            ));
        },
        [queryClient],
    );

    const file = React.useCallback(
        async (threadIds: string[], folder: FilableFolder, ids?: string[]) => {
            if (threadIds.length === 0) return;
            const copy = FILE_COPY[folder];
            const done =
                threadIds.length === 1
                    ? copy.done
                    : `${copy.done} ${threadIds.length.toLocaleString()}`;
            try {
                await moveFolder.mutateAsync({ ids, folder, threadIds });
                if (folder === "inbox") toast.success(done);
                else offerUndo(done, threadIds);
            } catch {
                // The rows left the list on click; the refetch is putting them
                // back, and the toast has to say so or it reads as a glitch.
                toast.error(
                    threadIds.length === 1
                        ? `${copy.failed}. It's back in the list.`
                        : `${copy.failed} ${threadIds.length.toLocaleString()} conversations. They're back in the list.`,
                );
            }
        },
        [moveFolder, offerUndo],
    );

    const setSeen = React.useCallback(
        (threadIds: string[], seen: boolean) => {
            if (threadIds.length === 0) return;
            markSeen.mutate({ threadIds, seen });
            if (threadIds.length > 1) {
                toast.success(
                    `${plural(threadIds.length, "Conversation")} marked as ${seen ? "read" : "unread"}`,
                );
            }
        },
        [markSeen],
    );

    const snooze = React.useCallback(
        async (threadIds: string[], until: Date) => {
            if (threadIds.length === 0) return;
            if (
                Number.isNaN(until.getTime()) ||
                until.getTime() <= Date.now() + 5_000
            ) {
                toast.error("Pick a future time (a few seconds out, please)");
                return;
            }
            if (until.getTime() - Date.now() > SNOOZE_MAX_MS) {
                toast.error("Snooze can't be more than 90 days out");
                return;
            }
            // Gone from the list the moment it is snoozed; the refetch confirms
            // it, and on error that same refetch is the rollback.
            await removeThreadsFromLists(queryClient, threadIds, "snooze");
            try {
                await snoozeThreads(threadIds, until);
                toast.success(
                    threadIds.length === 1
                        ? "Snoozed"
                        : `Snoozed ${threadIds.length.toLocaleString()}`,
                );
            } catch {
                toast.error("Couldn't snooze");
            } finally {
                queryClient.invalidateQueries({ queryKey: ["unibox"] });
            }
        },
        [queryClient],
    );

    const unsnooze = React.useCallback(
        async (threadIds: string[]) => {
            if (threadIds.length === 0) return;
            try {
                await unsnoozeThreads(threadIds);
                toast.success("Un-snoozed");
            } catch {
                toast.error("Couldn't un-snooze");
            } finally {
                queryClient.invalidateQueries({ queryKey: ["unibox"] });
            }
        },
        [queryClient],
    );

    return { file, setSeen, snooze, unsnooze, filing: moveFolder.isPending };
}
