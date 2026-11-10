// The agency home: the send pool, totals and what needs attention across every
// client, then one row per client, worst first.

import React from "react";
import { Link, useNavigate } from "@tanstack/react-router";
import {
    AlertTriangleIcon,
    ArrowUpRightIcon,
    BarChart3Icon,
    ChevronRightIcon,
    CopyIcon,
    FileBarChartIcon,
    GaugeIcon,
    InboxIcon,
    Link2Icon,
    MoreHorizontalIcon,
    PlusIcon,
} from "lucide-react";
import { EmptyBlock, Page, PageBody, PageTopbar, SectionBar, Stat, StatStrip, TopbarAction } from "@/components/layout/Page";
import { PopoverMenu, PopoverMenuContent, PopoverMenuItem, PopoverMenuSeparator, PopoverMenuTrigger } from "@/components/ui/popover-menu";
import { DashboardImage } from "@/components/ui/dashboard-image";
import ConnectLinksDialog from "@/components/app/emails/connect-links/ConnectLinksDialog";
import { AddClientDialog } from "@/components/app/agency/AddClientDialog";
import { PoolMeter } from "@/components/app/agency/PoolMeter";
import { formatNumber } from "@/components/app/agency/styles";
import { useAgencyAIBalance, useAgencyClients, useCurrentAgency } from "@/lib/api/hooks/app/agency/useAgency";
import { fmtUsd } from "@/lib/money";
import { useAgencyLive, useAgencyOverview, useOpenClient } from "@/lib/api/hooks/app/agency/useAgencyHome";
import type { AgencyAttention, AgencyOverviewClient } from "@/lib/api/models/app/agency/AgencyHome";
import { cn } from "@/lib/utils";

function initials(name: string) {
    return name
        .split(" ")
        .filter(Boolean)
        .map((w) => w[0])
        .join("")
        .toUpperCase()
        .slice(0, 2);
}

export function ClientAvatar({ name, avatar, size = "md" }: { name: string; avatar?: string; size?: "sm" | "md" }) {
    const box = size === "sm" ? "size-4 text-[8px] rounded-[4px]" : "size-6 text-[10px] rounded-md";
    return (
        <span className={cn("ring-1 ring-slate-200 flex items-center justify-center shrink-0 overflow-hidden", box, avatar ? "bg-white" : "bg-slate-900")}>
            {avatar ? (
                <DashboardImage src={avatar} alt="" className="w-full h-full object-cover" />
            ) : (
                <span className="font-bold text-white leading-none tracking-tight">{initials(name)}</span>
            )}
        </span>
    );
}

const pct = (n: number) => `${(n * 100).toFixed(n >= 0.1 ? 0 : 1)}%`;
const plural = (n: number, one: string, many = `${one}s`) => `${formatNumber(n)} ${n === 1 ? one : many}`;

function attentionText(a: AgencyAttention): string {
    switch (a.kind) {
        case "mailbox_disconnected":
            return `${plural(a.count, "mailbox", "mailboxes")} disconnected or failing to sign in`;
        case "mailbox_held":
            return `${plural(a.count, "mailbox", "mailboxes")} quarantined or blocked from warmup`;
        case "bounce_spike":
            return `Bounce rate ${pct(a.rate ?? 0)} over the last 7 days`;
        case "campaigns_paused_limits":
            return `${plural(a.count, "campaign")} paused by a sending limit`;
        case "campaigns_paused_billing":
            return `${plural(a.count, "campaign")} paused until the plan is active`;
        case "client_cap_near":
            return (a.rate ?? 0) >= 1 ? "Monthly cap reached" : `${pct(a.rate ?? 0)} of its monthly cap used`;
        case "pool_low":
            return a.count === 0 ? "The send pool is used up" : `The send pool is running low: ${formatNumber(a.count)} sends left`;
    }
}

type Nav = ReturnType<typeof useNavigate>;

