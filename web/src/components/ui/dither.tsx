// Dithered canvas primitives for stats surfaces (billing, usage): ordered
// Bayer-4x4 dither fills on a tiny hand-rolled canvas engine. Same look as
// dither-kit charts but with zero new dependencies — no d3, no chart lib.
//
// Exports:
//   DitherBarChart — daily series bars with entrance stagger + hover tooltip
//   DitherMeter    — horizontal progress fill with a dithered leading edge
//   DitherRing     — donut with a dithered sweep tip
//   DitherSlider   — pointer/keyboard slider whose track is a DitherMeter
//   AnimatedNumber — rAF count-up on value change

import React from "react";
import { useReducedMotion } from "framer-motion";

// Ordered 8x8 Bayer matrix: 64 threshold levels, so density ramps render as
// smooth halftone gradients instead of the harsh stripe patterns a 4x4 gives.
const BAYER = [
    [0, 32, 8, 40, 2, 34, 10, 42],
    [48, 16, 56, 24, 50, 18, 58, 26],
    [12, 44, 4, 36, 14, 46, 6, 38],
    [60, 28, 52, 20, 62, 30, 54, 22],
    [3, 35, 11, 43, 1, 33, 9, 41],
    [51, 19, 59, 27, 49, 17, 57, 25],
    [15, 47, 7, 39, 13, 45, 5, 37],
    [63, 31, 55, 23, 61, 29, 53, 21],
];

// Threshold in [0,1) for a device pixel; dots stay 1 CSS px on retina.
function bayer(x: number, y: number, dpr: number): number {
    const bx = Math.floor(x / dpr) & 7;
    const by = Math.floor(y / dpr) & 7;
    return (BAYER[by][bx] + 0.5) / 64;
}

// 500-level hues: the 600s read harsh as full dots on the white theme.
const TONES = {
    sky: [14, 165, 233],
    amber: [245, 158, 11],
    rose: [244, 63, 94],
    emerald: [16, 185, 129],
    violet: [139, 92, 246],
    slate: [71, 85, 105],
} as const;
export type DitherTone = keyof typeof TONES;

// Dither dots blend into the page instead of sitting on it; lines and
// hovered/selected elements stay fully opaque for definition.
const FILL_ALPHA = 215;

const TRACK: [number, number, number] = [226, 232, 240]; // slate-200

function clamp01(n: number): number {
    return Math.max(0, Math.min(1, n));
}

function easeOutCubic(c: number): number {
    return 1 - Math.pow(1 - c, 3);
}

// Observe an element's CSS-pixel size.
function useMeasure<T extends HTMLElement>(): [React.RefObject<T | null>, { w: number; h: number }] {
    const ref = React.useRef<T | null>(null);
    const [size, setSize] = React.useState({ w: 0, h: 0 });
    React.useEffect(() => {
        const el = ref.current;
        if (!el) return;
        const ro = new ResizeObserver((entries) => {
            const r = entries[0].contentRect;
            const w = Math.round(r.width);
            const h = Math.round(r.height);
            setSize((s) => (s.w === w && s.h === h ? s : { w, h }));
        });
        ro.observe(el);
        return () => ro.disconnect();
    }, []);
    return [ref, size];
}

function sizeCanvas(el: HTMLCanvasElement, w: number, h: number, dpr: number): CanvasRenderingContext2D | null {
    const W = Math.max(1, Math.round(w * dpr));
    const H = Math.max(1, Math.round(h * dpr));
    if (el.width !== W) el.width = W;
    if (el.height !== H) el.height = H;
    return el.getContext("2d");
}

export interface DitherBarDatum {
    key: string;
    value: number;
    /** Tooltip line; falls back to `key · value`. */
    hint?: string;
}

