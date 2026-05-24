// OrgGate — redirects logged-in users to /select-org when they have
// no workspace yet (or no pending invitations + no current org).
//
// The user can land in this state in three ways:
//   - brand-new signup → orgs.length === 0
//   - removed from every org they were in
//   - they explicitly cleared currentOrganization
//
// We mount this above the AppShell so the redirect happens before any
// org-scoped queries (e.g. /unibox) try to run with a missing context.

import { useEffect } from "react";
import { useNavigate } from "react-router-dom";
import useOrganizations from "@/lib/api/hooks/app/organizations/useOrganizations";
import { useAppStore } from "@/stores";

// Side-effect only — returns null. Renders alongside AppLayout so the
// effect runs as soon as the organizations query lands; the navigate()
// unmounts the app subtree when no org is present.
export function OrgGate() {
    const navigate = useNavigate();
    const orgs = useOrganizations();
    const setOrganizations = useAppStore((s) => s.setOrganizations);
    const currentOrg = useAppStore((s) => s.currentOrganization);

    useEffect(() => {
        if (orgs.isPending) return;
        const list = orgs.data ?? [];
        setOrganizations(list);
        if (list.length === 0 && !currentOrg) {
            navigate("/select-org", { replace: true });
        }
    }, [orgs.isPending, orgs.data, currentOrg, setOrganizations, navigate]);

    return null;
}
