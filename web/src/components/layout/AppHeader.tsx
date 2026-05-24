// Top breadcrumb bar.
//
// Reads as one continuous line across the entire top of the shell:
//
//   [Warmbly logo]  >  [Org picker]  >  [Current section]      [⌘K  ⚡]
//
// The logo sits over the sidebar column, the org picker + section live
// in the open area, the right side has connection indicator + search.
// All on the sky-colored chrome — text is white-ish, dividers are faint.
//
// This component is purely the row. Layout (where it sits) is decided by
// AppShell, not here.

import { Link, useLocation } from "react-router-dom";
import { ChevronRight, Search } from "lucide-react";
import { Logo } from "@/components/svg";
import { useAppStore } from "@/stores";
import { ConnectionIndicator } from "@/components/shared/ConnectionIndicator";
import { OrgSwitcher } from "./OrgSwitcher";
import { PlanPill } from "./PlanPill";

// Pretty labels for path segments. Anything missing falls back to the
// raw segment with its first letter capitalised.
const labelMap: Record<string, string> = {
    app: "Home",
    emails: "Accounts",
    unibox: "Inbox",
    contacts: "Contacts",
    campaigns: "Campaigns",
    analytics: "Analytics",
    crm: "CRM",
    pipelines: "Pipelines",
    deals: "Deals",
    tasks: "Tasks",
    templates: "Templates",
    "api-keys": "API Keys",
    settings: "Settings",
    billing: "Billing",
    team: "Team",
    admin: "Admin",
    workers: "Workers",
    credentials: "Credentials",
    audit: "Audit",
    leads: "Leads",
    preferences: "Preferences",
    schedule: "Schedule",
    sequences: "Sequences",
};

function pretty(segment: string): string {
    return labelMap[segment] ?? segment.charAt(0).toUpperCase() + segment.slice(1);
}

export function AppHeader() {
    const { pathname } = useLocation();
    const setCommandPaletteOpen = useAppStore((s) => s.setCommandPaletteOpen);

    // Path under /app — first segment is the section ("emails", "admin", ...),
    // subsequent ones are subpages. Don't show UUID-looking segments verbatim
    // because nobody wants "Campaigns > 47a3-..." in their chrome.
    const segments = pathname
        .split("/")
        .filter(Boolean)
        .filter((s) => s !== "app");
    const crumbs = segments.filter(
        (s) => !/^[0-9a-f]{8}-[0-9a-f]{4}/i.test(s),
    );

    return (
        <div className="h-14 flex items-center shrink-0">
            {/* Sidebar-width logo zone — Warmbly wordmark. */}
            <Link
                to="/app/emails"
                className="w-64 px-5 h-full flex items-center gap-2.5 shrink-0 group"
            >
                {/* Cool blue-leaning gray at rest; deeper blue-gray on hover.
                    Light enough to read as neutral chrome, but with a clear
                    blue lean so the brand sneaks in. */}
                {/* Logo color tuned to read as a real brand mark, not
                    a washed-out accent. Deep slate (#0f172a) at rest +
                    slight warm shift on hover. The earlier blue-gray
                    was too pale and competed with the chrome rather
                    than anchoring it. */}
                <Logo className="w-7 text-slate-900 group-hover:text-slate-700 transition-colors duration-150" />
                <span
                    style={{ fontFamily: "var(--font-display)" }}
                    className="font-extrabold text-[15.5px] tracking-tight text-slate-900"
                >
                    Warmbly
                </span>
            </Link>

            {/* Breadcrumb: org > section > subpages. */}
            <div className="flex items-center gap-1.5 min-w-0 flex-1 pr-4">
                <Crumb>
                    <OrgSwitcher />
                </Crumb>
                {crumbs.map((seg, i) => (
                    <Crumb key={i}>
                        <ChevronRight className="w-3.5 h-3.5 text-slate-300 shrink-0" />
                        <span
                            className={
                                i === crumbs.length - 1
                                    ? "text-[13px] font-medium text-slate-900 truncate"
                                    : "text-[13px] text-slate-500 truncate"
                            }
                        >
                            {pretty(seg)}
                        </span>
                    </Crumb>
                ))}
            </div>

            <div className="flex items-center gap-2 px-4 shrink-0">
                <PlanPill />
                <div className="h-4 w-px bg-slate-200/80" />
                <ConnectionIndicator />
                <button
                    onClick={() => setCommandPaletteOpen(true)}
                    className="flex items-center gap-2 px-2 h-7 rounded-md text-slate-500 hover:text-slate-900 hover:bg-slate-200/60 transition-colors text-[12.5px]"
                >
                    <Search className="w-3.5 h-3.5" />
                    <span className="hidden sm:inline">Search</span>
                    <kbd className="hidden md:inline-flex h-4 items-center px-1 rounded border border-slate-300/70 bg-white/60 font-mono text-[10px] text-slate-500 ml-0.5">
                        ⌘K
                    </kbd>
                </button>
            </div>
        </div>
    );
}

function Crumb({ children }: { children: React.ReactNode }) {
    return <div className="flex items-center gap-2 min-w-0">{children}</div>;
}
