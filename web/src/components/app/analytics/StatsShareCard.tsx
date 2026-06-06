import React from "react";
import { Logo } from "@/components/svg";
import { type ChartPoint } from "@/components/ui/charts";

// A branded analytics card rasterized to a shareable PNG (see useExportCard).
// Design: a flat branded sky background, the Warmbly logo white on the sky, and
// one clean white panel holding the title, metrics, and area chart.
//
// Capture-safe for html-to-image (SVG foreignObject):
//   - sky / glow / haze are pure CSS gradients set inline.
//   - NO CSS filter: blur() (unreliable in capture) and NO mix-blend-mode.
//   - the chart is an inline SVG with an internal <linearGradient>.

export interface ShareMetric {
    label: string;
    value: string;
    sub?: string;
}

export interface ShareCardData {
    title: string;
    subtitle?: string;
    metrics: ShareMetric[]; // up to 4
    daily: ChartPoint[]; // primary "sent" series
}

export type ShareAspect = "1:1" | "3:2" | "16:9";

const DIMENSIONS: Record<ShareAspect, { width: number; height: number }> = {
    "1:1": { width: 1080, height: 1080 },
    "3:2": { width: 1620, height: 1080 },
    "16:9": { width: 1920, height: 1080 },
};

// Marketing-site hero sky palette, but SYMMETRIC for a standalone card: a soft
// top-center radial in the bright sky-300 → sky-400 band (never the hero's
// sky-800 navy). Centered at 50%, so neither side is darker than the other —
// no "dark on the right". Bright at top, gently deeper toward the bottom.
const SKY_BASE =
    "radial-gradient(ellipse 125% 105% at 50% -12%," +
    " #9fe0fb 0%, #74cef7 30%, #4ec1f6 58%, #2fb6f2 82%, #18abed 100%)";

// Additive light wash (the .sky-breathe layer, baked static) — centered to match.
const SKY_BREATHE =
    "radial-gradient(ellipse 125% 105% at 50% -12%," +
    " rgba(224,242,254,0.85) 0%, rgba(125,211,252,0.38) 24%, rgba(56,189,248,0.14) 46%, transparent 64%)";

// Lifts the exposed bottom band toward light sky so it stays airy.
const HAZE_BOTTOM =
    "linear-gradient(to top, rgba(186,230,253,0.40) 0%, rgba(125,211,252,0.16) 42%, transparent 100%)";

const PANEL: React.CSSProperties = {
    background: "#ffffff",
    borderRadius: 26,
    border: "1px solid rgba(255,255,255,0.85)",
};

function SkyBackdrop() {
    return (
        <div className="absolute inset-0 overflow-hidden pointer-events-none">
            {/* additive light wash */}
            <div className="absolute inset-0" style={{ background: SKY_BREATHE, opacity: 0.6 }} />
            {/* lift the bottom band */}
            <div className="absolute inset-x-0 bottom-0" style={{ height: "55%", background: HAZE_BOTTOM }} />
        </div>
    );
}

function Stat({ metric, valueSize }: { metric: ShareMetric; valueSize: number }) {
    return (
        <div className="px-7 first:pl-0 last:pr-0">
            <div className="text-[14px] font-medium uppercase tracking-[0.14em] text-slate-400">{metric.label}</div>
            <div className="mt-2.5 font-mono leading-none tabular-nums text-slate-900" style={{ fontSize: valueSize }}>
                {metric.value}
            </div>
            {metric.sub && <div className="mt-2 text-[14px] text-slate-400">{metric.sub}</div>}
        </div>
    );
}

// Long area chart — smooth sky line + gradient fill, ALWAYS with a bottom
// baseline (so it reads as a chart even with no data). Fills its flex parent.
function ShareAreaChart({ points }: { points: ChartPoint[] }) {
    const vals = points.map((p) => p.value || 0);
    const hasData = points.length > 0 && vals.reduce((a, b) => a + b, 0) > 0;

    const W = 1000;
    const H = 320;
    const padX = 4;
    const padTop = 14;
    const baseY = H - 6;
    const max = Math.max(1, ...vals);
    const w = W - padX * 2;
    const h = baseY - padTop;
    const step = vals.length > 1 ? w / (vals.length - 1) : 0;
    const pts = vals.map((v, i) => [padX + i * step, baseY - (v / max) * h] as const);
    const line = pts.map(([x, y], i) => `${i === 0 ? "M" : "L"} ${x.toFixed(1)} ${y.toFixed(1)}`).join(" ");
    const lastX = padX + Math.max(0, vals.length - 1) * step;
    const area = `${line} L ${lastX.toFixed(1)} ${baseY} L ${padX} ${baseY} Z`;

    return (
        <div className="flex-1 min-h-0 flex flex-col">
            <div className="relative flex-1 min-h-0">
                <svg
                    viewBox={`0 0 ${W} ${H}`}
                    preserveAspectRatio="none"
                    width="100%"
                    height="100%"
                    style={{ display: "block" }}
                >
                    <defs>
                        <linearGradient id="shareArea" x1="0" y1="0" x2="0" y2="1">
                            <stop offset="0%" stopColor="#0ea5e9" stopOpacity="0.28" />
                            <stop offset="100%" stopColor="#0ea5e9" stopOpacity="0" />
                        </linearGradient>
                    </defs>
                    {hasData && <path d={area} fill="url(#shareArea)" />}
                    {hasData && (
                        <path
                            d={line}
                            fill="none"
                            stroke="#0284c7"
                            strokeWidth={3}
                            strokeLinejoin="round"
                            strokeLinecap="round"
                            vectorEffect="non-scaling-stroke"
                        />
                    )}
                    {/* baseline — always present */}
                    <line
                        x1={padX}
                        y1={baseY}
                        x2={W - padX}
                        y2={baseY}
                        stroke="#cbd5e1"
                        strokeWidth={2}
                        vectorEffect="non-scaling-stroke"
                    />
                </svg>
                {!hasData && (
                    <div className="absolute inset-0 flex items-center justify-center">
                        <span className="text-[16px] text-slate-300">No sends in this window yet</span>
                    </div>
                )}
            </div>
            <div className="mt-3 flex justify-between font-mono text-[12px] tabular-nums text-slate-400">
                <span>{hasData ? shortDate(points[0].label) : ""}</span>
                <span>{hasData ? shortDate(points[points.length - 1].label) : ""}</span>
            </div>
        </div>
    );
}

