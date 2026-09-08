// Landing page: the instance problems strip, the platform counters, the
// growth trends, the send/user timeseries and where signups came from. No
// chart library: the bars are CSS so the admin bundle does not pay for one
// screen. Every analytics endpoint here gates on ViewAnalytics, so the
// queries are disabled (not failing) for an admin without that bit.

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import {
    Activity,
    AlertTriangle,
    BarChart3,
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
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { InstanceProblemsPanel } from "./InstanceHealthPanel";
import { useAdminPerm } from "@/hooks/useAdminPerm";
import { AdminPerm } from "@/lib/auth/permissions";
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

export default function OverviewPage() {
    const canView = useAdminPerm(AdminPerm.ViewAnalytics);
    const [days, setDays] = useState<RangeDays>(30);

    return (
        <div>
            <PageHeader
                title="Overview"
                description="What this instance is doing right now: problems that need a decision, the platform counters, and how sending and signups are trending."
            >
                <RangePicker value={days} onChange={setDays} />
            </PageHeader>

            <InstanceProblemsPanel />

            {!canView ? (
                <div className="rounded-lg border border-dashed border-border p-10 text-center text-sm text-muted-foreground">
                    Platform counters and trends need the View analytics permission.
                </div>
            ) : (
                <>
                    <Counters />
                    <TrendCards />

                    <section className="mt-6">
                        <h2 className="mb-2 text-sm font-semibold">
                            Email volume, last {days} days
                        </h2>
                        <DailyEmailChart days={days} />
                    </section>

                    <section className="mt-6 grid gap-3 md:grid-cols-2">
                        <HourlyChart />
                        <div>
                            <h2 className="mb-2 text-sm font-semibold">
                                User growth, last {days} days
                            </h2>
                            <UserGrowthChart days={days} />
                        </div>
                    </section>

                    <section className="mt-6">
                        <AcquisitionCard days={days} />
                    </section>
                </>
            )}
        </div>
    );
}

// One window drives every "last N days" query on the page, so the charts
// and the acquisition card always describe the same period.
function RangePicker({
    value,
    onChange,
}: {
    value: RangeDays;
    onChange: (d: RangeDays) => void;
}) {
    return (
        <div
            role="radiogroup"
            aria-label="Date range"
            className="inline-flex h-8 items-center rounded-md border border-border bg-white p-0.5"
        >
            {RANGES.map((d) => {
                const active = d === value;
                return (
                    <button
                        key={d}
                        type="button"
                        role="radio"
                        aria-checked={active}
                        onClick={() => onChange(d)}
                        className={`h-7 rounded px-2.5 text-[12.5px] transition-colors ${
                            active
                                ? "bg-[var(--admin-accent-soft)] font-medium text-foreground"
                                : "text-muted-foreground hover:text-foreground"
                        }`}
                    >
                        {d}d
                    </button>
                );
            })}
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

    return (
        <>
            <div className="mb-3 grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-4">
                <StatCard
                    icon={Server}
                    label="Workers active"
                    value={formatNum(ov?.active_workers)}
                    sub={`${formatNum(ov?.total_workers)} registered`}
                    loading={loading}
                    tone={
                        ov && (ov.total_workers ?? 0) > 0 && (ov.active_workers ?? 0) === 0
                            ? "warn"
                            : "neutral"
                    }
                />
                <StatCard
                    icon={Send}
                    label="Emails sent today"
                    value={formatNum(ov?.emails_sent_today)}
                    sub={`${formatNum(ov?.total_emails_sent)} all time`}
                    loading={loading}
                />
                <StatCard
                    icon={Megaphone}
                    label="Campaigns active"
                    value={formatNum(ov?.active_campaigns)}
                    sub={`${formatNum(ov?.total_campaigns)} created`}
                    loading={loading}
                />
                <StatCard
                    icon={Users}
                    label="Users active, 30d"
                    value={formatNum(ov?.active_users)}
                    sub={`${formatNum(ov?.total_users)} total`}
                    loading={loading}
                />
            </div>
            <div className="mb-6 grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-6">
                <MiniStat icon={UserPlus} label="New today" value={formatNum(ov?.new_users_today)} />
                <MiniStat
                    icon={UserPlus}
                    label="New this week"
                    value={formatNum(ov?.new_users_this_week)}
                />
                <MiniStat
                    icon={Activity}
                    label="Subscriptions"
                    value={formatNum(ov?.active_subscriptions)}
                />
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
            </div>
        </>
    );
}

function TrendCards() {
    const { data, isLoading } = useQuery({
        queryKey: ["admin", "analytics", "trends"],
        queryFn: getAnalyticsTrends,
        staleTime: 60_000,
    });

    if (isLoading) {
        return (
            <div className="grid gap-3 md:grid-cols-4">
                {Array.from({ length: 4 }).map((_, i) => (
                    <Skeleton key={i} className="h-24" />
                ))}
            </div>
        );
    }

    return (
        <div className="grid gap-3 md:grid-cols-4">
            <TrendCard icon={<Users className="size-4" />} label="Users" pct={data?.users_growth_percent} />
            <TrendCard
                icon={<Mail className="size-4" />}
                label="Emails sent"
                pct={data?.emails_growth_percent}
            />
            <TrendCard
                icon={<Megaphone className="size-4" />}
                label="Campaigns"
                pct={data?.campaigns_growth_percent}
            />
            <TrendCard
                icon={<BarChart3 className="size-4" />}
                label="Revenue"
                pct={data?.revenue_growth_percent}
            />
        </div>
    );
}

function TrendCard({ icon, label, pct }: { icon: React.ReactNode; label: string; pct?: number }) {
    const v = pct ?? 0;
    const tone = v > 0 ? "text-emerald-600" : v < 0 ? "text-red-600" : "text-muted-foreground";
    return (
        <div className="rounded-lg border border-border bg-card p-3">
            <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
                {icon}
                <span>{label}</span>
            </div>
            <div className={`mt-1 text-2xl font-semibold tabular-nums ${tone}`}>
                {pct == null ? "—" : `${v > 0 ? "+" : ""}${v.toFixed(1)}%`}
            </div>
            <div className="mt-0.5 flex items-center gap-1 text-[10px] text-muted-foreground">
                {v > 0 ? (
                    <TrendingUp className="size-3" />
                ) : v < 0 ? (
                    <TrendingDown className="size-3" />
                ) : null}
                vs. previous period
            </div>
        </div>
    );
}

function DailyEmailChart({ days }: { days: number }) {
    const { data, isLoading } = useQuery({
        queryKey: ["admin", "analytics", "emails", "daily", days],
        queryFn: () => getDailyEmailStats(days),
        staleTime: 60_000,
    });

    if (isLoading) return <Skeleton className="h-48" />;
    const rows = data ?? [];
    if (rows.length === 0) {
        return (
            <div className="rounded-md border border-border bg-card p-4 text-sm text-muted-foreground">
                No email activity in the last {days} days. Bars appear here once a campaign or
                warmup send goes out.
            </div>
        );
    }

    const maxSent = Math.max(1, ...rows.map((r) => r.total_sent));
    return (
        <div className="rounded-lg border border-border bg-card p-4">
            <div className="flex h-40 items-end gap-1">
                {rows.map((r) => (
                    <DayBar key={r.date} stat={r} maxSent={maxSent} dense={rows.length > 45} />
                ))}
            </div>
            <Legend />
        </div>
    );
}

function DayBar({ stat, maxSent, dense }: { stat: DailyEmailStat; maxSent: number; dense: boolean }) {
    const h = (n: number) => `${Math.max(1, (n / maxSent) * 100)}%`;
    const other = Math.max(0, stat.total_delivered - stat.total_replied - stat.total_bounced);
    return (
        <div
            className="group relative flex flex-1 flex-col items-center"
            title={`${new Date(stat.date).toLocaleDateString()}\nsent: ${stat.total_sent}\ndelivered: ${stat.total_delivered}\nbounced: ${stat.total_bounced}\nreplied: ${stat.total_replied}`}
        >
            <div className="flex w-full flex-1 flex-col justify-end gap-0.5">
                {stat.total_bounced > 0 && (
                    <div className="w-full bg-red-400" style={{ height: h(stat.total_bounced) }} />
                )}
                {stat.total_replied > 0 && (
                    <div className="w-full bg-emerald-400" style={{ height: h(stat.total_replied) }} />
                )}
                {other > 0 && (
                    <div className="w-full bg-[var(--admin-accent)]" style={{ height: h(other) }} />
                )}
            </div>
            {/* Ninety labels do not fit; keep every third so the axis stays readable. */}
            <div
                className={`mt-1 origin-top-left -rotate-45 whitespace-nowrap text-[8px] text-muted-foreground ${
                    dense && new Date(stat.date).getDate() % 3 !== 1 ? "invisible" : ""
                }`}
            >
                {new Date(stat.date).toLocaleDateString(undefined, {
                    month: "numeric",
                    day: "numeric",
                })}
            </div>
        </div>
    );
}

function Legend() {
    return (
        <div className="mt-3 flex items-center gap-3 text-[10px] text-muted-foreground">
            <LegendDot color="bg-[var(--admin-accent)]" label="Delivered" />
            <LegendDot color="bg-emerald-400" label="Replied" />
            <LegendDot color="bg-red-400" label="Bounced" />
        </div>
    );
}

function LegendDot({ color, label }: { color: string; label: string }) {
    return (
        <span className="inline-flex items-center gap-1">
            <span className={`inline-block size-2 rounded ${color}`} />
            {label}
        </span>
    );
}

function HourlyChart() {
    const { data, isLoading } = useQuery({
        queryKey: ["admin", "analytics", "emails", "hourly"],
        queryFn: getHourlyEmailStats,
        staleTime: 60_000,
    });

    const rows = data ?? [];
    const max = Math.max(1, ...rows.map((r) => r.total_sent));

    return (
        <div>
            <h2 className="mb-2 text-sm font-semibold">Today by hour</h2>
            {isLoading ? (
                <Skeleton className="h-40" />
            ) : (
                <div className="rounded-lg border border-border bg-card p-4">
                    {rows.length === 0 ? (
                        <p className="text-xs text-muted-foreground">
                            No sends today yet. The hourly profile fills in as workers send.
                        </p>
                    ) : (
                        <div className="flex h-32 items-end gap-1">
                            {rows.map((r) => (
                                <div
                                    key={r.hour}
                                    className="flex flex-1 flex-col items-center"
                                    title={`${r.hour}:00, ${r.total_sent} sent`}
                                >
                                    <div
                                        className="w-full bg-[var(--admin-accent)]"
                                        style={{ height: `${Math.max(1, (r.total_sent / max) * 100)}%` }}
                                    />
                                    <div className="mt-1 text-[8px] text-muted-foreground">{r.hour}</div>
                                </div>
                            ))}
                        </div>
                    )}
                </div>
            )}
        </div>
    );
}

function UserGrowthChart({ days }: { days: number }) {
    const { data, isLoading } = useQuery({
        queryKey: ["admin", "analytics", "users", "growth", days],
        queryFn: () => getUserGrowthStats(days),
        staleTime: 60_000,
    });

    if (isLoading) return <Skeleton className="h-40" />;
    const rows = data ?? [];
    if (rows.length === 0) {
        return (
            <div className="rounded-md border border-border bg-card p-4 text-sm text-muted-foreground">
                No new users in the last {days} days.
            </div>
        );
    }
    const maxNew = Math.max(1, ...rows.map((r) => r.new_users));
    return (
        <div className="rounded-lg border border-border bg-card p-4">
            <div className="flex h-32 items-end gap-1">
                {rows.map((r) => (
                    <div
                        key={r.date}
                        className="flex flex-1 flex-col items-center"
                        title={`${new Date(r.date).toLocaleDateString()}\n+${r.new_users} new (${r.total_users} total)`}
                    >
                        <div
                            className="w-full bg-emerald-500"
                            style={{ height: `${Math.max(1, (r.new_users / maxNew) * 100)}%` }}
                        />
                    </div>
                ))}
            </div>
            <div className="mt-2 text-[10px] text-muted-foreground">
                New users per day (hover for totals).
            </div>
        </div>
    );
}

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
        <Card>
            <CardHeader>
                <CardTitle>Acquisition</CardTitle>
                <CardDescription>
                    Signups over the last {days} days by the UTM channel and referrer they arrived
                    with, and how many of them converted to a paid plan.
                </CardDescription>
            </CardHeader>
            <CardContent className="space-y-4 pt-0">
                {acqQ.isLoading && <Skeleton className="h-32 w-full" />}
                {acqQ.isError && (
                    <p className="text-sm text-muted-foreground">
                        Acquisition data is unavailable on this backend.
                    </p>
                )}
                {a && (
                    <>
                        <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
                            <MiniStat icon={UserPlus} label="Signups" value={formatNum(a.signups)} />
                            <MiniStat
                                icon={BarChart3}
                                label="With a channel"
                                value={formatNum(a.with_channel)}
                            />
                            <MiniStat
                                icon={TrendingUp}
                                label="Converted"
                                value={`${formatNum(a.converted)} · ${rate}`}
                            />
                            <MiniStat
                                icon={AlertTriangle}
                                label="Trials ending, 7d"
                                value={formatNum(a.trials_expiring_7d)}
                                warn={a.trials_expiring_7d > 0}
                            />
                        </div>
                        <div className="grid gap-4 md:grid-cols-2">
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
            </CardContent>
        </Card>
    );
}

function RankedList({
    title,
    empty,
    rows,
}: {
    title: string;
    empty: string;
    rows: { key: string; label: string; value: number; note?: string; pct: number }[];
}) {
    return (
        <div>
            <div className="mb-2 text-[10px] font-semibold uppercase tracking-wider text-muted-foreground">
                {title}
            </div>
            {rows.length === 0 ? (
                <p className="text-xs text-muted-foreground">{empty}</p>
            ) : (
                <ul className="space-y-1.5">
                    {rows.map((r) => (
                        <li key={r.key} className="text-xs">
                            <div className="flex items-center justify-between gap-3">
                                <span className="truncate text-foreground">{r.label}</span>
                                <span className="shrink-0 tabular-nums text-muted-foreground">
                                    {formatNum(r.value)}
                                    {r.note && <span className="ml-1 text-emerald-600">· {r.note}</span>}
                                </span>
                            </div>
                            <div className="mt-0.5 h-1 w-full rounded bg-muted">
                                <div
                                    className="h-1 rounded bg-[var(--admin-accent)]"
                                    style={{ width: `${Math.max(2, r.pct)}%` }}
                                />
                            </div>
                        </li>
                    ))}
                </ul>
            )}
        </div>
    );
}

interface StatCardProps {
    icon: React.ComponentType<{ className?: string }>;
    label: string;
    value: string;
    sub?: string;
    loading?: boolean;
    tone?: "neutral" | "warn";
}

function StatCard({ icon: Icon, label, value, sub, loading, tone = "neutral" }: StatCardProps) {
    return (
        <Card className={tone === "warn" ? "border-amber-300 bg-amber-50/40" : undefined}>
            <CardContent className="p-4">
                <div className="flex items-center gap-2 text-xs font-medium uppercase tracking-wider text-muted-foreground">
                    <Icon className="size-3.5" />
                    {label}
                </div>
                <div className="mt-2 text-2xl font-semibold tabular-nums">
                    {loading ? <Skeleton className="h-7 w-16" /> : value}
                </div>
                {sub && <div className="mt-1 text-xs text-muted-foreground">{sub}</div>}
            </CardContent>
        </Card>
    );
}

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
        <div
            className={`rounded-md border px-3 py-2 ${
                warn ? "border-amber-200 bg-amber-50/60" : "border-border bg-white"
            }`}
        >
            <div className="flex items-center gap-1.5 text-[10px] font-semibold uppercase tracking-wider text-muted-foreground">
                <Icon className={`size-3 ${warn ? "text-amber-600" : ""}`} />
                {label}
            </div>
            <div className="mt-0.5 text-sm font-semibold tabular-nums text-foreground">{value}</div>
        </div>
    );
}
