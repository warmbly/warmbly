// Deliverability dashboard — surfaces the bounce/complaint/spam-placement
// signals the platform already collects, with health bands tied to the
// documented shared-pool thresholds (see CLAUDE.md). Live-updating: the query
// is keyed under ["analytics", ...] which useRealtimeEvents already invalidates.
// The visible sections and the time window are user-customizable and persisted.

import { useMemo, useState } from "react";
import { Link } from "react-router-dom";
import { AlertTriangleIcon, ArrowUpRightIcon, CheckIcon, RefreshCcwIcon, SlidersHorizontalIcon } from "lucide-react";
import { EmptyBlock, Page, PageBody, PageTopbar, SectionBar, Stat, StatStrip } from "@/components/layout/Page";
import { MultiTrend, type TrendSeries } from "@/components/ui/charts";
import { TONE_DOT } from "@/components/ui/tones";
import { DitherStack, type DitherTone } from "@/components/ui/dither";
import {
    PopoverMenu,
    PopoverMenuContent,
    PopoverMenuItem,
    PopoverMenuLabel,
    PopoverMenuTrigger,
    SelectButton,
} from "@/components/ui/popover-menu";
import useDeliverability from "@/lib/api/hooks/app/analytics/useDeliverability";
import type { DeliverabilityBand, ProviderPlacement, WarmupDomainPlacement } from "@/lib/api/models/app/analytics/Deliverability";

type Range = "7d" | "30d" | "90d";
type Metric = "bounces" | "complaints" | "opens" | "replies" | "sent";
type SectionKey = "chart" | "totals" | "providers" | "warmup" | "mailboxes" | "campaigns";

const RANGE_LABEL: Record<Range, string> = {
    "7d": "Last 7 days",
    "30d": "Last 30 days",
    "90d": "Last 90 days",
};

const METRICS: { key: Metric; label: string; tone: DitherTone }[] = [
    { key: "bounces", label: "Bounces", tone: "rose" },
    { key: "complaints", label: "Complaints", tone: "amber" },
    { key: "opens", label: "Opens", tone: "emerald" },
    { key: "replies", label: "Replies", tone: "sky" },
    { key: "sent", label: "Sent", tone: "slate" },
];