const StatsShareCard = React.forwardRef<HTMLDivElement, { data: ShareCardData; aspect?: ShareAspect }>(
    function StatsShareCard({ data, aspect = "1:1" }, ref) {
        const { width, height } = DIMENSIONS[aspect];
        const metrics = data.metrics.slice(0, 4);
        const landscape = aspect !== "1:1";

        const pad = aspect === "16:9" ? 64 : aspect === "3:2" ? 56 : 48;
        const logoClass = landscape ? "w-[60px] h-[60px]" : "w-[54px] h-[54px]";
        const wordmarkSize = landscape ? 40 : 36;
        const titleSize = aspect === "16:9" ? 50 : aspect === "3:2" ? 46 : 40;
        const valueSize = landscape ? 50 : 46;

        return (
            <div
                ref={ref}
                style={{ width, height, background: SKY_BASE, position: "relative", overflow: "hidden", borderRadius: 0 }}
            >
                <SkyBackdrop />

                <div className="relative z-10 flex flex-col h-full" style={{ padding: pad }}>
                    {/* logo on the sky */}
                    <div className="flex items-center justify-between">
                        <div className="inline-flex items-center gap-3.5">
                            <Logo className={`${logoClass} text-white`} />
                            <span
                                className="text-white font-extrabold tracking-tight"
                                style={{ fontFamily: "var(--font-display)", fontSize: wordmarkSize }}
                            >
                                Warmbly
                            </span>
                        </div>
                        <span className="font-mono text-[15px] tabular-nums text-white/85">
                            {todayLabel()}
                        </span>
                    </div>

                    {/* single white panel */}
                    <div style={PANEL} className="mt-6 flex-1 flex flex-col min-h-0" >
                        <div className="flex flex-col h-full" style={{ padding: landscape ? 44 : 40 }}>
                            {/* title */}
                            {data.subtitle && (
                                <div className="text-[14px] font-semibold uppercase tracking-[0.16em] text-sky-600">
                                    {data.subtitle}
                                </div>
                            )}
                            <h1
                                className="mt-2 font-semibold leading-tight tracking-tight text-slate-900 line-clamp-2 break-words"
                                style={{ fontSize: titleSize }}
                            >
                                {data.title}
                            </h1>

                            {/* divided metric row */}
                            <div
                                className="mt-8 grid divide-x divide-slate-200"
                                style={{ gridTemplateColumns: `repeat(${metrics.length || 1}, minmax(0, 1fr))` }}
                            >
                                {metrics.map((m) => (
                                    <Stat key={m.label} metric={m} valueSize={valueSize} />
                                ))}
                            </div>

                            <div className="mt-8 h-px bg-slate-200/70" />

                            {/* long area chart */}
                            <div className="mt-6 flex items-center justify-between">
                                <span className="text-[14px] font-medium uppercase tracking-[0.14em] text-slate-400">
                                    Sends over time
                                </span>
                            </div>
                            <div className="mt-4 flex-1 min-h-0 flex flex-col">
                                <ShareAreaChart points={data.daily} />
                            </div>
                        </div>
                    </div>

                    {/* footer on the sky */}
                    <div className="mt-5 flex items-center justify-between text-[16px]">
                        <span className="text-white font-semibold">
                            warmbly.com
                        </span>
                        <span className="text-white/80">
                            Cold email, warmed up.
                        </span>
                    </div>
                </div>
            </div>
        );
    },
);

function shortDate(iso: string): string {
    try {
        return new Date(iso).toLocaleDateString("en-US", { month: "short", day: "numeric" });
    } catch {
        return iso;
    }
}

function todayLabel(): string {
    return new Date().toLocaleDateString("en-US", { year: "numeric", month: "short", day: "numeric" });
}

export default StatsShareCard;
