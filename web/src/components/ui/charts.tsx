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

/**
 * DailyBars — single-series vertical bar chart with date ticks + hover tooltip.
 * Falls back to <EmptyChart> when there is no data (or the series is all zero),
 * keeping the same height so the layout never jumps.
 */
export function DailyBars({
    points,
    height = 200,
    barClass = "bg-sky-500",
    emptyLabel = "No activity yet",
    formatValue = (v: number) => v.toLocaleString(),
    className,
}: {
    points: ChartPoint[];
    height?: number;
    barClass?: string;
    emptyLabel?: string;
    formatValue?: (v: number) => string;
    className?: string;
}) {
    const [hover, setHover] = React.useState<number | null>(null);

    const total = points.reduce((s, p) => s + (p.value || 0), 0);
    const isEmpty = points.length === 0 || total === 0;
    const max = Math.max(1, ...points.map((p) => p.value || 0));

    if (isEmpty) return <EmptyChart height={height} label={emptyLabel} className={className} />;

    const tickH = 18;
    const barAreaH = height - tickH - 4;

    return (
        <div className={cn("relative w-full select-none", className)} style={{ height }}>
            <div className="absolute inset-x-0 top-0 flex items-end gap-px md:gap-[2px]" style={{ height: barAreaH }}>
                {points.map((p, i) => {
                    const h = p.value > 0 ? Math.max(2, (p.value / max) * barAreaH) : 0;
                    return (
                        <div
                            key={i}
                            className="relative flex-1 flex items-end justify-center h-full"
                            onMouseEnter={() => setHover(i)}
                            onMouseLeave={() => setHover(null)}
                            // Touch affordance: tap toggles the tooltip (hover
                            // emulation on touch devices is unreliable).
                            onClick={() => setHover((cur) => (cur === i ? null : i))}
                        >
                            <div
                                className={cn(
                                    "w-full rounded-sm transition-opacity",
                                    barClass,
                                    hover === null || hover === i ? "opacity-100" : "opacity-50",
                                )}
                                // Cap + center each bar so a sparse series (1–2 points) renders
                                // as a normal bar, not a full-width block. Dense series have
                                // cells narrower than the cap, so this is a no-op for them.
                                style={{ height: h, maxWidth: 64 }}
                            />
                            {hover === i && (
                                <div
                                    className={cn(
                                        "absolute -top-7 z-10 whitespace-nowrap rounded bg-slate-900 px-1.5 py-0.5 text-[10px] font-medium text-white shadow-sm",
                                        // Pin edge tooltips inward so they don't
                                        // poke past the viewport on the first/last bar.
                                        i === 0
                                            ? "left-0"
                                            : i === points.length - 1
                                              ? "right-0"
                                              : "left-1/2 -translate-x-1/2",
                                    )}
                                >
                                    {formatValue(p.value)} · {shortDate(p.label)}
                                </div>
                            )}
                        </div>
                    );
                })}
            </div>
            <div className="absolute left-0 right-0 h-px bg-slate-200/80" style={{ bottom: tickH }} />
            <div className="absolute left-0 right-0 bottom-0 flex justify-between font-mono text-[9.5px] text-slate-400">
                <span>{shortDate(points[0].label)}</span>
                <span>{shortDate(points[points.length - 1].label)}</span>
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
