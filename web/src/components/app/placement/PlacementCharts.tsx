// Warmup placement building blocks shared by the mailbox drawer and the
// deliverability page: the rate badge, daily landing columns, the rolling
// inbox-rate trend, the provider breakdown and the filter controls.
import React, { useState } from "react";
import { ChevronRightIcon } from "lucide-react";
import { DitherColumns, DitherStack, type DitherTone } from "@/components/ui/dither";
import { EmptyChart } from "@/components/ui/charts";
import ProviderLogo from "@/components/app/emails/ProviderLogo";
import { mailHostLabel } from "@/lib/mailHost";
import ScrollStrip from "@/components/ui/scroll-strip";
import { cn } from "@/lib/utils";
import type { PlacementGroup, PlacementProvider, PlacementRate } from "@/lib/api/models/app/analytics/WarmupPlacement";
import {
    BAND,
    GROUP_LABEL,
    GROUP_LOGO,
    LANDED,
    RANGES,
    bandForRate,
    fmtNum,
    fmtPct,
    rateSentence,
    shortDate,
    type DayView,
    type GroupFilter,
    type RangeKey,
} from "./placement";

/* ── rate badge ─────────────────────── */

// The mailbox list's deliverability cell: the rolling inbox rate, or how far
// the sample is from the floor.
export function PlacementRateBadge({ rate, className }: { rate?: PlacementRate | null; className?: string }) {
    if (!rate || rate.band === "none") {
        return (
            <span className={cn("font-mono text-[11px] text-slate-300", className)} title="No warmup deliveries in the last 7 days">
                —
            </span>
        );
    }
    const style = BAND[rate.band];
    if (rate.inbox_rate == null) {
        return (
            <span className={cn("inline-flex items-center gap-1.5 font-mono text-[11px] text-slate-400 tabular-nums", className)} title={`Collecting data. ${rateSentence(rate)}`}>
                <span className={cn("size-1.5 rounded-full", style.dot)} />
                {rate.delivered}/{rate.min_sample}
            </span>
        );
    }
    return (
        <span className={cn("inline-flex items-center gap-1.5 font-mono text-[11.5px] font-medium tabular-nums", style.text, className)} title={`${style.label}. ${rateSentence(rate)}`}>
            <span className={cn("size-1.5 rounded-full", style.dot)} />
            {Math.floor(rate.inbox_rate)}%
        </span>
    );
}

export function BandChip({ band, className }: { band: PlacementRate["band"]; className?: string }) {
    const style = BAND[band];
    return (
        <span className={cn("inline-flex items-center gap-1 rounded px-1.5 h-5 text-[10px] font-medium border shrink-0", style.chip, className)}>
            <span className={cn("size-1.5 rounded-full", style.dot)} />
            {style.label}
        </span>
    );
}

/* ── controls ─────────────────────── */

export function RangeTabs({ value, onChange, className }: { value: RangeKey; onChange: (r: RangeKey) => void; className?: string }) {
    return (
        <div className={cn("inline-flex items-center gap-0.5 rounded-md bg-slate-100 p-0.5", className)}>
            {RANGES.map((r) => (
                <button
                    key={r.key}
                    type="button"
                    onClick={() => onChange(r.key)}
                    className={cn(
                        "h-6 px-2 rounded text-[11px] font-medium transition-colors",
                        value === r.key ? "bg-white text-slate-900 shadow-sm" : "text-slate-500 hover:text-slate-900",
                    )}
                >
                    {r.key}
                </button>
            ))}
        </div>
    );
}

// Only the groups the report actually has deliveries at are offered.
export function GroupFilterChips({
    value,
    onChange,
    groups,
    className,
}: {
    value: GroupFilter;
    onChange: (g: GroupFilter) => void;
    groups: PlacementGroup[];
    className?: string;
}) {
    if (groups.length < 2) return null;
    const options: GroupFilter[] = ["all", ...groups];
    return (
        <ScrollStrip activeKey={value} className={cn("rounded-md bg-slate-100", className)} innerClassName="gap-0.5 p-0.5" fade="from-slate-100">
            {options.map((g) => (
                <button
                    key={g}
                    type="button"
                    data-active={value === g}
                    onClick={() => onChange(g)}
                    className={cn(
                        "h-6 px-2 rounded text-[11px] font-medium transition-colors inline-flex items-center gap-1.5 shrink-0",
                        value === g ? "bg-white text-slate-900 shadow-sm" : "text-slate-500 hover:text-slate-900",
                    )}
                >
                    {g !== "all" && <ProviderLogo id={GROUP_LOGO[g]} size="xs" framed={false} />}
                    {g === "all" ? "All providers" : GROUP_LABEL[g].split(" ")[0]}
                </button>
            ))}
        </ScrollStrip>
    );
}

