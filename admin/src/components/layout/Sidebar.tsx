// Left rail navigation. Mirrors the dashboard's general structure
// (icon + label rows, collapsible sections) but uses the admin-tinted
// sidebar background and amber accent for active items so it never
// gets confused with the dashboard's sidebar.

import { NavLink } from "react-router-dom";
import {
    Bell,
    Activity,
    BarChart3,
    Building2,
    FileText,
    Flame,
    Gauge,
    HeartPulse,
    LayoutDashboard,
    Mailbox,
    Megaphone,
    Radio,
    Ruler,
    Send,
    Server,
    Settings2,
    ShieldCheck,
    SlidersHorizontal,
    Sparkles,
    Users,
} from "lucide-react";
import { cn } from "@/lib/utils";
import { Logo } from "@/components/Logo";
import { useMe } from "@/hooks/useMe";
import { AdminPerm, hasAdminPerm } from "@/lib/auth/permissions";
import { findingCount, useInstanceHealth, worstSeverity } from "@/hooks/useInstanceHealth";
import type { CheckSeverity } from "@/lib/api/client/admin/instance";
import { AdminBadge } from "./AdminBadge";

interface NavItem {
    to: string;
    label: string;
    icon: React.ComponentType<{ className?: string }>;
    end?: boolean;
    // Admin permission bit the backend gates this route's data on.
    perm?: number;
    // Renders the live count of instance findings next to the label.
    healthBadge?: boolean;
}

interface NavGroup {
    label: string;
    items: NavItem[];
}

const GROUPS: NavGroup[] = [
    {
        label: "Operations",
        items: [
            { to: "/", label: "Overview", icon: LayoutDashboard, end: true },
            { to: "/workers", label: "Workers", icon: Server, end: true },
            { to: "/mailboxes", label: "Mailboxes", icon: Mailbox },
            { to: "/warmup", label: "Warmup", icon: Flame, end: true },
            { to: "/warmup/appeals", label: "Warmup Appeals", icon: ShieldCheck },
            { to: "/warmup-content", label: "Warmup Content", icon: Sparkles },
            { to: "/campaigns", label: "Campaigns", icon: Megaphone },
        ],
    },
    {
        label: "Accounts",
        items: [
            { to: "/users", label: "Users", icon: Users },
            { to: "/organizations", label: "Organizations", icon: Building2 },
            { to: "/limit-requests", label: "Limit requests", icon: Gauge },
            { to: "/outreach", label: "Outreach", icon: Send },
        ],
    },
    {
        label: "Insight",
        items: [
            { to: "/analytics", label: "Analytics", icon: BarChart3 },
            { to: "/events", label: "Live Events", icon: Radio },
            { to: "/audit", label: "Audit Log", icon: FileText },
        ],
    },
    {
        label: "Instance",
        items: [
            {
                to: "/health",
                label: "Setup and health",
                icon: HeartPulse,
                perm: AdminPerm.ViewAnalytics,
                healthBadge: true,
            },
            {
                to: "/configuration",
                label: "Configuration",
                icon: SlidersHorizontal,
                perm: AdminPerm.ManageSettings,
                end: true,
            },
            {
                to: "/configuration/settings",
                label: "Instance settings",
                icon: Settings2,
                perm: AdminPerm.ManageSettings,
            },
            {
                to: "/configuration/notifications",
                label: "Notifications",
                icon: Bell,
                perm: AdminPerm.ManageSettings,
            },
            {
                to: "/limits",
                label: "Effective limits",
                icon: Ruler,
                perm: AdminPerm.ViewAnalytics,
            },
            {
                to: "/system",
                label: "System Status",
                icon: Activity,
                perm: AdminPerm.ViewAnalytics,
            },
        ],
    },
];

