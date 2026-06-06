// Sidebar for the sky-chrome shell.
//
// Brae-density structure: small tracked-uppercase section labels, h-8
// nav rows, hairline dividers between sections. The header slot is
// the LivePanel — a small ambient telemetry card that replaces the
// generic "+ New Campaign" pill. Cold-email work is always-on; the
// sidebar should reflect that rather than nag with a CTA.

import { Link, useLocation } from "react-router-dom";
import {
    BarChart3Icon,
    CableIcon,
    CheckSquareIcon,
    CircleDollarSignIcon,
    FileTextIcon,
    FlameIcon,
    GitBranchIcon,
    InboxIcon,
    KeyIcon,
    ListChecksIcon,
    type LucideIcon,
    MailIcon,
    MegaphoneIcon,
    SettingsIcon,
    UsersIcon,
    XIcon,
} from "lucide-react";
import { useMemo } from "react";
import { useAppStore } from "@/stores";
import useFeatureAccess from "@/hooks/useFeatureAccess";
import useCampaigns from "@/lib/api/hooks/app/campaigns/useCampaigns";
import useEmails from "@/lib/api/hooks/app/emails/useEmails";
import { UserNav } from "./UserNav";
import { Logo } from "@/components/svg";
import { cn } from "@/lib/utils";

interface NavItem {
    title: string;
    url: string;
    icon: LucideIcon;
    badgeStoreKey?: "unseenCount";
    /** Feature gate key — when set, sidebar dims the row and shows a plan badge. */
    requires?: "inbox" | "advanced";
    /** Role gate — when set, sidebar hides the row entirely for non-matching roles. */
    rolesAllowed?: "manage";
    /** Live indicator key — renders an ambient, realtime activity cluster.
     *  Each key has its OWN motif (campaigns = dot-grid, accounts = flame) so
     *  the rows stay visually distinct rather than a column of identical loaders. */
    indicator?: "campaigns" | "accounts";
}

// Plan badge shown on locked sidebar rows. Plan names + colors come
// from lib/plans so the marketing site, header pill and sidebar
// badge all agree on "what does Starter look like".
//
//   inbox    → Starter (any paid tier)
//   advanced → Business (15k/day + dedicated IPs tier)
const REQUIRES_TO_BADGE: Record<NonNullable<NavItem["requires"]>, { label: string; classes: string }> = {
    inbox:    { label: "Starter",  classes: "bg-emerald-50 text-emerald-700 border-emerald-100" },
    advanced: { label: "Business", classes: "bg-indigo-50 text-indigo-700 border-indigo-100" },
};

interface NavSection {
    label: string;
    items: NavItem[];
}

const topItems: NavItem[] = [
    {
        title: "Inbox",
        url: "/app/unibox",
        icon: InboxIcon,
        badgeStoreKey: "unseenCount",
        requires: "inbox",
    },
];

const sections: NavSection[] = [
    {
        label: "Email",
        items: [
            { title: "Accounts", url: "/app/emails", icon: MailIcon, indicator: "accounts" },
            { title: "Campaigns", url: "/app/campaigns", icon: MegaphoneIcon, indicator: "campaigns" },
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
            { title: "Integrations", url: "/app/integrations", icon: CableIcon },
            { title: "API Keys", url: "/app/api-keys", icon: KeyIcon },
            { title: "Audit log", url: "/app/audit", icon: ListChecksIcon, rolesAllowed: "manage" },
        ],
    },
];

