// Deliverability dashboard — surfaces the bounce/complaint/spam-placement
// signals the platform already collects, with health bands tied to the
// documented shared-pool thresholds (see CLAUDE.md). Live-updating: the query
// is keyed under ["analytics", ...] which useRealtimeEvents already invalidates.

import { useMemo, useState } from "react";
import { Link } from "react-router-dom";
import { AlertTriangleIcon, ArrowUpRightIcon, RefreshCcwIcon } from "lucide-react";
import { EmptyBlock, Page, PageBody, PageTopbar, SectionBar, Stat, StatStrip } from "@/components/layout/Page";
import { DailyBars, type ChartPoint } from "@/components/ui/charts";
import useDeliverability from "@/lib/api/hooks/app/analytics/useDeliverability";
import type { DeliverabilityBand } from "@/lib/api/models/app/analytics/Deliverability";

type Range = "7d" | "30d" | "90d";
type Metric = "bounces" | "complaints" | "opens" | "replies" | "sent";

const RANGE_LABEL: Record<Range, string> = {
    "7d": "Last 7 days",
    "30d": "Last 30 days",
    "90d": "Last 90 days",
};

const METRICS: { key: Metric; label: string; bar: string }[] = [
    { key: "bounces", label: "Bounces", bar: "bg-rose-500" },
    { key: "complaints", label: "Complaints", bar: "bg-amber-500" },
    { key: "opens", label: "Opens", bar: "bg-emerald-500" },
    { key: "replies", label: "Replies", bar: "bg-sky-500" },
    { key: "sent", label: "Sent", bar: "bg-slate-400" },
];

const BAND_CHIP: Record<DeliverabilityBand, string> = {
    healthy: "bg-emerald-50 text-emerald-700 border-emerald-200",
    warning: "bg-amber-50 text-amber-700 border-amber-200",
    quarantine: "bg-orange-50 text-orange-700 border-orange-200",
    blocked: "bg-rose-50 text-rose-700 border-rose-200",
};
const BAND_DOT: Record<DeliverabilityBand, string> = {
    healthy: "bg-emerald-500",
    warning: "bg-amber-500",
    quarantine: "bg-orange-500",
    blocked: "bg-rose-500",
};
const BAND_LABEL: Record<DeliverabilityBand, string> = {
    healthy: "Healthy",
    warning: "Watch",
    quarantine: "Quarantine",
    blocked: "Blocked",
};

function pct(v: number | null | undefined): string {
    return v == null ? "—" : `${v.toFixed(2)}%`;
}
function num(v: number | undefined): string {
    return (v ?? 0).toLocaleString();
}

function BandChip({ band }: { band: DeliverabilityBand }) {
    return (
        <span className={`inline-flex items-center rounded px-1.5 h-5 text-[10px] font-medium border ${BAND_CHIP[band]}`}>
            {BAND_LABEL[band]}
        </span>
    );
}

