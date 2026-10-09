// Landing page: the instance problems strip, the platform counters, the
// growth trends, the send/user timeseries and where signups came from. No
// chart library: the bars are CSS so the admin bundle does not pay for one
// screen. Every analytics endpoint here gates on ViewAnalytics, so the
// queries are disabled (not failing) for an admin without that bit.

import { useState, type ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import {
    Activity,
    AlertTriangle,
    BarChart3,
    Lock,
    Mail,
    Megaphone,
    Send,
    Server,
    ShieldAlert,
    TrendingDown,
    TrendingUp,
    UserPlus,
    Users,
} from "lucide-react";
import { PageHeader } from "@/components/layout/PageHeader";
import { Skeleton } from "@/components/ui/skeleton";
import { EmptyState, Panel, Section, Segmented, Stat, StatGrid } from "@/components/ui/kit";
import { InstanceProblemsPanel } from "./InstanceHealthPanel";
import { InstanceMonitoringPanel } from "./InstanceMonitoringPanel";
import { useAdminPerm } from "@/hooks/useAdminPerm";
import { AdminPerm } from "@/lib/auth/permissions";
import { TONE_DOT, TONE_TEXT } from "@/lib/tones";
import { cn } from "@/lib/utils";
import {
    getAcquisition,
    getAnalyticsTrends,
    getDailyEmailStats,
    getHourlyEmailStats,
    getPlatformOverview,
    getUserGrowthStats,
} from "@/lib/api/client/admin/analytics";
import type { DailyEmailStat } from "@/lib/api/models/admin";

const RANGES = [7, 30, 90] as const;
type RangeDays = (typeof RANGES)[number];

function formatNum(n: number | undefined): string {
    if (n === undefined || n === null || Number.isNaN(n)) return "—";
    return new Intl.NumberFormat("en-US").format(n);
}

const compactFmt = new Intl.NumberFormat("en-US", { notation: "compact", maximumFractionDigits: 1 });

// Rounds a peak up to 1, 2, 4, 6, 8 or 10 times a power of ten so the gridline
// labels read as round numbers.
function niceCeil(n: number): number {
    if (n <= 1) return 1;
    const p = 10 ** Math.floor(Math.log10(n));
    const m = n / p;
    const step = [1, 2, 4, 6, 8, 10].find((s) => m <= s) ?? 10;
    return step * p;
}

function pct(n: number, max: number): string {
    return `${Math.min(100, (n / max) * 100)}%`;
}

export default function OverviewPage() {
    const canView = useAdminPerm(AdminPerm.ViewAnalytics);
    const [days, setDays] = useState<RangeDays>(30);

    return (
        <div>
            <PageHeader
                title="Overview"
                description="What this instance is doing right now: problems that need a decision, the platform counters, and how sending and signups are trending."
            >
                {/* One window drives every "last N days" query, so all panels describe the same period. */}
                <Segmented
                    value={days}
                    onChange={setDays}
                    ariaLabel="Date range"
                    options={RANGES.map((d) => ({ value: d, label: `${d}d` }))}
                />
            </PageHeader>

            <InstanceProblemsPanel />
            <InstanceMonitoringPanel compact />

            {!canView ? (
                <div className="rounded-lg border border-dashed border-border-strong">
                    <EmptyState
                        icon={Lock}
                        title="Analytics permission required"
                        hint="Platform counters and trends need the View analytics permission."
                    />
                </div>
            ) : (
                <>
                    <Section className="mt-0">
                        <Counters />
                    </Section>

                    <Section title="Growth" description="Each metric against the previous period of the same length.">
                        <TrendCards />
                    </Section>

                    <Section title="Sending">
                        <DailyEmailChart days={days} />
                        <div className="mt-4 grid gap-4 lg:grid-cols-2">
                            <HourlyChart />
                            <UserGrowthChart days={days} />
                        </div>
                    </Section>

                    <AcquisitionCard days={days} />
                </>
            )}
        </div>
    );
}

// The counters are the one query on this page that polls: nothing in the
// realtime spine invalidates ["admin","analytics"], and EMAIL_SENT matches
// no group on purpose, so without this the "today" numbers never move.
function Counters() {
    const overviewQ = useQuery({
        queryKey: ["admin", "analytics", "overview"],
        queryFn: getPlatformOverview,
        refetchInterval: 60_000,
    });
    const ov = overviewQ.data;
    const loading = overviewQ.isLoading;
    const workersDown = !!ov && (ov.total_workers ?? 0) > 0 && (ov.active_workers ?? 0) === 0;

    return (
        <>
            <StatGrid>
                <Stat
                    icon={Server}
                    label="Workers active"
                    value={formatNum(ov?.active_workers)}
                    sub={`${formatNum(ov?.total_workers)} registered`}
                    loading={loading}
                    tone={workersDown ? "warning" : undefined}
                />
                <Stat
                    icon={Send}
                    label="Emails sent today"
                    value={formatNum(ov?.emails_sent_today)}
                    sub={`${formatNum(ov?.total_emails_sent)} all time`}
                    loading={loading}
                />
                <Stat
                    icon={Megaphone}
                    label="Campaigns active"
                    value={formatNum(ov?.active_campaigns)}
                    sub={`${formatNum(ov?.total_campaigns)} created`}
                    loading={loading}
                />
                <Stat
                    icon={Users}
                    label="Users active, 30d"
                    value={formatNum(ov?.active_users)}
                    sub={`${formatNum(ov?.total_users)} total`}
                    loading={loading}
                />
            </StatGrid>
            <StatGrid className="mt-3 md:grid-cols-3 lg:grid-cols-6">
                <MiniStat icon={UserPlus} label="New today" value={formatNum(ov?.new_users_today)} />
                <MiniStat icon={UserPlus} label="New this week" value={formatNum(ov?.new_users_this_week)} />
                <MiniStat icon={Activity} label="Subscriptions" value={formatNum(ov?.active_subscriptions)} />
                <MiniStat icon={Activity} label="Trialing" value={formatNum(ov?.trialing_users)} />
                <MiniStat
                    icon={ShieldAlert}
                    label="Warmup blocked"
                    value={formatNum(ov?.warmup_blocked_count)}
                    warn={(ov?.warmup_blocked_count ?? 0) > 0}
                />
                <MiniStat
                    icon={AlertTriangle}
                    label="Pending appeals"
                    value={formatNum(ov?.pending_appeals)}
                    warn={(ov?.pending_appeals ?? 0) > 0}
                />
            </StatGrid>
        </>
    );
}

// Label left, value right: a dense secondary counter inside a StatGrid.
function MiniStat({
    icon: Icon,
    label,
    value,
    warn,
}: {
    icon: React.ComponentType<{ className?: string }>;
    label: string;
    value: string;
    warn?: boolean;
}) {
    return (
        <div className="flex min-w-0 items-center justify-between gap-2 bg-card px-4 py-2.5">
            <span className="flex min-w-0 items-center gap-1.5 text-xs text-muted-foreground">
                <Icon className={cn("size-3.5 shrink-0", warn ? TONE_TEXT.warning : "text-subtle-foreground")} />
                <span className="truncate">{label}</span>
            </span>
            <span className={cn("text-[13px] font-semibold tabular-nums", warn ? TONE_TEXT.warning : "text-foreground")}>
                {value}
            </span>
        </div>
    );
}

function TrendCards() {
    const { data, isLoading } = useQuery({
        queryKey: ["admin", "analytics", "trends"],
        queryFn: getAnalyticsTrends,
        staleTime: 60_000,
    });

    return (
        <StatGrid>
            <TrendStat icon={Users} label="Users" pct={data?.users_growth_percent} loading={isLoading} />
            <TrendStat icon={Mail} label="Emails sent" pct={data?.emails_growth_percent} loading={isLoading} />
            <TrendStat icon={Megaphone} label="Campaigns" pct={data?.campaigns_growth_percent} loading={isLoading} />
            <TrendStat icon={BarChart3} label="Revenue" pct={data?.revenue_growth_percent} loading={isLoading} />
        </StatGrid>
    );
}

function TrendStat({
    icon,
    label,
    pct: value,
    loading,
}: {
    icon: typeof Users;
    label: string;
    pct?: number;
    loading: boolean;
}) {
    const v = value ?? 0;
    const Arrow = v > 0 ? TrendingUp : v < 0 ? TrendingDown : null;
    return (
        <Stat
            icon={icon}
            label={label}
            loading={loading}
            tone={value == null ? undefined : v > 0 ? "success" : v < 0 ? "danger" : "neutral"}
            value={value == null ? "—" : `${v > 0 ? "+" : ""}${v.toFixed(1)}%`}
            sub={
                <span className="inline-flex items-center gap-1">
                    {Arrow && <Arrow className={cn("size-3", v > 0 ? TONE_TEXT.success : TONE_TEXT.danger)} />}
                    vs. previous period
                </span>
            }
        />
    );
}

// ---- Charts ------------------------------------------------------------------

// Gridlines with their values on the left, bars on top, optional x labels below.
function ChartFrame({
    max,
    height,
    axis,
    children,
}: {
    max: number;
    height: string;
    axis?: ReactNode;
    children: ReactNode;
}) {
    const ticks = max >= 2 ? [max, max / 2, 0] : [max, 0];
    return (
        <div className="flex gap-2">
            <div className={cn("relative w-8 shrink-0 text-[10.5px] tabular-nums text-subtle-foreground", height)}>
                {ticks.map((t, i) => (
                    <span
                        key={t}
                        className="absolute right-0 -translate-y-1/2 leading-none"
                        style={{ top: `${ticks.length === 2 ? i * 100 : i * 50}%` }}
                    >
                        {compactFmt.format(t)}
                    </span>
                ))}
            </div>
            <div className="min-w-0 flex-1">
                <div className={cn("relative", height)}>
                    <div className="pointer-events-none absolute inset-0">
                        {ticks.map((t, i) => (
                            <div
                                key={t}
                                className={cn("absolute inset-x-0 h-px", t === 0 ? "bg-border-strong" : "bg-border/70")}
                                style={{ top: `${ticks.length === 2 ? i * 100 : i * 50}%` }}
                            />
                        ))}
                    </div>
                    <div className="relative flex h-full items-end gap-[3px]">{children}</div>
                </div>
                {axis && (
                    <div className="mt-2 flex h-3.5 gap-[3px] text-[10.5px] leading-none text-subtle-foreground tabular-nums">
                        {axis}
                    </div>
                )}
            </div>
        </div>
    );
}

// One x-axis slot per bar so labels line up with their column.
function AxisLabel({ show, children }: { show: boolean; children: ReactNode }) {
    return (
        <div className="relative min-w-0 flex-1">
            {show && (
                <span className="absolute left-1/2 -translate-x-1/2 whitespace-nowrap">{children}</span>
            )}
        </div>
    );
}

// A full-height column with a hover band behind the bar.
function Column({ title, children }: { title: string; children: ReactNode }) {
    return (
        <div className="group relative flex h-full min-w-0 flex-1 flex-col justify-end" title={title}>
            <div className="absolute -inset-x-px inset-y-0 rounded-[3px] bg-accent opacity-0 transition-opacity group-hover:opacity-100" />
            {children}
        </div>
    );
}

function ChartLegend({
    items,
}: {
    items: { label: string; color: string; value?: string }[];
}) {
    return (
        <div className="flex flex-wrap items-center gap-x-5 gap-y-2">
            {items.map((it) => (
                <div key={it.label} className="flex items-center gap-1.5 text-xs text-muted-foreground">
                    <span className={cn("size-2 rounded-[2px]", it.color)} />
                    {it.label}
                    {it.value !== undefined && (
                        <span className="font-medium text-foreground tabular-nums">{it.value}</span>
                    )}
                </div>
            ))}
        </div>
    );
}

function sum<T>(rows: T[], pick: (r: T) => number): number {
    return rows.reduce((acc, r) => acc + pick(r), 0);
}

function shortDate(iso: string): string {
    return new Date(iso).toLocaleDateString(undefined, { month: "short", day: "numeric" });
}

function DailyEmailChart({ days }: { days: number }) {
    const { data, isLoading } = useQuery({
        queryKey: ["admin", "analytics", "emails", "daily", days],
        queryFn: () => getDailyEmailStats(days),
        staleTime: 60_000,
    });
    const rows = data ?? [];

    return (
        <Panel title="Email volume" description={`Last ${days} days`}>
            {isLoading ? (
                <Skeleton className="h-56" />
            ) : rows.length === 0 ? (
                <EmptyState
                    icon={BarChart3}
                    title="No email activity"
                    hint={`No email activity in the last ${days} days. Bars appear here once a campaign or warmup send goes out.`}
                    className="py-10"
                />
            ) : (
                <DailyBars rows={rows} />
            )}
        </Panel>
    );
}

function DailyBars({ rows }: { rows: DailyEmailStat[] }) {
    const max = niceCeil(Math.max(1, ...rows.map((r) => r.total_sent)));
    // Show about eight dates, always including the most recent one.
    const step = Math.max(1, Math.ceil(rows.length / 8));
    const showLabel = (i: number) => (rows.length - 1 - i) % step === 0;

    return (
        <>
            <ChartLegend
                items={[
                    { label: "Sent", color: "bg-border-strong", value: formatNum(sum(rows, (r) => r.total_sent)) },
                    { label: "Delivered", color: "bg-[var(--admin-accent)]", value: formatNum(sum(rows, (r) => r.total_delivered)) },
                    { label: "Replied", color: TONE_DOT.success, value: formatNum(sum(rows, (r) => r.total_replied)) },
                    { label: "Bounced", color: TONE_DOT.danger, value: formatNum(sum(rows, (r) => r.total_bounced)) },
                ]}
            />
            <div className="mt-5">
                <ChartFrame
                    max={max}
                    height="h-44"
                    axis={rows.map((r, i) => (
                        <AxisLabel key={r.date} show={showLabel(i)}>
                            {shortDate(r.date)}
                        </AxisLabel>
                    ))}
                >
                    {rows.map((r) => (
                        <DayBar key={r.date} stat={r} max={max} />
                    ))}
                </ChartFrame>
            </div>
        </>
    );
}

function DayBar({ stat, max }: { stat: DailyEmailStat; max: number }) {
    const other = Math.max(0, stat.total_delivered - stat.total_replied - stat.total_bounced);
    const stacked = stat.total_bounced + stat.total_replied + other;
    return (
        <Column
            title={`${new Date(stat.date).toLocaleDateString()}\nsent: ${stat.total_sent}\ndelivered: ${stat.total_delivered}\nbounced: ${stat.total_bounced}\nreplied: ${stat.total_replied}`}
        >
            {stacked > 0 && (
                <div
                    className="relative flex min-h-[2px] w-full flex-col gap-px overflow-hidden rounded-t-[2px]"
                    style={{ height: pct(stacked, max) }}
                >
                    {stat.total_bounced > 0 && <div className={TONE_DOT.danger} style={{ flexGrow: stat.total_bounced }} />}
                    {stat.total_replied > 0 && <div className={TONE_DOT.success} style={{ flexGrow: stat.total_replied }} />}
                    {other > 0 && <div className="bg-[var(--admin-accent)]" style={{ flexGrow: other }} />}
                </div>
            )}
        </Column>
    );
}

function HourlyChart() {
    const { data, isLoading } = useQuery({
        queryKey: ["admin", "analytics", "emails", "hourly"],
        queryFn: getHourlyEmailStats,
        staleTime: 60_000,
    });

    const rows = data ?? [];
    const max = niceCeil(Math.max(1, ...rows.map((r) => r.total_sent)));
    const total = sum(rows, (r) => r.total_sent);

    return (
        <Panel
            title="Today by hour"
            description="Sends per hour"
            actions={
                rows.length > 0 && (
                    <span className="text-xs text-muted-foreground">
                        <span className="font-medium text-foreground tabular-nums">{formatNum(total)}</span> sent
                    </span>
                )
            }
        >
            {isLoading ? (
                <Skeleton className="h-40" />
            ) : rows.length === 0 ? (
                <EmptyState
                    icon={Send}
                    title="No sends today yet"
                    hint="The hourly profile fills in as workers send."
                    className="py-10"
                />
            ) : (
                <ChartFrame
                    max={max}
                    height="h-36"
                    axis={rows.map((r) => (
                        <AxisLabel key={r.hour} show={rows.length <= 12 || r.hour % 3 === 0}>
                            {r.hour}
                        </AxisLabel>
                    ))}
                >
                    {rows.map((r) => (
                        <Column key={r.hour} title={`${r.hour}:00, ${r.total_sent} sent`}>
                            {r.total_sent > 0 && (
                                <div
                                    className="relative min-h-[2px] w-full rounded-t-[2px] bg-[var(--admin-accent)]"
                                    style={{ height: pct(r.total_sent, max) }}
                                />
                            )}
                        </Column>
                    ))}
                </ChartFrame>
            )}
        </Panel>
    );
}

function UserGrowthChart({ days }: { days: number }) {
    const { data, isLoading } = useQuery({
        queryKey: ["admin", "analytics", "users", "growth", days],
        queryFn: () => getUserGrowthStats(days),
        staleTime: 60_000,
    });

    const rows = data ?? [];
    const max = niceCeil(Math.max(1, ...rows.map((r) => r.new_users)));
    const added = sum(rows, (r) => r.new_users);
    const step = Math.max(1, Math.ceil(rows.length / 6));

    return (
        <Panel
            title="User growth"
            description={`Last ${days} days`}
            actions={
                rows.length > 0 && (
                    <span className="text-xs text-muted-foreground">
                        <span className={cn("font-medium tabular-nums", TONE_TEXT.success)}>+{formatNum(added)}</span> new
                        <span className="mx-1.5 text-subtle-foreground">·</span>
                        <span className="font-medium text-foreground tabular-nums">
                            {formatNum(rows[rows.length - 1]?.total_users)}
                        </span>{" "}
                        total
                    </span>
                )
            }
        >
            {isLoading ? (
                <Skeleton className="h-40" />
            ) : rows.length === 0 ? (
                <EmptyState
                    icon={UserPlus}
                    title="No new users"
                    hint={`No new users in the last ${days} days.`}
                    className="py-10"
                />
            ) : (
                <>
                    <ChartFrame
                        max={max}
                        height="h-36"
                        axis={rows.map((r, i) => (
                            <AxisLabel key={r.date} show={(rows.length - 1 - i) % step === 0}>
                                {shortDate(r.date)}
                            </AxisLabel>
                        ))}
                    >
                        {rows.map((r) => (
                            <Column
                                key={r.date}
                                title={`${new Date(r.date).toLocaleDateString()}\n+${r.new_users} new (${r.total_users} total)`}
                            >
                                {r.new_users > 0 && (
                                    <div
                                        className="relative min-h-[2px] w-full rounded-t-[2px] bg-chart-2"
                                        style={{ height: pct(r.new_users, max) }}
                                    />
                                )}
                            </Column>
                        ))}
                    </ChartFrame>
                    <div className="mt-3 text-xs text-subtle-foreground">New users per day (hover for totals).</div>
                </>
            )}
        </Panel>
    );
}

// ---- Acquisition ---------------------------------------------------------------

// Where signups came from: the UTM channel and referrer each account was
// tagged with at registration, and how many of them went on to pay.
function AcquisitionCard({ days }: { days: number }) {
    const acqQ = useQuery({
        queryKey: ["admin", "analytics", "acquisition", days],
        queryFn: () => getAcquisition(days),
        staleTime: 60_000,
    });
    const a = acqQ.data;
    const channels = (a?.channels ?? []).slice(0, 6);
    const referrers = (a?.referrers ?? []).slice(0, 6);
    const maxChannel = Math.max(1, ...channels.map((c) => c.signups));
    const maxRef = Math.max(1, ...referrers.map((r) => r.signups));
    const rate = a && a.signups > 0 ? `${((a.converted / a.signups) * 100).toFixed(1)}%` : "—";

    return (
        <Section
            title="Acquisition"
            description={`Signups over the last ${days} days by the UTM channel and referrer they arrived with, and how many of them converted to a paid plan.`}
        >
            {acqQ.isLoading && (
                <div className="space-y-3">
                    <Skeleton className="h-[86px] w-full" />
                    <Skeleton className="h-48 w-full" />
                </div>
            )}
            {acqQ.isError && (
                <div className="rounded-lg border border-dashed border-border-strong px-4 py-6 text-center text-[13px] text-muted-foreground">
                    Acquisition data is unavailable on this backend.
                </div>
            )}
            {a && (
                <>
                    <StatGrid>
                        <Stat icon={UserPlus} label="Signups" value={formatNum(a.signups)} />
                        <Stat icon={BarChart3} label="With a channel" value={formatNum(a.with_channel)} />
                        <Stat
                            icon={TrendingUp}
                            label="Converted"
                            value={formatNum(a.converted)}
                            sub={`${rate} of signups`}
                        />
                        <Stat
                            icon={AlertTriangle}
                            label="Trials ending, 7d"
                            value={formatNum(a.trials_expiring_7d)}
                            tone={a.trials_expiring_7d > 0 ? "warning" : undefined}
                        />
                    </StatGrid>
                    <div className="mt-4 grid overflow-hidden surface-lit rounded-xl border border-border bg-card md:grid-cols-2">
                        <RankedList
                            title="Top channels"
                            empty="No signup carried a UTM source or medium in this window."
                            rows={channels.map((c) => ({
                                key: `${c.source}/${c.medium}`,
                                label: c.medium ? `${c.source} / ${c.medium}` : c.source,
                                value: c.signups,
                                note: c.converted > 0 ? `${c.converted} paid` : undefined,
                                pct: (c.signups / maxChannel) * 100,
                            }))}
                        />
                        <RankedList
                            title="Top referrers"
                            empty="No signup arrived with a referrer in this window."
                            className="border-t border-border md:border-t-0 md:border-l"
                            rows={referrers.map((r) => ({
                                key: r.host,
                                label: r.host,
                                value: r.signups,
                                pct: (r.signups / maxRef) * 100,
                            }))}
                        />
                    </div>
                </>
            )}
        </Section>
    );
}

function RankedList({
    title,
    empty,
    rows,
    className,
}: {
    title: string;
    empty: string;
    rows: { key: string; label: string; value: number; note?: string; pct: number }[];
    className?: string;
}) {
    return (
        <div className={cn("min-w-0", className)}>
            <div className="flex h-9 items-center justify-between border-b border-border px-4 text-xs font-medium text-muted-foreground">
                <span>{title}</span>
                <span>Signups</span>
            </div>
            {rows.length === 0 ? (
                <p className="px-4 py-8 text-center text-[12.5px] text-muted-foreground">{empty}</p>
            ) : (
                <ul className="space-y-1 p-2">
                    {rows.map((r) => (
                        <li key={r.key} className="relative flex h-8 items-center justify-between gap-3 px-2 text-[13px]">
                            <span
                                className="absolute inset-y-0 left-0 rounded-[5px] bg-[var(--admin-accent-soft)] opacity-70"
                                style={{ width: `${Math.max(2, r.pct)}%` }}
                            />
                            <span className="relative truncate text-foreground">{r.label}</span>
                            <span className="relative shrink-0 tabular-nums text-muted-foreground">
                                {r.note && <span className={cn("mr-2 text-xs", TONE_TEXT.success)}>{r.note}</span>}
                                <span className="font-medium text-foreground">{formatNum(r.value)}</span>
                            </span>
                        </li>
                    ))}
                </ul>
            )}
        </div>
    );
}
