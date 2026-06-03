// Left rail navigation. Mirrors the dashboard's general structure
// (icon + label rows, collapsible sections) but uses the admin-tinted
// sidebar background and amber accent for active items so it never
// gets confused with the dashboard's sidebar.

import { NavLink } from "react-router-dom";
import {
    Activity,
    BarChart3,
    Briefcase,
    Building2,
    Cloud,
    Cog,
    CreditCard,
    FileText,
    Flame,
    Gauge,
    HardDrive,
    Inbox,
    LayoutDashboard,
    Mailbox,
    Megaphone,
    Rocket,
    Send,
    Server,
    ServerCog,
    ShieldCheck,
    Sparkles,
    Ticket,
    Users,
} from "lucide-react";
import { cn } from "@/lib/utils";
import { Logo } from "@/components/Logo";
import { AdminBadge } from "./AdminBadge";

interface NavItem {
    to: string;
    label: string;
    icon: React.ComponentType<{ className?: string }>;
    end?: boolean;
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
            {
                to: "/workers/provisioning-jobs",
                label: "Provisioning Jobs",
                icon: Rocket,
            },
            { to: "/mailboxes", label: "Mailboxes", icon: Mailbox },
            { to: "/warmup", label: "Warmup", icon: Flame, end: true },
            { to: "/warmup/appeals", label: "Warmup Appeals", icon: ShieldCheck },
            { to: "/warmup-content", label: "Warmup Content", icon: Sparkles },
            { to: "/placement", label: "Inbox Placement", icon: Inbox },
            { to: "/campaigns", label: "Campaigns", icon: Megaphone },
        ],
    },
    {
        label: "Accounts",
        items: [
            { to: "/users", label: "Users", icon: Users },
            { to: "/organizations", label: "Organizations", icon: Building2 },
            { to: "/limit-requests", label: "Limit requests", icon: Gauge },
            { to: "/plans", label: "Plans & Billing", icon: CreditCard },
            { to: "/discounts", label: "Discounts", icon: Ticket },
            { to: "/enterprise", label: "Enterprise", icon: Briefcase },
            { to: "/outreach", label: "Outreach", icon: Send },
        ],
    },
    {
        label: "Insight",
        items: [
            { to: "/analytics", label: "Analytics", icon: BarChart3 },
            { to: "/audit", label: "Audit Log", icon: FileText },
        ],
    },
    {
        label: "Settings",
        items: [
            { to: "/settings/cloud-providers", label: "Cloud Providers", icon: Cloud },
            {
                to: "/settings/provisioning-templates",
                label: "Provisioning Templates",
                icon: ServerCog,
            },
            { to: "/settings/infrastructure", label: "Infrastructure", icon: HardDrive },
        ],
    },
];

export function Sidebar() {
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
                {GROUPS.map((group) => (
                    <div key={group.label}>
                        <div className="px-2 mb-1.5 text-[10px] font-semibold uppercase tracking-wider text-muted-foreground">
                            {group.label}
                        </div>
                        <ul className="space-y-0.5">
                            {group.items.map((item) => (
                                <li key={item.to}>
                                    <SidebarLink {...item} />
                                </li>
                            ))}
                        </ul>
                    </div>
                ))}
            </nav>

            <div className="px-4 py-3 border-t border-sidebar-border flex items-center gap-2 text-[11px] text-muted-foreground">
                <Activity className="size-3" />
                <span>Admin surface · do not share</span>
            </div>
        </aside>
    );
}

function SidebarLink({ to, label, icon: Icon, end }: NavItem) {
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
        </NavLink>
    );
}

// Standalone icon export so other surfaces (mobile drawer, breadcrumbs)
// can reuse the same icon-per-route mapping if needed later.
export const NAV_GROUPS = GROUPS;
export type { NavItem };
// Re-export the inbox icon so the "Open dashboard" link in UserMenu can
// use a consistent visual without redeclaring the import elsewhere.
export const InboxIcon = Inbox;
export const CogIcon = Cog;