export function Sidebar() {
    const { data: me } = useMe();
    const mask = me?.admin_permissions;
    const canReadHealth = hasAdminPerm(mask, AdminPerm.ViewAnalytics);
    const healthQ = useInstanceHealth({ enabled: canReadHealth });
    const findings = findingCount(healthQ.data);
    const worst = worstSeverity(healthQ.data);

    return (
        <aside
            className={cn(
                "hidden md:flex md:w-64 lg:w-72 shrink-0 flex-col",
                "border-r border-sidebar-border bg-sidebar admin-sidebar-pattern",
            )}
        >
            {/* h-14 matches the Topbar so the two headers sit on one line. */}
            <div className="flex h-14 shrink-0 items-center justify-between gap-2 px-4 border-b border-sidebar-border">
                <div className="flex items-center gap-2 min-w-0">
                    <Logo className="size-6 shrink-0 text-foreground" />
                    <div className="min-w-0 leading-none">
                        <div className="text-sm font-semibold text-sidebar-foreground leading-none truncate">
                            Warmbly
                        </div>
                        <div className="text-[11px] text-muted-foreground mt-1">
                            Control plane
                        </div>
                    </div>
                </div>
                <AdminBadge compact />
            </div>

            <nav className="flex-1 overflow-y-auto px-2 py-3 space-y-5">
                {GROUPS.map((group) => {
                    const items = group.items.filter(
                        (item) => item.perm === undefined || hasAdminPerm(mask, item.perm),
                    );
                    if (items.length === 0) return null;
                    return (
                        <div key={group.label}>
                            <div className="px-2 mb-1.5 text-[10px] font-semibold uppercase tracking-wider text-muted-foreground">
                                {group.label}
                            </div>
                            <ul className="space-y-0.5">
                                {items.map((item) => (
                                    <li key={item.to}>
                                        <SidebarLink
                                            {...item}
                                            badge={
                                                item.healthBadge && findings > 0
                                                    ? { count: findings, severity: worst }
                                                    : undefined
                                            }
                                        />
                                    </li>
                                ))}
                            </ul>
                        </div>
                    );
                })}
            </nav>

            <div className="px-4 py-3 border-t border-sidebar-border flex items-center gap-2 text-[11px] text-muted-foreground">
                <Activity className="size-3" />
                <span>Admin surface · do not share</span>
            </div>
        </aside>
    );
}

interface CountBadge {
    count: number;
    severity: CheckSeverity | null;
}

const BADGE_TONES: Record<CheckSeverity, string> = {
    error: "bg-red-600 text-white",
    warning: "bg-amber-500 text-white",
    info: "bg-sky-600 text-white",
};

function SidebarLink({ to, label, icon: Icon, end, badge }: NavItem & { badge?: CountBadge }) {
    return (
        <NavLink
            to={to}
            end={end}
            className={({ isActive }) =>
                cn(
                    "group flex items-center gap-2.5 rounded-md px-2 py-1.5 text-sm transition-colors",
                    "text-sidebar-foreground/80 hover:text-sidebar-foreground hover:bg-sidebar-accent",
                    isActive &&
                        // Active state uses the admin accent on the left edge and a
                        // soft amber wash. Distinct from the dashboard's blue active
                        // state without losing the same shape.
                        "bg-[var(--admin-accent-soft)] text-[var(--admin-accent-strong)] font-medium relative " +
                            "before:absolute before:left-0 before:top-1.5 before:bottom-1.5 before:w-0.5 before:rounded-r before:bg-[var(--admin-accent)]",
                )
            }
        >
            <Icon className="size-4 shrink-0 opacity-80 group-hover:opacity-100" />
            <span className="truncate">{label}</span>
            {badge && (
                <span
                    className={cn(
                        "ml-auto shrink-0 rounded-full px-1.5 text-[10px] font-semibold leading-4 tabular-nums",
                        BADGE_TONES[badge.severity ?? "info"],
                    )}
                >
                    {badge.count}
                </span>
            )}
        </NavLink>
    );
}

// Standalone export so other surfaces (mobile drawer, breadcrumbs)
// can reuse the same icon-per-route mapping if needed later.
export const NAV_GROUPS = GROUPS;
export type { NavItem };