export function DitherBarChart({
    data,
    height = 72,
    tone = "sky",
    className,
    renderTooltip,
    ghost,
    selected = null,
    onSelect,
}: {
    data: DitherBarDatum[];
    height?: number;
    tone?: DitherTone;
    className?: string;
    renderTooltip?: (d: DitherBarDatum) => React.ReactNode;
    /** Background reference bars (e.g. a target volume), same scale as data. */
    ghost?: number[];
    /** Index drawn at full density (paired with onSelect for tap-to-pin). */
    selected?: number | null;
    onSelect?: (i: number) => void;
}) {
    const reduced = useReducedMotion();
    const [wrapRef, { w }] = useMeasure<HTMLDivElement>();
    const canvasRef = React.useRef<HTMLCanvasElement | null>(null);
    const [hover, setHover] = React.useState<number | null>(null);
    // Per-bar entrance progress, mutated by the rAF loop below.
    const progressRef = React.useRef<number[]>([]);

    const paint = React.useCallback(() => {
        const el = canvasRef.current;
        if (!el || w <= 0) return;
        const dpr = Math.max(1, Math.floor(window.devicePixelRatio || 1));
        const ctx = sizeCanvas(el, w, height, dpr);
        if (!ctx) return;
        const W = el.width;
        const H = el.height;
        const img = ctx.createImageData(W, H);
        const buf = img.data;
        const [cr, cg, cb] = TONES[tone];
        const n = data.length;
        if (n > 0) {
            const slot = W / n;
            const gap = Math.min(2 * dpr, slot * 0.3);
            const max = Math.max(1, ...data.map((d) => d.value), ...(ghost ?? []));
            for (let i = 0; i < n; i++) {
                const p = progressRef.current[i] ?? 1;
                const x0 = Math.round(i * slot);
                const x1 = Math.max(x0 + dpr, Math.round((i + 1) * slot - gap));
                const hot = i === hover || i === selected;
                const g = ghost?.[i] ?? 0;
                if (g > 0) {
                    // Reference bar behind the data: solid track color.
                    const gTop = H - (g / max) * p * H;
                    for (let x = x0; x < x1 && x < W; x++) {
                        for (let y = Math.max(0, Math.floor(gTop)); y < H; y++) {
                            const o = (y * W + x) * 4;
                            buf[o] = TRACK[0];
                            buf[o + 1] = TRACK[1];
                            buf[o + 2] = TRACK[2];
                            buf[o + 3] = 255;
                        }
                    }
                }
                const stub = data[i].value > 0 ? 3 * dpr : 1.5 * dpr;
                const barH = Math.max(stub, (data[i].value / max) * p * H);
                const top = H - barH;
                for (let x = x0; x < x1 && x < W; x++) {
                    for (let y = Math.max(0, Math.floor(top)); y < H; y++) {
                        const rel = (y - top) / barH;
                        // Solid cap, then density fades toward the baseline.
                        const a = hot ? 1 : y - top <= 1.5 * dpr ? 1 : 0.92 - 0.68 * rel;
                        if (a < bayer(x, y, dpr)) continue;
                        const o = (y * W + x) * 4;
                        buf[o] = cr;
                        buf[o + 1] = cg;
                        buf[o + 2] = cb;
                        buf[o + 3] = hot ? 255 : FILL_ALPHA;
                    }
                }
            }
        }
        ctx.putImageData(img, 0, 0);
    }, [data, w, height, tone, hover, ghost, selected]);

    const paintRef = React.useRef(paint);
    React.useEffect(() => {
        paintRef.current = paint;
        paint();
    }, [paint]);

    // Entrance: bars grow up with a light left-to-right stagger.
    React.useEffect(() => {
        const n = data.length;
        if (n === 0) return;
        if (reduced) {
            progressRef.current = data.map(() => 1);
            paintRef.current();
            return;
        }
        progressRef.current = data.map(() => 0);
        const t0 = performance.now();
        const dur = 420;
        const span = 260;
        let raf = 0;
        const tick = (now: number) => {
            const t = now - t0;
            let done = true;
            progressRef.current = data.map((_, i) => {
                const c = clamp01((t - (i / n) * span) / dur);
                if (c < 1) done = false;
                return easeOutCubic(c);
            });
            paintRef.current();
            if (!done) raf = requestAnimationFrame(tick);
        };
        raf = requestAnimationFrame(tick);
        return () => cancelAnimationFrame(raf);
    }, [data, reduced]);

    const onMove = (e: React.PointerEvent<HTMLDivElement>) => {
        if (data.length === 0 || w <= 0) return;
        const rect = e.currentTarget.getBoundingClientRect();
        const i = Math.floor(((e.clientX - rect.left) / rect.width) * data.length);
        setHover(i >= 0 && i < data.length ? i : null);
    };

    const d = hover !== null ? data[hover] : null;
    const tipLeft = hover !== null ? Math.max(48, Math.min(w - 48, ((hover + 0.5) / data.length) * w)) : 0;

    return (
        <div
            ref={wrapRef}
            className={`relative ${onSelect ? "cursor-pointer" : ""} ${className ?? ""}`}
            style={{ height }}
            onPointerMove={onMove}
            onPointerLeave={() => setHover(null)}
            onClick={(e) => {
                if (!onSelect || data.length === 0 || w <= 0) return;
                const rect = e.currentTarget.getBoundingClientRect();
                const i = Math.floor(((e.clientX - rect.left) / rect.width) * data.length);
                if (i >= 0 && i < data.length) onSelect(i);
            }}
        >
            <canvas ref={canvasRef} className="absolute inset-0 block h-full w-full" />
            {d && (
                <div
                    className="pointer-events-none absolute z-10 -translate-x-1/2 whitespace-nowrap rounded-md border border-slate-200 bg-white px-2 py-1 text-[10.5px] text-slate-600 shadow-sm"
                    style={{ left: tipLeft, bottom: height + 4 }}
                >
                    {renderTooltip ? renderTooltip(d) : (d.hint ?? `${d.key} · ${d.value.toLocaleString()}`)}
                </div>
            )}
        </div>
    );
}

// Monotone cubic (Fritsch-Carlson) tangents for unit-spaced points, so the
// curve never overshoots the data.
function monotoneTangents(ys: number[]): number[] {
    const n = ys.length;
    if (n < 2) return ys.map(() => 0);
    const d: number[] = [];
    for (let i = 0; i < n - 1; i++) d.push(ys[i + 1] - ys[i]);
    const m: number[] = new Array(n).fill(0);
    m[0] = d[0];
    m[n - 1] = d[n - 2];
    for (let i = 1; i < n - 1; i++) {
        m[i] = d[i - 1] * d[i] <= 0 ? 0 : (d[i - 1] + d[i]) / 2;
    }
    for (let i = 0; i < n - 1; i++) {
        if (d[i] === 0) {
            m[i] = 0;
            m[i + 1] = 0;
            continue;
        }
        const a = m[i] / d[i];
        const b = m[i + 1] / d[i];
        const s = a * a + b * b;
        if (s > 9) {
            const t = 3 / Math.sqrt(s);
            m[i] = t * a * d[i];
            m[i + 1] = t * b * d[i];
        }
    }
    return m;
}

// Chart paddings in CSS px; shared by the canvas painter and the DOM overlay
// so the crosshair dot lands exactly on the drawn line. Exported so wrappers
// (MultiTrend gridlines) can map values to the same y coordinates.
export const AREA_PAD_TOP = 5;
export const AREA_PAD_BOTTOM = 1;

// Area charts render on a low-res backing canvas scaled up `pixelated` —
// chunky 2 CSS px dither cells are the texture, per the dither-kit recipe.
const CELL = 2;
const MAX_COLS = 520;
const MAX_ROWS = 200;
// 4x4 thresholds are the right coarseness at cell scale (8x8 is for the
// fine-grained device-px surfaces: bars, meters, rings).
const BAYER4 = [
    [0, 8, 2, 10],
    [12, 4, 14, 6],
    [3, 11, 1, 9],
    [15, 7, 13, 5],
].map((row) => row.map((v) => (v + 0.5) / 16));

function sizeCellCanvas(el: HTMLCanvasElement, w: number, h: number) {
    const cols = Math.min(MAX_COLS, Math.max(8, Math.round(w / CELL)));
    const rows = Math.min(MAX_ROWS, Math.max(8, Math.round(h / CELL)));
    if (el.width !== cols) el.width = cols;
    if (el.height !== rows) el.height = rows;
    return { ctx: el.getContext("2d"), cols, rows };
}

// Paint one backing column of an area band: densest just under the value
// line, dissolving toward the baseline — the airy read of the dashboard's
// gradient sparklines, but as chunky ordered-dither cells. Cells that miss
// the threshold still get a whisper of the same color so the fill stays a
// continuous field instead of speckle on white. The smooth value line is
// stroked on a separate full-res canvas, so the fill starts strictly below
// the curve (ceil) and never pokes dots above it. `sparse` raises the
// threshold to thin out front layers so overlapping series stay readable.
function paintAreaColumn(
    ctx: CanvasRenderingContext2D,
    x: number,
    top: number,
    rows: number,
    rgb: readonly [number, number, number],
    sparse: number,
    lift: number,
) {
    const [r, g, b] = rgb;
    const t = Math.max(0, Math.ceil(top));
    const depth = rows - t;
    if (depth <= 0) return;
    for (let y = t; y < rows; y++) {
        const density = 1 - (y - t) / depth;
        // Hover lift: slightly more dots, slightly brighter — the fill leans
        // in while the pointer is over the chart.
        const lit = density > BAYER4[y & 3][x & 3] + sparse - 0.1 * lift;
        const k = (0.05 + 0.27 * density) * (1 + 0.22 * lift);
        ctx.fillStyle = `rgba(${r},${g},${b},${(lit ? k : k * 0.3).toFixed(3)})`;
        ctx.fillRect(x, y, 1, 1);
    }
}

