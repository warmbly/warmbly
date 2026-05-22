// Top toolbar inside the content column.
//
// Slim 40px strip. Left side: breadcrumb (section > subpage). Right side:
// connection indicator + search button. No logo here (it's in the sidebar
// header), no org switcher (also in the sidebar). One hairline bottom
// border separates it from the page content.

import { useLocation } from "react-router-dom";
import { ChevronRight, Search } from "lucide-react";
import { useAppStore } from "@/stores";
import { ConnectionIndicator } from "@/components/shared/ConnectionIndicator";

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

    const segments = pathname
        .split("/")
        .filter(Boolean)
        .filter((s) => s !== "app");
    const crumbs = segments.filter(
        (s) => !/^[0-9a-f]{8}-[0-9a-f]{4}/i.test(s),
    );

    return (
        <div className="h-10 flex items-center px-4 shrink-0 border-b border-slate-200 gap-1.5">
            {crumbs.length === 0 ? (
                <span className="text-[12.5px] font-medium text-slate-900">Home</span>
            ) : (
                crumbs.map((seg, i) => (
                    <div key={i} className="flex items-center gap-1.5 min-w-0">
                        {i > 0 && <ChevronRight className="w-3 h-3 text-slate-300 shrink-0" />}
                        <span
                            className={
                                i === crumbs.length - 1
                                    ? "text-[12.5px] font-medium text-slate-900 truncate"
                                    : "text-[12.5px] text-slate-500 truncate"
                            }
                        >
                            {pretty(seg)}
                        </span>
                    </div>
                ))
            )}

            <div className="ml-auto flex items-center gap-1 shrink-0">
                <ConnectionIndicator />
                <button
                    onClick={() => setCommandPaletteOpen(true)}
                    className="flex items-center gap-1.5 px-1.5 h-6 rounded text-slate-500 hover:text-slate-900 hover:bg-slate-100 transition-colors text-[12px]"
                >
                    <Search className="w-3 h-3" />
                    <span className="hidden sm:inline">Search</span>
                    <kbd className="hidden md:inline-flex h-4 items-center px-1 rounded border border-slate-200 bg-slate-50 font-mono text-[10px] text-slate-500 ml-0.5">
                        ⌘K
                    </kbd>
                </button>
            </div>
        </div>
    );
}
