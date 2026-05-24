import { useMutation, useQueryClient } from "@tanstack/react-query";
import switchOrganization from "@/lib/api/client/app/organizations/switchOrganization";

// Switching org changes the entire data context — campaigns, contacts,
// emails, suppression, mailboxes, members, audits all become org-scoped
// data for a *different* org. Invalidating one or two queries leaves
// stale rows on screen for everything else. Cheaper-to-the-user fix:
// drop every cached query so each surface refetches on mount.
//
// `auth/me` is preserved because identity didn't change, just the
// active workspace.
export default function useSwitchOrganization() {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: (id: string) => switchOrganization(id),
        onSuccess: () => {
            queryClient.removeQueries({
                predicate: (q) => {
                    const root = q.queryKey[0];
                    return root !== "auth";
                },
            });
        },
    });
}
