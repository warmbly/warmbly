// What the workspace's mailboxes did today, each against its last 14 days.
// Sends and replies come from the dashboard query the sidebar meter already
// holds; warmup volume and inbox placement from the workspace placement report.

import React, { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import AnimatedNumber from "@/components/ui/AnimatedNumber";
import { TrendStat, type TrendTone } from "@/components/ui/TrendStat";
import { dailyPoints, utcDayRange, type TrendPoint } from "@/lib/trendPoints";
import { StatStrip } from "@/components/layout/Page";
import { usePermission } from "@/hooks/usePermission";
import useUtcToday from "@/hooks/useUtcToday";
import { dashboardQuery } from "@/lib/api/hooks/app/analytics/useDashboard";
import useWarmupPlacement from "@/lib/api/hooks/app/analytics/useWarmupPlacement";
import type { PlacementBand } from "@/lib/api/models/app/analytics/WarmupPlacement";
import { ACCOUNTS_TREND_DAYS, accountsTrendWindow } from "./accountsTrend";

export interface WarmupToday {
    state: "ready" | "checking" | "unavailable";
    sent: number;
    target: number;
    mailboxes: number;
    inCampaign: number;
}

const BAND_TONE: Record<PlacementBand, TrendTone> = { good: "emerald", fair: "amber", poor: "rose", collecting: "slate", none: "slate" };

const muted = (text: string) => <span className="text-slate-400">{text}</span>;

export default function AccountsTrendStrip({ warmup, filtered = false }: { warmup: WarmupToday; filtered?: boolean }) {
    const canAnalytics = usePermission("VIEW_ANALYTICS");
    const dash = useQuery({ ...dashboardQuery("30d"), enabled: canAnalytics });
    const today = useUtcToday();
    const { from, to } = useMemo(() => accountsTrendWindow(today), [today]);
    const placement = useWarmupPlacement(undefined, from, to, canAnalytics);
    const days = useMemo(() => utcDayRange(from, to), [from, to]);

    const series = useMemo(() => {
        const trend = dash.data?.daily_trend;
        const placed = placement.data?.daily;
        const lastDay = days[days.length - 1];
        const warmupDays = dailyPoints(days, placed, (d) => d.sent);
        return {
            sent: dailyPoints(days, trend, (d) => d.sent),
            replies: dailyPoints(days, trend, (d) => d.replies),
            // Today's warmup is the mailboxes' own count, so the line ends where the number says.
            warmup: warmupDays.map((p) => (p.date === lastDay && warmup.state === "ready" ? { ...p, value: warmup.sent } : p)),
            inbox: dailyPoints(days, placed, (d) => d.rolling_inbox_rate, null),
        };
    }, [dash.data, placement.data, days, warmup.state, warmup.sent]);

    const last = (points: TrendPoint[]) => points[points.length - 1]?.value ?? 0;
    const analyticsValue = (q: { isPending: boolean; isError: boolean; data?: unknown }, ready: () => React.ReactNode) =>
        !canAnalytics ? "—" : q.isPending ? "Checking…" : q.isError && !q.data ? "Unavailable" : ready();

    const sentToday = last(series.sent);
    const capacity = dash.data?.capacity_today?.capacity;
    const repliesTotal = series.replies.reduce((sum, p) => sum + (p.value ?? 0), 0);

    const rate = placement.data?.rate;
    const inbox = series.inbox;
    const latest = [...inbox].reverse().find((p) => p.value != null);
    const weekAgo = latest ? inbox[inbox.indexOf(latest) - 7]?.value : null;
    const delta = latest?.value != null && weekAgo != null ? Math.round(latest.value - weekAgo) : null;

    const warmupValue =
        warmup.state === "checking" ? "Checking…" : warmup.state === "unavailable" ? "Unavailable" : <AnimatedNumber value={warmup.sent} />;

    return (
        <StatStrip cols={4}>
            <TrendStat
                label="Sent today"
                value={analyticsValue(dash, () => <AnimatedNumber value={sentToday} />)}
                unit={!canAnalytics ? "no analytics access" : capacity ? `/ ${capacity.toLocaleString()}` : "campaign emails"}
                note={warmup.state === "ready" && warmup.inCampaign > 0 ? muted(`${warmup.inCampaign} in campaigns`) : undefined}
                points={canAnalytics && dash.data ? series.sent : undefined}
                pending={canAnalytics && dash.isPending}
                kind="line"
                tone="slate"
            />
            <TrendStat
                label="Warmup today"
                value={warmupValue}
                unit={warmup.state === "ready" && warmup.target > 0 ? `/ ${warmup.target.toLocaleString()}${filtered ? ", matching" : ""}` : undefined}
                note={warmup.state === "ready" && warmup.mailboxes > 0 ? muted(`${warmup.mailboxes} warming`) : undefined}
                points={canAnalytics && placement.data && !filtered ? series.warmup : undefined}
                pending={canAnalytics && placement.isPending}
                kind="area"
                tone="sky"
            />
            <TrendStat
                label="Inbox placement"
                value={analyticsValue(placement, () => (rate?.inbox_rate == null ? "—" : `${Math.round(rate.inbox_rate)}%`))}
                unit={
                    !canAnalytics
                        ? "no analytics access"
                        : rate?.band === "collecting"
                          ? `collecting, ${rate.delivered} of ${rate.min_sample}`
                          : rate
                            ? `${rate.window_days} days`
                            : undefined
                }
                note={
                    !canAnalytics || delta == null ? undefined : delta === 0 ? muted("steady") : (
                        <span className={delta > 0 ? "text-emerald-700" : "text-amber-700"}>
                            {delta > 0 ? "+" : "−"}
                            {Math.abs(delta)} {Math.abs(delta) === 1 ? "pt" : "pts"}
                        </span>
                    )
                }
                points={canAnalytics && placement.data ? series.inbox : undefined}
                pending={canAnalytics && placement.isPending}
                kind="line"
                tone={rate ? BAND_TONE[rate.band] : "slate"}
                rate
                format={(v) => `${Math.round(v)}%`}
            />
            <TrendStat
                label="Replies"
                value={analyticsValue(dash, () => <AnimatedNumber value={last(series.replies)} />)}
                unit={!canAnalytics ? "no analytics access" : "to today's sends"}
                note={canAnalytics && dash.data ? muted(`${repliesTotal.toLocaleString()} over ${ACCOUNTS_TREND_DAYS} days`) : undefined}
                points={canAnalytics && dash.data ? series.replies : undefined}
                pending={canAnalytics && dash.isPending}
                kind="bars"
                tone="slate"
                last
            />
        </StatStrip>
    );
}
