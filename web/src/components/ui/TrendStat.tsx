// TrendStat: one StatStrip cell holding today's number and the days before it
// as a fluid sparkline. Hovering a day reads that day in place of today.

import React from "react";
import { cn } from "@/lib/utils";
import type { TrendPoint } from "@/lib/trendPoints";

export type TrendTone = "slate" | "sky" | "emerald" | "amber" | "rose";

const TONE: Record<TrendTone, string> = {
    slate: "text-slate-700",
    sky: "text-sky-500",
    emerald: "text-emerald-500",
    amber: "text-amber-500",
    rose: "text-rose-500",
};

// viewBox units; the svg stretches to the cell, strokes stay 1.5px.
const W = 140;
const H = 28;
const PAD = 2;

function dayLabel(iso: string): string {
    const d = new Date(`${iso}T00:00:00Z`);
    return Number.isNaN(d.getTime()) ? iso : d.toLocaleDateString(undefined, { month: "short", day: "numeric", timeZone: "UTC" });
}

export function TrendStat({
    label,
    value,
    unit,
    note,
    points,
    kind = "line",
    tone = "slate",
    rate = false,
    format = (v) => v.toLocaleString(),
    pending = false,
    when = dayLabel,
    last = false,
}: {
    label: string;
    value: React.ReactNode;
    /** Muted text after the value: a denominator or a window. */
    unit?: React.ReactNode;
    /** Right of the label: a delta or a short fact. */
    note?: React.ReactNode;
    points?: TrendPoint[];
    kind?: "line" | "area" | "bars";
    tone?: TrendTone;
    /** A 0-100 rate: the axis starts just under the lowest day instead of at zero. */
    rate?: boolean;
    format?: (v: number) => string;
    /** The chart is still loading. */
    pending?: boolean;
    /** Names a hovered point; days by default. */
    when?: (date: string) => string;
    last?: boolean;
}) {
    const [hovered, setHovered] = React.useState<number | null>(null);
    const n = points?.length ?? 0;
    const present = (points ?? []).map((p) => p.value).filter((v): v is number => v != null);

    const lo = rate && present.length ? Math.max(0, Math.min(60, Math.floor((Math.min(...present) - 5) / 10) * 10)) : 0;
    const hi = rate ? 100 : Math.max(1, ...present);
    const x = (i: number) => (n <= 1 ? W / 2 : PAD + (i / (n - 1)) * (W - PAD * 2));
    const y = (v: number) => PAD + (1 - (v - lo) / (hi - lo || 1)) * (H - PAD * 2);

    let line = "";
    let area = "";
    if (kind !== "bars" && points) {
        let open = false;
        let runStart = 0;
        points.forEach((p, i) => {
            if (p.value == null) {
                if (open) area += `L${x(i - 1).toFixed(1)},${H}L${x(runStart).toFixed(1)},${H}Z`;
                open = false;
                return;
            }
            const seg = `${x(i).toFixed(1)},${y(p.value).toFixed(1)}`;
            if (!open) runStart = i;
            line += `${open ? "L" : "M"}${seg}`;
            area += `${open ? "L" : "M"}${seg}`;
            open = true;
        });
        if (open) area += `L${x(n - 1).toFixed(1)},${H}L${x(runStart).toFixed(1)},${H}Z`;
    }

    const focus = hovered ?? (n ? n - 1 : null);
    const focusPoint = focus != null ? points?.[focus] : undefined;
    const scrub = hovered != null ? points?.[hovered] : undefined;

    const onMove = (e: React.PointerEvent<HTMLDivElement>) => {
        if (!n) return;
        const r = e.currentTarget.getBoundingClientRect();
        const t = Math.min(1, Math.max(0, (e.clientX - r.left) / r.width));
        setHovered(kind === "bars" ? Math.min(n - 1, Math.floor(t * n)) : Math.round(t * (n - 1)));
    };

    // Arrow keys walk the days the pointer scrubs, so the history reads without a mouse.
    const onKey = (e: React.KeyboardEvent<HTMLDivElement>) => {
        if (!n) return;
        const at = hovered ?? n - 1;
        const next = { ArrowLeft: at - 1, ArrowRight: at + 1, Home: 0, End: n - 1 }[e.key];
        if (next === undefined) return;
        e.preventDefault();
        setHovered(Math.min(n - 1, Math.max(0, next)));
    };
    const readout = (i: number) => {
        const p = points![i];
        return `${when(p.date)}: ${p.value == null ? "no data" : format(p.value)}`;
    };

    const slot = n ? W / n : 0;
    const bw = slot * 0.62;

    return (
        <div className={cn("px-5 py-2.5 md:py-3.5 min-w-0", !last && "border-r border-slate-200")}>
            <div className="flex items-center gap-2 min-w-0">
                <span className="text-[12px] font-medium text-slate-500 truncate">{label}</span>
                {note && <span className="ml-auto shrink-0 text-[11px] font-medium tabular-nums">{note}</span>}
            </div>
            <div className="mt-1.5 flex items-baseline gap-1 min-w-0">
                <span className="text-[20px] md:text-[22px] font-semibold tracking-[-0.02em] text-slate-900 leading-none tabular-nums whitespace-nowrap">
                    {scrub ? (scrub.value == null ? "—" : format(scrub.value)) : value}
                </span>
                <span className="text-[12px] text-slate-400 tabular-nums truncate">{scrub ? when(scrub.date) : unit}</span>
            </div>
            <div
                className={cn(
                    "relative mt-2.5 h-7 rounded-sm outline-none focus-visible:ring-2 focus-visible:ring-sky-200",
                    TONE[tone],
                    n > 0 && "cursor-crosshair",
                )}
                onPointerMove={onMove}
                onPointerLeave={() => setHovered(null)}
                {...(n > 0 && !pending
                    ? {
                          role: "slider",
                          tabIndex: 0,
                          "aria-label": `${label} over time`,
                          "aria-valuemin": 0,
                          "aria-valuemax": n - 1,
                          "aria-valuenow": focus ?? n - 1,
                          "aria-valuetext": readout(focus ?? n - 1),
                          onKeyDown: onKey,
                          onBlur: () => setHovered(null),
                      }
                    : { "aria-hidden": true })}
            >
                {pending ? (
                    <div className="h-full rounded bg-slate-100 animate-pulse" />
                ) : !points ? null : n === 0 ? (
                    <div className="absolute inset-x-0 bottom-0 border-t border-dashed border-slate-200" />
                ) : (
                    <>
                        <svg viewBox={`0 0 ${W} ${H}`} preserveAspectRatio="none" aria-hidden className="block w-full h-full overflow-visible">
                            {kind === "bars" ? (
                                points!.map((p, i) => {
                                    const v = p.value ?? 0;
                                    const h = Math.max(1, ((v - lo) / (hi - lo || 1)) * (H - PAD));
                                    return (
                                        <rect
                                            key={p.date}
                                            x={i * slot + (slot - bw) / 2}
                                            y={H - h}
                                            width={bw}
                                            height={h}
                                            rx={1}
                                            fill="currentColor"
                                            opacity={v ? (i === focus ? 1 : 0.32) : 0.16}
                                        />
                                    );
                                })
                            ) : (
                                <>
                                    {kind === "area" && <path d={area} fill="currentColor" opacity={0.12} />}
                                    <path d={line} fill="none" stroke="currentColor" strokeWidth={1.5} strokeLinejoin="round" strokeLinecap="round" vectorEffect="non-scaling-stroke" />
                                </>
                            )}
                        </svg>
                        {hovered != null && kind !== "bars" && (
                            <span
                                className="pointer-events-none absolute -top-0.5 -bottom-0.5 w-px bg-slate-200"
                                style={{ left: `${(x(hovered) / W) * 100}%` }}
                            />
                        )}
                        {kind !== "bars" && focusPoint?.value != null && focus != null && (
                            <span
                                className="pointer-events-none absolute size-[7px] -translate-x-1/2 -translate-y-1/2 rounded-full bg-current ring-2 ring-white"
                                style={{ left: `${(x(focus) / W) * 100}%`, top: `${(y(focusPoint.value) / H) * 100}%` }}
                            />
                        )}
                    </>
                )}
            </div>
        </div>
    );
}
