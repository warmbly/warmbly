import { useMutation, useQueryClient } from "@tanstack/react-query";
import moveFolder, { type FilableFolder } from "@/lib/api/client/app/unibox/moveFolder";
import { removeThreadsFromLists } from "./listCache";

interface MoveFolderInput {
    ids: string[];
    folder: FilableFolder;
    /** Conversation the ids belong to, so its row leaves the open list at once. */
    threadId?: string;
}

export default function useMoveFolder() {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: ({ ids, folder }: MoveFolderInput) => moveFolder({ ids, folder }),
        // The row goes now; the refetch below confirms it.
        onMutate: async ({ threadId }) => {
            if (threadId) await removeThreadsFromLists(queryClient, [threadId]);
        },
        // A move changes which scopes the thread belongs to and every folder's
        // counts, so the whole unibox tree is re-read rather than patched. On
        // error that same re-read is what puts the row back.
        onSettled: () => {
            queryClient.invalidateQueries({ queryKey: ["unibox"] });
        },
    });
}
