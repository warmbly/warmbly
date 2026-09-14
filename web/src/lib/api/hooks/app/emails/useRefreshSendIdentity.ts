import { useMutation, useQueryClient } from "@tanstack/react-query";
import refreshSendIdentity from "@/lib/api/client/app/emails/refreshSendIdentity";
import type SendIdentity from "@/lib/api/models/app/emails/SendIdentity";

// Re-reads the provider's send-as list, optionally importing its signature.
// An import rewrites the mailbox row, and the refresh can clear a send-as
// choice the provider no longer verifies, so the mailbox queries are
// invalidated alongside the identity itself.
export default function useRefreshSendIdentity(id: string) {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: (importSignature: boolean) => refreshSendIdentity(id, importSignature),
        onSuccess: (data: SendIdentity) => {
            queryClient.setQueryData(["emails", id, "identity"], data);
            queryClient.invalidateQueries({ queryKey: ["emails"] });
        },
    });
}