export function LandedLegend({ className }: { className?: string }) {
    return (
        <div className={cn("flex items-center gap-3 text-[10px] text-slate-400", className)}>
            {LANDED.map((l) => (
                <span key={l.key} className="inline-flex items-center gap-1">
                    <span className={cn("w-2 h-2 rounded-sm", l.dot)} /> {l.label}
                </span>
            ))}
        </div>
    );
}

/* ── daily landing columns ─────────────────────── */

// Stacked inbox / tabs / spam per day, with a readout line for the day under
// the pointer (or the whole range when nothing is hovered).
export function PlacementColumns({ days, height = 140 }: { days: DayView[]; height?: number }) {
    const [hover, setHover] = useState<number | null>(null);
    const data = React.useMemo(() => days.map((d) => ({ key: d.date, parts: [d.inbox, d.tabs, d.spam] })), [days]);
    const any = days.some((d) => d.delivered > 0);
    if (!any) return <EmptyChart height={height + 34} label="No warmup deliveries in this window" />;

    const d = hover != null ? days[hover] : null;
    return (
        <div>
            <div className="relative w-full" style={{ height: height + 16 }}>
                <DitherColumns data={data} tones={["emerald", "violet", "rose"]} height={height} onHover={setHover} />
                <div className="absolute left-0 right-0 h-px bg-slate-200/80" style={{ top: height }} />
                <div className="absolute left-0 right-0 bottom-0 flex justify-between font-mono text-[9.5px] text-slate-400">
                    <span>{shortDate(days[0].date)}</span>
                    <span>{shortDate(days[days.length - 1].date)}</span>
                </div>
            </div>
            <p className="mt-2 h-4 text-[11px] text-slate-500 font-mono tabular-nums truncate">
                {d ? (
                    d.delivered > 0 ? (
                        <>
                            {shortDate(d.date)}: <span className="text-emerald-600">{d.inbox} inbox</span>
                            {d.tabs > 0 && <> · <span className="text-violet-600">{d.tabs} tabs</span></>} · <span className="text-rose-600">{d.spam} spam</span>
                            {d.rescued > 0 && <> ({d.rescued} rescued)</>} · {fmtPct(((d.inbox + d.tabs) / d.delivered) * 100)} inbox
                        </>
                    ) : (
                        <>{shortDate(d.date)}: no deliveries</>
                    )
                ) : (
                    <span className="text-slate-400">Point at a day for its breakdown</span>
                )}
            </p>
        </div>
    );
}

/* ── rolling rate trend ─────────────────────── */