function NavRow({ item }: { item: NavItem }) {
    const { pathname } = useLocation();
    const unseen = useAppStore((s) => s.unseenCount);
    const access = useFeatureAccess();
    const active =
        pathname === item.url || pathname.startsWith(item.url + "/");
    const badge = item.badgeStoreKey === "unseenCount" ? unseen : undefined;

    // Role-gated items disappear from the sidebar for users that
    // can't access them, instead of showing a lock — these are
    // administrative tools, not premium features to tease.
    if (item.rolesAllowed === "manage" && !access.canManage) return null;

    // Subscription-gated items stay visible but dim with a lock so
    // the user knows the feature exists.
    const locked =
        (item.requires === "inbox" && !access.hasInbox) ||
        (item.requires === "advanced" && !access.hasAdvanced);

    const planBadge = locked && item.requires ? REQUIRES_TO_BADGE[item.requires] : null;

    return (
        <Link
            to={item.url}
            title={planBadge ? `${item.title} · ${planBadge.label} plan` : undefined}
            className={cn(
                "group mx-2 flex items-center gap-2.5 px-2.5 h-7 rounded-md text-[12.5px] transition-colors duration-100",
                active
                    ? "bg-slate-200/70 text-slate-900 font-medium"
                    : locked
                        ? "text-slate-400 hover:text-slate-700 hover:bg-slate-200/40"
                        : "text-slate-600 hover:text-slate-900 hover:bg-slate-200/40",
            )}
        >
            <item.icon
                className={cn(
                    "w-[14px] h-[14px] shrink-0 transition-colors",
                    active
                        ? "text-slate-700"
                        : locked
                            ? "text-slate-300 group-hover:text-slate-500"
                            : "text-slate-400 group-hover:text-slate-600",
                )}
                strokeWidth={active ? 2 : 1.6}
            />
            <span className="truncate flex-1">{item.title}</span>
            {item.indicator === "campaigns" && !locked && <CampaignActivity />}
            {item.indicator === "accounts" && !locked && <MailboxActivity />}
            {planBadge ? (
                <span
                    className={cn(
                        "h-4 px-1.5 rounded text-[9.5px] font-semibold uppercase tracking-[0.06em] border inline-flex items-center",
                        planBadge.classes,
                    )}
                >
                    {planBadge.label}
                </span>
            ) : (
                badge != null && badge > 0 && (
                    <span className="text-[10px] font-medium bg-red-500 text-white rounded-full min-w-[16px] h-4 flex items-center justify-center px-1 tabular-nums">
                        {badge > 99 ? "99+" : badge}
                    </span>
                )
            )}
        </Link>
    );
}

// CampaignActivity is the ambient, realtime indicator on the Campaigns nav
// row: a small 3x3 dot-grid loader + a count of campaigns sending right now,
// and nothing when none are sending. The count comes from the shared
// campaigns-list cache, which the realtime layer invalidates on campaign
// events, so it stays live without a refresh. Draft / paused / completed are
// review states surfaced inside the Campaigns page, not here.
function CampaignActivity() {
    const { campaigns } = useCampaigns({ query: "", folder: "" });
    const active = useMemo(
        () => campaigns.filter((c) => c.status === "active").length,
        [campaigns],
    );

    if (active === 0) return null;

    return (
        <span
            className="ml-auto inline-flex items-center gap-1.5 shrink-0 text-sky-600"
            title={`${active} campaign${active === 1 ? "" : "s"} sending now`}
        >
            <span className="campaign-grid" aria-hidden />
            <span className="text-[10.5px] font-semibold tabular-nums leading-none">
                {active}
            </span>
        </span>
    );
}

// MailboxActivity is the Accounts-row indicator — deliberately a DIFFERENT
// motif than the campaigns dot-grid: a flickering flame + count of mailboxes
// warming up right now (warmup enabled and not paused). Hidden when none are
// warming. Counts come from the shared emails-list cache, which the realtime
// layer invalidates on account/warmup events, so it stays live.
function MailboxActivity() {
    const { emails } = useEmails({ query: "", tag: "" });
    const warming = useMemo(
        () => emails.filter((e) => !!e.warmup && !e.warmup_paused_at).length,
        [emails],
    );

    if (warming === 0) return null;

    return (
        <span
            className="ml-auto inline-flex items-center gap-1.5 shrink-0 text-orange-500"
            title={`${warming} mailbox${warming === 1 ? "" : "es"} warming up`}
        >
            <FlameIcon className="w-3.5 h-3.5 flame-flicker" strokeWidth={2.2} />
            <span className="text-[10.5px] font-semibold tabular-nums leading-none text-orange-600">
                {warming}
            </span>
        </span>
    );
}

