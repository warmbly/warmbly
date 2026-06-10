import { useMemo, useState } from "react";
import { Link } from "react-router-dom";
import {
    ActivityIcon,
    AlertTriangleIcon,
    ArrowUpRightIcon,
    Loader2Icon,
    MailCheckIcon,
    MousePointerClickIcon,
    RefreshCcwIcon,
    ReplyIcon,
    SendIcon,
    TriangleAlertIcon,
} from "lucide-react";
import {
    EmptyBlock,
    Page,
    PageBody,
    PageTopbar,
    SectionBar,
    Stat,
    StatStrip,
} from "@/components/layout/Page";
import { DailyBars, type ChartPoint } from "@/components/ui/charts";
import AnalyticsShareButton from "@/components/app/analytics/AnalyticsShareButton";
import useDashboard from "@/lib/api/hooks/app/analytics/useDashboard";

type Range = "7d" | "30d" | "90d";
type Metric = "sent" | "opens" | "clicks" | "replies";

const RANGE_LABEL: Record<Range, string> = {
    "7d": "Last 7 days",
    "30d": "Last 30 days",
    "90d": "Last 90 days",
};

const METRICS: { key: Metric; label: string; bar: string }[] = [
    { key: "sent", label: "Sent", bar: "bg-sky-500" },
    { key: "opens", label: "Opens", bar: "bg-emerald-500" },
    { key: "clicks", label: "Clicks", bar: "bg-violet-500" },
    { key: "replies", label: "Replies", bar: "bg-amber-500" },
];

function pct(v: number | undefined): string {
    return v == null ? "—" : `${v.toFixed(1)}%`;
}
function num(v: number | undefined): string {
    return (v ?? 0).toLocaleString();
}