export default function DeliverabilityPage() {
    const [range, setRange] = useState<Range>("7d");
    const [metric, setMetric] = useState<Metric>("bounces");
    const q = useDeliverability(range);
    const d = q.data;

    const series: ChartPoint[] = useMemo(
        () => (d?.timeseries ?? []).map((p) => ({ label: p.date, value: p[metric] ?? 0 })),
        [d?.timeseries, metric],
    );

    return (
        <Page>
            <PageTopbar eyebrow="Deliverability" subtitle="Bounce, complaint, and inbox-placement health across your mailboxes">
                <RangeTabs value={range} onChange={setRange} />
            </PageTopbar>

            <StatStrip cols={4}>
                <Stat
                    label="Bounce rate"
                    value={pct(d?.bounce_rate)}
                    sub={`${num(d?.bounce_count)} of ${num(d?.emails_sent)} sent`}
                    accent={!!d && d.bounce_rate >= 5}
                />
                <Stat label="Complaint rate" value={pct(d?.complaint_rate)} sub={`${num(d?.complaint_count)} complaints`} accent={!!d && d.complaint_rate >= 0.1} />
                <Stat
                    label="Spam placement"
                    value={d?.placement_samples ? pct(d?.spam_placement_rate) : "—"}
                    sub={d?.placement_samples ? `${num(d?.placement_samples)} seed samples` : "no seed data"}
                />
                <Stat label="Suppressed" value={num(d?.suppressed_recipients)} sub="active suppressions" last />
            </StatStrip>

            {q.isError ? (
                <PageBody>
                    <ErrorState onRetry={() => q.refetch()} isRefetching={q.isFetching} />
                </PageBody>
            ) : (
                <>
                    <div className="grid lg:grid-cols-[1fr_320px] min-h-0 flex-1">
                        <section className="flex flex-col min-h-0 lg:border-r lg:border-slate-200">
                            <SectionBar label="Over time">
                                <div className="inline-flex items-center gap-0.5 rounded-md bg-slate-100 p-0.5 max-w-full overflow-x-auto">
                                    {METRICS.map((m) => (
                                        <button
                                            key={m.key}
                                            onClick={() => setMetric(m.key)}
                                            className={`h-6 px-2 rounded text-[11px] font-medium transition-colors ${
                                                metric === m.key ? "bg-white text-slate-900 shadow-sm" : "text-slate-500 hover:text-slate-900"
                                            }`}
                                        >
                                            {m.label}
                                        </button>
                                    ))}
                                </div>
                            </SectionBar>
                            <div className="flex-1 px-5 py-4">
                                {q.isPending ? (
                                    <div className="h-52 rounded-md bg-slate-50 animate-pulse" />
                                ) : (
                                    <DailyBars
                                        points={series}
                                        height={220}
                                        barClass={METRICS.find((m) => m.key === metric)?.bar}
                                        emptyLabel="No activity in this window yet"
                                    />
                                )}
                            </div>
                        </section>

                        <aside className="flex flex-col min-h-0 bg-slate-50/40">
                            <SectionBar label="Window totals" />
                            <div className="divide-y divide-slate-200/60">
                                {[
                                    { label: "Sent", value: d?.emails_sent, dot: "bg-slate-400" },
                                    { label: "Opens", value: d?.open_count, dot: "bg-emerald-500" },
                                    { label: "Clicks", value: d?.click_count, dot: "bg-violet-500" },
                                    { label: "Replies", value: d?.reply_count, dot: "bg-sky-500" },
                                    { label: "Bounces", value: d?.bounce_count, dot: "bg-rose-500" },
                                    { label: "Complaints", value: d?.complaint_count, dot: "bg-amber-500" },
                                    { label: "Unsubscribes", value: d?.unsubscribe_count, dot: "bg-orange-500" },
                                ].map((row) => (
                                    <div key={row.label} className="h-9 px-4 flex items-center gap-2">
                                        <span className={`size-1.5 rounded-full ${row.dot}`} />
                                        <span className="text-[12px] text-slate-700">{row.label}</span>
                                        <span className="ml-auto font-mono text-[11px] text-slate-500 tabular-nums">{q.isPending ? "—" : num(row.value)}</span>
                                    </div>
                                ))}
                            </div>
                            {d?.placement_samples ? (
                                <>
                                    <SectionBar label="Inbox placement" />
                                    <div className="px-4 py-3 grid grid-cols-2 gap-2 text-center">
                                        <div>
                                            <div className="text-[18px] font-semibold text-emerald-600 tabular-nums">{pct(d.inbox_placement_rate)}</div>
                                            <div className="text-[10px] uppercase tracking-[0.14em] text-slate-400">Inbox</div>
                                        </div>
                                        <div>
                                            <div className="text-[18px] font-semibold text-rose-600 tabular-nums">{pct(d.spam_placement_rate)}</div>
                                            <div className="text-[10px] uppercase tracking-[0.14em] text-slate-400">Spam</div>
                                        </div>
                                    </div>
                                </>
                            ) : null}
                        </aside>
                    </div>

                    <SectionBar label="Mailboxes at risk" count={d?.by_mailbox?.length || undefined}>
                        <Link to="/app/mailboxes" className="inline-flex items-center gap-1 text-[11px] text-slate-500 hover:text-slate-900 transition-colors">
                            All mailboxes
                            <ArrowUpRightIcon className="w-3 h-3" />
                        </Link>
                    </SectionBar>
                    {q.isPending ? (
                        <SkeletonRows />
                    ) : (d?.by_mailbox?.length ?? 0) === 0 ? (
                        <EmptyBlock title="No bounce or complaint activity" body="Mailboxes with bounces or complaints in this window will show up here, ranked by risk." />
                    ) : (
                        <div className="divide-y divide-slate-200/60">
                            {d!.by_mailbox.map((m) => (
                                <div key={m.email_account_id} className="h-11 px-5 flex items-center gap-3">
                                    <span className={`size-1.5 rounded-full shrink-0 ${BAND_DOT[m.band]}`} />
                                    <span className="text-[12.5px] font-medium text-slate-900 truncate max-w-[36%]">{m.email}</span>
                                    <span className="ml-auto flex items-center gap-2 md:gap-4 font-mono text-[11px] text-slate-500 tabular-nums shrink-0">
                                        <span title="Sent" className="hidden md:inline">{num(m.sent)} sent</span>
                                        <span title="Bounce rate" className="text-rose-600">{pct(m.bounce_rate)} bounce</span>
                                        <span title="Complaint rate" className="hidden md:inline text-amber-600">{pct(m.complaint_rate)} spam</span>
                                    </span>
                                    <BandChip band={m.band} />
                                </div>
                            ))}
                        </div>
                    )}

                    <SectionBar label="Campaigns at risk" count={d?.by_campaign?.length || undefined} />
                    <PageBody>
                        {q.isPending ? (
                            <SkeletonRows />
                        ) : (d?.by_campaign?.length ?? 0) === 0 ? (
                            <EmptyBlock title="No campaign deliverability issues" body="Campaigns with bounces or complaints in this window will appear here." />
                        ) : (
                            <div className="divide-y divide-slate-200/60">
                                {d!.by_campaign.map((c) => (
                                    <Link
                                        key={c.campaign_id}
                                        to={`/app/campaigns/${c.campaign_id}`}
                                        className="group h-11 px-5 flex items-center gap-3 hover:bg-slate-50 transition-colors"
                                    >
                                        <span className={`size-1.5 rounded-full shrink-0 ${BAND_DOT[c.band]}`} />
                                        <span className="text-[12.5px] font-medium text-slate-900 truncate max-w-[36%]">{c.name}</span>
                                        <span className="ml-auto flex items-center gap-2 md:gap-4 font-mono text-[11px] text-slate-500 tabular-nums shrink-0">
                                            <span title="Sent" className="hidden md:inline">{num(c.sent)} sent</span>
                                            <span title="Bounce rate" className="text-rose-600">{pct(c.bounce_rate)} bounce</span>
                                            <span title="Complaint rate" className="hidden md:inline text-amber-600">{pct(c.complaint_rate)} spam</span>
                                        </span>
                                        <BandChip band={c.band} />
                                    </Link>
                                ))}
                            </div>
                        )}
                    </PageBody>
                </>
            )}
        </Page>
    );
}

