// Warmup Content section shell. Each tab is now its own route — this layout
// renders the shared page header plus a sub-nav tab bar (NavLink, so the
// active route highlights) and an <Outlet/> for the active tab's full-width
// page body.
//
//   /warmup-content/overview   — headline counts, AI/schedule status, A/B
//   /warmup-content/library    — generated-thread library + actions
//   /warmup-content/jobs       — generation jobs, polled live

import { NavLink, Outlet } from "react-router-dom";
import { Inbox, Layers, Play, type LucideIcon } from "lucide-react";
import { PageHeader } from "@/components/layout/PageHeader";
import { cn } from "@/lib/utils";

interface SubTab {
    to: string;
    label: string;
    icon: LucideIcon;
}

const TABS: SubTab[] = [
    { to: "/warmup-content/overview", label: "Overview", icon: Layers },
    { to: "/warmup-content/library", label: "Library", icon: Inbox },
    { to: "/warmup-content/jobs", label: "Jobs", icon: Play },
];

export default function WarmupContentLayout() {
    return (
        <div className="w-full">
            <PageHeader
                title="Warmup Content"
                description="Observe the autonomous warmup-content controller, review its generated library, and inspect generation history. Content volume and refresh are managed from live warmup demand."
            />

            <div className="mb-6 border-b border-border">
                <nav className="-mb-px flex flex-wrap items-center gap-1">
                    {TABS.map(({ to, label, icon: Icon }) => (
                        <NavLink
                            key={to}
                            to={to}
                            className={({ isActive }) =>
                                cn(
                                    "relative inline-flex items-center gap-1.5 rounded-t-md border-b-2 px-3 py-2 text-sm font-medium transition-colors",
                                    isActive
                                        ? "border-[var(--admin-accent)] text-[var(--admin-accent-strong)]"
                                        : "border-transparent text-foreground/60 hover:text-foreground",
                                )
                            }
                        >
                            <Icon className="size-4" />
                            {label}
                        </NavLink>
                    ))}
                </nav>
            </div>

            <Outlet />
        </div>
    );
}
