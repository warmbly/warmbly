import { useMutation, useQueryClient } from "@tanstack/react-query";
import refreshAuthCheck from "@/lib/api/client/app/emails/refreshAuthCheck";

// Re-checks a mailbox's sending domain and persists the verdict.
//
// This is the owner's way out of the send gate: fix the DNS, press re-check,
// and sending resumes instead of waiting for the background sweep. So it has to
// invalidate the mailbox queries: the drawer and the list both show the stored
// auth state, and leaving them stale is how "I fixed it and nothing happened"
// happens anyway.
export default function useRefreshAuthCheck(id: string) {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: () => refreshAuthCheck(id),
        onSuccess: (data) => {
            queryClient.setQueryData(["analytics", "accounts", id, "auth-check"], data);
            // The verdict is per-domain, so it can move sibling mailboxes too:
            // invalidate the whole list rather than patching this one row.
            queryClient.invalidateQueries({ queryKey: ["emails"] });
        },
    });
}
