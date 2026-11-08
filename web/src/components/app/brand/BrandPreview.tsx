// BrandPreview — a connect page and a shared report header drawn with a brand,
// so a change is visible before a client ever opens a link.

import ScrollStrip from "@/components/ui/scroll-strip";
import { DashboardImage } from "@/components/ui/dashboard-image";
import type { PublicBrand } from "@/lib/api/models/app/brand/Brand";
import React from "react";

const DEFAULT_ACCENT = "#0284c7";

function BrandHeader({ brand, fallbackName }: { brand: PublicBrand; fallbackName: string }) {
    const name = brand.name || (brand.logo_url ? "" : fallbackName);
    return (
        <div className="flex items-center gap-2 min-w-0">
            {brand.logo_url && <DashboardImage src={brand.logo_url} alt="" className="h-6 max-w-[110px] object-contain" />}
            {name && <span className="text-[12.5px] font-semibold text-slate-900 truncate">{name}</span>}
        </div>
    );
}

function PoweredBy({ brand }: { brand: PublicBrand }) {
    if (!brand.powered_by) return null;
    return <div className="text-center text-[10px] text-slate-400 pt-2">Powered by {brand.powered_by.name}</div>;
}

export default function BrandPreview({ brand, fallbackName }: { brand: PublicBrand; fallbackName: string }) {
    const [view, setView] = React.useState<"connect" | "report">("connect");
    const accent = brand.accent_color || DEFAULT_ACCENT;
    return (
        <div className="rounded-md border border-slate-200 bg-slate-50 overflow-hidden">
            <div className="shrink-0 px-3 flex items-center gap-1 border-b border-slate-200 bg-white">
                <ScrollStrip activeKey={view}>
                    {(["connect", "report"] as const).map((v) => (
                        <button
                            key={v}
                            type="button"
                            data-active={view === v || undefined}
                            onClick={() => setView(v)}
                            className={`relative h-9 px-2.5 inline-flex items-center text-[12px] ${
                                view === v ? "text-slate-900 font-medium" : "text-slate-500 hover:text-slate-800"
                            }`}
                        >
                            {v === "connect" ? "Connect page" : "Report"}
                            {view === v && <span className="absolute left-2 right-2 -bottom-px h-0.5 rounded-full bg-sky-600" />}
                        </button>
                    ))}
                </ScrollStrip>
            </div>
            <div className="p-4">
                <div className="rounded-lg bg-white border border-slate-200 shadow-sm overflow-hidden">
                    <div className="h-1" style={{ background: accent }} />
                    <div className="px-4 py-3 border-b border-slate-100 flex items-center justify-between gap-2">
                        <BrandHeader brand={brand} fallbackName={fallbackName} />
                        {brand.website_url && (
                            <span className="text-[10.5px] text-slate-400 truncate">{brand.website_url.replace(/^https:\/\//, "")}</span>
                        )}
                    </div>
                    {view === "connect" ? (
                        <div className="px-4 py-4 space-y-2.5">
                            <div className="text-[13px] font-semibold text-slate-900">Connect your mailbox</div>
                            <div className="text-[11.5px] text-slate-500">Sign in with your email provider to connect it.</div>
                            <div className="space-y-1.5 pt-1">
                                {["Google", "Microsoft", "Other provider"].map((p, i) => (
                                    <div
                                        key={p}
                                        className="h-8 rounded-md text-[11.5px] font-medium flex items-center justify-center"
                                        style={i === 0 ? { background: accent, color: "#fff" } : { border: "1px solid #e2e8f0", color: "#334155" }}
                                    >
                                        Continue with {p}
                                    </div>
                                ))}
                            </div>
                        </div>
                    ) : (
                        <div className="px-4 py-4 space-y-3">
                            <div className="text-[13px] font-semibold text-slate-900">Campaign report</div>
                            <div className="grid grid-cols-3 gap-2">
                                {[
                                    ["Sent", "1,240"],
                                    ["Replies", "38"],
                                    ["Reply rate", "3.1%"],
                                ].map(([label, value]) => (
                                    <div key={label} className="rounded-md border border-slate-100 px-2 py-1.5">
                                        <div className="text-[9.5px] uppercase tracking-[0.14em] text-slate-400">{label}</div>
                                        <div className="text-[13px] font-semibold" style={{ color: accent }}>
                                            {value}
                                        </div>
                                    </div>
                                ))}
                            </div>
                            <div className="flex items-end gap-1 h-12">
                                {[30, 45, 38, 60, 52, 70, 64].map((h, i) => (
                                    <div key={i} className="flex-1 rounded-sm opacity-80" style={{ height: `${h}%`, background: accent }} />
                                ))}
                            </div>
                        </div>
                    )}
                </div>
                <PoweredBy brand={brand} />
            </div>
        </div>
    );
}
