// Linear-style sidebar.
//
// White background, 220px wide, a single hairline right border. No promo
// pills, no shadows, no section eyebrows in caps — just a tight stack of
// rows. Active row = bg-slate-100, slate-900 text. Inactive = slate-600.
// Section labels are sentence-case small text, not uppercase tracking.
//
// Rows are 28px tall with 13px text. Icons are 14px. The whole thing reads
// as a quiet outline of the app, not a sales surface.

import { Link, useLocation } from "react-router-dom";
import {
    BarChart3Icon,
    CheckSquareIcon,
    CircleDollarSignIcon,
    FileTextIcon,
    GitBranchIcon,
    InboxIcon,
    KeyIcon,
    type LucideIcon,
    MailIcon,
    MegaphoneIcon,
    SettingsIcon,
    UsersIcon,
} from "lucide-react";
import { useAppStore } from "@/stores";
import { Logo } from "@/components/svg";
import { OrgSwitcher } from "./OrgSwitcher";
import { UserNav } from "./UserNav";
import { cn } from "@/lib/utils";

interface NavItem {
    title: string;
    url: string;
    icon: LucideIcon;
    badgeStoreKey?: "unseenCount";
}

interface NavSection {
    label?: string;
    items: NavItem[];
}

const sections: NavSection[] = [
    {
        items: [
            { title: "Inbox", url: "/app/unibox", icon: InboxIcon, badgeStoreKey: "unseenCount" },
        ],
    },
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
                "group mx-1.5 flex items-center gap-2 px-2 h-7 rounded text-[13px] transition-colors duration-75",
                active
                    ? "bg-slate-100 text-slate-900 font-medium"
                    : "text-slate-600 hover:text-slate-900 hover:bg-slate-50",
            )}
        >
            <item.icon
                className={cn(
                    "w-[14px] h-[14px] shrink-0",
                    active ? "text-slate-700" : "text-slate-500 group-hover:text-slate-700",
                )}
                strokeWidth={1.75}
            />
            <span className="truncate">{item.title}</span>
            {badge != null && badge > 0 && (
                <span className="ml-auto text-[10.5px] font-medium text-slate-500 tabular-nums">
                    {badge > 99 ? "99+" : badge}
                </span>
            )}
        </Link>
    );
}

function Section({ section }: { section: NavSection }) {
    return (
        <div className="mt-4">
            {section.label && (
                <div className="px-3 mb-1">
                    <span className="text-[11px] text-slate-400 font-medium">
                        {section.label}
                    </span>
                </div>
            )}
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
        <aside className="w-[220px] shrink-0 flex flex-col bg-white border-r border-slate-200">
            {/* Top — logo + org switcher. One slim row, no flourish. */}
            <div className="h-11 flex items-center px-3 shrink-0 border-b border-slate-200">
                <Link to="/app/emails" className="flex items-center gap-2 group min-w-0 flex-1">
                    <Logo className="w-5 text-slate-700 group-hover:text-slate-900 transition-colors shrink-0" />
                    <span
                        style={{ fontFamily: "var(--font-display)" }}
                        className="font-semibold text-[13px] tracking-tight text-slate-900 truncate"
                    >
                        Warmbly
                    </span>
                </Link>
            </div>

            <div className="px-1.5 pt-2 pb-1 shrink-0">
                <OrgSwitcher />
            </div>

            <nav className="flex-1 overflow-y-auto py-1">
                {sections.map((s, i) => (
                    <Section key={s.label ?? i} section={s} />
                ))}
            </nav>

            <div className="shrink-0 border-t border-slate-200 py-1">
                <NavRow
                    item={{ title: "Settings", url: "/app/settings", icon: SettingsIcon }}
                />
            </div>

            <div className="shrink-0 border-t border-slate-200">
                <UserNav />
            </div>
        </aside>
    );
}
