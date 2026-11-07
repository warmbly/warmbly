// What a member restricted to selected resources may open in the dashboard.
// The server enforces the scope; this only keeps them off pages it would refuse.

import { hasPermission, PERMISSION_BITS } from "@/lib/permissions";

const SCOPED_PREFIXES = [
    "/app/unibox",
    "/app/campaigns",
    "/app/analytics",
    "/app/settings/profile",
    "/app/settings/notifications",
    "/app/settings/security",
];

// Campaign tabs that read contacts, senders or settings outside a campaign's own data.
const UNSCOPED_CAMPAIGN_TABS = /^\/app\/campaigns\/[^/]+\/(leads|preferences)(\/|$)/;

export function isScopedPath(pathname: string): boolean {
    if (UNSCOPED_CAMPAIGN_TABS.test(pathname)) return false;
    return SCOPED_PREFIXES.some((p) => pathname === p || pathname.startsWith(p + "/"));
}

/** Where a restricted member lands: the first surface their roles let them read. */
export function restrictedHome(org: { permissions?: number } | null | undefined): string {
    const perms = org?.permissions;
    if (hasPermission(perms, PERMISSION_BITS.VIEW_CAMPAIGNS)) return "/app/campaigns";
    if (hasPermission(perms, PERMISSION_BITS.ACCESS_UNIBOX)) return "/app/unibox";
    if (hasPermission(perms, PERMISSION_BITS.VIEW_ANALYTICS)) return "/app/analytics";
    return "/app/settings/profile";
}

/** What any role gives a restricted member, read-only; mirrors models.RestrictedReadPermissions. */
export const SCOPE_READ_PERMISSIONS = PERMISSION_BITS.VIEW_CAMPAIGNS | PERMISSION_BITS.VIEW_ANALYTICS | PERMISSION_BITS.ACCESS_UNIBOX;

/** What a role that acts in scope lets a restricted member use inside their grants; mirrors models.ScopeActionPermissions. */
export const SCOPE_ACTION_PERMISSIONS = SCOPE_READ_PERMISSIONS | PERMISSION_BITS.MANAGE_CAMPAIGNS | PERMISSION_BITS.SEND_CAMPAIGNS;

/** Whether a restricted member may act with bit inside their grants. */
export function scopeCanAct(org: { scope_actions?: number } | null | undefined, bit: number): boolean {
    return bit !== 0 && hasPermission((org?.scope_actions ?? 0) & SCOPE_ACTION_PERMISSIONS, bit);
}

/** Whether every permission of a role applies to a restricted member, given whether the role acts in scope. */
export function roleScopeSafe(permissions: number, actsInScope: boolean): boolean {
    const reach = actsInScope ? SCOPE_ACTION_PERMISSIONS : SCOPE_READ_PERMISSIONS;
    return (permissions & ~reach) === 0;
}

/** Step actions a restricted member may set: ones that touch nothing outside the campaign. */
export const SCOPE_STEP_ACTIONS: ReadonlySet<string> = new Set(["wait", "end"]);

export interface ClientRolePreset {
    key: "client_view" | "client_reply" | "client_campaigns";
    name: string;
    summary: string;
    color: string;
    permissions: number;
    acts_in_scope: boolean;
}

/** One-click roles for clients, created in the workspace the first time one is picked. */
export const CLIENT_ROLE_PRESETS: readonly ClientRolePreset[] = [
    {
        key: "client_view",
        name: "Client: view only",
        summary: "Sees the shared campaigns and their analytics, and reads the shared mailboxes' conversations.",
        color: "#64748b",
        permissions: SCOPE_READ_PERMISSIONS,
        acts_in_scope: false,
    },
    {
        key: "client_reply",
        name: "Client: can reply",
        summary: "Everything in view only, plus replying, labelling, archiving and snoozing in the shared mailboxes.",
        color: "#0ea5e9",
        permissions: SCOPE_READ_PERMISSIONS,
        acts_in_scope: true,
    },
    {
        key: "client_campaigns",
        name: "Client: manage campaigns",
        summary: "Everything in can reply, plus editing the shared campaigns' steps and schedule and starting or pausing them.",
        color: "#10b981",
        permissions: SCOPE_ACTION_PERMISSIONS,
        acts_in_scope: true,
    },
];

/** The preset a role matches by name, so a renamed preset reads as a custom role. */
export function presetForRole(role: { name: string } | null | undefined): ClientRolePreset | undefined {
    return CLIENT_ROLE_PRESETS.find((p) => p.name === role?.name);
}
