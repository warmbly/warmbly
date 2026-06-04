import React from "react";
import { Logo } from "@/components/svg";
import { DailyBars, type ChartPoint } from "@/components/ui/charts";

// A fixed-size, branded analytics card meant to be rasterized to a shareable
// PNG (see useExportCard). It mirrors the live dashboard's slate/sky language
// so the export looks like Warmbly, not a generic screenshot. Rendered
// off-viewport by AnalyticsShareButton.

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

const StatsShareCard = React.forwardRef<HTMLDivElement, { data: ShareCardData }>(
    function StatsShareCard({ data }, ref) {
        const metrics = data.metrics.slice(0, 4);
        return (
            <div
                ref={ref}
                style={{ width: 1080, height: 1080 }}
                className="flex flex-col bg-white text-slate-900 p-[72px]"
            >
                {/* header */}
                <div className="flex items-center">
                    <Logo className="w-12 h-12 text-sky-600" />
                    <span className="ml-4 text-[34px] font-semibold tracking-tight">Warmbly</span>
                    {data.subtitle && (
                        <span className="ml-auto text-[20px] font-medium uppercase tracking-[0.18em] text-slate-400">
                            {data.subtitle}
                        </span>
                    )}
                </div>

                <h1 className="mt-14 text-[52px] font-semibold leading-tight tracking-tight">
                    {data.title}
                </h1>

                {/* metric grid */}
                <div className="mt-12 grid grid-cols-2 gap-x-12 gap-y-10">
                    {metrics.map((m) => (
                        <div key={m.label} className="border-l-2 border-sky-500 pl-6">
                            <div className="text-[18px] font-medium uppercase tracking-[0.14em] text-slate-400">
                                {m.label}
                            </div>
                            <div className="mt-3 font-mono text-[64px] leading-none tabular-nums text-slate-900">
                                {m.value}
                            </div>
                            {m.sub && <div className="mt-3 text-[18px] text-slate-400">{m.sub}</div>}
                        </div>
                    ))}
                </div>

                {/* chart snapshot */}
                <div className="mt-auto">
                    <div className="text-[18px] font-medium uppercase tracking-[0.14em] text-slate-400 mb-4">
                        Sends over time
                    </div>
                    <DailyBars points={data.daily} height={220} emptyLabel="No sends in this window" />
                </div>

                {/* footer */}
                <div className="mt-12 flex items-center justify-between text-[18px] text-slate-400">
                    <span className="font-medium text-slate-500">warmbly.com</span>
                    <span className="font-mono tabular-nums">{todayLabel()}</span>
                </div>
            </div>
        );
    },
);

function todayLabel(): string {
    return new Date().toLocaleDateString("en-US", { year: "numeric", month: "short", day: "numeric" });
}

export default StatsShareCard;
