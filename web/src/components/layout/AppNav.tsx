// Sidebar contents for the new sky-chrome shell.
//
// The chrome is dark sky, so everything in here is tuned for a dark
// background: text is white-ish, dividers are faint white, active rows
// have a translucent white pill rather than a solid grey one.
//
// Structure matches brae's "document outline" pattern — small-caps
// section labels, hairline gap between rows, primary action at the
// top. Animated icons skipped here on purpose; the auth page is where
// motion lives, the dashboard sidebar should be calm.

import { Link, useLocation } from "react-router-dom";
import {
    BarChart3Icon,
    CheckSquareIcon,
    CircleDollarSignIcon,
    FileTextIcon,
    GitBranchIcon,
    HomeIcon,
    InboxIcon,
    KeyIcon,
    type LucideIcon,
    MailIcon,
    MegaphoneIcon,
    PlusIcon,
    SettingsIcon,
    UsersIcon,
} from "lucide-react";
import { useAppStore } from "@/stores";
import { UserNav } from "./UserNav";
import { cn } from "@/lib/utils";

interface NavItem {
    title: string;
    url: string;
    icon: LucideIcon;
    badgeStoreKey?: "unseenCount";
}

interface NavSection {
    label: string;
    items: NavItem[];
}

const topItems: NavItem[] = [
    { title: "Home", url: "/app/emails", icon: HomeIcon },
    { title: "Inbox", url: "/app/unibox", icon: InboxIcon, badgeStoreKey: "unseenCount" },
];

const sections: NavSection[] = [
    {
        label: "Email",
        items: [
            { title: "Accounts", url: "/app/emails", icon: MailIcon },
            { title: "Campaigns", url: "/app/campaigns", icon: MegaphoneIcon },
            { title: "Contacts", url: "/app/contacts", icon: UsersIcon },
            { title: "Analytics", url: "/app/analytics", icon: BarChart3Icon },
        ],
    },
    {
        label: "CRM",
        items: [
            { title: "Pipelines", url: "/app/crm/pipelines", icon: GitBranchIcon },
            { title: "Deals", url: "/app/crm/deals", icon: CircleDollarSignIcon },
            { title: "Tasks", url: "/app/crm/tasks", icon: CheckSquareIcon },
        ],
    },
    {
        label: "Resources",
        items: [
            { title: "Templates", url: "/app/templates", icon: FileTextIcon },
            { title: "API Keys", url: "/app/api-keys", icon: KeyIcon },
        ],
    },
];

function NavRow({ item }: { item: NavItem }) {
    const { pathname } = useLocation();
    const unseen = useAppStore((s) => s.unseenCount);
    const active =
        pathname === item.url || pathname.startsWith(item.url + "/");
    const badge = item.badgeStoreKey === "unseenCount" ? unseen : undefined;

    return (
        <Link
            to={item.url}
            className={cn(
                "group relative mx-2 flex items-center gap-2.5 pl-3 pr-2.5 h-8 rounded-md text-[13px] transition-colors duration-150",
                active
                    ? "bg-sky-50 text-sky-900 font-medium"
                    : "text-slate-600 hover:text-slate-900 hover:bg-white/70",
            )}
        >
            {/* Active rail — 2.5px sky bar at the left edge. Subtler than
                a full pill, more confident than a coloured underline. */}
            {active && (
                <span className="absolute left-0 top-1 bottom-1 w-[2.5px] rounded-full bg-sky-500" />
            )}
            <item.icon
                className={cn(
                    "w-[15px] h-[15px] shrink-0 transition-colors",
                    active ? "text-sky-600" : "text-slate-400 group-hover:text-slate-600",
                )}
                strokeWidth={active ? 2 : 1.75}
            />
            <span className="truncate">{item.title}</span>
            {badge != null && badge > 0 && (
                <span className="ml-auto text-[10.5px] font-medium bg-sky-600 text-white rounded-full min-w-[18px] h-[18px] flex items-center justify-center px-1.5">
                    {badge > 99 ? "99+" : badge}
                </span>
            )}
        </Link>
    );
}

function Section({ section }: { section: NavSection }) {
    return (
        <div className="mt-5">
            <div className="px-5 mb-1">
                <span className="text-[10.5px] uppercase tracking-[0.16em] text-slate-400 font-semibold">
                    {section.label}
                </span>
            </div>
            <div className="space-y-px">
                {section.items.map((it) => (
                    <NavRow key={it.url} item={it} />
                ))}
            </div>
        </div>
    );
}

export function AppNav() {
    return (
        <aside className="w-64 shrink-0 flex flex-col text-slate-900">
            {/* Primary action — "New Campaign". A confident sky pill that
                stands out against the off-white sidebar without shouting. */}
            <div className="px-3 pt-1 pb-3 shrink-0">
                <Link
                    to="/app/campaigns"
                    className="flex items-center justify-center gap-2 h-9 rounded-lg bg-sky-600 hover:bg-sky-700 text-white text-[13px] font-medium transition-colors shadow-[0_1px_2px_rgba(2,132,199,0.35)]"
                >
                    <PlusIcon className="w-3.5 h-3.5" />
                    <span>New Campaign</span>
                </Link>
            </div>

            <nav className="flex-1 overflow-y-auto pb-3">
                <div className="space-y-px">
                    {topItems.map((it) => (
                        <NavRow key={it.url + it.title} item={it} />
                    ))}
                </div>
                {sections.map((s) => (
                    <Section key={s.label} section={s} />
                ))}
            </nav>

            <div className="border-t border-slate-200/60 py-2 shrink-0">
                <NavRow
                    item={{ title: "Settings", url: "/app/settings", icon: SettingsIcon }}
                />
            </div>

            <div className="border-t border-slate-200/60 shrink-0">
                <UserNav />
            </div>
        </aside>
    );
}