function Section({ section, first = false }: { section: NavSection; first?: boolean }) {
    return (
        <div className={first ? "" : "mt-4 pt-4 border-t border-slate-200/50"}>
            <div className="px-4 mb-1.5">
                <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">
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

/**
 * LivePanel — replaces the old "+ New Campaign" pill.
 *
 * Anatomy:
 *
 *   ┌──────────────────────────────────┐
 *   │  ●  LIVE         42 of 50 / day  │   ← status dot + label + cap pace
 *   │  8 mailboxes      sending now    │   ← mailbox composition
 *   │  ▂▃▅▇▆▄▂  ▂▃▅▇▆▄▂                │   ← optional 24h sparkline
 *   └──────────────────────────────────┘
 *
 * Reads as ambient telemetry: even when idle, it tells you "system is
 * up, n mailboxes ready." Clicking jumps to analytics. The dot pulses
 * when at least one mailbox is actively warming or sending.
 *
 * Data sources at this layer:
 *   - useAppStore.emails  → mailbox count, active count
 *   - useAppStore.connectionStatus → online/offline state
 *
 * Volume numbers ("42 of 50") will be wired up to a future today-summary
 * endpoint; for now they fall back to a derived cap based on mailbox
 * count × 50 (default cold cap from internal/config/constants.go).
 */
function LivePanel() {
    const emails = useAppStore((s) => s.emails);
    const connection = useAppStore((s) => s.connectionStatus);
    const latencyMs = useAppStore((s) => s.wsLatencyMs);
    const unseenCount = useAppStore((s) => s.unseenCount);

    const { active, mailboxes, capacity } = useMemo(() => {
        const m = emails.length;
        const a = emails.filter(
            (e) => e.status === "healthy" || e.status === "warming",
        ).length;
        return { active: a, mailboxes: m, capacity: m * 50 };
    }, [emails]);

    const live = connection === "connected";
    const label =
        connection === "disconnected"
            ? "OFFLINE"
            : connection === "connecting"
                ? "CONNECTING"
                : active > 0
                    ? "LIVE"
                    : "IDLE";

    // Latency bucketing: <100ms great, <300ms okay, ≥300ms poor.
    const latencyTone =
        latencyMs == null
            ? "text-slate-400"
            : latencyMs < 100
                ? "text-emerald-600"
                : latencyMs < 300
                    ? "text-amber-600"
                    : "text-red-500";

    return (
        <Link
            to="/app/analytics"
            className="group block mx-2 mt-2 mb-3 rounded-md bg-white/80 hover:bg-white border border-slate-200/70 hover:border-slate-300 px-2.5 py-2 transition-colors"
        >
            <div className="flex items-center gap-1.5">
                <span className="relative inline-flex shrink-0">
                    <span
                        className={cn(
                            "w-1.5 h-1.5 rounded-full",
                            connection === "disconnected"
                                ? "bg-slate-400"
                                : connection === "connecting"
                                    ? "bg-amber-500"
                                    : active > 0
                                        ? "bg-emerald-500"
                                        : "bg-slate-400",
                        )}
                    />
                    {live && active > 0 && (
                        <span className="absolute inset-0 rounded-full bg-emerald-500/40 animate-ping" />
                    )}
                </span>
                <span className="text-[10px] uppercase tracking-[0.14em] text-slate-500 font-semibold">
                    {label}
                </span>
                <span
                    className={cn(
                        "ml-auto font-mono text-[10px] tabular-nums",
                        latencyTone,
                    )}
                    title={latencyMs != null ? `Websocket roundtrip` : "Not connected"}
                >
                    {latencyMs != null ? `${latencyMs}ms` : "—"}
                </span>
            </div>

            <div className="mt-1.5 flex items-baseline gap-1.5">
                <span className="text-[15px] text-slate-900 tabular-nums leading-none">
                    {mailboxes}
                </span>
                <span className="text-[11px] text-slate-500">
                    {mailboxes === 1 ? "mailbox" : "mailboxes"}
                </span>
                {active > 0 && (
                    <span className="ml-auto text-[10.5px] text-emerald-600 tabular-nums">
                        {active} active
                    </span>
                )}
            </div>

            <div className="mt-1.5 flex items-center justify-between gap-2 text-[10.5px]">
                <span className="text-slate-400">Inbox</span>
                <span
                    className={cn(
                        "font-mono tabular-nums",
                        unseenCount > 0 ? "text-sky-600" : "text-slate-400",
                    )}
                >
                    {unseenCount > 99 ? "99+" : unseenCount} unread
                </span>
            </div>

            <div className="mt-1 flex items-center justify-between gap-2 text-[10.5px]">
                <span className="text-slate-400">Today</span>
                <span className="font-mono text-slate-400 tabular-nums">
                    0/{capacity || "—"}
                </span>
            </div>

            <Sparkline />
        </Link>
    );
}

/**
 * Sparkline — 14 thin vertical bars, last hours of today's volume.
 * Placeholder data for now (zeros render as faint bars). When a
 * /summary endpoint lands, swap the array.
 */
function Sparkline() {
    const bars = useMemo(() => Array.from({ length: 14 }, () => 0), []);
    return (
        <div className="mt-2 flex items-end gap-0.5 h-4">
            {bars.map((v, i) => (
                <div
                    key={i}
                    className="flex-1 rounded-sm bg-slate-200 group-hover:bg-slate-300 transition-colors"
                    style={{ height: `${Math.max(8, v)}%`, minHeight: "2px" }}
                />
            ))}
        </div>
    );
}

export function AppNav({ open = false, onClose }: { open?: boolean; onClose?: () => void }) {
    return (
        <>
            {/* Mobile-only scrim. Tapping it closes the drawer. */}
            <div
                aria-hidden
                onClick={onClose}
                className={cn(
                    "fixed inset-0 z-40 bg-slate-900/40 transition-opacity duration-300 md:hidden",
                    open ? "opacity-100" : "pointer-events-none opacity-0",
                )}
            />

            <aside
                className={cn(
                    // Mobile: off-canvas drawer that slides in from the left.
                    "fixed inset-y-0 left-0 z-50 w-64 flex flex-col text-slate-900 bg-white shadow-2xl transition-transform duration-300 ease-out",
                    open ? "translate-x-0" : "-translate-x-full",
                    // >=md: static sidebar column over the chrome, no transform/shadow.
                    "md:static md:z-auto md:translate-x-0 md:bg-transparent md:shadow-none md:transition-none shrink-0",
                )}
            >
                {/* Mobile drawer header: brand + close. (The desktop sidebar
                    has no chrome of its own — the brand lives in AppHeader.) */}
                <div className="md:hidden flex items-center justify-between px-3 h-14 border-b border-slate-200/70">
                    <Link to="/app/emails" onClick={onClose} className="flex items-center gap-2.5">
                        <Logo className="w-6 text-slate-900" />
                        <span
                            style={{ fontFamily: "var(--font-display)" }}
                            className="font-extrabold text-[15px] tracking-tight text-slate-900"
                        >
                            Warmbly
                        </span>
                    </Link>
                    <button
                        type="button"
                        onClick={onClose}
                        aria-label="Close menu"
                        className="w-8 h-8 -mr-1 rounded-md flex items-center justify-center text-slate-500 hover:text-slate-900 hover:bg-slate-100 transition-colors"
                    >
                        <XIcon className="w-4 h-4" />
                    </button>
                </div>

            <LivePanel />

            <nav className="flex-1 overflow-y-auto pb-3">
                <div className="space-y-px">
                    {topItems.map((it) => (
                        <NavRow key={it.url + it.title} item={it} />
                    ))}
                </div>
                {sections.map((s, i) => (
                    <Section key={s.label} section={s} first={i === 0 && topItems.length === 0} />
                ))}
            </nav>

            <div className="border-t border-slate-200/60 py-1 shrink-0">
                <NavRow
                    item={{ title: "Settings", url: "/app/settings", icon: SettingsIcon }}
                />
            </div>

            <div className="border-t border-slate-200/60 shrink-0">
                <UserNav />
            </div>
            </aside>
        </>
    );
}