const SECTIONS: { key: SectionKey; label: string }[] = [
    { key: "chart", label: "Over time" },
    { key: "totals", label: "Window totals" },
    { key: "providers", label: "Placement by provider" },
    { key: "warmup", label: "Warmup placement by domain" },
    { key: "mailboxes", label: "Mailboxes at risk" },
    { key: "campaigns", label: "Campaigns at risk" },
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

const PROVIDER_LABEL: Record<string, string> = {
    gmail: "Gmail",
    outlook: "Outlook",
    smtp_imap: "IMAP / SMTP",
};

function providerLabel(p: string): string {
    return PROVIDER_LABEL[p] ?? (p ? p : "Other");
}

function pct(v: number | null | undefined): string {
    return v == null ? "—" : `${v.toFixed(2)}%`;
}
function num(v: number | undefined): string {
    return (v ?? 0).toLocaleString();
}

// View preferences (window + hidden sections), persisted per browser.
const VIEW_KEY = "warmbly:deliverability-view";

interface ViewPrefs {
    range: Range;
    hidden: SectionKey[];
}

function loadView(): ViewPrefs {
    try {
        const raw = localStorage.getItem(VIEW_KEY);
        if (raw) {
            const v = JSON.parse(raw) as Partial<ViewPrefs>;
            return {
                range: v.range === "30d" || v.range === "90d" ? v.range : "7d",
                hidden: Array.isArray(v.hidden) ? v.hidden.filter((k): k is SectionKey => SECTIONS.some((s) => s.key === k)) : [],
            };
        }
    } catch {
        // Corrupt or unavailable storage falls back to defaults.
    }
    return { range: "7d", hidden: [] };
}

function BandChip({ band }: { band: DeliverabilityBand }) {
    return (
        <span className={`inline-flex items-center rounded px-1.5 h-5 text-[10px] font-medium border ${BAND_CHIP[band]}`}>
            {BAND_LABEL[band]}
        </span>
    );
}

export default function DeliverabilityPage() {
    const [view, setView] = useState<ViewPrefs>(loadView);
    // Legend toggles: every metric charts together; "sent" starts hidden so
    // it doesn't dwarf the failure signals this page exists to surface.
    const [hiddenMetrics, setHiddenMetrics] = useState<Metric[]>(["sent"]);
    const toggleMetric = (k: Metric) =>
        setHiddenMetrics((cur) => {
            if (cur.includes(k)) return cur.filter((x) => x !== k);
            // Keep at least one series on the graph.
            if (cur.length >= METRICS.length - 1) return cur;
            return [...cur, k];
        });
    const q = useDeliverability(view.range);
    const d = q.data;

    const updateView = (next: ViewPrefs) => {
        setView(next);
        try {
            localStorage.setItem(VIEW_KEY, JSON.stringify(next));
        } catch {
            // Persisting is best-effort; the in-memory view still applies.
        }
    };
    const show = (k: SectionKey) => !view.hidden.includes(k);
    const toggleSection = (k: SectionKey) =>
        updateView({ ...view, hidden: show(k) ? [...view.hidden, k] : view.hidden.filter((h) => h !== k) });

    const trend = useMemo(() => {
        const rows = d?.timeseries ?? [];
        const series: TrendSeries[] = METRICS.filter((m) => !hiddenMetrics.includes(m.key)).map((m) => ({
            key: m.key,
            label: m.label,
            tone: m.tone,
            values: rows.map((p) => p[m.key] ?? 0),
        }));
        return { labels: rows.map((p) => p.date), series };
    }, [d?.timeseries, hiddenMetrics]);

    // The score is meaningless before any signal exists; show a dash instead
    // of a perfect 100 on an empty window.
    const hasActivity =
        !!d && (d.emails_sent > 0 || d.events_total > 0 || d.placement_samples > 0 || (d.warmup_placement?.length ?? 0) > 0);

    return (
        <Page>
            <PageTopbar eyebrow="Deliverability" subtitle="Inbox placement, bounce, and complaint health across your mailboxes">
                <CustomizeMenu hidden={view.hidden} onToggle={toggleSection} />
                <RangeTabs value={view.range} onChange={(range) => updateView({ ...view, range })} />
            </PageTopbar>

            <StatStrip cols={5}>
                <Stat
                    label="Deliverability score"
                    value={
                        hasActivity ? (
                            <span className="inline-flex items-center gap-2">
                                {d!.score}
                                <BandChip band={d!.band} />
                            </span>
                        ) : (
                            "—"
                        )
                    }
                    sub={hasActivity ? "out of 100" : "no activity in this window"}
                />
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
                    {(show("chart") || show("totals")) && (
                        <div className={`grid min-h-0 ${show("chart") && show("totals") ? "lg:grid-cols-[1fr_320px]" : ""}`}>
                            {show("chart") && (
                                <section className="flex flex-col min-h-0 lg:border-r lg:border-slate-200">
                                    <SectionBar label="Over time">
                                        <div className="inline-flex items-center gap-0.5 rounded-md bg-slate-100 p-0.5 max-w-full overflow-x-auto">
                                            {METRICS.map((m) => {
                                                const visible = !hiddenMetrics.includes(m.key);
                                                return (
                                                    <button
                                                        key={m.key}
                                                        onClick={() => toggleMetric(m.key)}
                                                        className={`h-6 px-2 rounded text-[11px] font-medium transition-colors inline-flex items-center gap-1.5 ${
                                                            visible ? "bg-white text-slate-900 shadow-sm" : "text-slate-400 hover:text-slate-600"
                                                        }`}
                                                    >
                                                        <span className={`size-1.5 rounded-full ${visible ? TONE_DOT[m.tone] : "bg-slate-300"}`} />
                                                        {m.label}
                                                    </button>
                                                );
                                            })}
                                        </div>
                                    </SectionBar>
                                    <div className="flex-1 px-5 py-4">
                                        {q.isPending ? (
                                            <div className="h-52 rounded-md bg-slate-50 animate-pulse" />
                                        ) : (
                                            <MultiTrend
                                                labels={trend.labels}
                                                series={trend.series}
                                                height={220}
                                                emptyLabel="No activity in this window yet"
                                            />
                                        )}
                                    </div>
                                </section>
                            )}

                            {show("totals") && (
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
                            )}
                        </div>
                    )}

                    {show("providers") && (
                        <>
                            <SectionBar label="Placement by provider" count={d?.by_provider?.length || undefined} />
                            {q.isPending ? (
                                <SkeletonRows />
                            ) : (d?.by_provider?.length ?? 0) === 0 ? (
                                <EmptyBlock
                                    title="No placement test samples in this window"
                                    body="Run a seed placement test to see where your mail lands (inbox, promotions, or spam) at each provider."
                                />
                            ) : (
                                <div className="divide-y divide-slate-200/60">
                                    {d!.by_provider.map((p) => (
                                        <ProviderRow key={p.provider} p={p} />
                                    ))}
                                </div>
                            )}
                        </>
                    )}

                    {show("warmup") && (
                        <>
                            <SectionBar label="Warmup placement by domain" count={d?.warmup_placement?.length || undefined} />
                            {q.isPending ? (
                                <SkeletonRows />
                            ) : (d?.warmup_placement?.length ?? 0) === 0 ? (
                                <EmptyBlock
                                    title="No warmup deliveries in this window"
                                    body="Once warmup is running, every verified delivery reports whether it reached the inbox or the spam folder, broken down by the recipient's domain."
                                />
                            ) : (
                                <div className="divide-y divide-slate-200/60">
                                    {d!.warmup_placement.map((w) => (
                                        <WarmupDomainRow key={w.domain} w={w} />
                                    ))}
                                </div>
                            )}
                        </>
                    )}

                    {show("mailboxes") && (
                        <>
                            <SectionBar label="Mailboxes at risk" count={d?.by_mailbox?.length || undefined}>
                                <Link to="/app/emails" className="inline-flex items-center gap-1 text-[11px] text-slate-500 hover:text-slate-900 transition-colors">
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
                        </>
                    )}

                    {show("campaigns") && (
                        <SectionBar label="Campaigns at risk" count={d?.by_campaign?.length || undefined} />
                    )}
                    <PageBody>
                        {show("campaigns") &&
                            (q.isPending ? (
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
                            ))}
                    </PageBody>
                </>
            )}
        </Page>
    );
}