// Paint one backing column where several bands overlap. Layering per-series
// dots reads as two-color confetti, so instead every covering band
// contributes to ONE blended hue per cell — overlaps deepen and shift color
// like overlapping watercolor washes, rendered in a single coherent dot
// pattern. Lines on top keep the pure series colors for identification.
function paintBlendedColumn(
    ctx: CanvasRenderingContext2D,
    x: number,
    tops: number[],
    rows: number,
    rgbs: (readonly [number, number, number])[],
    lift: number,
) {
    const S = tops.length;
    let yStart = rows;
    for (let s = 0; s < S; s++) yStart = Math.min(yStart, Math.ceil(tops[s]));
    for (let y = Math.max(0, yStart); y < rows; y++) {
        let wSum = 0;
        let r = 0;
        let g = 0;
        let b = 0;
        let trans = 1;
        let dMax = 0;
        for (let s = 0; s < S; s++) {
            const t = tops[s];
            if (y < Math.ceil(t)) continue;
            const density = 1 - (y - t) / Math.max(1, rows - t);
            if (density > dMax) dMax = density;
            const a = (0.05 + 0.27 * density) * (1 + 0.22 * lift);
            wSum += a;
            r += a * rgbs[s][0];
            g += a * rgbs[s][1];
            b += a * rgbs[s][2];
            trans *= 1 - a;
        }
        if (wSum <= 0) continue;
        // Stacked coverage deepens the wash, capped so it stays airy.
        const A = Math.min(0.5, 1 - trans);
        const lit = dMax > BAYER4[y & 3][x & 3] - 0.1 * lift;
        ctx.fillStyle = `rgba(${Math.round(r / wSum)},${Math.round(g / wSum)},${Math.round(
            b / wSum,
        )},${(lit ? A : A * 0.3).toFixed(3)})`;
        ctx.fillRect(x, y, 1, 1);
    }
}

// Smooth anti-aliased value lines on the full-res overlay canvas, revealed
// left-to-right in sync with the dithered fill underneath.
function strokeCurves(
    el: HTMLCanvasElement | null,
    w: number,
    h: number,
    curves: { ys: number[]; rgb: readonly [number, number, number] }[],
    reveal: number,
) {
    if (!el || w <= 0) return;
    const dpr = Math.max(1, Math.floor(window.devicePixelRatio || 1));
    const ctx = sizeCanvas(el, w, h, dpr);
    if (!ctx) return;
    const W = el.width;
    ctx.clearRect(0, 0, W, el.height);
    const maxX = Math.floor(reveal * W);
    if (maxX <= 0) return;
    ctx.lineWidth = 1.5 * dpr;
    ctx.lineJoin = "round";
    ctx.lineCap = "round";
    for (const c of curves) {
        const pts = sampleCurve(c.ys, monotoneTangents(c.ys), W);
        ctx.beginPath();
        ctx.moveTo(0, pts[0]);
        for (let x = 1; x <= maxX && x < W; x++) ctx.lineTo(x, pts[x]);
        ctx.strokeStyle = `rgb(${c.rgb[0]},${c.rgb[1]},${c.rgb[2]})`;
        ctx.stroke();
    }
}

// Hermite spline through per-point cell rows, sampled at one y per column.
function sampleCurve(ys: number[], tans: number[], cols: number): number[] {
    const n = ys.length;
    const out = new Array<number>(cols);
    for (let x = 0; x < cols; x++) {
        const t = n > 1 ? (x / Math.max(1, cols - 1)) * (n - 1) : 0;
        const i = Math.min(n - 2, Math.max(0, Math.floor(t)));
        const hh = n > 1 ? t - i : 0;
        if (n === 1) {
            out[x] = ys[0];
            continue;
        }
        const h00 = 2 * hh ** 3 - 3 * hh ** 2 + 1;
        const h10 = hh ** 3 - 2 * hh ** 2 + hh;
        const h01 = -2 * hh ** 3 + 3 * hh ** 2;
        const h11 = hh ** 3 - hh ** 2;
        out[x] = h00 * ys[i] + h10 * tans[i] + h01 * ys[i + 1] + h11 * tans[i + 1];
    }
    return out;
}

// Latest painted geometry, published by paint() for the sparkle layer.
type AreaGeom = { curves: number[][]; cols: number; rows: number; points: number };

// Drives area-chart motion: the first paint (or a shape change) sweeps in
// left-to-right; a same-shape data change glides every point toward its new
// value instead of replaying the entrance; hovering eases a density lift.
// All hot state lives in refs — `repaint` runs once per frame while moving.
function useAreaMotion(target: number[][], repaint: () => void, reduced: boolean | null) {
    const disp = React.useRef<number[][]>([]);
    const reveal = React.useRef(1);
    const revealT0 = React.useRef(0);
    const intensity = React.useRef(0);
    const hoverLift = React.useRef(0);
    const targetRef = React.useRef(target);
    const raf = React.useRef(0);

    const run = React.useCallback(() => {
        cancelAnimationFrame(raf.current);
        const step = (now: number) => {
            let moving = false;
            if (revealT0.current > 0) {
                const c = clamp01((now - revealT0.current) / 700);
                reveal.current = easeOutCubic(c);
                if (c < 1) moving = true;
                else revealT0.current = 0;
            }
            const tgt = targetRef.current;
            const d = disp.current;
            for (let s = 0; s < tgt.length; s++) {
                for (let i = 0; i < tgt[s].length; i++) {
                    const diff = tgt[s][i] - d[s][i];
                    if (Math.abs(diff) > 0.002) {
                        d[s][i] += diff * 0.16;
                        moving = true;
                    } else d[s][i] = tgt[s][i];
                }
            }
            const li = hoverLift.current - intensity.current;
            if (Math.abs(li) > 0.004) {
                intensity.current += li * 0.16;
                moving = true;
            } else intensity.current = hoverLift.current;
            repaint();
            if (moving) raf.current = requestAnimationFrame(step);
        };
        raf.current = requestAnimationFrame(step);
    }, [repaint]);

    React.useEffect(() => {
        targetRef.current = target;
        const d = disp.current;
        const sameShape =
            d.length === target.length && d.every((row, s) => row.length === target[s].length);
        if (reduced || !sameShape) {
            disp.current = target.map((r) => r.slice());
            if (reduced) {
                reveal.current = 1;
            } else {
                reveal.current = 0;
                revealT0.current = performance.now();
            }
        }
        run();
        return () => cancelAnimationFrame(raf.current);
    }, [target, reduced, run]);

    const setLift = React.useCallback(
        (on: boolean) => {
            hoverLift.current = on ? 1 : 0;
            run();
        },
        [run],
    );

    return { disp, reveal, intensity, setLift };
}

