import { useMutation, useQueryClient } from "@tanstack/react-query";
import switchOrganization from "@/lib/api/client/app/organizations/switchOrganization";

interface UseSwitchOrganizationOptions {
    // How to reconcile the query cache after the server session switch lands:
    //
    //  - "reset" (default): the user is switching to a *different* workspace
    //    from the UI (OrgSwitcher / select-org / new workspace). Campaigns,
    //    contacts, emails, suppression, mailboxes, members, audits all become
    //    a different org's data, so drop every cached query and let each
    //    surface refetch fresh on mount — no flash of the previous org's rows.
    //
    //  - "sync": OrgGate is only reconciling the *server session* to the
    //    workspace the UI already shows (the DB session row is NULL right after
    //    a fresh login — see OrgGate). The org is NOT changing. removeQueries
    //    here is actively harmful: the dashboard's bootstrap queries (the
    //    emails list, the unread count, …) fire on mount and are still
    //    in-flight when this POST resolves, so removing them mid-fetch orphans
    //    their observers and the page hangs on a loading spinner until it is
    //    remounted (tab switch) or reloaded. Instead we INVALIDATE: in-flight
    //    fetches resolve normally (never orphaned), and any org-scoped query
    //    that fired before the session sync refetches against the now-correct
    //    session. "organizations" is left alone (the membership list didn't
    //    change) to avoid re-churning OrgGate's own effect.
    //
    // `auth/*` is preserved in both modes — identity didn't change, just the
    // active workspace.
    mode?: "reset" | "sync";
}

export default function useSwitchOrganization({ mode = "reset" }: UseSwitchOrganizationOptions = {}) {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: (id: string) => switchOrganization(id),
        onSuccess: () => {
            if (mode === "sync") {
                queryClient.invalidateQueries({
                    predicate: (q) => {
                        const root = q.queryKey[0];
                        return root !== "auth" && root !== "organizations";
                    },
                });
                return;
            }
            queryClient.removeQueries({
                predicate: (q) => {
                    const root = q.queryKey[0];
                    return root !== "auth";
                },
            });
        },
    });
}