// One provider's seed placement rollup: a stacked inbox/promotions/spam/other
// bar plus the two rates that matter.
function ProviderRow({ p }: { p: ProviderPlacement }) {
    const segments = [
        { n: p.inbox, tone: "emerald" as DitherTone, label: "Inbox" },
        { n: p.promotions, tone: "violet" as DitherTone, label: "Promotions" },
        { n: p.spam, tone: "rose" as DitherTone, label: "Spam" },
        { n: p.other, tone: "slate" as DitherTone, label: "Other" },
    ].filter((s) => s.n > 0);
    return (
        <div className="h-11 px-5 flex items-center gap-3">
            <span className="text-[12.5px] font-medium text-slate-900 w-28 shrink-0 truncate">{providerLabel(p.provider)}</span>
            <div className="flex-1 min-w-16" title={segments.map((s) => `${s.label} ${s.n}`).join(" · ")}>
                <DitherStack
                    segments={segments.map((s) => ({ frac: s.n / Math.max(1, p.samples), tone: s.tone }))}
                    height={6}
                />
            </div>
            <span className="flex items-center gap-2 md:gap-4 font-mono text-[11px] tabular-nums shrink-0">
                <span title="Inbox rate" className="text-emerald-600">{pct(p.inbox_rate)} inbox</span>
                <span title="Spam rate" className="text-rose-600">{pct(p.spam_rate)} spam</span>
                <span title="Samples" className="hidden md:inline text-slate-500">{num(p.samples)} samples</span>
            </span>
        </div>
    );
}

// One recipient domain's continuous warmup placement signal.
function WarmupDomainRow({ w }: { w: WarmupDomainPlacement }) {
    return (
        <div className="h-11 px-5 flex items-center gap-3">
            <span className={`size-1.5 rounded-full shrink-0 ${w.spam_rate >= 20 ? "bg-rose-500" : w.spam_rate >= 10 ? "bg-amber-500" : "bg-emerald-500"}`} />
            <span className="text-[12.5px] font-medium text-slate-900 truncate max-w-[36%]">{w.domain}</span>
            <span className="hidden md:inline text-[11px] text-slate-400">{providerLabel(w.provider)}</span>
            <span className="ml-auto flex items-center gap-2 md:gap-4 font-mono text-[11px] tabular-nums shrink-0">
                <span title="Warmup deliveries" className="hidden md:inline text-slate-500">{num(w.delivered)} delivered</span>
                <span title="Inbox rate" className="text-emerald-600">{pct(w.inbox_rate)} inbox</span>
                <span title="Spam rate" className="text-rose-600">{pct(w.spam_rate)} spam</span>
            </span>
        </div>
    );
}

function CustomizeMenu({ hidden, onToggle }: { hidden: SectionKey[]; onToggle: (k: SectionKey) => void }) {
    return (
        <PopoverMenu align="end">
            <PopoverMenuTrigger asChild>
                <SelectButton icon={<SlidersHorizontalIcon className="w-3.5 h-3.5" />} label="Customize" />
            </PopoverMenuTrigger>
            <PopoverMenuContent minWidth={230}>
                <PopoverMenuLabel>Visible sections</PopoverMenuLabel>
                {SECTIONS.map((s) => {
                    const on = !hidden.includes(s.key);
                    return (
                        <PopoverMenuItem key={s.key} closeOnSelect={false} onSelect={() => onToggle(s.key)} trailing={<CheckSquare on={on} />}>
                            {s.label}
                        </PopoverMenuItem>
                    );
                })}
            </PopoverMenuContent>
        </PopoverMenu>
    );
}

function CheckSquare({ on }: { on: boolean }) {
    return (
        <span
            className={`inline-flex size-3.5 items-center justify-center rounded-[3px] border transition-colors ${
                on ? "bg-sky-600 border-sky-600 text-white" : "border-slate-300 bg-white text-transparent"
            }`}
        >
            <CheckIcon className="w-2.5 h-2.5" strokeWidth={3} />
        </span>
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