// Winking sparkles, dither-kit style: seeded single cells inside the fill
// that glint in the series color, flaring into a 4-point glint at the peak
// of the wink. They live on their own cell-res canvas repainted on a slow
// tick, so the fill and line layers never repaint for them.
type SparkStar = { s: number; xi: number; depth: number; phase: number };

function useSparkles(
    starRef: React.RefObject<HTMLCanvasElement | null>,
    geomRef: React.RefObject<AreaGeom | null>,
    tones: readonly (readonly [number, number, number])[],
    reveal: React.RefObject<number>,
    intensity: React.RefObject<number>,
    reduced: boolean | null,
) {
    React.useEffect(() => {
        if (reduced) return;
        let tick = 0;
        let stars: SparkStar[] = [];
        let starKey = "";
        const id = window.setInterval(() => {
            const el = starRef.current;
            const g = geomRef.current;
            if (!el || !g || g.cols <= 0 || g.points === 0) return;
            const key = `${tones.length}|${g.points}|${g.cols}`;
            if (key !== starKey) {
                starKey = key;
                stars = [];
                const per = Math.max(3, Math.round(g.cols / 28));
                for (let s = 0; s < tones.length; s++) {
                    for (let i = 0; i < per; i++) {
                        const seed = i * 67 + 13 + s * 131;
                        stars.push({
                            s,
                            xi: seed % g.points,
                            depth: ((seed * 53 + 7) % 100) / 100,
                            phase: (seed * 41) % 360,
                        });
                    }
                }
            }
            if (el.width !== g.cols) el.width = g.cols;
            if (el.height !== g.rows) el.height = g.rows;
            const ctx = el.getContext("2d");
            if (!ctx) return;
            tick += 1;
            ctx.clearRect(0, 0, g.cols, g.rows);
            const maxX = reveal.current * g.cols;
            for (const star of stars) {
                const curve = g.curves[star.s];
                if (!curve) continue;
                const sx = Math.round((star.xi / Math.max(g.points - 1, 1)) * (g.cols - 1));
                if (sx > maxX) continue;
                const top = curve[sx] ?? 0;
                const sy = Math.round(top + star.depth * (g.rows - top));
                const tw = (Math.sin((tick + star.phase) * 0.35) + 1) / 2;
                const lift = tw * (0.7 + 0.3 * intensity.current);
                if (lift < 0.55 || sy <= top || sy >= g.rows) continue;
                const [r, gg, b] = tones[star.s];
                ctx.fillStyle = `rgba(${r},${gg},${b},${lift.toFixed(3)})`;
                ctx.fillRect(sx, sy, 1, 1);
                if (tw > 0.9) {
                    ctx.fillStyle = `rgba(${r},${gg},${b},${(lift * 0.6 * (tw - 0.9) * 10).toFixed(3)})`;
                    ctx.fillRect(sx - 1, sy, 1, 1);
                    ctx.fillRect(sx + 1, sy, 1, 1);
                    ctx.fillRect(sx, sy - 1, 1, 1);
                    ctx.fillRect(sx, sy + 1, 1, 1);
                }
            }
        }, 130);
        return () => window.clearInterval(id);
    }, [starRef, geomRef, tones, reveal, intensity, reduced]);
}

