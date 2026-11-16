// Client view: agency staff looking at a client workspace the way one of its members does. It changes
// what this tab draws (the client's brand, and only what the picked role may do); the server still
// answers with the staff member's own access, so it can narrow what is shown and never widen it.

import { ALL_PERMISSIONS } from "@/lib/permissions";

export interface ClientView {
    orgId: string;
    /** "owner", or a workspace role's id. */
    roleId: string;
    roleName: string;
    permissions: number;
    /** The agency page the view was opened from, which its way back returns to. */
    returnTo?: ClientViewReturn;
}

export type ClientViewReturn = "/app/agency" | "/app/agency/branding";

interface Member {
    id: string;
    role: string;
    permissions?: number;
}

const KEY = "warmbly.client-view";

// Per tab: another tab of the same person keeps working as staff.
export function readClientView(): ClientView | null {
    try {
        const raw = sessionStorage.getItem(KEY);
        if (!raw) return null;
        const v = JSON.parse(raw) as Partial<ClientView>;
        if (typeof v.orgId !== "string" || typeof v.roleId !== "string" || typeof v.permissions !== "number") return null;
        return {
            orgId: v.orgId,
            roleId: v.roleId,
            roleName: typeof v.roleName === "string" ? v.roleName : "",
            permissions: v.permissions,
            returnTo: v.returnTo === "/app/agency/branding" ? v.returnTo : undefined,
        };
    } catch {
        return null;
    }
}

export function writeClientView(view: ClientView | null): void {
    try {
        if (view) sessionStorage.setItem(KEY, JSON.stringify(view));
        else sessionStorage.removeItem(KEY);
    } catch {
        /* storage blocked: the view lasts until the page reloads */
    }
}

/** The member as the picked role sees the workspace: never more than the staff member holds. */
export function overlayClientView<T extends Member>(org: T, view: ClientView | null): T {
    if (!view || view.orgId !== org.id) return org;
    const own = org.role === "owner" ? ALL_PERMISSIONS : (org.permissions ?? 0);
    const owner = view.roleId === "owner";
    return {
        ...org,
        // Owner-only screens follow the role, so a non-owner never sees them drawn as theirs.
        role: owner ? org.role : view.roleId,
        permissions: own & (owner ? ALL_PERMISSIONS : view.permissions),
    };
}

/** The role a client view opens with: the one most of the workspace's members hold. */
export function defaultClientRole<R extends { member_count: number; permissions: number }>(roles: R[]): R | undefined {
    return [...roles].sort((a, b) => b.member_count - a.member_count || bitCount(a.permissions) - bitCount(b.permissions))[0];
}

function bitCount(n: number): number {
    let c = 0;
    for (let v = n >>> 0; v; v &= v - 1) c++;
    return c;
}
