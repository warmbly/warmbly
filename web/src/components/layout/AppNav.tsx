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
                "relative mx-2 flex items-center gap-2.5 px-2.5 h-8 rounded-md text-[12.5px] transition-colors duration-100",
                active
                    ? "bg-white/[0.12] text-white font-medium"
                    : "text-white/65 hover:text-white hover:bg-white/[0.06]",
            )}
        >
            <item.icon
                className={cn(
                    "w-[15px] h-[15px] shrink-0 transition-colors",
                    active ? "text-white" : "text-white/55",
                )}
                strokeWidth={active ? 2 : 1.75}
            />
            <span className="truncate">{item.title}</span>
            {badge != null && badge > 0 && (
                <span className="ml-auto text-[10.5px] font-medium bg-white text-sky-700 rounded-full min-w-[18px] h-[18px] flex items-center justify-center px-1.5">
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
                <span className="text-[10px] uppercase tracking-[0.16em] text-white/40 font-medium">
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
        <aside className="w-64 shrink-0 flex flex-col text-white/90">
            {/* Primary action — "New Campaign". Sits up top, separate from the
                navigation list so it reads as a verb, not a destination. */}
            <div className="px-3 pt-1 pb-2 shrink-0">
                <Link
                    to="/app/campaigns"
                    className="flex items-center justify-center gap-2 h-8 rounded-md bg-white/[0.10] hover:bg-white/[0.15] border border-white/15 text-white text-[12.5px] font-medium transition-colors backdrop-blur"
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

            {/* Settings row pinned just above the user menu, faintly separated. */}
            <div className="border-t border-white/10 py-2 shrink-0">
                <NavRow
                    item={{ title: "Settings", url: "/app/settings", icon: SettingsIcon }}
                />
            </div>

            <div className="border-t border-white/10 shrink-0">
                <UserNav />
            </div>
        </aside>
    );
}