function RangeTabs({ value, onChange }: { value: Range; onChange: (r: Range) => void }) {
    return (
        <div className="inline-flex items-center gap-0.5 rounded-md bg-slate-100 p-0.5">
            {(Object.keys(RANGE_LABEL) as Range[]).map((r) => (
                <button
                    key={r}
                    onClick={() => onChange(r)}
                    className={`h-7 px-2.5 rounded text-[12px] font-medium transition-colors ${
                        value === r ? "bg-white text-slate-900 shadow-sm" : "text-slate-500 hover:text-slate-900"
                    }`}
                >
                    {r}
                </button>
            ))}
        </div>
    );
}

function SkeletonRows() {
    return (
        <div className="divide-y divide-slate-200/60">
            {Array.from({ length: 3 }).map((_, i) => (
                <div key={i} className="h-11 px-5 flex items-center gap-3">
                    <div className="size-1.5 rounded-full bg-slate-200" />
                    <div className="h-3 w-40 bg-slate-100 rounded animate-pulse" />
                    <div className="ml-auto h-3 w-32 bg-slate-100 rounded animate-pulse" />
                </div>
            ))}
        </div>
    );
}

function ErrorState({ onRetry, isRefetching }: { onRetry: () => void; isRefetching: boolean }) {
    return (
        <div className="px-5 py-16 flex flex-col items-center text-center gap-3">
            <span className="inline-flex size-9 items-center justify-center rounded-full bg-rose-50 text-rose-600">
                <AlertTriangleIcon className="w-4 h-4" />
            </span>
            <div className="text-[13px] font-medium text-slate-900">Couldn't load deliverability</div>
            <button
                type="button"
                onClick={onRetry}
                disabled={isRefetching}
                className="h-7 px-3 rounded-md border border-slate-200 hover:border-slate-300 text-[12px] text-slate-700 inline-flex items-center gap-1.5 disabled:opacity-60"
            >
                <RefreshCcwIcon className={`w-3.5 h-3.5 ${isRefetching ? "animate-spin" : ""}`} />
                Retry
            </button>
        </div>
    );
}
