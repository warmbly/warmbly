// The one nav model. The sidebar, the page header's section tabs, the command
// palette and the document title all read it, so a route added here is
// reachable and titled everywhere at once.
//
// An entry is a section of one or more pages. A section with several pages
// gets one sidebar row; its pages show as tabs in the page header. Every page
// keeps its own URL.

import type { ComponentType } from "react";
import {
    ArrowLeftRight,
    Blocks,
    Building2,
    Flame,
    HeartPulse,
    History,
    LayoutDashboard,
    Mailbox,
    Network,
    Send,
    SlidersHorizontal,
    Terminal,
    TicketPercent,
    UserCog,
    Users,
} from "lucide-react";
import { AdminPerm, hasAdminPerm } from "@/lib/auth/permissions";

export type NavIcon = ComponentType<{ className?: string }>;

export interface NavPage {
    to: string;
    // Tab label inside its section.
    label: string;
    // Standalone name, for the palette and the browser tab. Defaults to label.
    title?: string;
    // Match the path exactly instead of as a prefix.
    end?: boolean;
    // The page has sub-routes of its own (/warmup-content/library).
    nested?: boolean;
    // Admin permission bit the backend gates this route's data on.
    perm?: number;
}

export interface NavItem {
    label: string;
    icon: NavIcon;
    pages: NavPage[];
    // Renders the live count of instance findings next to the label.
    healthBadge?: boolean;
}

export interface NavGroup {
    label: string;
    items: NavItem[];
}

export const NAV_GROUPS: NavGroup[] = [
    {
        label: "Overview",
        items: [{ label: "Overview", icon: LayoutDashboard, pages: [{ to: "/", label: "Overview", end: true }] }],
    },
    {
        label: "Operations",
        items: [
            {
                label: "Fleet",
                icon: Network,
                pages: [
                    { to: "/workers", label: "Workers", perm: AdminPerm.ViewWorkers },
                    { to: "/fleet", label: "Placement", title: "Fleet placement", perm: AdminPerm.ViewWorkers },
                ],
            },
            {
                label: "Mailboxes",
                icon: Mailbox,
                pages: [
                    { to: "/mailboxes", label: "Mailboxes", perm: AdminPerm.ViewUsers },
                    { to: "/sync", label: "Sync", title: "Mailbox sync", perm: AdminPerm.ViewUsers },
                ],
            },
            {
                label: "Warmup",
                icon: Flame,
                pages: [
                    { to: "/warmup", label: "Pool", title: "Warmup", end: true, perm: AdminPerm.ViewWarmupPool },
                    { to: "/warmup/appeals", label: "Appeals", title: "Warmup appeals", perm: AdminPerm.ReviewAppeals },
                    { to: "/warmup-content", label: "Content", title: "Warmup content", nested: true, perm: AdminPerm.ViewWarmupPool },
                    { to: "/placement", label: "Seed panel", perm: AdminPerm.ViewWarmupPool },
                ],
            },
            {
                label: "Sending",
                icon: Send,
                pages: [
                    { to: "/campaigns", label: "Campaigns", perm: AdminPerm.ViewCampaigns },
                    { to: "/sends", label: "Sends", perm: AdminPerm.ViewCampaigns },
                ],
            },
        ],
    },
    {
        label: "Accounts",
        items: [
            { label: "Users", icon: Users, pages: [{ to: "/users", label: "Users", perm: AdminPerm.ViewUsers }] },
            {
                label: "Organizations",
                icon: Building2,
                pages: [
                    { to: "/organizations", label: "Organizations", perm: AdminPerm.ViewOrganizations },
                    { to: "/limit-requests", label: "Limit requests", perm: AdminPerm.ViewOrganizations },
                ],
            },
            {
                label: "Growth",
                icon: TicketPercent,
                pages: [
                    { to: "/discounts", label: "Promo codes", perm: AdminPerm.ViewOrganizations },
                    { to: "/outreach", label: "Outreach", perm: AdminPerm.ViewOrganizations },
                ],
            },
            {
                label: "Developer apps",
                icon: Blocks,
                pages: [{ to: "/developer-apps", label: "Developer apps", perm: AdminPerm.ViewOrganizations }],
            },
            {
                label: "Admins",
                icon: UserCog,
                pages: [
                    { to: "/admins", label: "Admins", perm: AdminPerm.GrantAdminAccess },
                    { to: "/testers", label: "Testers", perm: AdminPerm.ViewUsers },
                ],
            },
        ],
    },
    {
        label: "Instance",
        items: [
            { label: "CLI sign-in", icon: Terminal, pages: [{ to: "/device", label: "CLI sign-in", title: "Approve CLI sign-in" }] },
            {
                label: "Setup and health",
                icon: HeartPulse,
                healthBadge: true,
                pages: [{ to: "/health", label: "Setup and health", perm: AdminPerm.ViewAnalytics }],
            },
            {
                label: "Configuration",
                icon: SlidersHorizontal,
                pages: [{ to: "/configuration", label: "Configuration", perm: AdminPerm.ManageSettings }],
            },
            {
                label: "Activity",
                icon: History,
                pages: [
                    { to: "/audit", label: "Audit log", perm: AdminPerm.ViewAuditLogs },
                    { to: "/events", label: "Live events" },
                    { to: "/jobs", label: "Jobs", perm: AdminPerm.ViewAnalytics },
                ],
            },
            {
                label: "Transfers",
                icon: ArrowLeftRight,
                pages: [{ to: "/transfers", label: "Transfers", perm: AdminPerm.ViewOrganizations }],
            },
        ],
    },
];

// visibleNavGroups drops every page the signed-in admin cannot open, then
// every section and group left empty.
export function visibleNavGroups(mask: number | undefined): NavGroup[] {
    return NAV_GROUPS.map((group) => ({
        ...group,
        items: group.items
            .map((item) => ({
                ...item,
                pages: item.pages.filter((p) => p.perm === undefined || hasAdminPerm(mask, p.perm)),
            }))
            .filter((item) => item.pages.length > 0),
    })).filter((group) => group.items.length > 0);
}

export function pageMatches(page: NavPage, pathname: string): boolean {
    if (page.to === "/" || page.end) return pathname === page.to;
    return pathname === page.to || pathname.startsWith(`${page.to}/`);
}

// isSectionPage is true on a section's own pages, not on detail or unknown
// paths under their prefix.
export function isSectionPage(page: NavPage, pathname: string): boolean {
    return page.to === pathname || (!!page.nested && pathname.startsWith(`${page.to}/`));
}

export function sectionActive(item: NavItem, pathname: string): boolean {
    return item.pages.some((p) => pageMatches(p, pathname));
}

// The section a path belongs to, from the full model (not permission-filtered).
export function findSection(pathname: string): NavItem | undefined {
    return NAV_GROUPS.flatMap((g) => g.items).find((item) => sectionActive(item, pathname));
}

export interface FlatPage {
    to: string;
    title: string;
    icon: NavIcon;
}

export function flatPages(groups: NavGroup[]): FlatPage[] {
    return groups.flatMap((g) =>
        g.items.flatMap((item) => item.pages.map((p) => ({ to: p.to, title: p.title ?? p.label, icon: item.icon }))),
    );
}
