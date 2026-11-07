import removeEmail from "@/lib/api/client/app/emails/removeEmail";
import { mutationOptions, useMutation, useQueryClient, type QueryClient } from "@tanstack/react-query";
import invalidateAfterMailboxRemoval from "./invalidateAfterMailboxRemoval";
import patchEmailLists from "./patchEmailLists";

export default function useRemoveEmail(id: string) {
    return useMutation(removeEmailOptions(useQueryClient(), id));
}

export function removeEmailOptions(queryClient: QueryClient, id: string) {
    return mutationOptions({
        mutationFn: () => removeEmail(id),
        onSuccess: () => {
            // Drop the row first so the list reacts immediately, then refetch.
            patchEmailLists(queryClient, (rows) => rows.filter((c) => c.id !== id));
            void invalidateAfterMailboxRemoval(queryClient);
        },
    });
}