export function DitherAreaChart({
    data,
    height = 96,
    tone = "sky",
    className,
    renderTooltip,
}: {
    data: DitherBarDatum[];
    height?: number;
    tone?: DitherTone;
    className?: string;
    renderTooltip?: (d: DitherBarDatum) => React.ReactNode;
}) {
    const reduced = useReducedMotion();
    const [wrapRef, { w }] = useMeasure<HTMLDivElement>();
    const canvasRef = React.useRef<HTMLCanvasElement | null>(null);
    const lineRef = React.useRef<HTMLCanvasElement | null>(null);
    const starRef = React.useRef<HTMLCanvasElement | null>(null);
    const geomRef = React.useRef<AreaGeom | null>(null);
    const [hover, setHover] = React.useState<number | null>(null);

    const max = Math.max(1, ...data.map((d) => d.value));
    const yCssFor = React.useCallback(
        (v: number) => AREA_PAD_TOP + (1 - v / max) * (height - AREA_PAD_TOP - AREA_PAD_BOTTOM),
        [max, height],
    );

    const target = React.useMemo(() => [data.map((d) => d.value)], [data]);
    const paintRef = React.useRef<() => void>(() => {});
    const repaint = React.useCallback(() => paintRef.current(), []);
    const { disp, reveal, intensity, setLift } = useAreaMotion(target, repaint, reduced);

    const paint = React.useCallback(() => {
        const el = canvasRef.current;
        if (!el || w <= 0) return;
        const sized = sizeCellCanvas(el, w, height);
        const ctx = sized.ctx;
        if (!ctx) return;
        const { cols, rows } = sized;
        ctx.clearRect(0, 0, cols, rows);
        const n = data.length;
        if (n === 0) return;
        const vals = disp.current[0] ?? data.map((d) => d.value);
        const ys = vals.map((v) => (yCssFor(v) / height) * rows);
        const curve = sampleCurve(ys, monotoneTangents(ys), cols);
        const rgb = TONES[tone];
        const revealX = reveal.current * cols;
        const lift = intensity.current;
        for (let x = 0; x < cols; x++) {
            if (x > revealX) break;
            paintAreaColumn(ctx, x, curve[x], rows, rgb, 0, lift);
        }
        geomRef.current = { curves: [curve], cols, rows, points: n };
        const dpr = Math.max(1, Math.floor(window.devicePixelRatio || 1));
        strokeCurves(
            lineRef.current,
            w,
            height,
            [{ ys: vals.map((v) => yCssFor(v) * dpr), rgb }],
            reveal.current,
        );
    }, [data, w, height, tone, yCssFor, disp, reveal, intensity]);

    React.useEffect(() => {
        paintRef.current = paint;
        paint();
    }, [paint]);

    const tones = React.useMemo(() => [TONES[tone]] as const, [tone]);
    useSparkles(starRef, geomRef, tones, reveal, intensity, reduced);

    const onMove = (e: React.PointerEvent<HTMLDivElement>) => {
        if (data.length === 0 || w <= 0) return;
        const rect = e.currentTarget.getBoundingClientRect();
        const i = Math.round(((e.clientX - rect.left) / rect.width) * (data.length - 1));
        setHover(i >= 0 && i < data.length ? i : null);
    };

    const d = hover !== null ? data[hover] : null;
    const hoverX = hover !== null && data.length > 1 ? (hover / (data.length - 1)) * w : w / 2;
    const [cr, cg, cb] = TONES[tone];

    return (
        <div
            ref={wrapRef}
            className={`relative ${className ?? ""}`}
            style={{ height }}
            onPointerEnter={() => setLift(true)}
            onPointerMove={onMove}
            onPointerLeave={() => {
                setHover(null);
                setLift(false);
            }}
        >
            <canvas
                ref={canvasRef}
                className="absolute inset-0 block h-full w-full"
                style={{ imageRendering: "pixelated" }}
            />
            <canvas
                ref={starRef}
                className="pointer-events-none absolute inset-0 block h-full w-full"
                style={{ imageRendering: "pixelated" }}
            />
            <canvas ref={lineRef} className="absolute inset-0 block h-full w-full" />
            {d && (
                <>
                    <div
                        className="pointer-events-none absolute inset-y-0 border-l border-dashed border-slate-300"
                        style={{ left: hoverX }}
                    />
                    <div
                        className="pointer-events-none absolute size-2 -translate-x-1/2 -translate-y-1/2 rounded-full border-2 border-white shadow-sm"
                        style={{ left: hoverX, top: yCssFor(d.value), backgroundColor: `rgb(${cr}, ${cg}, ${cb})` }}
                    />
                    <div
                        className="pointer-events-none absolute z-10 -translate-x-1/2 whitespace-nowrap rounded-md border border-slate-200 bg-white px-2 py-1 text-[10.5px] text-slate-600 shadow-sm"
                        style={{
                            left: Math.max(56, Math.min(w - 56, hoverX)),
                            // Sit above the hovered dot; flip below when the
                            // line runs close to the top edge.
                            top: yCssFor(d.value) > 40 ? yCssFor(d.value) - 32 : yCssFor(d.value) + 10,
                        }}
                    >
                        {renderTooltip ? renderTooltip(d) : (d.hint ?? `${d.key} · ${d.value.toLocaleString()}`)}
                    </div>
                </>
            )}
        </div>
    );
}

export interface DitherAreaSeries {
    label: string;
    tone: DitherTone;
    values: number[];
}

// DitherMultiAreaChart — several metrics composed on one graph: a smooth
// anti-aliased line per series over one blended dither fill (overlapping
// bands mix into a single hue per cell rather than interleaving dots). One
// shared crosshair lists every series' value for the hovered day.
export function DitherMultiAreaChart({
    labels,
    series,
    height = 96,
    className,
    renderTooltip,
}: {
    labels: string[];
    series: DitherAreaSeries[];
    height?: number;
    className?: string;
    renderTooltip?: (i: number) => React.ReactNode;
}) {
    const reduced = useReducedMotion();
    const [wrapRef, { w }] = useMeasure<HTMLDivElement>();
    const canvasRef = React.useRef<HTMLCanvasElement | null>(null);
    const lineRef = React.useRef<HTMLCanvasElement | null>(null);
    const starRef = React.useRef<HTMLCanvasElement | null>(null);
    const geomRef = React.useRef<AreaGeom | null>(null);
    const [hover, setHover] = React.useState<number | null>(null);

    const max = Math.max(1, ...series.flatMap((s) => s.values));
    const yCssFor = React.useCallback(
        (v: number) => AREA_PAD_TOP + (1 - v / max) * (height - AREA_PAD_TOP - AREA_PAD_BOTTOM),
        [max, height],
    );

    const target = React.useMemo(() => series.map((s) => s.values), [series]);
    const paintRef = React.useRef<() => void>(() => {});
    const repaint = React.useCallback(() => paintRef.current(), []);
    const { disp, reveal, intensity, setLift } = useAreaMotion(target, repaint, reduced);

    const paint = React.useCallback(() => {
        const el = canvasRef.current;
        if (!el || w <= 0) return;
        const sized = sizeCellCanvas(el, w, height);
        const ctx = sized.ctx;
        if (!ctx) return;
        const { cols, rows } = sized;
        ctx.clearRect(0, 0, cols, rows);
        const n = labels.length;
        const S = series.length;
        if (n === 0 || S === 0) return;
        const revealX = reveal.current * cols;
        const lift = intensity.current;
        const curves: number[][] = [];
        const rgbs: (readonly [number, number, number])[] = [];
        for (let s = 0; s < S; s++) {
            const vals = disp.current[s] ?? series[s].values;
            const ys = vals.map((v) => (yCssFor(v) / height) * rows);
            curves.push(sampleCurve(ys, monotoneTangents(ys), cols));
            rgbs.push(TONES[series[s].tone]);
        }
        const tops = new Array<number>(S);
        for (let x = 0; x < cols; x++) {
            if (x > revealX) break;
            for (let s = 0; s < S; s++) tops[s] = curves[s][x];
            paintBlendedColumn(ctx, x, tops, rows, rgbs, lift);
        }
        geomRef.current = { curves, cols, rows, points: n };
        const dpr = Math.max(1, Math.floor(window.devicePixelRatio || 1));
        strokeCurves(
            lineRef.current,
            w,
            height,
            series.map((s, si) => ({
                ys: (disp.current[si] ?? s.values).map((v) => yCssFor(v) * dpr),
                rgb: TONES[s.tone],
            })),
            reveal.current,
        );
    }, [labels, series, w, height, yCssFor, disp, reveal, intensity]);

    React.useEffect(() => {
        paintRef.current = paint;
        paint();
    }, [paint]);

    const tones = React.useMemo(() => series.map((s) => TONES[s.tone]), [series]);
    useSparkles(starRef, geomRef, tones, reveal, intensity, reduced);

    const onMove = (e: React.PointerEvent<HTMLDivElement>) => {
        if (labels.length === 0 || w <= 0) return;
        const rect = e.currentTarget.getBoundingClientRect();
        const i = Math.round(((e.clientX - rect.left) / rect.width) * (labels.length - 1));
        setHover(i >= 0 && i < labels.length ? i : null);
    };

    const hoverX = hover !== null && labels.length > 1 ? (hover / (labels.length - 1)) * w : w / 2;
    const flip = hoverX > w / 2;

    return (
        <div
            ref={wrapRef}
            className={`relative ${className ?? ""}`}
            style={{ height }}
            onPointerEnter={() => setLift(true)}
            onPointerMove={onMove}
            onPointerLeave={() => {
                setHover(null);
                setLift(false);
            }}
        >
            <canvas
                ref={canvasRef}
                className="absolute inset-0 block h-full w-full"
                style={{ imageRendering: "pixelated" }}
            />
            <canvas
                ref={starRef}
                className="pointer-events-none absolute inset-0 block h-full w-full"
                style={{ imageRendering: "pixelated" }}
            />
            <canvas ref={lineRef} className="absolute inset-0 block h-full w-full" />
            {hover !== null && (
                <>
                    <div
                        className="pointer-events-none absolute inset-y-0 border-l border-dashed border-slate-300"
                        style={{ left: hoverX }}
                    />
                    {series.map((s) => {
                        const [cr, cg, cb] = TONES[s.tone];
                        return (
                            <div
                                key={s.label}
                                className="pointer-events-none absolute size-2 -translate-x-1/2 -translate-y-1/2 rounded-full border-2 border-white shadow-sm"
                                style={{
                                    left: hoverX,
                                    top: yCssFor(s.values[hover] ?? 0),
                                    backgroundColor: `rgb(${cr}, ${cg}, ${cb})`,
                                }}
                            />
                        );
                    })}
                    <div
                        className={`pointer-events-none absolute top-1 z-10 rounded-md border border-slate-200 bg-white px-2 py-1 text-[10.5px] text-slate-600 shadow-sm ${
                            flip ? "-translate-x-full" : ""
                        }`}
                        style={{ left: flip ? hoverX - 8 : hoverX + 8 }}
                    >
                        {renderTooltip ? (
                            renderTooltip(hover)
                        ) : (
                            <>
                                <div className="font-medium text-slate-900 whitespace-nowrap">{labels[hover]}</div>
                                {series.map((s) => {
                                    const [cr, cg, cb] = TONES[s.tone];
                                    return (
                                        <div key={s.label} className="flex items-center gap-1.5 whitespace-nowrap">
                                            <span
                                                className="size-1.5 rounded-full"
                                                style={{ backgroundColor: `rgb(${cr}, ${cg}, ${cb})` }}
                                            />
                                            <span>{s.label}</span>
                                            <span className="ml-auto pl-2 font-mono tabular-nums text-slate-700">
                                                {(s.values[hover] ?? 0).toLocaleString()}
                                            </span>
                                        </div>
                                    );
                                })}
                            </>
                        )}
                    </div>
                </>
            )}
        </div>
    );
}