// The trailing inbox rate as a line on a fixed percent scale, with the 90%
// and 80% band lines drawn in, so a slide reads against the thresholds.
export function RateTrend({ days, height = 120, windowDays = 7 }: { days: DayView[]; height?: number; windowDays?: number }) {
    const [hover, setHover] = useState<number | null>(null);
    const values = days.map((d) => d.rolling);
    const present = values.filter((v): v is number => v != null);
    if (present.length === 0) {
        return <EmptyChart height={height + 16} label="Not enough deliveries yet for a trailing rate" />;
    }
    const lowest = Math.min(...present);
    const floor = Math.max(0, Math.min(70, Math.floor((lowest - 5) / 10) * 10));
    const y = (v: number) => ((100 - v) / (100 - floor)) * 100;
    const x = (i: number) => (days.length === 1 ? 50 : (i / (days.length - 1)) * 100);

    // Break the line wherever a day is below the floor.
    const segments: string[] = [];
    let cur = "";
    values.forEach((v, i) => {
        if (v == null) {
            if (cur) segments.push(cur);
            cur = "";
            return;
        }
        cur += `${cur ? "L" : "M"}${x(i).toFixed(2)},${y(v).toFixed(2)}`;
    });
    if (cur) segments.push(cur);

    const h = hover != null ? days[hover] : null;
    const latest = [...values].reverse().find((v) => v != null) ?? null;
    const tone = BAND[bandForRate(latest)];

    return (
        <div>
            <div
                className="relative w-full select-none"
                style={{ height }}
                onPointerMove={(e) => {
                    const rect = e.currentTarget.getBoundingClientRect();
                    const i = Math.round(((e.clientX - rect.left) / rect.width) * (days.length - 1));
                    setHover(i >= 0 && i < days.length ? i : null);
                }}
                onPointerLeave={() => setHover(null)}
            >
                {[90, 80].filter((g) => g > floor).map((g) => (
                    <div key={g} className="pointer-events-none absolute left-0 right-0" style={{ top: `${y(g)}%` }}>
                        <div className={cn("border-t border-dashed", g === 90 ? "border-emerald-200" : "border-amber-200")} />
                        <span className="absolute right-0 -top-3.5 font-mono text-[9px] tabular-nums text-slate-300">{g}%</span>
                    </div>
                ))}
                <svg viewBox="0 0 100 100" preserveAspectRatio="none" className="absolute inset-0 h-full w-full overflow-visible">
                    {segments.map((d, i) => (
                        <path key={i} d={d} fill="none" stroke="currentColor" strokeWidth={1.6} vectorEffect="non-scaling-stroke" strokeLinejoin="round" className={tone.text} />
                    ))}
                    {hover != null && <line x1={x(hover)} x2={x(hover)} y1={0} y2={100} stroke="#cbd5e1" strokeWidth={1} vectorEffect="non-scaling-stroke" />}
                </svg>
                {hover != null && values[hover] != null && (
                    <span
                        className={cn("pointer-events-none absolute size-2 -ml-1 -mt-1 rounded-full ring-2 ring-white", BAND[bandForRate(values[hover])].dot)}
                        style={{ left: `${x(hover)}%`, top: `${y(values[hover] as number)}%` }}
                    />
                )}
                <span className="absolute left-0 bottom-0 font-mono text-[9px] text-slate-300">{floor}%</span>
            </div>
            <div className="mt-1 h-px bg-slate-200/80" />
            <div className="mt-1 flex justify-between font-mono text-[9.5px] text-slate-400">
                <span>{shortDate(days[0].date)}</span>
                <span>{shortDate(days[days.length - 1].date)}</span>
            </div>
            <p className="mt-1.5 h-4 text-[11px] text-slate-500 font-mono tabular-nums truncate">
                {h ? (
                    h.rolling != null ? (
                        <>
                            {windowDays} days to {shortDate(h.date)}: <span className={BAND[bandForRate(h.rolling)].text}>{fmtPct(h.rolling)} inbox</span>
                        </>
                    ) : (
                        <>{shortDate(h.date)}: too few deliveries for a rate</>
                    )
                ) : (
                    <span className="text-slate-400">Trailing {windowDays}-day inbox rate. Dashed lines mark 90% and 80%.</span>
                )}
            </p>
        </div>
    );
}

/* ── nullable sparkline ─────────────────────── */

// A mailbox's daily inbox rate in a table row; gaps are days with no deliveries.
export function RateSpark({ values, width = 96, height = 22 }: { values: (number | null)[]; width?: number; height?: number }) {
    const present = values.filter((v): v is number => v != null);
    if (present.length === 0) return <div style={{ width, height }} className="rounded bg-slate-50" />;
    const floor = Math.max(0, Math.min(60, Math.floor((Math.min(...present) - 5) / 10) * 10));
    const px = (i: number) => 1 + (values.length === 1 ? (width - 2) / 2 : (i / (values.length - 1)) * (width - 2));
    const py = (v: number) => 2 + ((100 - v) / (100 - floor)) * (height - 4);
    let d = "";
    let open = false;
    values.forEach((v, i) => {
        if (v == null) {
            open = false;
            return;
        }
        d += `${open ? "L" : "M"}${px(i).toFixed(1)},${py(v).toFixed(1)}`;
        open = true;
    });
    const last = present[present.length - 1];
    const lastIndex = values.lastIndexOf(last);
    const tone = BAND[bandForRate(last)].text;
    return (
        <svg width={width} height={height} viewBox={`0 0 ${width} ${height}`} className={cn("block shrink-0", tone)}>
            <line x1={0} x2={width} y1={py(90)} y2={py(90)} stroke="#e2e8f0" strokeDasharray="2 2" />
            <path d={d} fill="none" stroke="currentColor" strokeWidth={1.3} strokeLinejoin="round" strokeLinecap="round" />
            <circle cx={px(lastIndex)} cy={py(last)} r={1.8} fill="currentColor" />
        </svg>
    );
}