export default function AnalyticsPage() {
    const [range, setRange] = useState<Range>("7d");
    const [metric, setMetric] = useState<Metric>("sent");
    const dash = useDashboard(range);
    const d = dash.data;
    const os = d?.overall_stats;

    const series: ChartPoint[] = useMemo(
        () => (d?.daily_trend ?? []).map((p) => ({ label: p.date, value: p[metric] ?? 0 })),
        [d?.daily_trend, metric],
    );

    const breakdown = [
        { label: "Sent", value: os?.total_emails_sent, icon: SendIcon, dot: "bg-slate-400" },
        { label: "Opens", value: os?.total_opens, icon: MailCheckIcon, dot: "bg-emerald-500" },
        { label: "Clicks", value: os?.total_clicks, icon: MousePointerClickIcon, dot: "bg-violet-500" },
        { label: "Replies", value: os?.total_replies, icon: ReplyIcon, dot: "bg-amber-500" },
        { label: "Bounces", value: os?.total_bounces, icon: TriangleAlertIcon, dot: "bg-rose-500" },
    ];

    const shareData = {
        title: "Workspace performance",
        subtitle: RANGE_LABEL[range],
        metrics: [
            { label: "Sent", value: num(os?.total_emails_sent), sub: "emails" },
            { label: "Open rate", value: pct(os?.open_rate) },
            { label: "Reply rate", value: pct(os?.reply_rate) },
            { label: "Bounce rate", value: pct(os?.bounce_rate) },
        ],
        daily: (d?.daily_trend ?? []).map((p) => ({ label: p.date, value: p.sent })),
    };

    return (
        <Page>
            <PageTopbar eyebrow="Analytics" subtitle="Deliverability across the workspace">
                <RangeTabs value={range} onChange={setRange} />
                <AnalyticsShareButton data={shareData} filename={`warmbly-analytics-${range}.png`} />
            </PageTopbar>

            <StatStrip cols={4}>
                <Stat label="Total sent" value={num(os?.total_emails_sent)} sub="in range" accent={!!os && os.total_emails_sent > 0} />
                <Stat label="Open rate" value={pct(os?.open_rate)} sub="after delivery" />
                <Stat label="Reply rate" value={pct(os?.reply_rate)} sub="incl. positive" />
                <Stat label="Bounce rate" value={pct(os?.bounce_rate)} sub="hard + soft" last />
            </StatStrip>

            {dash.isError ? (
                <PageBody>
                    <ErrorState onRetry={() => dash.refetch()} isRefetching={dash.isFetching} />
                </PageBody>
            ) : (
                <>
                    <div className="grid lg:grid-cols-[1fr_300px] min-h-0 flex-1">
                        <section className="flex flex-col min-h-0 lg:border-r lg:border-slate-200">
                            <SectionBar label="Email performance">
                                <div className="inline-flex items-center gap-0.5 rounded-md border border-slate-200 bg-white p-0.5 max-w-full overflow-x-auto">
                                    {METRICS.map((m) => (
                                        <button
                                            key={m.key}
                                            onClick={() => setMetric(m.key)}
                                            className={`h-6 px-2 rounded text-[11px] font-medium transition-colors ${
                                                metric === m.key
                                                    ? "bg-slate-900 text-white"
                                                    : "text-slate-500 hover:text-slate-900"
                                            }`}
                                        >
                                            {m.label}
                                        </button>
                                    ))}
                                </div>
                            </SectionBar>
                            <div className="flex-1 px-5 py-4">
                                {dash.isPending ? (
                                    <div className="h-52 rounded-md bg-slate-50 animate-pulse" />
                                ) : (
                                    <DailyBars
                                        points={series}
                                        height={220}
                                        barClass={METRICS.find((m) => m.key === metric)?.bar}
                                        emptyLabel="No sends in this window yet"
                                    />
                                )}
                            </div>
                        </section>

                        <aside className="flex flex-col min-h-0 bg-slate-50/40">
                            <SectionBar label="Breakdown" />
                            <div className="divide-y divide-slate-200/60">
                                {breakdown.map((q) => (
                                    <div key={q.label} className="h-9 px-4 flex items-center gap-2">
                                        <span className={`size-1.5 rounded-full ${q.dot}`} />
                                        <span className="text-[12px] text-slate-700">{q.label}</span>
                                        <span className="ml-auto font-mono text-[11px] text-slate-500 tabular-nums">
                                            {dash.isPending ? "—" : num(q.value)}
                                        </span>
                                    </div>
                                ))}
                            </div>
                            {d?.account_health && (
                                <>
                                    <SectionBar label="Account health" />
                                    <div className="px-4 py-3 grid grid-cols-3 gap-2 text-center">
                                        <HealthCell n={d.account_health.healthy_accounts} label="Healthy" tone="text-emerald-600" />
                                        <HealthCell n={d.account_health.warning_accounts} label="At risk" tone="text-amber-600" />
                                        <HealthCell n={d.account_health.error_accounts} label="Issues" tone="text-rose-600" />
                                    </div>
                                </>
                            )}
                        </aside>
                    </div>

                    <SectionBar label="Top campaigns">
                        <Link
                            to="/app/campaigns"
                            className="inline-flex items-center gap-1 text-[11px] text-slate-500 hover:text-slate-900 transition-colors"
                        >
                            All campaigns
                            <ArrowUpRightIcon className="w-3 h-3" />
                        </Link>
                    </SectionBar>
                    {dash.isPending ? (
                        <div className="divide-y divide-slate-200/60">
                            {Array.from({ length: 3 }).map((_, i) => (
                                <div key={i} className="h-11 px-5 flex items-center gap-3">
                                    <div className="size-1.5 rounded-full bg-slate-200" />
                                    <div className="h-3 w-40 bg-slate-100 rounded animate-pulse" />
                                    <div className="ml-auto h-3 w-24 bg-slate-100 rounded animate-pulse" />
                                </div>
                            ))}
                        </div>
                    ) : (d?.top_campaigns?.length ?? 0) === 0 ? (
                        <EmptyBlock title="No campaign sends yet" body="Once a campaign starts sending, your best performers show up here." />
                    ) : (
                        <div className="divide-y divide-slate-200/60">
                            {d!.top_campaigns.map((c) => {
                                const dot =
                                    c.status === "active" ? "bg-emerald-500" : c.status === "paused" ? "bg-amber-500" : "bg-slate-300";
                                return (
                                    <Link
                                        key={c.campaign_id}
                                        to={`/app/campaigns/${c.campaign_id}`}
                                        className="group h-11 px-5 flex items-center gap-3 hover:bg-slate-50 transition-colors"
                                    >
                                        <span className={`size-1.5 rounded-full shrink-0 ${dot}`} />
                                        <span className="text-[12.5px] font-medium text-slate-900 truncate max-w-[40%]">{c.name}</span>
                                        <span className="ml-auto flex items-center gap-2 md:gap-4 font-mono text-[11px] text-slate-500 tabular-nums shrink-0">
                                            <span title="Emails sent">{num(c.emails_sent)} sent</span>
                                            <span title="Open rate" className="hidden md:inline text-emerald-600">{pct(c.open_rate)} open</span>
                                            <span title="Reply rate" className="text-amber-600">{pct(c.reply_rate)} reply</span>
                                        </span>
                                    </Link>
                                );
                            })}
                        </div>
                    )}

                    <SectionBar label="Recent activity" />
                    <PageBody>
                        {dash.isPending ? (
                            <div className="divide-y divide-slate-200/60">
                                {Array.from({ length: 4 }).map((_, i) => (
                                    <div key={i} className="h-10 px-5 flex items-center gap-3">
                                        <div className="size-4 rounded-full bg-slate-100" />
                                        <div className="h-3 w-64 bg-slate-100 rounded animate-pulse" />
                                    </div>
                                ))}
                            </div>
                        ) : (d?.recent_activity?.length ?? 0) === 0 ? (
                            <EmptyBlock title="Nothing yet" body="Opens, clicks, replies and bounces will stream in here as your campaigns send." />
                        ) : (
                            <div className="divide-y divide-slate-200/60">
                                {d!.recent_activity.map((a, i) => (
                                    <ActivityRow key={i} a={a} />
                                ))}
                            </div>
                        )}
                    </PageBody>
                </>
            )}
        </Page>
    );
}