export function DitherMeter({
    frac,
    tone = "sky",
    height = 4,
    animate = true,
    className,
}: {
    frac: number;
    tone?: DitherTone;
    height?: number;
    animate?: boolean;
    className?: string;
}) {
    const reduced = useReducedMotion();
    const [wrapRef, { w }] = useMeasure<HTMLDivElement>();
    const canvasRef = React.useRef<HTMLCanvasElement | null>(null);
    const curRef = React.useRef(0);

    const paint = React.useCallback(
        (cur: number) => {
            const el = canvasRef.current;
            if (!el || w <= 0) return;
            const dpr = Math.max(1, Math.floor(window.devicePixelRatio || 1));
            const ctx = sizeCanvas(el, w, height, dpr);
            if (!ctx) return;
            const W = el.width;
            const H = el.height;
            const img = ctx.createImageData(W, H);
            const buf = img.data;
            const [cr, cg, cb] = TONES[tone];
            const fillEnd = cur * W;
            const ramp = Math.min(6 * dpr, W);
            for (let y = 0; y < H; y++) {
                for (let x = 0; x < W; x++) {
                    const o = (y * W + x) * 4;
                    let a = 0;
                    if (x < fillEnd) {
                        a = fillEnd - x < ramp ? 0.95 * ((fillEnd - x) / ramp) : 0.95;
                    }
                    if (a >= bayer(x, y, dpr)) {
                        buf[o] = cr;
                        buf[o + 1] = cg;
                        buf[o + 2] = cb;
                        buf[o + 3] = 255;
                    } else {
                        buf[o] = TRACK[0];
                        buf[o + 1] = TRACK[1];
                        buf[o + 2] = TRACK[2];
                        buf[o + 3] = 255;
                    }
                }
            }
            ctx.putImageData(img, 0, 0);
        },
        [w, height, tone],
    );

    React.useEffect(() => {
        const target = clamp01(frac);
        if (!animate || reduced) {
            curRef.current = target;
            paint(target);
            return;
        }
        let raf = 0;
        const tick = () => {
            const diff = target - curRef.current;
            if (Math.abs(diff) < 0.004) {
                curRef.current = target;
                paint(target);
                return;
            }
            curRef.current += diff * 0.16;
            paint(curRef.current);
            raf = requestAnimationFrame(tick);
        };
        raf = requestAnimationFrame(tick);
        return () => cancelAnimationFrame(raf);
    }, [frac, animate, reduced, paint]);

    return (
        <div ref={wrapRef} className={`overflow-hidden rounded-full ${className ?? ""}`} style={{ height }}>
            <canvas ref={canvasRef} className="block h-full w-full" />
        </div>
    );
}

