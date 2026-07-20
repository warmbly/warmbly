// Library-free, on-theme charts shared across the analytics + campaign
// surfaces. The dashboard ships no charting dependency on purpose (keeps the
// bundle small and the visuals consistent with the hairline chrome), so these
// are hand-rolled SVG/flex primitives.
//
// The key idea: a chart ALWAYS occupies its footprint. When there's no data we
// render a dashed "empty frame" with a faint baseline + caption rather than
// hiding the graph — so the user sees an empty graph, never a blank gap.

import React from "react";
import { cn } from "@/lib/utils";
import { DitherMultiAreaChart, type DitherTone } from "@/components/ui/dither";

export interface ChartPoint {
    /** x-tick source — typically an ISO date string. */
    label: string;
    value: number;
}

function shortDate(iso: string): string {
    try {
        return new Date(iso).toLocaleDateString("en-US", { month: "short", day: "numeric" });
    } catch {
        return iso;
    }
}

/**
 * EmptyChart — preserves the chart's footprint and reads as an empty graph
 * (dashed border + faint baseline + centered caption) instead of vanishing.
 */
export function EmptyChart({
    height = 200,
    label = "No data in this window",
    className,
}: {
    height?: number;
    label?: string;
    className?: string;
}) {
    return (
        <div
            style={{ height }}
            className={cn(
                "relative w-full rounded-md border border-dashed border-slate-200 bg-slate-50/40",
                className,
            )}
        >
            {/* faint baseline so the frame still reads as an axis */}
            <div className="absolute left-0 right-0 bottom-6 h-px bg-slate-200/70" />
            <div className="absolute inset-0 flex items-center justify-center">
                <span className="text-[11.5px] text-slate-400">{label}</span>
            </div>
        </div>
    );
}

export interface TrendSeries {
    key: string;
    label: string;
    tone: DitherTone;
    values: number[];
}

/**
 * MultiTrend — several metrics composed on one graph (dither-kit style): a
 * smoothed dithered area line per series, one shared crosshair listing every
 * value, plus date ticks. Falls back to <EmptyChart> when there is no data
 * (or every visible series is all zero), keeping the same height so the
 * layout never jumps.
 */
export function MultiTrend({
    labels,
    series,
    height = 200,
    emptyLabel = "No activity yet",
    className,
}: {
    /** ISO dates, one per x position (shared by every series). */
    labels: string[];
    series: TrendSeries[];
    height?: number;
    emptyLabel?: string;
    className?: string;
}) {
    const total = series.reduce((s, x) => s + x.values.reduce((a, v) => a + (v || 0), 0), 0);
    const isEmpty = labels.length === 0 || series.length === 0 || total === 0;

    const areaSeries = React.useMemo(
        () => series.map(({ label, tone, values }) => ({ label, tone, values })),
        [series],
    );
    const shortLabels = React.useMemo(() => labels.map(shortDate), [labels]);

    if (isEmpty) return <EmptyChart height={height} label={emptyLabel} className={className} />;

    const tickH = 18;

    return (
        <div className={cn("relative w-full select-none", className)} style={{ height }}>
            <DitherMultiAreaChart labels={shortLabels} series={areaSeries} height={height - tickH - 4} />
            <div className="absolute left-0 right-0 h-px bg-slate-200/80" style={{ bottom: tickH }} />
            <div className="absolute left-0 right-0 bottom-0 flex justify-between font-mono text-[9.5px] text-slate-400">
                <span>{shortDate(labels[0])}</span>
                <span>{shortDate(labels[labels.length - 1])}</span>
            </div>
        </div>
    );
}

/**
 * Sparkline — smooth single-series line + gradient fill for inline trends.
 * On-theme (sky) and library-free; mirrors the api-keys Sparkline but lives
 * here so analytics/campaign code shares one implementation.
 */
export function Sparkline({
    values,
    width = 120,
    height = 28,
    stroke = "#0284c7",
    className,
}: {
    values: number[];
    width?: number;
    height?: number;
    stroke?: string;
    className?: string;
}) {
    if (!values || values.length === 0) {
        return <div style={{ width, height }} className={cn("rounded bg-slate-50", className)} />;
    }

    const max = Math.max(1, ...values);
    const pad = 2;
    const w = width - pad * 2;
    const h = height - pad * 2;
    const step = values.length > 1 ? w / (values.length - 1) : 0;
    const points = values.map((v, i) => [pad + i * step, pad + h - (v / max) * h] as const);
    const linePath = points
        .map(([x, y], i) => `${i === 0 ? "M" : "L"} ${x.toFixed(2)} ${y.toFixed(2)}`)
        .join(" ");
    const areaPath = `${linePath} L ${pad + (values.length - 1) * step} ${pad + h} L ${pad} ${pad + h} Z`;
    const gid = `spark-${stroke.replace("#", "")}`;

    return (
        <svg width={width} height={height} viewBox={`0 0 ${width} ${height}`} className={cn("block", className)}>
            <defs>
                <linearGradient id={gid} x1="0" y1="0" x2="0" y2="1">
                    <stop offset="0%" stopColor={stroke} stopOpacity="0.18" />
                    <stop offset="100%" stopColor={stroke} stopOpacity="0" />
                </linearGradient>
            </defs>
            <path d={areaPath} fill={`url(#${gid})`} />
            <path d={linePath} fill="none" stroke={stroke} strokeWidth={1.4} strokeLinejoin="round" strokeLinecap="round" />
        </svg>
    );
}
