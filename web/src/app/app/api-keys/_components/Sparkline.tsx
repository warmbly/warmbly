// Tiny inline SVG charts. Two flavours:
//
//   <Sparkline values={...} />            // smooth single-series line
//   <StackedBars buckets={...} />         // 2xx / 4xx / 5xx stacked column chart
//
// Built without a chart library so the bundle stays light and the visual
// stays consistent with the dashboard chrome (hairlines, mono labels,
// no axis chrome unless asked).

import React from "react";

export function Sparkline({
    values,
    width = 120,
    height = 28,
    stroke = "#0284c7",
}: {
    values: number[];
    width?: number;
    height?: number;
    stroke?: string;
}) {
    if (!values || values.length === 0) {
        return <div style={{ width, height }} className="bg-slate-50 rounded" />;
    }

    const max = Math.max(1, ...values);
    const pad = 2;
    const w = width - pad * 2;
    const h = height - pad * 2;
    const step = values.length > 1 ? w / (values.length - 1) : 0;

    const points = values.map((v, i) => {
        const x = pad + i * step;
        const y = pad + h - (v / max) * h;
        return [x, y] as const;
    });

    const linePath = points
        .map(([x, y], i) => `${i === 0 ? "M" : "L"} ${x.toFixed(2)} ${y.toFixed(2)}`)
        .join(" ");

    const areaPath =
        linePath + ` L ${pad + (values.length - 1) * step} ${pad + h} L ${pad} ${pad + h} Z`;

    return (
        <svg width={width} height={height} viewBox={`0 0 ${width} ${height}`} className="block">
            <defs>
                <linearGradient id={`spark-${stroke.replace("#", "")}`} x1="0" y1="0" x2="0" y2="1">
                    <stop offset="0%" stopColor={stroke} stopOpacity="0.18" />
                    <stop offset="100%" stopColor={stroke} stopOpacity="0" />
                </linearGradient>
            </defs>
            <path d={areaPath} fill={`url(#spark-${stroke.replace("#", "")})`} />
            <path d={linePath} fill="none" stroke={stroke} strokeWidth={1.4} strokeLinejoin="round" strokeLinecap="round" />
        </svg>
    );
}

export interface BucketLike {
    bucket: string;
    success: number;
    client_errors: number;
    server_errors: number;
    total: number;
}

export function StackedBars({
    buckets,
    height = 140,
    onHoverBucket,
}: {
    buckets: BucketLike[];
    height?: number;
    onHoverBucket?: (b: BucketLike | null) => void;
}) {
    if (!buckets || buckets.length === 0) {
        return (
            <div
                style={{ height }}
                className="flex items-center justify-center text-[11.5px] text-slate-400 border border-dashed border-slate-200 rounded-md"
            >
                No traffic in this window
            </div>
        );
    }

    const max = Math.max(1, ...buckets.map((b) => b.total));
    const barGap = 2;

    return (
        <div className="relative w-full" style={{ height }}>
            <div className="absolute inset-0 flex items-end gap-[2px]">
                {buckets.map((b, i) => {
                    const totalH = (b.total / max) * (height - 16);
                    const successH = b.total > 0 ? (b.success / b.total) * totalH : 0;
                    const clientH = b.total > 0 ? (b.client_errors / b.total) * totalH : 0;
                    const serverH = b.total > 0 ? (b.server_errors / b.total) * totalH : 0;
                    return (
                        <div
                            key={`${b.bucket}-${i}`}
                            className="flex-1 flex flex-col justify-end group cursor-default"
                            onMouseEnter={() => onHoverBucket?.(b)}
                            onMouseLeave={() => onHoverBucket?.(null)}
                            style={{ marginRight: i === buckets.length - 1 ? 0 : barGap }}
                        >
                            <div className="flex flex-col-reverse rounded-sm overflow-hidden" style={{ height: totalH }}>
                                {successH > 0 && (
                                    <div className="bg-emerald-500/80 group-hover:bg-emerald-500 transition-colors" style={{ height: successH }} />
                                )}
                                {clientH > 0 && (
                                    <div className="bg-amber-400/80 group-hover:bg-amber-500 transition-colors" style={{ height: clientH }} />
                                )}
                                {serverH > 0 && (
                                    <div className="bg-rose-500/80 group-hover:bg-rose-500 transition-colors" style={{ height: serverH }} />
                                )}
                            </div>
                        </div>
                    );
                })}
            </div>
            {/* horizontal hairline at base */}
            <div className="absolute left-0 right-0 bottom-3 h-px bg-slate-200/80" />
            <div className="absolute left-0 right-0 bottom-0 flex justify-between font-mono text-[9.5px] text-slate-400">
                <span>{labelTick(buckets[0]?.bucket)}</span>
                <span>{labelTick(buckets[buckets.length - 1]?.bucket)}</span>
            </div>
        </div>
    );
}

function labelTick(iso: string | undefined): string {
    if (!iso) return "";
    try {
        const d = new Date(iso);
        return d.toLocaleString("en-US", { month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" });
    } catch {
        return "";
    }
}