// DitherColumns — vertical stacked columns with one dithered segment per
// part (e.g. 2xx/4xx/5xx traffic). Parts stack bottom-up in array order.
export function DitherColumns({
    data,
    tones,
    height = 140,
    className,
    onHover,
}: {
    data: { key: string; parts: number[] }[];
    tones: DitherTone[];
    height?: number;
    className?: string;
    onHover?: (i: number | null) => void;
}) {
    const reduced = useReducedMotion();
    const [wrapRef, { w }] = useMeasure<HTMLDivElement>();
    const canvasRef = React.useRef<HTMLCanvasElement | null>(null);
    const [hover, setHover] = React.useState<number | null>(null);
    const progressRef = React.useRef<number[]>([]);

    const paint = React.useCallback(() => {
        const el = canvasRef.current;
        if (!el || w <= 0) return;
        const dpr = Math.max(1, Math.floor(window.devicePixelRatio || 1));
        const ctx = sizeCanvas(el, w, height, dpr);
        if (!ctx) return;
        const W = el.width;
        const H = el.height;
        const img = ctx.createImageData(W, H);
        const buf = img.data;
        const n = data.length;
        if (n > 0) {
            const slot = W / n;
            const gap = Math.min(2 * dpr, slot * 0.3);
            const max = Math.max(1, ...data.map((d) => d.parts.reduce((s, v) => s + v, 0)));
            for (let i = 0; i < n; i++) {
                const p = progressRef.current[i] ?? 1;
                const x0 = Math.round(i * slot);
                const x1 = Math.max(x0 + dpr, Math.round((i + 1) * slot - gap));
                const hot = i === hover;
                // Segment boundaries from the baseline up, scaled by entrance.
                let acc = 0;
                for (let s = 0; s < data[i].parts.length; s++) {
                    const v = data[i].parts[s];
                    if (v <= 0) continue;
                    const y1 = H - (acc / max) * p * H;
                    acc += v;
                    const y0 = H - (acc / max) * p * H;
                    const rgb = TONES[tones[s] ?? "sky"];
                    for (let x = x0; x < x1 && x < W; x++) {
                        for (let y = Math.max(0, Math.floor(y0)); y < Math.min(H, Math.ceil(y1)); y++) {
                            const a = hot ? 1 : 0.85;
                            if (a < bayer(x, y, dpr)) continue;
                            const o = (y * W + x) * 4;
                            buf[o] = rgb[0];
                            buf[o + 1] = rgb[1];
                            buf[o + 2] = rgb[2];
                            buf[o + 3] = hot ? 255 : FILL_ALPHA;
                        }
                    }
                }
            }
        }
        ctx.putImageData(img, 0, 0);
    }, [data, tones, w, height, hover]);

    const paintRef = React.useRef(paint);
    React.useEffect(() => {
        paintRef.current = paint;
        paint();
    }, [paint]);

    React.useEffect(() => {
        const n = data.length;
        if (n === 0) return;
        if (reduced) {
            progressRef.current = data.map(() => 1);
            paintRef.current();
            return;
        }
        progressRef.current = data.map(() => 0);
        const t0 = performance.now();
        const dur = 420;
        const span = 260;
        let raf = 0;
        const tick = (now: number) => {
            const t = now - t0;
            let done = true;
            progressRef.current = data.map((_, i) => {
                const c = clamp01((t - (i / n) * span) / dur);
                if (c < 1) done = false;
                return easeOutCubic(c);
            });
            paintRef.current();
            if (!done) raf = requestAnimationFrame(tick);
        };
        raf = requestAnimationFrame(tick);
        return () => cancelAnimationFrame(raf);
    }, [data, reduced]);

    const setHoverBoth = (i: number | null) => {
        setHover(i);
        onHover?.(i);
    };

    return (
        <div
            ref={wrapRef}
            className={`relative ${className ?? ""}`}
            style={{ height }}
            onPointerMove={(e) => {
                if (data.length === 0 || w <= 0) return;
                const rect = e.currentTarget.getBoundingClientRect();
                const i = Math.floor(((e.clientX - rect.left) / rect.width) * data.length);
                setHoverBoth(i >= 0 && i < data.length ? i : null);
            }}
            onPointerLeave={() => setHoverBoth(null)}
        >
            <canvas ref={canvasRef} className="absolute inset-0 block h-full w-full" />
        </div>
    );
}

// DitherStack — a horizontal share bar split into dithered segments (e.g.
// inbox / promotions / spam placement). Fractions should sum to <= 1; any
// remainder stays track-colored.
export function DitherStack({
    segments,
    height = 8,
    className,
}: {
    segments: { frac: number; tone: DitherTone }[];
    height?: number;
    className?: string;
}) {
    const reduced = useReducedMotion();
    const [wrapRef, { w }] = useMeasure<HTMLDivElement>();
    const canvasRef = React.useRef<HTMLCanvasElement | null>(null);
    const revealRef = React.useRef(1);

    const paint = React.useCallback(() => {
        const el = canvasRef.current;
        if (!el || w <= 0) return;
        const dpr = Math.max(1, Math.floor(window.devicePixelRatio || 1));
        const ctx = sizeCanvas(el, w, height, dpr);
        if (!ctx) return;
        const W = el.width;
        const H = el.height;
        const img = ctx.createImageData(W, H);
        const buf = img.data;
        // Segment boundaries in device px, revealed left to right.
        const reveal = revealRef.current;
        const bounds: { end: number; rgb: readonly [number, number, number] }[] = [];
        let acc = 0;
        for (const s of segments) {
            acc += clamp01(s.frac);
            bounds.push({ end: Math.min(1, acc) * W * reveal, rgb: TONES[s.tone] });
        }
        for (let y = 0; y < H; y++) {
            for (let x = 0; x < W; x++) {
                const o = (y * W + x) * 4;
                const seg = bounds.find((b) => x < b.end);
                if (seg && 0.9 >= bayer(x, y, dpr)) {
                    buf[o] = seg.rgb[0];
                    buf[o + 1] = seg.rgb[1];
                    buf[o + 2] = seg.rgb[2];
                } else {
                    buf[o] = TRACK[0];
                    buf[o + 1] = TRACK[1];
                    buf[o + 2] = TRACK[2];
                }
                buf[o + 3] = 255;
            }
        }
        ctx.putImageData(img, 0, 0);
    }, [segments, w, height]);

    const paintRef = React.useRef(paint);
    React.useEffect(() => {
        paintRef.current = paint;
        paint();
    }, [paint]);

    React.useEffect(() => {
        if (reduced) {
            revealRef.current = 1;
            paintRef.current();
            return;
        }
        revealRef.current = 0;
        const t0 = performance.now();
        const dur = 550;
        let raf = 0;
        const tick = (now: number) => {
            const c = clamp01((now - t0) / dur);
            revealRef.current = easeOutCubic(c);
            paintRef.current();
            if (c < 1) raf = requestAnimationFrame(tick);
        };
        raf = requestAnimationFrame(tick);
        return () => cancelAnimationFrame(raf);
    }, [segments, reduced]);

    return (
        <div ref={wrapRef} className={`overflow-hidden rounded-full ${className ?? ""}`} style={{ height }}>
            <canvas ref={canvasRef} className="block h-full w-full" />
        </div>
    );
}

