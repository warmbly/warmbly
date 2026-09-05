// Sidebar for the sky-chrome shell.
//
// Brae-density structure: small tracked-uppercase section labels, h-8
// nav rows, hairline dividers between sections. The header slot is
// the LivePanel — a small ambient telemetry card that replaces the
// generic "+ New Campaign" pill. Cold-email work is always-on; the
// sidebar should reflect that rather than nag with a CTA.

import { Link, useLocation } from "react-router-dom";
import {
    ClipboardListIcon,
    BarChart3Icon,
    CableIcon,
    CalendarClockIcon,
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
    ShieldCheckIcon,
    UsersIcon,
    LockIcon,
    XIcon,
    ZapIcon,
} from "lucide-react";
import { type ReactNode, useMemo, useState } from "react";
import { useAppStore } from "@/stores";
import useFeatureAccess from "@/hooks/useFeatureAccess";
import { usePermission, type PermissionKey } from "@/hooks/usePermission";
import { useUpgradeDialog } from "@/hooks/context/upgrade";
import { PLAN_ACCENT_CLASSES, getPlan, type PlanID } from "@/lib/plans";
import AccessLockedDialog from "./AccessLockedDialog";
import useCampaigns from "@/lib/api/hooks/app/campaigns/useCampaigns";
import useEmails from "@/lib/api/hooks/app/emails/useEmails";
import useTasksSummary from "@/lib/api/hooks/app/crm/tasks/useTasksSummary";
import useMeetingsSummary from "@/lib/api/hooks/app/meetings/useMeetingsSummary";
import useDealsSummary from "@/lib/api/hooks/app/crm/deals/useDealsSummary";
import { EMPTY_TASK_SEARCH } from "@/lib/api/models/app/crm/SearchTasks";
import { EMPTY_DEAL_SEARCH } from "@/lib/api/models/app/crm/SearchDeals";
import useSearchContacts from "@/lib/api/hooks/app/contacts/useSearchContacts";
import type SearchContacts from "@/lib/api/models/app/contacts/SearchContacts";
import usePipelines from "@/lib/api/hooks/app/crm/pipelines/usePipelines";
import useTemplates from "@/lib/api/hooks/app/templates/useTemplates";
import useUsageOverview from "@/lib/api/hooks/app/analytics/useUsageOverview";
import useDashboard from "@/lib/api/hooks/app/analytics/useDashboard";
import mailboxDisplayStatus from "@/lib/mailboxStatus";
import useAPIKeys from "@/lib/api/hooks/app/api-keys/useAPIKeys";
import useIntegrationConnections from "@/lib/api/hooks/app/integrations/useIntegrationConnections";
import AnimatedNumber from "@/components/ui/AnimatedNumber";
import AdvisorNavBadge from "@/components/app/advisor/AdvisorNavBadge";
import type { AdvisorSurface } from "@/lib/api/models/app/advisor/Advisor";
import { UserNav } from "./UserNav";
import { Logo } from "@/components/svg";
import { cn } from "@/lib/utils";

// Stable (module-level) empty contacts search so the sidebar's contact-count
// query key never changes identity between renders (which would refetch-loop).
// limit 1 keeps the payload tiny — we only read pagination.total.
const CONTACTS_COUNT_SEARCH: SearchContacts = {
    query: "",
    filters: [],
    campaign_ids: [],
    sort_by: "created_at",
    reverse: false,
};

interface NavItem {
    title: string;
    url: string;
    icon: LucideIcon;
    badgeStoreKey?: "unseenCount";
    /** Feature gate key — when set, sidebar dims the row and shows a plan badge. */
    requires?: "inbox" | "advanced" | "subscription";
    /** Role gate — when set, sidebar hides the row entirely for non-matching roles. */
    rolesAllowed?: "manage";
    /** Permission gate — when the member lacks it, the row shows a lock and a
     *  click pops an access dialog instead of navigating to an empty page. */
    permission?: PermissionKey;
    /** Friendly label of the permission, for the access dialog. */
    permissionLabel?: string;
    /** Advisor surface — when set, the row badges the count of critical/high
     *  recommendations the Advisor has open for that area, so a problem is
     *  visible from the sidebar on the tab where its fix lives. */
    advisorSurface?: AdvisorSurface;
    /** Live indicator key — renders an ambient, realtime activity cluster.
     *  Each key has its OWN motif (campaigns = dot-grid, accounts = flame,
     *  tasks = red attention dot) so the rows stay visually distinct rather
     *  than a column of identical loaders. */
    indicator?:
        | "campaigns"
        | "accounts"
        | "tasks"
        | "contacts"
        | "deals"
        | "pipelines"
        | "meetings"
        | "templates"
        | "analytics"
        | "apikeys"
        | "integrations";
}