export default function AgencyHome() {
    const { agency } = useCurrentAgency();
    const overview = useAgencyOverview(agency?.id);
    const clients = useAgencyClients(agency?.id);
    const ai = useAgencyAIBalance(agency?.id);
    const navigate = useNavigate();
    const { open, pending } = useOpenClient();
    const [adding, setAdding] = React.useState(false);
    const [connectOpen, setConnectOpen] = React.useState(false);
    useAgencyLive(agency?.id);

    // The agency shell renders this only once there is an agency.
    if (!agency) return null;
    const canManage = agency.role === "owner" || agency.role === "admin";
    const data = overview.data;

    // Each attention item and row action opens the page the fix lives on.
    const go = (a: AgencyAttention) => {
        switch (a.kind) {
            case "pool_low":
                return navigate({ to: "/app/agency/usage" });
            case "client_cap_near":
                return navigate({ to: "/app/agency/clients" });
            case "campaigns_paused_billing":
                return navigate({ to: "/app/agency/billing" });
        }
        if (!a.organization_id) return;
        const then = (nav: Nav) => {
            switch (a.kind) {
                case "bounce_spike":
                    return nav({ to: "/app/deliverability" });
                case "campaigns_paused_limits":
                    return nav({ to: "/app/campaigns" });
                default:
                    return nav({ to: "/app/emails" });
            }
        };
        void open(a.organization_id, then);
    };

    const sendConnectLink = async (orgId: string) => {
        if (await open(orgId, () => undefined)) setConnectOpen(true);
    };

    const ranked = rankClients(data?.clients ?? [], data?.attention ?? []);
    const t = data?.totals;

    return (
        <Page>
            <PageTopbar eyebrow="Overview" subtitle={agency.name}>
                {canManage && (
                    <TopbarAction icon={<PlusIcon className="w-3 h-3" />} onClick={() => setAdding(true)}>
                        Add client
                    </TopbarAction>
                )}
            </PageTopbar>
            <PageBody>
                {data?.usage && <PoolMeter usage={data.usage} />}
                {ai.data && !ai.data.unmetered && (
                    <Link
                        to="/app/agency/usage"
                        className="px-5 h-9 flex items-center gap-2 border-b border-slate-200 text-[12px] text-slate-600 hover:bg-slate-50/80"
                    >
                        <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">AI balance</span>
                        <span className="tabular-nums text-slate-900">{fmtUsd(ai.data.balance_usd)}</span>
                        <span className="text-slate-500">left this period, shared by every client</span>
                    </Link>
                )}
                <StatStrip cols={6}>
                    <Stat label="Clients" value={t?.clients ?? "-"} />
                    <Stat label="Sent today" value={t?.sent_today ?? "-"} />
                    <Stat label="Replies waiting" value={t?.replies_waiting ?? "-"} href="/app/agency/inbox" />
                    <Stat label="Positive replies" value={t?.positive_replies_7d ?? "-"} sub="last 7 days" />
                    <Stat label="Active campaigns" value={t?.active_campaigns ?? "-"} />
                    <Stat label="Mailbox problems" value={t?.mailbox_problems ?? "-"} sub={t ? `of ${formatNumber(t.mailboxes)} mailboxes` : undefined} last />
                </StatStrip>

                <SectionBar label="Needs attention" count={data ? data.attention.length : undefined} />
                {data && data.attention.length === 0 && (
                    <p className="px-5 py-3 text-[12px] text-emerald-700 border-b border-slate-200/60">Nothing needs attention across your clients.</p>
                )}
                {data?.attention.map((a, i) => (
                    <button
                        key={`${a.kind}-${a.organization_id ?? "agency"}-${i}`}
                        type="button"
                        disabled={pending}
                        onClick={() => go(a)}
                        className="group w-full min-h-10 px-5 py-1.5 flex items-center gap-3 border-b border-slate-200/60 text-left hover:bg-slate-50/80 disabled:opacity-60"
                    >
                        <span className={cn("size-1.5 rounded-full shrink-0", a.severity === "critical" ? "bg-red-500" : "bg-amber-500")} />
                        <span className="w-36 shrink-0 truncate text-[12px] font-medium text-slate-900">{a.client_name || agency.name}</span>
                        <span className="flex-1 min-w-0 truncate text-[12.5px] text-slate-600">{attentionText(a)}</span>
                        <ChevronRightIcon className="w-3.5 h-3.5 text-slate-300 group-hover:text-slate-600 shrink-0" />
                    </button>
                ))}

                <SectionBar label="Clients" count={data?.clients.length} />
                {data && data.clients.length === 0 ? (
                    <EmptyBlock
                        title="No clients yet"
                        body="Each client gets its own workspace. Add one, then send it a connect link so it can add its own mailboxes."
                        cta={canManage ? <TopbarAction onClick={() => setAdding(true)}>Add client</TopbarAction> : undefined}
                    />
                ) : (
                    <div>
                        <div className="hidden lg:flex h-8 px-5 items-center gap-3 border-b border-slate-200/60 text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">
                            <span className="flex-1 min-w-0">Client</span>
                            <span className="w-28 text-right">Pool this period</span>
                            <span className="w-16 text-right">Today</span>
                            <span className="w-16 text-right">Replies</span>
                            <span className="w-16 text-right">Positive</span>
                            <span className="w-16 text-right">Active</span>
                            <span className="w-44">Mailboxes</span>
                            <span className="w-16 text-right">Bounce</span>
                            <span className="w-24">Advisor</span>
                            <span className="w-7" />
                        </div>
                        {ranked.map((c) => (
                            <ClientRow
                                key={c.organization_id}
                                client={c}
                                onOpen={(then) => void open(c.organization_id, then)}
                                onConnect={() => void sendConnectLink(c.organization_id)}
                                onCap={() => navigate({ to: "/app/agency/clients" })}
                            />
                        ))}
                    </div>
                )}
            </PageBody>
            <AddClientDialog open={adding} onClose={() => setAdding(false)} agencyId={agency.id} clients={clients.data ?? []} />
            <ConnectLinksDialog open={connectOpen} onClose={() => setConnectOpen(false)} />
        </Page>
    );
}