export function DitherRing({
    frac,
    size = 72,
    thickness = 7,
    tone = "sky",
    className,
    children,
}: {
    frac: number;
    size?: number;
    thickness?: number;
    tone?: DitherTone;
    className?: string;
    children?: React.ReactNode;
}) {
    const reduced = useReducedMotion();
    const canvasRef = React.useRef<HTMLCanvasElement | null>(null);
    const curRef = React.useRef(0);

    const paint = React.useCallback(
        (cur: number) => {
            const el = canvasRef.current;
            if (!el) return;
            const dpr = Math.max(1, Math.floor(window.devicePixelRatio || 1));
            const ctx = sizeCanvas(el, size, size, dpr);
            if (!ctx) return;
            const S = el.width;
            const img = ctx.createImageData(S, S);
            const buf = img.data;
            const [cr, cg, cb] = TONES[tone];
            const c = S / 2;
            const rOut = c;
            const rIn = c - thickness * dpr;
            const TWO_PI = Math.PI * 2;
            for (let y = 0; y < S; y++) {
                for (let x = 0; x < S; x++) {
                    const dx = x - c;
                    const dy = y - c;
                    const dist = Math.sqrt(dx * dx + dy * dy);
                    if (dist < rIn || dist > rOut) continue;
                    const o = (y * S + x) * 4;
                    // Normalized angle: 0 at 12 o'clock, growing clockwise.
                    const t = (Math.atan2(dy, dx) + Math.PI / 2 + TWO_PI) % TWO_PI / TWO_PI;
                    if (cur > 0 && t <= cur) {
                        // Dithered tip on the leading 6% of the sweep.
                        const a = cur - t < 0.06 ? 0.95 * ((cur - t) / 0.06) : 0.95;
                        if (a >= bayer(x, y, dpr)) {
                            buf[o] = cr;
                            buf[o + 1] = cg;
                            buf[o + 2] = cb;
                            buf[o + 3] = 255;
                            continue;
                        }
                    }
                    buf[o] = TRACK[0];
                    buf[o + 1] = TRACK[1];
                    buf[o + 2] = TRACK[2];
                    buf[o + 3] = 255;
                }
            }
            ctx.putImageData(img, 0, 0);
        },
        [size, thickness, tone],
    );

    React.useEffect(() => {
        const target = clamp01(frac);
        if (reduced) {
            curRef.current = target;
            paint(target);
            return;
        }
        const from = curRef.current;
        const t0 = performance.now();
        const dur = 600;
        let raf = 0;
        const tick = (now: number) => {
            const p = easeOutCubic(clamp01((now - t0) / dur));
            curRef.current = from + (target - from) * p;
            paint(curRef.current);
            if (p < 1) raf = requestAnimationFrame(tick);
        };
        raf = requestAnimationFrame(tick);
        return () => cancelAnimationFrame(raf);
    }, [frac, reduced, paint]);

    return (
        <div className={`relative ${className ?? ""}`} style={{ width: size, height: size }}>
            <canvas ref={canvasRef} className="block h-full w-full" />
            {children && <div className="absolute inset-0 flex items-center justify-center">{children}</div>}
        </div>
    );
}

export function DitherSlider({
    value,
    min,
    max,
    step = 1,
    onChange,
    tone = "sky",
    disabled,
    className,
    label,
}: {
    value: number;
    min: number;
    max: number;
    step?: number;
    onChange: (v: number) => void;
    tone?: DitherTone;
    disabled?: boolean;
    className?: string;
    label?: string;
}) {
    const trackRef = React.useRef<HTMLDivElement | null>(null);
    const frac = max > min ? clamp01((value - min) / (max - min)) : 0;
    const [cr, cg, cb] = TONES[tone];

    const snap = (raw: number) => Math.max(min, Math.min(max, Math.round(raw / step) * step));

    const setFromClient = (clientX: number) => {
        const el = trackRef.current;
        if (!el) return;
        const rect = el.getBoundingClientRect();
        const f = clamp01((clientX - rect.left) / rect.width);
        const v = snap(min + f * (max - min));
        if (v !== value) onChange(v);
    };

    return (
        <div
            ref={trackRef}
            className={`relative flex h-5 select-none items-center ${disabled ? "opacity-50" : "cursor-pointer"} ${className ?? ""}`}
            onPointerDown={(e) => {
                if (disabled) return;
                e.currentTarget.setPointerCapture(e.pointerId);
                setFromClient(e.clientX);
            }}
            onPointerMove={(e) => {
                if (disabled) return;
                if (e.currentTarget.hasPointerCapture(e.pointerId)) setFromClient(e.clientX);
            }}
        >
            <div className="w-full">
                <DitherMeter frac={frac} tone={tone} height={4} animate={false} />
            </div>
            <button
                type="button"
                role="slider"
                aria-label={label ?? "Value"}
                aria-valuemin={min}
                aria-valuemax={max}
                aria-valuenow={value}
                disabled={disabled}
                onKeyDown={(e) => {
                    if (disabled) return;
                    if (e.key === "ArrowLeft" || e.key === "ArrowDown") {
                        e.preventDefault();
                        onChange(snap(value - step));
                    } else if (e.key === "ArrowRight" || e.key === "ArrowUp") {
                        e.preventDefault();
                        onChange(snap(value + step));
                    } else if (e.key === "Home") {
                        e.preventDefault();
                        onChange(min);
                    } else if (e.key === "End") {
                        e.preventDefault();
                        onChange(max);
                    }
                }}
                className="absolute size-3.5 -translate-x-1/2 rounded-full border-2 bg-white shadow-sm transition-shadow focus:outline-none focus-visible:ring-2 focus-visible:ring-sky-100"
                style={{ left: `${frac * 100}%`, borderColor: `rgb(${cr}, ${cg}, ${cb})` }}
            />
        </div>
    );
}

export function AnimatedNumber({
    value,
    className,
    format,
}: {
    value: number;
    className?: string;
    format?: (n: number) => string;
}) {
    const reduced = useReducedMotion();
    const [disp, setDisp] = React.useState(value);
    const prevRef = React.useRef(value);

    React.useEffect(() => {
        const from = prevRef.current;
        prevRef.current = value;
        if (reduced || from === value) {
            setDisp(value);
            return;
        }
        const t0 = performance.now();
        const dur = 450;
        let raf = 0;
        const tick = (now: number) => {
            const c = clamp01((now - t0) / dur);
            setDisp(from + (value - from) * easeOutCubic(c));
            if (c < 1) raf = requestAnimationFrame(tick);
        };
        raf = requestAnimationFrame(tick);
        return () => cancelAnimationFrame(raf);
    }, [value, reduced]);

    return <span className={className}>{format ? format(disp) : Math.round(disp).toLocaleString()}</span>;
}
