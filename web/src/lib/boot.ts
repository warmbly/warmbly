// The dashboard's session, workspace, identity and plan, resolved in two parallel rounds before its first paint.

import { isRedirect, redirect } from "@tanstack/react-router";
import type { QueryClient } from "@tanstack/react-query";

import getToken from "./helper/getToken";
import { acquisitionSearch } from "./acquisition";
import { clearClientSession } from "./session";
import { AuthError } from "./errors/auth";
import type { AppError } from "./api/client/normalizeError";
import switchOrganization from "./api/client/app/organizations/switchOrganization";
import { organizationsQuery } from "./api/hooks/app/organizations/useOrganizations";
import { userQuery } from "./api/hooks/auth/useUser";
import { timezonesQuery } from "./api/hooks/app/useTimezones";
import { subscriptionQuery } from "./api/hooks/app/subscription/useSubscription";
import { authConfigQuery } from "./api/hooks/auth/useAuthConfig";
import { useAppStore } from "@/stores/useAppStore";

let boot: Promise<void> | null = null;
// The workspace this page has already put on the server session.
let syncedOrgId: string | null = null;

export function isOrgSynced(id: string): boolean {
    return syncedOrgId === id;
}

export function markOrgSynced(id: string | null): void {
    syncedOrgId = id;
}

/** Forget the boot, so the next dashboard navigation resolves the session afresh. */
export function resetBoot(): void {
    boot = null;
    syncedOrgId = null;
}

function isAuthFailure(err: unknown): boolean {
    if (err instanceof AuthError) return true;
    const e = err as AppError | undefined;
    return e?.status === 401 || e?.redirect === true;
}

export function requireDashboardToken(location: { pathname: string; href: string }) {
    if (getToken()) return;
    redirectToLogin(location);
}

function redirectToLogin(location: { pathname: string; href: string }) {
    // A Slack link code is single-use and short-lived, so it survives sign-in.
    const next = location.pathname === "/app/slack/link" ? location.href : undefined;
    const search = acquisitionSearch(new URL(location.href, window.location.origin).search);
    throw redirect({ to: "/auth/login", search: next ? { ...search, next } : search, replace: true });
}

/** Resolves once the dashboard session is ready; page loaders await it before org-scoped fetches. */
export function dashboardReady(): Promise<void> {
    return boot ?? Promise.resolve();
}

export async function bootDashboard(queryClient: QueryClient, location: { pathname: string; href: string }) {
    requireDashboardToken(location);
    boot ??= runBoot(queryClient, location).catch((err) => {
        boot = null;
        throw err;
    });
    await boot;
}

async function runBoot(queryClient: QueryClient, location: { pathname: string; href: string }) {
    const store = useAppStore.getState();
    const remembered = store.currentOrganization;

    // Reference data nobody waits on.
    void queryClient.prefetchQuery(timezonesQuery);
    void queryClient.prefetchQuery(authConfigQuery);

    try {
        // The remembered workspace goes on the session in parallel with the
        // membership list; a stale one is refused and resolved below.
        const [orgs, rememberedSync] = await Promise.all([
            queryClient.ensureQueryData(organizationsQuery),
            remembered
                ? switchOrganization(remembered.id).then(() => true, () => false)
                : Promise.resolve(false),
        ]);
        store.setOrganizations(orgs);

        const stillMember = !!remembered && orgs.some((o) => o.id === remembered.id);
        if (stillMember && remembered) {
            if (rememberedSync) syncedOrgId = remembered.id;
        } else if (orgs.length === 1) {
            await switchOrganization(orgs[0].id);
            syncedOrgId = orgs[0].id;
            store.setCurrentOrganization(orgs[0]);
        } else {
            throw redirect({ to: "/select-org", replace: true });
        }

        // Identity carries the workspace's folders and labels, so it follows the switch.
        const [user] = await Promise.all([
            queryClient.ensureQueryData(userQuery),
            queryClient.ensureQueryData(subscriptionQuery).catch(() => undefined),
        ]);
        if (!user.onboarding_completed_at) {
            throw redirect({ to: "/onboarding", replace: true });
        }
    } catch (err) {
        if (isAuthFailure(err)) {
            clearClientSession(queryClient);
            redirectToLogin(location);
        }
        if (isRedirect(err)) throw err;
        // Anything else is shown by the providers' error states, and the next navigation boots again.
        boot = null;
    }
}