// Clients with critical problems first, then warnings, then by name.
function rankClients(clients: AgencyOverviewClient[], attention: AgencyAttention[]) {
    const score = new Map<string, number>();
    for (const a of attention) {
        if (!a.organization_id) continue;
        score.set(a.organization_id, (score.get(a.organization_id) ?? 0) + (a.severity === "critical" ? 100 : 1));
    }
    return [...clients].sort((a, b) => (score.get(b.organization_id) ?? 0) - (score.get(a.organization_id) ?? 0) || a.name.localeCompare(b.name));
}

function Num({ value, tone }: { value?: number; tone?: string }) {
    if (value === undefined) return <span className="text-slate-300">-</span>;
    return <span className={cn("tabular-nums", tone ?? (value === 0 ? "text-slate-400" : "text-slate-700"))}>{formatNumber(value)}</span>;
}

function Chip({ n, label, tone }: { n: number; label: string; tone: "red" | "amber" }) {
    if (n <= 0) return null;
    return (
        <span className={cn("h-5 px-1.5 rounded text-[10.5px] font-medium inline-flex items-center whitespace-nowrap", tone === "red" ? "bg-red-50 text-red-700" : "bg-amber-50 text-amber-700")}>
            {n} {label}
        </span>
    );
}

function ClientRow({
    client: c,
    onOpen,
    onConnect,
    onCap,
}: {
    client: AgencyOverviewClient;
    onOpen: (then?: (nav: Nav) => unknown) => void;
    onConnect: () => void;
    onCap: () => void;
}) {
    const m = c.mailboxes;
    const k = c.campaigns;
    const limit = c.monthly_send_limit ?? null;
    const capShare = limit ? Math.min(1, c.pool_used / limit) : null;
    const member = c.access !== "none";
    const bounceTone = k && k.sent_7d >= 20 && k.bounce_rate_7d >= 0.05 ? "text-red-600 font-medium" : undefined;

    const mailboxChips = m && (
        <span className="flex items-center gap-1 flex-wrap">
            <span className="text-[12px] tabular-nums text-slate-700 mr-0.5">{m.total}</span>
            <Chip n={m.disconnected + m.erroring} label="disconnected" tone="red" />
            <Chip n={m.blocked} label="blocked" tone="red" />
            <Chip n={m.quarantined} label="quarantined" tone="red" />
            <Chip n={m.throttled} label="throttled" tone="amber" />
            <Chip n={m.watch} label="watch" tone="amber" />
        </span>
    );

    return (
        <div
            role={member ? "button" : undefined}
            tabIndex={member ? 0 : undefined}
            onClick={member ? () => onOpen() : undefined}
            onKeyDown={(e) => {
                if (member && e.key === "Enter" && e.target === e.currentTarget) onOpen();
            }}
            className={cn(
                "group min-h-11 px-5 py-2 lg:py-1.5 flex flex-wrap lg:flex-nowrap items-center gap-x-3 gap-y-1.5 border-b border-slate-200/60",
                member && "hover:bg-slate-50/80 cursor-pointer",
            )}
        >
            <span className="flex-1 min-w-0 flex items-center gap-2">
                <ClientAvatar name={c.name} avatar={c.avatar_url} />
                <span className="text-[12.5px] text-slate-900 font-medium truncate">{c.name}</span>
                {c.access === "restricted" && <span className="h-5 px-1.5 rounded bg-slate-100 text-slate-500 text-[10.5px] inline-flex items-center">Limited access</span>}
                {c.access === "none" && <span className="h-5 px-1.5 rounded bg-slate-100 text-slate-500 text-[10.5px] inline-flex items-center">Not a member</span>}
            </span>
            <span className="w-28 lg:text-right text-[12px] tabular-nums text-slate-600 max-lg:order-last max-lg:w-full">
                <span className="lg:hidden text-slate-400">Pool </span>
                {formatNumber(c.pool_used)}
                {limit !== null && <span className="text-slate-400"> / {formatNumber(limit)}</span>}
                {capShare !== null && (
                    <span className="mt-1 block h-1 rounded-full bg-slate-100 overflow-hidden">
                        <span className={cn("block h-full", capShare >= 1 ? "bg-red-500" : capShare >= 0.9 ? "bg-amber-500" : "bg-sky-600")} style={{ width: `${capShare * 100}%` }} />
                    </span>
                )}
            </span>
            {/* Desktop columns; on narrow screens the same numbers wrap as labelled pairs. */}
            <span className="w-16 text-right text-[12px] hidden lg:inline"><Num value={k?.sent_today} /></span>
            <span className="w-16 text-right text-[12px] hidden lg:inline"><Num value={c.inbox?.replies_waiting} tone={c.inbox?.replies_waiting ? "text-sky-700 font-medium" : undefined} /></span>
            <span className="w-16 text-right text-[12px] hidden lg:inline"><Num value={k?.positive_replies_7d} tone={k?.positive_replies_7d ? "text-emerald-700 font-medium" : undefined} /></span>
            <span className="w-16 text-right text-[12px] hidden lg:inline"><Num value={k?.active} /></span>
            <span className="w-44 hidden lg:block">{mailboxChips ?? <span className="text-slate-300 text-[12px]">-</span>}</span>
            <span className="w-16 text-right text-[12px] hidden lg:inline">
                {k ? <span className={cn("tabular-nums", bounceTone ?? "text-slate-600")}>{k.sent_7d > 0 ? pct(k.bounce_rate_7d) : "-"}</span> : <span className="text-slate-300">-</span>}
            </span>
            <span className="w-24 hidden lg:flex items-center gap-1">
                {c.advisor ? (
                    c.advisor.critical + c.advisor.high > 0 ? (
                        <>
                            <Chip n={c.advisor.critical} label="critical" tone="red" />
                            <Chip n={c.advisor.high} label="high" tone="amber" />
                        </>
                    ) : (
                        <span className="text-[12px] text-slate-400">{c.advisor.medium + c.advisor.low || "None"}</span>
                    )
                ) : (
                    <span className="text-slate-300 text-[12px]">-</span>
                )}
            </span>
            {member && (
                <div className="lg:hidden w-full flex flex-wrap gap-x-4 gap-y-1 text-[11.5px] text-slate-500">
                    {k && <span>Today <Num value={k.sent_today} /></span>}
                    {c.inbox && <span>Replies <Num value={c.inbox.replies_waiting} /></span>}
                    {k && <span>Positive <Num value={k.positive_replies_7d} /></span>}
                    {k && <span>Active <Num value={k.active} /></span>}
                    {k && k.sent_7d > 0 && <span>Bounce <span className={bounceTone}>{pct(k.bounce_rate_7d)}</span></span>}
                    {mailboxChips && <span className="flex items-center gap-1">Mailboxes {mailboxChips}</span>}
                    {c.advisor && c.advisor.critical + c.advisor.high > 0 && (
                        <span className="inline-flex items-center gap-1 text-amber-700">
                            <AlertTriangleIcon className="w-3 h-3" />
                            {c.advisor.critical + c.advisor.high} Advisor
                        </span>
                    )}
                </div>
            )}
            {member ? (
                <div onClick={(e) => e.stopPropagation()} onKeyDown={(e) => e.stopPropagation()}>
                    <PopoverMenu align="end">
                        <PopoverMenuTrigger asChild>
                            <button
                                type="button"
                                aria-label={`Actions for ${c.name}`}
                                className="size-7 rounded-md text-slate-400 hover:text-slate-800 hover:bg-slate-100 inline-flex items-center justify-center opacity-100 md:opacity-0 md:group-hover:opacity-100 focus:opacity-100"
                            >
                                <MoreHorizontalIcon className="w-3.5 h-3.5" />
                            </button>
                        </PopoverMenuTrigger>
                        <PopoverMenuContent minWidth={210}>
                            <PopoverMenuItem icon={<ArrowUpRightIcon className="w-3 h-3" />} onSelect={() => onOpen()}>
                                Open workspace
                            </PopoverMenuItem>
                            {c.inbox && (
                                <PopoverMenuItem
                                    icon={<InboxIcon className="w-3 h-3" />}
                                    onSelect={() => onOpen((nav) => nav({ to: "/app/unibox/{-$scope}/{-$threadId}", params: { scope: "inbox", threadId: undefined } }))}
                                >
                                    Inbox
                                </PopoverMenuItem>
                            )}
                            <PopoverMenuItem icon={<BarChart3Icon className="w-3 h-3" />} onSelect={() => onOpen((nav) => nav({ to: "/app/analytics" }))}>
                                Analytics
                            </PopoverMenuItem>
                            <PopoverMenuSeparator />
                            <PopoverMenuItem icon={<Link2Icon className="w-3 h-3" />} onSelect={onConnect} description="Lets the client connect its own mailboxes">
                                Send connect link
                            </PopoverMenuItem>
                            <PopoverMenuItem icon={<CopyIcon className="w-3 h-3" />} onSelect={() => onOpen((nav) => nav({ to: "/app/campaigns" }))} description="Pick a campaign, then Copy to workspace">
                                Copy a campaign
                            </PopoverMenuItem>
                            <PopoverMenuItem icon={<FileBarChartIcon className="w-3 h-3" />} onSelect={() => onOpen((nav) => nav({ to: "/app/reports" }))}>
                                Open reports
                            </PopoverMenuItem>
                            <PopoverMenuItem icon={<GaugeIcon className="w-3 h-3" />} onSelect={onCap}>
                                Monthly cap
                            </PopoverMenuItem>
                        </PopoverMenuContent>
                    </PopoverMenu>
                </div>
            ) : (
                <Link to="/app/agency/staff" onClick={(e) => e.stopPropagation()} className="text-[11.5px] text-sky-700 hover:underline">
                    Staff settings
                </Link>
            )}
        </div>
    );
}
