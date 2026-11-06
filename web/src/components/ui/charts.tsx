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
import {
    AREA_PAD_BOTTOM,
    AREA_PAD_TOP,
    DitherBarChart,
    DitherMultiAreaChart,
    type DitherTone,
} from "@/components/ui/dither";
import useMeasure from "@/hooks/useMeasure";
import { TONE_DOT } from "@/components/ui/tones";

export interface ChartPoint {
    /** x-tick source — typically an ISO date string. */
    label: string;
    value: number;
}

// Dates are UTC days ("yyyy-MM-dd"); formatting in the viewer's zone would
// label each point a day early west of Greenwich.
function dayDate(day: string): Date {
    return new Date(`${day.slice(0, 10)}T00:00:00Z`);
}

function shortDate(day: string): string {
    return dayDate(day).toLocaleDateString("en-US", { month: "short", day: "numeric", timeZone: "UTC" });
}

function longDate(day: string): string {
    return dayDate(day).toLocaleDateString("en-US", {
        weekday: "short",
        month: "short",
        day: "numeric",
        year: "numeric",
        timeZone: "UTC",
    });
}

function compact(n: number): string {
    if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(n >= 10_000_000 ? 0 : 1)}M`;
    if (n >= 1_000) return `${(n / 1_000).toFixed(n >= 10_000 ? 0 : 1)}k`;
    return `${Math.round(n)}`;
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

const DAY_MS = 86_400_000;

// Every UTC day of the window, so a quiet day charts as zero instead of the
// days either side of it being drawn next to each other.
function fillDays(
    labels: string[],
    series: TrendSeries[],
    range?: { from: string; to: string } | null,
): { days: string[]; series: TrendSeries[] } {
    const keys = labels.map((l) => l.slice(0, 10));
    const sorted = [...keys].sort();
    // A day the server returned outside the window widens it rather than being dropped.
    const ends = [range?.from, range?.to, sorted[0], sorted[sorted.length - 1]].filter((d): d is string => !!d).sort();
    const from = ends[0];
    const to = ends[ends.length - 1];
    if (!from || !to) return { days: [], series };
    const days: string[] = [];
    const end = dayDate(to).getTime();
    for (let t = dayDate(from).getTime(); t <= end; t += DAY_MS) days.push(new Date(t).toISOString().slice(0, 10));
    const at = new Map(days.map((d, i) => [d, i]));
    return {
        days,
        series: series.map((s) => {
            const values = new Array<number>(days.length).fill(0);
            keys.forEach((k, i) => {
                const j = at.get(k);
                if (j !== undefined) values[j] += s.values[i] ?? 0;
            });
            return { ...s, values };
        }),
    };
}

// A y-axis top whose half is a whole number, so the midline label is exact.
function niceMax(v: number): number {
    if (v <= 10) return Math.max(2, Math.ceil(v / 2) * 2);
    const p = 10 ** Math.floor(Math.log10(v));
    const m = [1, 2, 3, 4, 5, 6, 8, 10].find((x) => x * p >= v) ?? 10;
    return m * p;
}

const GUTTER = 34;
const TICK_H = 20;
const PANEL_GAP = 30;

/**
 * MultiTrend: a day-by-day chart over the whole window. Quiet days stay on
 * the axis as zero. The `volumeKey` series (sends) draws as bars in its own
 * strip with its own scale, so a steady send volume never flattens the
 * engagement lines above it. One crosshair spans both and lists every value,
 * with each line as a share of the volume that day.
 */
export function MultiTrend({
    labels,
    series,
    height = 260,
    emptyLabel = "No activity yet",
    className,
    range,
    volumeKey,
}: {
    /** UTC days, one per value (shared by every series). Missing days chart as zero. */
    labels: string[];
    series: TrendSeries[];
    height?: number;
    emptyLabel?: string;
    className?: string;
    /** The full window to chart; defaults to the first through last label. */
    range?: { from: string; to: string } | null;
    /** Series drawn as bars below the lines, on its own scale. */
    volumeKey?: string;
}) {
    const [wrapRef, { w }] = useMeasure<HTMLDivElement>();
    const [pointer, setHover] = React.useState<number | null>(null);
    const [tipW, setTipW] = React.useState(160);

    // Keyed on content: callers rebuild these arrays every render, and a new
    // identity would replay the bars' entrance on each one.
    const contentKey = JSON.stringify([labels, series, range?.from, range?.to]);
    // eslint-disable-next-line react-hooks/exhaustive-deps
    const filled = React.useMemo(() => fillDays(labels, series, range), [contentKey]);
    const { days } = filled;
    const volume = volumeKey ? filled.series.find((s) => s.key === volumeKey) : undefined;
    const lines = React.useMemo(() => filled.series.filter((s) => s.key !== volumeKey), [filled.series, volumeKey]);
    const areaSeries = React.useMemo(() => lines.map(({ label, tone, values }) => ({ label, tone, values })), [lines]);
    const bars = React.useMemo(
        () => (volume ? volume.values.map((value, i) => ({ key: days[i], value })) : []),
        [volume, days],
    );
    const shortLabels = React.useMemo(() => days.map(shortDate), [days]);

    const sum = (s: TrendSeries) => s.values.reduce((a, v) => a + (v || 0), 0);
    const total = filled.series.reduce((a, s) => a + sum(s), 0);
    const isEmpty = days.length === 0 || filled.series.length === 0 || total === 0;

    const n = days.length;
    // The index from the last pointer move can outlive a shorter window.
    const hover = pointer !== null && pointer < n ? pointer : null;
    const plotW = Math.max(0, w - GUTTER);
    const slot = n > 0 ? plotW / n : 0;
    const plotH = height - TICK_H;
    const both = !!volume && lines.length > 0;
    const barsH = !volume ? 0 : both ? Math.min(84, Math.max(48, Math.round(plotH * 0.28))) : plotH;
    const linesH = lines.length === 0 ? 0 : both ? plotH - barsH - PANEL_GAP : plotH;
    const barsTop = plotH - barsH;

    const lineMax = niceMax(Math.max(0, ...lines.flatMap((s) => s.values)));
    const innerH = linesH - AREA_PAD_TOP - AREA_PAD_BOTTOM;
    const volMax = volume ? Math.max(1, ...volume.values) : 0;
    const linesQuiet = lines.length > 0 && lines.every((s) => sum(s) === 0);

    // Lines run between the first and last bar centers so both panels share x.
    const inset = n > 1 ? slot / 2 : 0;
    const cx = (i: number) => GUTTER + (i + 0.5) * slot;

    const maxTicks = Math.max(2, Math.floor(plotW / 72));
    const step = Math.max(1, Math.ceil(n / maxTicks));
    const ticks = days.map((_, i) => i).filter((i) => (n - 1 - i) % step === 0);

    const pick = (e: React.PointerEvent<HTMLDivElement>) => {
        const rect = e.currentTarget.getBoundingClientRect();
        const i = Math.floor(((e.clientX - rect.left) / rect.width) * n);
        setHover(i >= 0 && i < n ? i : null);
    };

    // Right of the crosshair when the card fits, else left of it, else pinned inside the chart.
    const tipLeft = (x: number) =>
        x + 10 + tipW <= w ? x + 10 : x - 10 - tipW >= 0 ? x - 10 - tipW : Math.max(0, w - tipW);
    const volAt = hover !== null && volume ? volume.values[hover] : 0;

    return (
        // The measured wrapper stays mounted while empty, so the size is known once data arrives.
        <div ref={wrapRef} className={cn("relative w-full select-none", className)} style={{ height }}>
            {isEmpty ? (
                <EmptyChart height={height} label={emptyLabel} />
            ) : w > 0 && (
                <>
                    {linesH > 0 && (
                        <>
                            {[0, 0.5, 1].map((f) => (
                                <div
                                    key={f}
                                    className="pointer-events-none absolute right-0"
                                    style={{ left: GUTTER, top: AREA_PAD_TOP + (1 - f) * innerH }}
                                >
                                    <div className={f === 0 ? "h-px bg-slate-200" : "h-px bg-slate-100"} />
                                    <span
                                        className="absolute -translate-y-1/2 pr-2 text-right font-mono text-[9.5px] tabular-nums text-slate-400"
                                        style={{ right: "100%", top: 0, width: GUTTER }}
                                    >
                                        {compact(lineMax * f)}
                                    </span>
                                </div>
                            ))}
                            <div
                                className="absolute top-0"
                                style={{ left: GUTTER + inset, width: Math.max(1, plotW - 2 * inset), height: linesH }}
                            >
                                <DitherMultiAreaChart
                                    labels={shortLabels}
                                    series={areaSeries}
                                    height={linesH}
                                    max={lineMax}
                                    hover={hover}
                                />
                            </div>
                            {linesQuiet && (
                                <div
                                    className="pointer-events-none absolute flex items-center justify-center text-[11px] text-slate-400"
                                    style={{ left: GUTTER, right: 0, top: 0, height: linesH }}
                                >
                                    No engagement recorded on these days yet
                                </div>
                            )}
                        </>
                    )}
                    {volume && (
                        <>
                            {both && (
                                <div
                                    className="pointer-events-none absolute flex items-end text-[9.5px] font-medium uppercase tracking-[0.14em] text-slate-400"
                                    style={{ left: GUTTER, top: barsTop - PANEL_GAP, height: PANEL_GAP - 6 }}
                                >
                                    {volume.label} per day
                                </div>
                            )}
                            <span
                                className="pointer-events-none absolute pr-2 text-right font-mono text-[9.5px] tabular-nums text-slate-400"
                                style={{ left: 0, top: barsTop - 1, width: GUTTER }}
                            >
                                {compact(volMax)}
                            </span>
                            <div className="pointer-events-none absolute" style={{ left: GUTTER, right: 0, top: barsTop }}>
                                <DitherBarChart data={bars} height={barsH} tone={volume.tone} selected={hover} />
                            </div>
                        </>
                    )}
                    <div className="absolute right-0 h-px bg-slate-200" style={{ left: GUTTER, top: plotH }} />
                    {ticks.map((i) => (
                        <span
                            key={days[i]}
                            className="pointer-events-none absolute bottom-0 -translate-x-1/2 whitespace-nowrap font-mono text-[9.5px] text-slate-400"
                            style={{ left: Math.min(Math.max(cx(i), GUTTER + 18), w - 18) }}
                        >
                            {shortLabels[i]}
                        </span>
                    ))}
                    <div
                        className="absolute top-0"
                        style={{ left: GUTTER, right: 0, height: plotH }}
                        onPointerMove={pick}
                        onPointerDown={pick}
                        onPointerLeave={() => setHover(null)}
                    />
                    {hover !== null && (
                        <>
                            <div
                                className="pointer-events-none absolute top-0 border-l border-dashed border-slate-300"
                                style={{ left: cx(hover), height: plotH }}
                            />
                            <div
                                ref={(el) => {
                                    if (el && el.offsetWidth !== tipW) setTipW(el.offsetWidth);
                                }}
                                className="pointer-events-none absolute top-1 z-10 min-w-[140px] rounded-md border border-slate-200 bg-white px-2.5 py-1.5 text-[11px] text-slate-600 shadow-sm"
                                style={{ left: tipLeft(cx(hover)) }}
                            >
                                <div className="mb-1 whitespace-nowrap font-medium text-slate-900">{longDate(days[hover])}</div>
                                {volume && <TipRow tone={volume.tone} label={volume.label} value={volAt} />}
                                {lines.map((s) => (
                                    <TipRow
                                        key={s.key}
                                        tone={s.tone}
                                        label={s.label}
                                        value={s.values[hover] ?? 0}
                                        share={volume && volAt > 0 ? (s.values[hover] ?? 0) / volAt : undefined}
                                    />
                                ))}
                            </div>
                        </>
                    )}
                </>
            )}
        </div>
    );
}

function TipRow({ tone, label, value, share }: { tone: DitherTone; label: string; value: number; share?: number }) {
    return (
        <div className="flex items-center gap-1.5 whitespace-nowrap leading-5">
            <span className={cn("size-1.5 rounded-full", TONE_DOT[tone])} />
            <span>{label}</span>
            <span className="ml-auto pl-3 font-mono tabular-nums text-slate-900">{value.toLocaleString()}</span>
            {share !== undefined && (
                <span className="w-10 text-right font-mono tabular-nums text-slate-400">{(share * 100).toFixed(share < 0.1 ? 1 : 0)}%</span>
            )}
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
