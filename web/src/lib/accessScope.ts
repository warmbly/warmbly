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