// Minimum plan behind each sidebar gate. The badge label and colour come
// from lib/plans so the marketing site, header pill, sidebar badge and the
// upgrade dialog all agree on "what does Starter look like".
//
//   inbox / subscription → Starter (any paid tier)
//   advanced             → Business (15k/day + isolated sending tier)
const REQUIRES_TO_MIN_PLAN: Record<NonNullable<NavItem["requires"]>, PlanID> = {
    inbox: "starter",
    subscription: "starter",
    advanced: "business",
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
        permission: "ACCESS_UNIBOX",
        permissionLabel: "Use unified inbox",
    },
];

const sections: NavSection[] = [
    {
        label: "Email",
        items: [
            { title: "Accounts", url: "/app/emails", icon: MailIcon, indicator: "accounts", advisorSurface: "emails", permission: "MANAGE_EMAILS", permissionLabel: "Manage mailboxes" },
            { title: "Campaigns", requires: "subscription", url: "/app/campaigns", icon: MegaphoneIcon, indicator: "campaigns", advisorSurface: "campaigns", permission: "VIEW_CAMPAIGNS", permissionLabel: "View campaigns" },
            { title: "Contacts", requires: "subscription", url: "/app/contacts", icon: UsersIcon, indicator: "contacts", advisorSurface: "contacts", permission: "VIEW_CONTACTS", permissionLabel: "View contacts" },
            { title: "Forms", requires: "subscription", url: "/app/forms", icon: ClipboardListIcon, permission: "VIEW_CONTACTS", permissionLabel: "View contacts" },
            { title: "Analytics", requires: "subscription", url: "/app/analytics", icon: BarChart3Icon, indicator: "analytics", permission: "VIEW_ANALYTICS", permissionLabel: "View analytics" },
            { title: "Deliverability", requires: "subscription", url: "/app/deliverability", icon: ShieldCheckIcon, advisorSurface: "deliverability", permission: "VIEW_ANALYTICS", permissionLabel: "View analytics" },
        ],
    },
    {
        label: "CRM",
        items: [
            { title: "Pipelines", requires: "subscription", url: "/app/crm/pipelines", icon: GitBranchIcon, indicator: "pipelines", permission: "VIEW_CONTACTS", permissionLabel: "View contacts" },
            { title: "Deals", requires: "subscription", url: "/app/crm/deals", icon: CircleDollarSignIcon, indicator: "deals", permission: "VIEW_CONTACTS", permissionLabel: "View contacts" },
            { title: "Tasks", requires: "subscription", url: "/app/crm/tasks", icon: CheckSquareIcon, indicator: "tasks", permission: "VIEW_CONTACTS", permissionLabel: "View contacts" },
            { title: "Meetings", requires: "subscription", url: "/app/crm/meetings", icon: CalendarClockIcon, indicator: "meetings", permission: "VIEW_CONTACTS", permissionLabel: "View contacts" },
        ],
    },
    {
        label: "Resources",
        items: [
            { title: "Templates", requires: "subscription", url: "/app/templates", icon: FileTextIcon, indicator: "templates" },
            { title: "Integrations", requires: "subscription", url: "/app/integrations", icon: CableIcon, indicator: "integrations", permission: "USE_INTEGRATIONS", permissionLabel: "Use integrations" },
            { title: "Automations", requires: "subscription", url: "/app/automations", icon: ZapIcon, permission: "USE_INTEGRATIONS", permissionLabel: "Use integrations" },
            { title: "API Keys", requires: "subscription", url: "/app/api-keys", icon: KeyIcon, indicator: "apikeys", permission: "MANAGE_API_KEYS", permissionLabel: "Manage API keys" },
            { title: "Audit log", requires: "subscription", url: "/app/audit", icon: ListChecksIcon, rolesAllowed: "manage" },
        ],
    },
];

