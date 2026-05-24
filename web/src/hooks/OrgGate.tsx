// OrgGate — gates the entire /app/* subtree on having a workspace
// selected, both client- and server-side.
//
// The user can land without a current workspace in several ways:
//   - brand-new signup → orgs.length === 0
//   - removed from every org they were in
//   - they explicitly cleared currentOrganization
//   - new login, persisted `currentOrganization` was wiped on logout
//   - they have orgs but never round-tripped a /organization/switch
//
// Resolution rules:
//   - 0 orgs: send to /select-org to create or accept an invite.
//   - 1 org with no current selection: pick it server-side and stay.
//     (Auto-pick is safe here because the choice is forced.)
//   - 2+ orgs with no current selection: send to /select-org to pick.
//   - current org no longer in membership list: send to /select-org.
//
// Auto-pick is intentionally NOT done client-side only — that was the
// original bug. The local selection has to match the session row, so
// we POST `/organization/switch/:id` first and only advance local
// state on success.

import { useEffect, useRef } from "react";
import { useNavigate } from "react-router-dom";
import useOrganizations from "@/lib/api/hooks/app/organizations/useOrganizations";
import useSwitchOrganization from "@/lib/api/hooks/app/organizations/useSwitchOrganization";
import { useAppStore } from "@/stores";

export function OrgGate() {
    const navigate = useNavigate();
    const orgs = useOrganizations();
    const setOrganizations = useAppStore((s) => s.setOrganizations);
    const setCurrentOrganization = useAppStore((s) => s.setCurrentOrganization);
    const currentOrg = useAppStore((s) => s.currentOrganization);
    const switchOrgMutation = useSwitchOrganization();

    // Guard against the single-org auto-pick re-firing while the
    // mutation is in flight (effect re-runs on data changes).
    const autoPickInFlight = useRef(false);

    useEffect(() => {
        if (orgs.isPending) return;
        const list = orgs.data ?? [];
        setOrganizations(list);

        const stillMember = currentOrg
            ? list.some((o) => o.id === currentOrg.id)
            : false;
        if (stillMember) return;

        if (list.length === 0) {
            navigate("/select-org", { replace: true });
            return;
        }

        if (list.length === 1) {
            if (autoPickInFlight.current) return;
            autoPickInFlight.current = true;
            const only = list[0];
            switchOrgMutation.mutate(only.id, {
                onSuccess: () => setCurrentOrganization(only),
                onError: () => navigate("/select-org", { replace: true }),
                onSettled: () => {
                    autoPickInFlight.current = false;
                },
            });
            return;
        }

        navigate("/select-org", { replace: true });
    }, [
        orgs.isPending,
        orgs.data,
        currentOrg,
        setOrganizations,
        setCurrentOrganization,
        switchOrgMutation,
        navigate,
    ]);

    return null;
}
