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
//   - current org still a member but never synced to the server session
//     this load: re-POST the switch so the DB session row matches the
//     locally-persisted pointer (see below).
//
// Why the re-sync matters: the backend resolves org context from the
// *server session* (sessions.current_organization_id), loaded fresh
// from Postgres on every request — NOT from the JWT. A fresh login
// starts with current_organization_id = NULL. The zustand
// `currentOrganization` is persisted across logins, so on the next
// login it's already populated and `stillMember` is true — which used
// to early-return and skip the switch entirely. The UI then showed an
// org as selected while the session row stayed NULL, so user-scoped
// tabs (emails) worked but org-scoped tabs (contacts, campaigns) 4xx'd
// with "no organization selected". We now POST the switch once per
// mount to guarantee the session row matches the local pointer.

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

    // Destructure `mutate` — react-query returns a new wrapper object
    // each render, but the bound mutate fn is stable. Putting the
    // whole mutation object in the effect deps below caused the effect
    // to fire on every render → repeated navigate() calls into
    // /select-org → "Too many calls to Location or History APIs" and a
    // setState loop from setOrganizations re-firing each time.
    const { mutate: switchOrgMutate } = useSwitchOrganization();

    // Guard against the single-org auto-pick re-firing while the
    // mutation is in flight (effect re-runs on data changes).
    const autoPickInFlight = useRef(false);
    // Remember the redirect target we last fired for the current
    // (list-shape, currentOrg) tuple. Without this guard, a state
    // update inside this effect (setOrganizations clearing a stale
    // currentOrganization to null) re-runs the effect before the
    // router has unmounted us, and we hit history.replaceState twice
    // in the same task — which throws DOMException in Firefox.
    const lastRedirectKey = useRef<string | null>(null);
    // The org id we've already re-synced to the server session this
    // mount. Without this the stillMember branch would re-POST the
    // switch on every effect run (e.g. when orgs.data settles), and
    // useSwitchOrganization wipes the query cache on success → refetch
    // storm. One sync per (mount, org) is enough.
    const syncedOrgId = useRef<string | null>(null);

    useEffect(() => {
        if (orgs.isPending) return;
        const list = orgs.data ?? [];
        setOrganizations(list);

        const stillMember = currentOrg
            ? list.some((o) => o.id === currentOrg.id)
            : false;
        if (stillMember && currentOrg) {
            lastRedirectKey.current = null;
            // Persisted local pointer is valid, but a fresh login's
            // session row may still be NULL. Re-POST the switch once so
            // the server session matches what the UI is showing.
            if (syncedOrgId.current !== currentOrg.id) {
                syncedOrgId.current = currentOrg.id;
                switchOrgMutate(currentOrg.id, {
                    onError: () => {
                        // Let it retry on a later run if the sync failed.
                        syncedOrgId.current = null;
                    },
                });
            }
            return;
        }

        if (list.length === 0) {
            const key = "empty";
            if (lastRedirectKey.current === key) return;
            lastRedirectKey.current = key;
            navigate("/select-org", { replace: true });
            return;
        }

        if (list.length === 1) {
            if (autoPickInFlight.current) return;
            autoPickInFlight.current = true;
            const only = list[0];
            switchOrgMutate(only.id, {
                onSuccess: () => {
                    syncedOrgId.current = only.id;
                    setCurrentOrganization(only);
                },
                onError: () => navigate("/select-org", { replace: true }),
                onSettled: () => {
                    autoPickInFlight.current = false;
                },
            });
            return;
        }

        const key = `multi:${list.map((o) => o.id).join(",")}`;
        if (lastRedirectKey.current === key) return;
        lastRedirectKey.current = key;
        navigate("/select-org", { replace: true });
    }, [
        orgs.isPending,
        orgs.data,
        currentOrg,
        setOrganizations,
        setCurrentOrganization,
        switchOrgMutate,
        navigate,
    ]);

    return null;
}