function HealthCell({ n, label, tone }: { n: number; label: string; tone: string }) {
    return (
        <div>
            <div className={`font-mono text-[18px] tabular-nums leading-none ${tone}`}>{n}</div>
            <div className="text-[10px] uppercase tracking-[0.12em] text-slate-400 mt-1">{label}</div>
        </div>
    );
}

const ACTIVITY_TONE: Record<string, { tone: string; verb: string }> = {
    sent: { tone: "text-slate-500", verb: "Sent to" },
    opened: { tone: "text-emerald-600", verb: "Opened by" },
    clicked: { tone: "text-violet-600", verb: "Clicked by" },
    replied: { tone: "text-amber-600", verb: "Reply from" },
    bounced: { tone: "text-rose-600", verb: "Bounced" },
};

function ActivityRow({ a }: { a: { type: string; campaign_name: string; contact_email: string; timestamp: string } }) {
    const meta = ACTIVITY_TONE[a.type] ?? { tone: "text-slate-500", verb: a.type };
    return (
        <div className="h-10 px-5 flex items-center gap-3">
            <ActivityIcon className={`w-3.5 h-3.5 shrink-0 ${meta.tone}`} />
            <span className="text-[12.5px] text-slate-900 truncate">
                <span className={meta.tone}>{meta.verb}</span> {a.contact_email}
            </span>
            <span className="text-[11.5px] text-slate-400 truncate hidden md:inline">{a.campaign_name}</span>
            <span className="ml-auto font-mono text-[10.5px] text-slate-400 tabular-nums shrink-0">
                <span className="md:hidden">
                    {new Date(a.timestamp).toLocaleString("en-US", { month: "short", day: "numeric" })}
                </span>
                <span className="hidden md:inline">
                    {new Date(a.timestamp).toLocaleString("en-US", { month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" })}
                </span>
            </span>
        </div>
    );
}

function RangeTabs({ value, onChange }: { value: Range; onChange: (v: Range) => void }) {
    const opts: Range[] = ["7d", "30d", "90d"];
    return (
        <div className="inline-flex items-center gap-0.5 rounded-md border border-slate-200 bg-white p-0.5">
            {opts.map((o) => (
                <button
                    key={o}
                    onClick={() => onChange(o)}
                    className={`h-6 px-2 rounded text-[11px] font-medium tabular-nums transition-colors ${
                        value === o ? "bg-slate-900 text-white" : "text-slate-500 hover:text-slate-900"
                    }`}
                >
                    {o}
                </button>
            ))}
        </div>
    );
}

function ErrorState({ onRetry, isRefetching }: { onRetry: () => void; isRefetching: boolean }) {
    return (
        <div className="px-5 py-12 text-center">
            <div className="mx-auto mb-3 size-8 rounded-md bg-rose-50 text-rose-600 flex items-center justify-center">
                <AlertTriangleIcon className="w-4 h-4" />
            </div>
            <p className="text-[12.5px] text-slate-900 font-medium">Couldn't load analytics</p>
            <p className="text-[11.5px] text-slate-500 mt-1 max-w-[44ch] mx-auto leading-relaxed">
                The request failed. The backend may be down or returning an error.
            </p>
            <button
                type="button"
                onClick={onRetry}
                disabled={isRefetching}
                className="mt-4 h-7 px-2.5 rounded-md bg-slate-900 hover:bg-slate-800 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors disabled:opacity-60"
            >
                {isRefetching ? <Loader2Icon className="w-3 h-3 animate-spin" /> : <RefreshCcwIcon className="w-3 h-3" />}
                Try again
            </button>
        </div>
    );
}
