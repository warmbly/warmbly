import { useMutation, useQueryClient } from "@tanstack/react-query";
import moveFolder, { type FilableFolder } from "@/lib/api/client/app/unibox/moveFolder";
import { removeThreadsFromLists } from "./listCache";

interface MoveFolderInput {
    /** Explicit message ids, for a caller that has the thread loaded. */
    ids?: string[];
    folder: FilableFolder;
    /**
     * Conversations to file. Their rows leave the open list at once, and the
     * server files every message in each, which a list row could not name.
     */
    threadIds?: string[];
}

export default function useMoveFolder() {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: ({ ids, folder, threadIds }: MoveFolderInput) =>
            moveFolder({ ids, threadIds, folder }),
        // The rows go now; the refetch below confirms it.
        onMutate: async ({ threadIds, folder }) => {
            if (threadIds?.length) await removeThreadsFromLists(queryClient, threadIds, folder);
        },
        // A move changes which scopes the thread belongs to and every folder's
        // counts, so the whole unibox tree is re-read rather than patched. On
        // error that same re-read is what puts the row back.
        onSettled: () => {
            queryClient.invalidateQueries({ queryKey: ["unibox"] });
        },
    });
}