/* ── provider breakdown ─────────────────────── */

// One row per recipient group; expanding it lists the hosts inside
// (Gmail vs Google Workspace, Outlook.com vs Microsoft 365).
export function ProviderBreakdown({ providers }: { providers: PlacementProvider[] }) {
    const [open, setOpen] = useState<PlacementGroup | null>(null);
    if (providers.length === 0) {
        return <p className="px-5 py-6 text-[12px] text-slate-400 text-center">No deliveries at any provider in this window.</p>;
    }
    return (
        <div className="divide-y divide-slate-200/60">
            {providers.map((p) => {
                const expanded = open === p.group;
                const hosts = p.hosts.filter((h) => h.delivered > 0);
                const canExpand = hosts.length > 1 || (hosts.length === 1 && hosts[0].host !== "");
                return (
                    <div key={p.group}>
                        {React.createElement(
                            canExpand ? "button" : "div",
                            {
                                type: canExpand ? "button" : undefined,
                                onClick: canExpand ? () => setOpen(expanded ? null : p.group) : undefined,
                                "aria-expanded": canExpand ? expanded : undefined,
                                className: cn("w-full min-h-11 px-5 py-2 flex items-center gap-3 text-left", canExpand && "hover:bg-slate-50/80 transition-colors"),
                            },
                            <ChevronRightIcon className={cn("w-3 h-3 shrink-0 text-slate-300 transition-transform", expanded && "rotate-90", !canExpand && "invisible")} />,
                            <ProviderLogo id={GROUP_LOGO[p.group]} size="sm" />,
                            <span className="text-[12.5px] font-medium text-slate-900 w-24 sm:w-32 shrink-0 truncate">{GROUP_LABEL[p.group]}</span>,
                            <PlacementBar inbox={p.inbox} tabs={p.tabs} spam={p.spam} />,
                            <RateCells inbox={p.inbox_rate} spam={p.spam_rate} delivered={p.delivered} />,
                        )}
                        {expanded && (
                            <div className="bg-slate-50/50 divide-y divide-slate-200/40">
                                {hosts.map((h) => (
                                    <div key={h.host || "unknown"} className="min-h-9 pl-14 pr-5 py-1.5 flex items-center gap-3">
                                        <span className="text-[11.5px] text-slate-600 w-24 sm:w-32 shrink-0 truncate">{mailHostLabel(h.host) || "Host not detected"}</span>
                                        <PlacementBar inbox={h.inbox} tabs={h.tabs} spam={h.spam} height={4} />
                                        <RateCells inbox={h.inbox_rate} spam={h.spam_rate} delivered={h.delivered} />
                                    </div>
                                ))}
                            </div>
                        )}
                    </div>
                );
            })}
        </div>
    );
}

function PlacementBar({ inbox, tabs, spam, height = 6 }: { inbox: number; tabs: number; spam: number; height?: number }) {
    const total = Math.max(1, inbox + tabs + spam);
    const title = `${inbox} inbox · ${tabs} other tabs · ${spam} spam`;
    return (
        <div className="flex-1 min-w-12" title={title}>
            <DitherStack
                height={height}
                segments={([
                    { frac: inbox / total, tone: "emerald" },
                    { frac: tabs / total, tone: "violet" },
                    { frac: spam / total, tone: "rose" },
                ] satisfies { frac: number; tone: DitherTone }[]).filter((s) => s.frac > 0)}
            />
        </div>
    );
}

function RateCells({ inbox, spam, delivered }: { inbox: number | null; spam: number | null; delivered: number }) {
    return (
        <span className="flex items-center gap-2 sm:gap-3 font-mono text-[11px] tabular-nums shrink-0">
            <span title="Inbox rate (tabs count as inbox)" className={BAND[bandForRate(inbox)].text}>{fmtPct(inbox)}</span>
            <span title="Spam rate" className="hidden sm:inline text-rose-600 w-12 text-right">{fmtPct(spam)} spam</span>
            <span title="Delivered" className="text-slate-400 w-10 text-right">{fmtNum(delivered)}</span>
        </span>
    );
}
