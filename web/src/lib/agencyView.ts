// The agency view and the workspace view: which one a path belongs to, which one
// the user was last in, and the workspace list grouped under each agency.

import type { Organization } from "@/stores/slices/organizationSlice";
import type { Agency } from "@/lib/api/models/app/agency/Agency";

const VIEW_KEY = "warmbly.view";

export type DashboardView = "agency" | "workspace";

export function isAgencyPath(pathname: string): boolean {
    return pathname === "/app/agency" || pathname.startsWith("/app/agency/");
}

/** The view the user was last in; fallback when this origin has never recorded one. */
export function lastView(fallback: DashboardView = "workspace"): DashboardView {
    try {
        const stored = localStorage.getItem(VIEW_KEY);
        if (stored === "agency" || stored === "workspace") return stored;
        return fallback;
    } catch {
        return fallback;
    }
}

export function rememberView(view: DashboardView): void {
    try {
        localStorage.setItem(VIEW_KEY, view);
    } catch {
        /* private mode: the next visit opens the default view */
    }
}

type Grouped = Pick<Organization, "name" | "agency">;

export interface AgencyGroup<O extends Grouped = Organization> {
    agency: Agency;
    clients: O[];
}

/** Each agency the user is staff of with its client workspaces, then every other workspace. */
export function groupWorkspaces<O extends Grouped>(orgs: O[], agencies: Agency[]): { groups: AgencyGroup<O>[]; own: O[] } {
    const staffOf = new Set(agencies.map((a) => a.id));
    const groups = agencies.map((agency) => ({
        agency,
        clients: orgs.filter((o) => o.agency?.id === agency.id).sort((a, b) => a.name.localeCompare(b.name)),
    }));
    const own = orgs.filter((o) => !o.agency || !staffOf.has(o.agency.id));
    return { groups, own };
}

export function matchesQuery(name: string, q: string): boolean {
    return !q || name.toLowerCase().includes(q.trim().toLowerCase());
}