function NavRow({ item }: { item: NavItem }) {
    const { pathname } = useLocation();
    const unseen = useAppStore((s) => s.unseenCount);
    const access = useFeatureAccess();
    const hasItemPermission = usePermission(item.permission ?? "VIEW_CAMPAIGNS");
    const [deniedOpen, setDeniedOpen] = useState(false);
    const upgradeDialog = useUpgradeDialog();
    const active =
        pathname === item.url || pathname.startsWith(item.url + "/");
    const badge = item.badgeStoreKey === "unseenCount" ? unseen : undefined;

    // Role-gated items disappear from the sidebar for users that
    // can't access them, instead of showing a lock — these are
    // administrative tools, not premium features to tease.
    if (item.rolesAllowed === "manage" && !access.canManage) return null;

    // Permission-gated items the member lacks: render a locked row that pops
    // an access dialog on click, so the feature is visibly unavailable (a
    // lock) rather than a blank/empty page that reads as "no data".
    const accessDenied = !!item.permission && !hasItemPermission;
    if (accessDenied) {
        return (
            <>
                <button
                    type="button"
                    onClick={() => setDeniedOpen(true)}
                    title={`${item.title} · no access`}
                    className="group w-[calc(100%-1rem)] mx-2 flex items-center gap-2.5 px-2.5 h-7 rounded-md text-[12.5px] text-slate-400 hover:text-slate-600 hover:bg-slate-200/40 transition-colors duration-100"
                >
                    <LockIcon className="w-[13px] h-[13px] shrink-0 text-slate-300 group-hover:text-slate-500" strokeWidth={1.8} />
                    <span className="truncate flex-1 min-w-0 text-left">{item.title}</span>
                </button>
                <AccessLockedDialog
                    open={deniedOpen}
                    onClose={() => setDeniedOpen(false)}
                    feature={item.title}
                    permissionLabel={item.permissionLabel ?? "the required"}
                />
            </>
        );
    }

    // Subscription-gated items stay visible but dim with a lock so
    // the user knows the feature exists.
    const locked =
        (item.requires === "inbox" && !access.hasInbox) ||
        (item.requires === "advanced" && !access.hasAdvanced) ||
        (item.requires === "subscription" && access.locked);

    const minPlan = locked && item.requires ? REQUIRES_TO_MIN_PLAN[item.requires] : null;
    const planBadge = minPlan
        ? { label: getPlan(minPlan).label, classes: PLAN_ACCENT_CLASSES[getPlan(minPlan).accent].pill }
        : null;

    // Plan-gated items the org's plan doesn't include: lock the row and open
    // the full-screen upgrade dialog on click (plans, interval, one-click
    // checkout), instead of routing to a teasing empty page.
    if (locked && minPlan && planBadge) {
        return (
            <button
                type="button"
                onClick={() => upgradeDialog.open({ feature: item.title, minPlan })}
                title={`${item.title} · ${planBadge.label} plan`}
                className="group w-[calc(100%-1rem)] mx-2 flex items-center gap-2.5 px-2.5 h-7 rounded-md text-[12.5px] text-slate-400 hover:text-slate-700 hover:bg-slate-200/40 transition-colors duration-100"
            >
                <LockIcon className="w-[13px] h-[13px] shrink-0 text-slate-300 group-hover:text-slate-500" strokeWidth={1.8} />
                <span className="truncate flex-1 min-w-0 text-left">{item.title}</span>
                <span
                    className={cn(
                        "h-4 px-1.5 rounded text-[9.5px] font-semibold uppercase tracking-[0.06em] border inline-flex items-center",
                        planBadge.classes,
                    )}
                >
                    {planBadge.label}
                </span>
            </button>
        );
    }

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
            {/* min-w-0 lets the label shrink/truncate so the count cluster (and its
                separator) is never pushed off the row — longer labels like
                "Campaigns"/"Accounts" used to clip it at narrower widths. */}
            <span className="truncate flex-1 min-w-0">{item.title}</span>
            {item.advisorSurface && !locked && <AdvisorNavBadge surface={item.advisorSurface} />}
            {item.indicator === "campaigns" && !locked && <CampaignActivity />}
            {item.indicator === "accounts" && !locked && <MailboxActivity />}
            {item.indicator === "tasks" && !locked && <TasksActivity />}
            {item.indicator === "meetings" && !locked && <MeetingsActivity />}
            {item.indicator === "contacts" && !locked && <ContactsActivity />}
            {item.indicator === "deals" && !locked && <DealsActivity />}
            {item.indicator === "pipelines" && !locked && <PipelinesActivity />}
            {item.indicator === "templates" && !locked && <TemplatesActivity />}
            {item.indicator === "analytics" && !locked && <AnalyticsActivity />}
            {item.indicator === "apikeys" && !locked && <ApiKeysActivity />}
            {item.indicator === "integrations" && !locked && <IntegrationsActivity />}
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

// compactN renders large counts tersely (12.3k, 1.2M) so a headline number like
// total emails sent fits a nav row.
function compactN(n: number): string {
    const v = Math.round(n);
    if (v >= 1_000_000) return `${(v / 1_000_000).toFixed(1)}M`;
    if (v >= 10_000) return `${Math.round(v / 1000)}k`;
    if (v >= 1_000) return `${(v / 1000).toFixed(1)}k`;
    return String(v);
}

// The "how many" total at the end of a nav row. Light slate so it reads as
// ambient metadata (lifting a touch on row hover), but visible, and it tweens
// (AnimatedNumber) on change. An optional `glyph` — a small coloured activity
// motif (sending dot-grid, warming flame, overdue ping) — sits in front to flag
// a live state without stealing the number, which stays the plain total. Hidden
// only when there's truly nothing to show.
const COUNT_LIGHT =
    "text-[10.5px] font-medium tabular-nums leading-none text-slate-300 transition-colors group-hover:text-slate-500";

function TabStat({
    total,
    glyph,
    format = compactN,
    title,
}: {
    total: number;
    glyph?: ReactNode;
    format?: (n: number) => string;
    title?: string;
}) {
    // Always render the number (including 0) so every data tab visibly carries a
    // count instead of going blank — it just tweens up as the query resolves.
    return (
        <span
            className="ml-auto inline-flex items-center gap-1.5 shrink-0"
            title={title}
        >
            {glyph}
            <AnimatedNumber value={total} format={format} className={COUNT_LIGHT} />
        </span>
    );
}

// TabDualStat shows TWO numbers on a row: the light "how many in total" (the calm
// baseline, e.g. all campaigns / all mailboxes) plus, when there's a live subset,
// a coloured sub-count with its motif (e.g. how many are sending / warming). Both
// tween. The total stays the faint baseline; the active subset is the coloured
// attention.
function TabDualStat({
    total,
    active,
    activeGlyph,
    activeClass,
    title,
}: {
    total: number;
    active: number;
    activeGlyph: ReactNode;
    activeClass: string;
    title?: string;
}) {
    return (
        <span
            className="ml-auto inline-flex items-center gap-2.5 shrink-0"
            title={title}
        >
            <AnimatedNumber value={total} format={compactN} className={COUNT_LIGHT} />
            {/* Hairline divider so the light total and the active count read as two
                separate values. Always present on a dual row so every one of them
                (campaigns, accounts, tasks) shows both numbers consistently. */}
            <span className="h-3 w-px shrink-0 bg-slate-200" aria-hidden />
            <span
                className={`inline-flex items-center gap-1 ${active > 0 ? activeClass : "text-slate-300"}`}
            >
                {/* The motif (sending dot-grid / warming flame / overdue ping) only
                    appears when there's actually a live subset; at 0 it's a calm
                    muted number. */}
                {active > 0 && activeGlyph}
                <AnimatedNumber
                    value={active}
                    format={compactN}
                    className="text-[10.5px] font-semibold tabular-nums leading-none"
                />
            </span>
        </span>
    );
}

// CampaignActivity is the ambient, realtime indicator on the Campaigns nav row.
// While campaigns are sending it escalates to a sky 3x3 dot-grid + a live count;
// otherwise it shows a faint total of all campaigns. The counts come from the
// shared campaigns-list cache, which the realtime layer invalidates on campaign
// events, so it stays live without a refresh.
function CampaignActivity() {
    const { campaigns } = useCampaigns({ query: "", folder: "" });
    const active = useMemo(
        () => campaigns.filter((c) => c.status === "active").length,
        [campaigns],
    );
    return (
        <TabDualStat
            total={campaigns.length}
            active={active}
            activeClass="text-sky-600"
            activeGlyph={<span className="campaign-grid" aria-hidden />}
            title={`${campaigns.length} campaign${campaigns.length === 1 ? "" : "s"}${active > 0 ? `, ${active} sending now` : ""}`}
        />
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
    return (
        <TabDualStat
            total={emails.length}
            active={warming}
            activeClass="text-orange-500"
            activeGlyph={
                <FlameIcon className="w-3.5 h-3.5 flame-flicker" strokeWidth={2.2} />
            }
            title={`${emails.length} mailbox${emails.length === 1 ? "" : "es"}${warming > 0 ? `, ${warming} warming up` : ""}`}
        />
    );
}

// TasksActivity is the Tasks-row indicator — its own motif again. Overdue is the
// urgent state (a soft red ping + count); when nothing is overdue it falls back
// to a quiet count of open tasks (todo) so the row still tells you how much work
// is waiting instead of going blank. Counts are SERVER aggregates (useTasksSummary)
// so "how many" is correct over the whole set, not a truncated page, and the
// realtime layer invalidates ["crm","tasks"] so they stay live. The number tweens
// (AnimatedNumber) when it changes.
function TasksActivity() {
    const { data } = useTasksSummary(EMPTY_TASK_SEARCH);
    const overdue = data?.overdue_count ?? 0;
    const todo = (data?.pending_count ?? 0) + (data?.in_progress_count ?? 0);
    return (
        <TabDualStat
            total={todo}
            active={overdue}
            activeClass="text-red-600"
            activeGlyph={
                <span className="relative inline-flex shrink-0">
                    <span className="w-1.5 h-1.5 rounded-full bg-red-500" />
                    <span className="absolute inset-0 rounded-full bg-red-500/40 animate-ping" />
                </span>
            }
            title={`${todo} open task${todo === 1 ? "" : "s"}${overdue > 0 ? `, ${overdue} overdue` : ""}`}
        />
    );
}

// MeetingsActivity — upcoming booked calls, with a live sky pulse on the ones
// happening today (a meeting today is the "act now" subset, like overdue tasks).
function MeetingsActivity() {
    const { data } = useMeetingsSummary();
    const upcoming = data?.upcoming ?? 0;
    const today = data?.today ?? 0;
    return (
        <TabDualStat
            total={upcoming}
            active={today}
            activeClass="text-sky-600"
            activeGlyph={
                <span className="relative inline-flex shrink-0">
                    <span className="w-1.5 h-1.5 rounded-full bg-sky-500" />
                    <span className="absolute inset-0 rounded-full bg-sky-500/40 animate-ping" />
                </span>
            }
            title={`${upcoming} upcoming meeting${upcoming === 1 ? "" : "s"}${today > 0 ? `, ${today} today` : ""}`}
        />
    );
}

// Contacts row: total contacts. Reads pagination.total from a small search — the
// limit MUST be >= the backend LimitMin (10) or validate.Limit rejects it (400)
// and the whole count comes back as 0.
function ContactsActivity() {
    const { data } = useSearchContacts({ options: CONTACTS_COUNT_SEARCH, limit: 10 });
    const total = data?.pages?.[0]?.pagination?.total ?? 0;
    return <TabStat total={total} title={`${total.toLocaleString()} contacts`} />;
}

// Deals row: open (not won/lost) deals.
function DealsActivity() {
    const { data } = useDealsSummary(EMPTY_DEAL_SEARCH);
    const open = data?.open_count ?? 0;
    return (
        <TabStat total={open} title={`${open} open deal${open === 1 ? "" : "s"}`} />
    );
}

// Pipelines row: how many pipelines exist.
function PipelinesActivity() {
    const { data } = usePipelines();
    const n = data?.length ?? 0;
    return (
        <TabStat total={n} title={`${n} pipeline${n === 1 ? "" : "s"}`} />
    );
}

// Templates row: how many saved templates.
function TemplatesActivity() {
    const { data } = useTemplates();
    const n = data?.length ?? 0;
    return (
        <TabStat total={n} title={`${n} template${n === 1 ? "" : "s"}`} />
    );
}

// Analytics row: a live, compact tally of emails sent this period — the headline
// throughput metric, surfaced right in the nav. From the org-wide usage overview.
function AnalyticsActivity() {
    const { data } = useUsageOverview();
    const sent = data?.campaigns?.emails_sent ?? 0;
    return (
        <TabStat
            total={sent}
            format={compactN}
            title={`${sent.toLocaleString()} emails sent this period`}
        />
    );
}

// API keys row: how many keys are currently active (not revoked / expired).
function ApiKeysActivity() {
    const { data } = useAPIKeys();
    const active = (data?.data ?? []).filter((k) => k.status === "active").length;
    return (
        <TabStat
            total={active}
            format={(v) => String(Math.round(v))}
            title={`${active} active API key${active === 1 ? "" : "s"}`}
        />
    );
}

// Integrations row: total connected integrations + a coloured "needs attention"
// sub-count (degraded / reauth-required) so a broken connection is visible from
// the sidebar. Reads the shared connections cache the realtime layer invalidates.
function IntegrationsActivity() {
    const { data } = useIntegrationConnections();
    const conns = data?.connections ?? [];
    const attention = conns.filter(
        (c) => c.status === "degraded" || c.status === "reauth_required" || c.health === "down",
    ).length;
    return (
        <TabDualStat
            total={conns.length}
            active={attention}
            activeClass="text-amber-600"
            activeGlyph={
                <span className="relative inline-flex shrink-0">
                    <span className="w-1.5 h-1.5 rounded-full bg-amber-500" />
                    <span className="absolute inset-0 rounded-full bg-amber-500/40 animate-ping" />
                </span>
            }
            title={`${conns.length} connected${attention > 0 ? `, ${attention} need attention` : ""}`}
        />
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
 *   │  128  of 400 sent today          │   ← hero number (scrubs on hover)
 *   │  ━━━━━━━─────────                │   ← capacity meter (today vs cap)
 *   │      ∿∿∿∿∿∿                     │   ← 14-day area sparkline
 *   │  ✉ 8   ● 5              ⬇ 3     │   ← mailboxes · active · unread
 *   └──────────────────────────────────┘
 *
 * Reads as ambient telemetry: even when idle, it tells you "n mailboxes,
 * n sent today." Clicking jumps to analytics; hovering a day on the
 * sparkline swaps the hero number to that day. There is deliberately no
 * LIVE/OFFLINE status row: the numbers ticking realtime already say the
 * system is up, so the panel spends its pixels on the data instead.
 *
 * Data sources at this layer:
 *   - useAppStore.emails  → mailbox count, active count
 *   - useDashboard("30d") daily_trend → today's sent volume + the sparkline
 *     (shares the dashboard page's query cache; realtime invalidation keeps
 *     it current)
 *
 * The capacity denominator sums each mailbox's configured campaign_limit
 * (default 50/day, from internal/config/constants.go).
 */
function LivePanel() {
    const emails = useAppStore((s) => s.emails);
    const unseenCount = useAppStore((s) => s.unseenCount);
    const dash = useDashboard("30d");
    const [hovered, setHovered] = useState<number | null>(null);

    const { active, mailboxes, capacity } = useMemo(() => {
        const m = emails.length;
        const a = emails.filter((e) => {
            const st = mailboxDisplayStatus(e);
            return st === "healthy" || st === "warming";
        }).length;
        // Capacity = the sum of each mailbox's configured daily campaign
        // limit (default 50/day), not a flat count × 50 — a tuned-down or
        // raised mailbox should move the meter's denominator.
        const cap = emails.reduce((sum, e) => sum + (e.campaign_limit ?? 50), 0);
        return { active: a, mailboxes: m, capacity: cap };
    }, [emails]);

    const { sentToday, trend } = useMemo(() => {
        // daily_trend only contains days that had sends; rebuild a continuous
        // last-14-days axis (zero-filling the gaps) so the sparkline's x
        // spacing is honest — otherwise a quiet week would be silently
        // squeezed out and two distant days would read as adjacent.
        const byDate = new Map(
            (dash.data?.daily_trend ?? []).map((d) => [d.date?.slice(0, 10), d.sent]),
        );
        const out: { date: string; sent: number }[] = [];
        const now = new Date();
        for (let i = 13; i >= 0; i--) {
            const d = new Date(now);
            d.setUTCDate(now.getUTCDate() - i);
            const key = d.toISOString().slice(0, 10);
            out.push({ date: key, sent: byDate.get(key) ?? 0 });
        }
        return { sentToday: out[out.length - 1].sent, trend: out };
    }, [dash.data]);

    const scrub = hovered != null ? trend[hovered] : undefined;
    const pct = capacity > 0 ? Math.min(100, (sentToday / capacity) * 100) : 0;

    return (
        <Link
            to="/app/analytics"
            className="group block mx-2 mt-2 mb-3 rounded-md bg-white/80 hover:bg-white border border-slate-200/70 hover:border-slate-300 pt-2 overflow-hidden transition-colors"
        >
            {/* Hero: today's sends against the derived daily cap. While the
                sparkline is being scrubbed it shows the hovered day instead. */}
            <div className="px-2.5 flex items-baseline gap-1.5 whitespace-nowrap">
                {scrub ? (
                    <>
                        <span className="text-[19px] font-semibold text-slate-900 leading-none">
                            {scrub.sent.toLocaleString()}
                        </span>
                        <span className="text-[10.5px] text-slate-500">
                            sent {formatTrendDay(scrub.date)}
                        </span>
                    </>
                ) : (
                    <>
                        <AnimatedNumber
                            value={sentToday}
                            className="text-[19px] font-semibold text-slate-900 leading-none"
                        />
                        <span className="text-[10.5px] text-slate-500">
                            {capacity > 0
                                ? `of ${capacity.toLocaleString()} sent today`
                                : "sent today"}
                        </span>
                    </>
                )}
            </div>

            {/* Capacity meter: same-ramp track so the unfilled part still reads
                as "room left today", not as a broken bar. */}
            <div
                className="mt-1.5 px-2.5"
                title={
                    capacity > 0
                        ? `${sentToday} of ${capacity} daily capacity used`
                        : "Connect a mailbox to start sending"
                }
            >
                <div className="h-1 rounded-full bg-sky-100 overflow-hidden">
                    <div
                        className="h-full rounded-full bg-sky-500 transition-[width] duration-700 ease-out"
                        style={{ width: `${pct}%` }}
                    />
                </div>
            </div>

            <Sparkline points={trend} hovered={hovered} onHover={setHovered} />

            {/* Glance chips: mailboxes · active senders · unread inbox. Icons
                carry the labels (title attrs spell them out) so this stays one
                quiet row instead of two label/value text lines. */}
            <div className="border-t border-slate-100 px-2.5 py-1.5 flex items-center gap-3 text-[10.5px]">
                <span
                    className="inline-flex items-center gap-1 text-slate-500"
                    title={`${mailboxes} ${mailboxes === 1 ? "mailbox" : "mailboxes"} connected`}
                >
                    <MailIcon className="w-3 h-3 text-slate-400" />
                    <span className="font-mono tabular-nums">{mailboxes}</span>
                </span>
                {active > 0 && (
                    <span
                        className="inline-flex items-center gap-1 text-emerald-600"
                        title={`${active} warming or sending`}
                    >
                        <span className="w-1.5 h-1.5 rounded-full bg-emerald-500" />
                        <span className="font-mono tabular-nums">{active}</span>
                    </span>
                )}
                <span
                    className={cn(
                        "ml-auto inline-flex items-center gap-1",
                        unseenCount > 0 ? "text-sky-600" : "text-slate-400",
                    )}
                    title={`${unseenCount} unread in inbox`}
                >
                    <InboxIcon className="w-3 h-3" />
                    <span className="font-mono tabular-nums">
                        {unseenCount > 99 ? "99+" : unseenCount}
                    </span>
                </span>
            </div>
        </Link>
    );
}

/** "2026-08-30" → "Aug 30" for the sparkline scrub readout. */
function formatTrendDay(iso: string): string {
    const d = new Date(iso);
    return Number.isNaN(d.getTime())
        ? iso
        : d.toLocaleDateString(undefined, { month: "short", day: "numeric", timeZone: "UTC" });
}

// Sparkline geometry. Width matches the card's inner width (sidebar w-64
// minus mx-2 and borders) so preserveAspectRatio="none" barely distorts
// the dots; side padding keeps markers clear of the overflow-hidden edges.
const SPARK_W = 238;
const SPARK_H = 34;
const SPARK_PAD_X = 6;
const SPARK_PAD_TOP = 6;
const SPARK_PAD_BOTTOM = 3;

/**
 * Sparkline — the last two weeks of send volume as a smooth area line
 * (Catmull-Rom smoothing, gradient wash under the stroke, end-of-series
 * dot with a surface ring). Full-bleed across the card; the chips row's
 * top border underneath doubles as the baseline. Invisible per-day hit
 * columns report the hovered day via onHover so the hero number above
 * scrubs with the cursor.
 */
function Sparkline({
    points,
    hovered,
    onHover,
}: {
    points: { date: string; sent: number }[];
    hovered: number | null;
    onHover: (i: number | null) => void;
}) {
    const { linePath, areaPath, dots, hasVolume } = useMemo(() => {
        const n = points.length;
        const baseY = SPARK_H - SPARK_PAD_BOTTOM;
        if (n < 2) {
            return {
                linePath: "",
                areaPath: "",
                dots: [] as { x: number; y: number }[],
                hasVolume: false,
            };
        }
        const max = Math.max(...points.map((p) => p.sent), 1);
        const span = SPARK_W - SPARK_PAD_X * 2;
        const usable = baseY - SPARK_PAD_TOP;
        const pts = points.map((p, i) => ({
            x: SPARK_PAD_X + (i / (n - 1)) * span,
            y: baseY - (p.sent / max) * usable,
        }));
        // Catmull-Rom → cubic bezier; control ys are clamped so a spike next
        // to a flat run never overshoots the frame.
        const clamp = (y: number) =>
            Math.min(baseY, Math.max(SPARK_PAD_TOP, y));
        let d = `M ${pts[0].x} ${pts[0].y}`;
        for (let i = 0; i < n - 1; i++) {
            const p0 = pts[i - 1] ?? pts[i];
            const p1 = pts[i];
            const p2 = pts[i + 1];
            const p3 = pts[i + 2] ?? p2;
            const c1x = p1.x + (p2.x - p0.x) / 6;
            const c1y = clamp(p1.y + (p2.y - p0.y) / 6);
            const c2x = p2.x - (p3.x - p1.x) / 6;
            const c2y = clamp(p2.y - (p3.y - p1.y) / 6);
            d += ` C ${c1x} ${c1y}, ${c2x} ${c2y}, ${p2.x} ${p2.y}`;
        }
        return {
            linePath: d,
            areaPath: `${d} L ${pts[n - 1].x} ${baseY} L ${pts[0].x} ${baseY} Z`,
            dots: pts,
            hasVolume: points.some((p) => p.sent > 0),
        };
    }, [points]);

    const n = points.length;
    const step = n > 1 ? (SPARK_W - SPARK_PAD_X * 2) / (n - 1) : 0;
    const hoverDot = hovered != null ? dots[hovered] : undefined;
    const endDot = dots[dots.length - 1];

    return (
        <svg
            viewBox={`0 0 ${SPARK_W} ${SPARK_H}`}
            preserveAspectRatio="none"
            aria-hidden
            className={cn(
                "mt-1 block w-full h-[34px]",
                hasVolume ? "text-sky-500" : "text-slate-300",
            )}
            onMouseLeave={() => onHover(null)}
        >
            <defs>
                <linearGradient id="livepanel-spark-fill" x1="0" y1="0" x2="0" y2="1">
                    <stop offset="0%" stopColor="currentColor" stopOpacity="0.18" />
                    <stop offset="100%" stopColor="currentColor" stopOpacity="0.02" />
                </linearGradient>
            </defs>
            {linePath && hasVolume && (
                <path d={areaPath} fill="url(#livepanel-spark-fill)" />
            )}
            {linePath && (
                <path
                    d={linePath}
                    fill="none"
                    stroke="currentColor"
                    strokeWidth="2"
                    strokeLinecap="round"
                    strokeLinejoin="round"
                    vectorEffect="non-scaling-stroke"
                />
            )}
            {/* Hover scrub: hairline + marker on the hovered day. */}
            {hoverDot && (
                <>
                    <line
                        x1={hoverDot.x}
                        y1={SPARK_PAD_TOP - 4}
                        x2={hoverDot.x}
                        y2={SPARK_H - SPARK_PAD_BOTTOM}
                        className="stroke-slate-200"
                        strokeWidth="1"
                        vectorEffect="non-scaling-stroke"
                    />
                    <circle
                        cx={hoverDot.x}
                        cy={hoverDot.y}
                        r="3"
                        fill="currentColor"
                        className="stroke-white"
                        strokeWidth="1.5"
                    />
                </>
            )}
            {/* End-of-series marker (today), ringed in the surface color. */}
            {endDot && hovered == null && (
                <circle
                    cx={endDot.x}
                    cy={endDot.y}
                    r="2.5"
                    fill="currentColor"
                    className="stroke-white"
                    strokeWidth="1.5"
                />
            )}
            {/* Invisible per-day hit columns driving the scrub. */}
            {n >= 2 &&
                points.map((_, i) => (
                    <rect
                        key={i}
                        x={SPARK_PAD_X + i * step - step / 2}
                        y={0}
                        width={step}
                        height={SPARK_H}
                        fill="transparent"
                        onMouseEnter={() => onHover(i)}
                    />
                ))}
        </svg>
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
