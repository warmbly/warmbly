import updateEmail from "@/lib/api/client/app/emails/updateEmail";
import type Inbox from "@/lib/api/models/app/emails/Inbox";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { overlapping, patchQueries, restoreQueries, settle, updateEntity } from "@/lib/api/hooks/optimistic";
import patchEmailLists from "./patchEmailLists";
import { cloudLinkScope, cloudLinkScopeCurrent, invalidateWarmupReports } from "../cloudlink/warmupReports";

// Writes to one mailbox share this prefix, so a reply only lands when it is the last one out.
export const mailboxMutationKey = (id: string) => ["emails", id];

// Optimistic: the switch, the rename and the tags show on the click; the reply replaces them.
// serverOwned fields are decided by the server, so they wait for its answer.
export default function useUpdateEmail(id: string, serverOwned: readonly (keyof Inbox)[] = []) {
    const queryClient = useQueryClient();
    const mutationKey = [...mailboxMutationKey(id), "update"];

    return useMutation({
        mutationKey,
        mutationFn: (inbox: Partial<Inbox>) => updateEmail(id, inbox),
        onMutate: async (inbox) => {
            const shown = { ...inbox };
            for (const k of serverOwned) delete shown[k];
            return { scope: cloudLinkScope(queryClient), snapshot: await patchQueries(
                queryClient,
                [["emails", "list"]],
                updateEntity<Inbox>(id, (row) => ({ ...row, ...shown, id })),
            ) };
        },
        onError: (_err, _inbox, context) => {
            if (context && !cloudLinkScopeCurrent(queryClient, context.scope)) return;
            restoreQueries(queryClient, context?.snapshot);
            settle(queryClient, mailboxMutationKey(id), [["emails", "list"]]);
        },
        onSuccess: (data, _inbox, context) => {
            if (context && !cloudLinkScopeCurrent(queryClient, context.scope)) return;
            if (overlapping(queryClient, mailboxMutationKey(id))) return;
            patchEmailLists(queryClient, (rows) => rows.map((c) => (c.id === id ? data : c)));

            queryClient.setQueryData<Inbox>(
                ["emails", id],
                data
            );
        },
        onSettled: (_data, _error, patch, context) => {
            if (serverOwned.some((field) => field in patch)) return invalidateWarmupReports(queryClient, context?.scope ?? cloudLinkScope(queryClient));
        },
    })
}
