import { useMemo, useState } from "react";
import {
    MailCheckIcon,
    MousePointerClickIcon,
    ReplyIcon,
    SendIcon,
    TriangleAlertIcon,
} from "lucide-react";
import { useCampaign } from "@/hooks/context/campaign";
import useCampaignAnalytics from "@/lib/api/hooks/app/analytics/useCampaignAnalytics";
import useCampaignDailyStats from "@/lib/api/hooks/app/analytics/useCampaignDailyStats";
import { SectionBar, Stat, StatStrip } from "@/components/layout/Page";
import { DailyBars, type ChartPoint } from "@/components/ui/charts";
import AnalyticsShareButton from "@/components/app/analytics/AnalyticsShareButton";
import TaskPreview from "@/components/app/campaigns/TaskPreview";
import AnimatedNumber from "@/components/ui/AnimatedNumber";

const pctFmt = (v: number) => `${v.toFixed(1)}%`;

type Metric = "sent" | "opens" | "clicks" | "replies";

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

export default function CampaignOverview() {
    const campaign = useCampaign();
    const id = campaign?.id ?? "";

    const analytics = useCampaignAnalytics(id);
    const daily = useCampaignDailyStats(id);

    const [metric, setMetric] = useState<Metric>("sent");

    const summary = analytics.data?.summary;
    const sequences = analytics.data?.sequences ?? [];
    const dailyStats = daily.data ?? [];

    const series: ChartPoint[] = useMemo(
        () => (daily.data ?? []).map((d) => ({ label: d.date, value: d[metric] ?? 0 })),
        [daily.data, metric],
    );

    const loading = analytics.isPending || daily.isPending;
    const hasSends = (summary?.emails_sent ?? 0) > 0;

    const shareData = {
        title: campaign?.name ?? "Campaign",
        subtitle: "Campaign",
        metrics: [
            { label: "Sent", value: num(summary?.emails_sent), sub: "emails" },
            { label: "Open rate", value: pct(summary?.open_rate) },
            { label: "Reply rate", value: pct(summary?.reply_rate) },
            { label: "Bounce rate", value: pct(summary?.bounce_rate) },
        ],
        daily: dailyStats.map((d) => ({ label: d.date, value: d.sent })),
    };

    if (!campaign) {
        return (
            <div className="space-y-4">
                <div className="grid grid-cols-2 md:grid-cols-5 gap-px bg-slate-200 rounded-md overflow-hidden">
                    {[...Array(5)].map((_, i) => (
                        <div key={i} className="h-20 bg-white animate-pulse" />
                    ))}
                </div>
                <div className="h-56 bg-slate-100 rounded-md animate-pulse" />
            </div>
        );
    }

    const breakdown = [
        { label: "Sent", value: summary?.emails_sent, icon: SendIcon, dot: "bg-slate-400" },
        { label: "Opens", value: summary?.unique_opens, icon: MailCheckIcon, dot: "bg-emerald-500" },
        { label: "Clicks", value: summary?.unique_clicks, icon: MousePointerClickIcon, dot: "bg-violet-500" },
        { label: "Replies", value: summary?.replies, icon: ReplyIcon, dot: "bg-amber-500" },
        { label: "Bounces", value: summary?.bounces, icon: TriangleAlertIcon, dot: "bg-rose-500" },
    ];

    return (
        <div className="space-y-5">
            <div className="grid lg:grid-cols-[1fr_340px] gap-5 items-start">
                {/* Main analytics column */}
                <div className="space-y-5 min-w-0">
                    <div className="rounded-md border border-slate-200 overflow-hidden bg-white">
                        <SectionBar label="Performance">
                            {campaign.status === "active" && (
                                <span className="inline-flex items-center gap-1 text-[10.5px] font-medium text-emerald-600 mr-1">
                                    <span className="relative flex size-1.5">
                                        <span className="absolute inline-flex h-full w-full rounded-full bg-emerald-500 opacity-60 animate-ping" />
                                        <span className="relative inline-flex size-1.5 rounded-full bg-emerald-500" />
                                    </span>
                                    Live
                                </span>
                            )}
                            <AnalyticsShareButton
                                data={shareData}
                                filename={`warmbly-${campaign.id}.png`}
                            />
                        </SectionBar>
                        <StatStrip cols={5}>
                            <Stat
                                label="Sent"
                                value={loading ? "—" : <AnimatedNumber value={summary?.emails_sent ?? 0} />}
                                sub="emails"
                                accent={hasSends}
                            />
                            <Stat
                                label="Open rate"
                                value={loading ? "—" : <AnimatedNumber value={summary?.open_rate ?? 0} format={pctFmt} />}
                                sub="after delivery"
                            />
                            <Stat
                                label="Click rate"
                                value={loading ? "—" : <AnimatedNumber value={summary?.click_rate ?? 0} format={pctFmt} />}
                                sub="of delivered"
                            />
                            <Stat
                                label="Reply rate"
                                value={loading ? "—" : <AnimatedNumber value={summary?.reply_rate ?? 0} format={pctFmt} />}
                                sub="incl. positive"
                            />
                            <Stat
                                label="Bounce rate"
                                value={loading ? "—" : <AnimatedNumber value={summary?.bounce_rate ?? 0} format={pctFmt} />}
                                sub="hard + soft"
                                last
                            />
                        </StatStrip>
                    </div>

                    {analytics.isError ? (
                        <div className="rounded-md border border-rose-200 bg-rose-50/40 px-5 py-8 text-center">
                            <p className="text-[12.5px] text-slate-900 font-medium">Couldn't load analytics</p>
                            <p className="text-[11.5px] text-slate-500 mt-1">The request failed — try refreshing.</p>
                        </div>
                    ) : (
                        <div className="rounded-md border border-slate-200 overflow-hidden bg-white">
                            <SectionBar label="Daily performance">
                                <div className="inline-flex items-center gap-0.5 rounded-md border border-slate-200 bg-white p-0.5">
                                    {METRICS.map((m) => (
                                        <button
                                            key={m.key}
                                            type="button"
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
                            <div className="px-5 py-4">
                                {loading ? (
                                    <div className="h-52 rounded-md bg-slate-50 animate-pulse" />
                                ) : (
                                    <DailyBars
                                        points={series}
                                        height={220}
                                        barClass={METRICS.find((m) => m.key === metric)?.bar}
                                        emptyLabel={
                                            hasSends
                                                ? "No activity in this window yet"
                                                : "No sends yet — start the campaign to see performance"
                                        }
                                    />
                                )}
                            </div>
                        </div>
                    )}

                    <div className="rounded-md border border-slate-200 overflow-hidden bg-white">
                        <SectionBar label="Step performance" count={sequences.length || undefined} />
                        {loading ? (
                            <div className="divide-y divide-slate-200/60">
                                {[...Array(2)].map((_, i) => (
                                    <div key={i} className="h-11 px-5 flex items-center gap-3">
                                        <div className="size-1.5 rounded-full bg-slate-200" />
                                        <div className="h-3 w-40 bg-slate-100 rounded animate-pulse" />
                                        <div className="ml-auto h-3 w-48 bg-slate-100 rounded animate-pulse" />
                                    </div>
                                ))}
                            </div>
                        ) : sequences.length === 0 ? (
                            <div className="px-5 py-10 text-center">
                                <p className="text-[12.5px] text-slate-700 font-medium mb-1">No step data yet</p>
                                <p className="text-[11.5px] text-slate-400 max-w-[34ch] mx-auto leading-relaxed">
                                    Once steps start sending, per-step opens, clicks, and replies show up here.
                                </p>
                            </div>
                        ) : (
                            <div className="divide-y divide-slate-200/60">
                                {/* header row */}
                                <div className="h-8 px-5 flex items-center gap-3 text-[10px] uppercase tracking-[0.12em] text-slate-400 font-medium">
                                    <span className="flex-1 min-w-0">Step</span>
                                    <span className="w-14 text-right">Sent</span>
                                    <span className="w-14 text-right">Opens</span>
                                    <span className="w-14 text-right hidden md:block">Clicks</span>
                                    <span className="w-14 text-right">Replies</span>
                                    <span className="w-16 text-right hidden md:block">Bounces</span>
                                </div>
                                {sequences.map((s) => (
                                    <div key={s.sequence_id} className="h-11 px-5 flex items-center gap-3">
                                        <span className="flex items-center gap-2 flex-1 min-w-0">
                                            <span className="font-mono text-[10.5px] text-slate-400 tabular-nums shrink-0">
                                                {s.position}
                                            </span>
                                            <span className="text-[12.5px] text-slate-900 truncate">{s.name}</span>
                                        </span>
                                        <span className="w-14 text-right font-mono text-[11.5px] text-slate-700 tabular-nums">
                                            <AnimatedNumber value={s.emails_sent ?? 0} />
                                        </span>
                                        <span className="w-14 text-right font-mono text-[11.5px] text-emerald-600 tabular-nums">
                                            <AnimatedNumber value={s.opens ?? 0} />
                                        </span>
                                        <span className="w-14 text-right font-mono text-[11.5px] text-violet-600 tabular-nums hidden md:block">
                                            <AnimatedNumber value={s.clicks ?? 0} />
                                        </span>
                                        <span className="w-14 text-right font-mono text-[11.5px] text-amber-600 tabular-nums">
                                            <AnimatedNumber value={s.replies ?? 0} />
                                        </span>
                                        <span className="w-16 text-right font-mono text-[11.5px] text-rose-600 tabular-nums hidden md:block">
                                            <AnimatedNumber value={s.bounces ?? 0} />
                                        </span>
                                    </div>
                                ))}
                            </div>
                        )}
                    </div>

                    {/* quick breakdown strip below sequence table, mobile-friendly summary */}
                    <div className="rounded-md border border-slate-200 overflow-hidden bg-white lg:hidden">
                        <SectionBar label="Totals" />
                        <div className="divide-y divide-slate-200/60">
                            {breakdown.map((q) => (
                                <div key={q.label} className="h-9 px-5 flex items-center gap-2">
                                    <span className={`size-1.5 rounded-full ${q.dot}`} />
                                    <span className="text-[12px] text-slate-700">{q.label}</span>
                                    <span className="ml-auto font-mono text-[11px] text-slate-500 tabular-nums">
                                        {loading ? "—" : <AnimatedNumber value={q.value ?? 0} />}
                                    </span>
                                </div>
                            ))}
                        </div>
                    </div>
                </div>

                {/* Live panel */}
                <aside className="space-y-5">
                    <TaskPreview campaignId={campaign.id} campaignStatus={campaign.status} />

                    <div className="rounded-md border border-slate-200 overflow-hidden bg-white hidden lg:block">
                        <SectionBar label="Totals" />
                        <div className="divide-y divide-slate-200/60">
                            {breakdown.map((q) => (
                                <div key={q.label} className="h-9 px-5 flex items-center gap-2">
                                    <span className={`size-1.5 rounded-full ${q.dot}`} />
                                    <span className="text-[12px] text-slate-700">{q.label}</span>
                                    <span className="ml-auto font-mono text-[11px] text-slate-500 tabular-nums">
                                        {loading ? "—" : <AnimatedNumber value={q.value ?? 0} />}
                                    </span>
                                </div>
                            ))}
                        </div>
                    </div>
                </aside>
            </div>
        </div>
    );
}
