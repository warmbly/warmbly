// The words around the projection: when it finishes, what holds it back, the
// warmup running beside it, and the pool mailbox by mailbox.

import React from "react";
import { FlameIcon, GaugeIcon } from "lucide-react";
import type {
    CampaignEstimateResult,
    CampaignEstimateSender,
    EstimateSenderState,
} from "@/lib/api/client/app/campaigns/estimateCampaign";
import { bottleneckText } from "./estimateText";
import ProviderLogo from "@/components/app/campaigns/preferences/ProviderLogo";
import { fmtDay, plural } from "./draft";
import { cn } from "@/lib/utils";

export function WarmupNote({ e, className }: { e: CampaignEstimateResult; className?: string }) {
    if (e.warmup.mailboxes === 0) return null;
    return (
        <p className={cn("flex items-start gap-2 text-[11.5px] text-slate-600 leading-relaxed", className)}>
            <FlameIcon className="w-3.5 h-3.5 text-amber-500 shrink-0 mt-0.5" />
            <span>
                Warmup keeps running on {plural(e.warmup.mailboxes, "mailbox", "mailboxes")}, about{" "}
                {e.warmup.per_day.toLocaleString()} emails a day in total. It drops to 5 a day per mailbox while the campaign
                runs and uses the same spacing between sends, which this estimate already accounts for.
            </span>
        </p>
    );
}

export function BottleneckNote({ e, tz, className }: { e: CampaignEstimateResult; tz?: string; className?: string }) {
    const text = bottleneckText(e, tz);
    if (!text) return null;
    return (
        <p className={cn("flex items-start gap-2 text-[11.5px] text-slate-600 leading-relaxed", className)}>
            <GaugeIcon className="w-3.5 h-3.5 text-slate-400 shrink-0 mt-0.5" />
            <span>{text}</span>
        </p>
    );
}

const STATE: Record<EstimateSenderState, { label: string; cls: string }> = {
    ready: { label: "Ready", cls: "bg-emerald-50 text-emerald-700" },
    ramping: { label: "Ramping up", cls: "bg-sky-50 text-sky-700" },
    throttled: { label: "Reduced volume", cls: "bg-amber-50 text-amber-700" },
    health_hold: { label: "On hold", cls: "bg-rose-50 text-rose-700" },
    domain_auth: { label: "DNS failing", cls: "bg-rose-50 text-rose-700" },
    resting: { label: "Resting", cls: "bg-slate-100 text-slate-600" },
    no_worker: { label: "Reconnecting", cls: "bg-slate-100 text-slate-600" },
    send_recovery: { label: "Send recovery", cls: "bg-rose-50 text-rose-700" },
    send_cooldown: { label: "Cooling down", cls: "bg-amber-50 text-amber-700" },
    send_authority: { label: "Send unavailable", cls: "bg-slate-100 text-slate-600" },
};

function capLabel(s: CampaignEstimateSender, tz?: string): string {
    if (s.steady_cap === 0) return "Sends nothing";
    if (s.first_day_cap < s.steady_cap) {
        return `${s.first_day_cap} → ${s.steady_cap}/day${s.full_cap_at ? ` by ${fmtDay(s.full_cap_at, tz)}` : ""}`;
    }
    return `${s.steady_cap}/day`;
}

export function SenderList({ senders, total, tz }: { senders: CampaignEstimateSender[]; total: number; tz?: string }) {
    const [all, setAll] = React.useState(false);
    const limit = 6;
    const shown = all ? senders : senders.slice(0, limit);
    if (senders.length === 0) return null;
    return (
        <div className="border border-slate-200 rounded-md overflow-hidden">
            <ul className="divide-y divide-slate-100">
                {shown.map((s) => {
                    const st = STATE[s.state] ?? STATE.ready;
                    return (
                        <li key={s.id} className="px-3 h-9 flex items-center gap-2.5 text-[12px]">
                            <ProviderLogo provider={s.provider} className="size-3.5" />
                            <span className="min-w-0 flex-1 truncate text-slate-800">{s.email}</span>
                            {s.state !== "ready" && (
                                <span className={cn("shrink-0 h-5 px-1.5 rounded text-[10.5px] font-medium inline-flex items-center", st.cls)}>
                                    {st.label}
                                </span>
                            )}
                            {s.warmup_per_day > 0 && (
                                <span
                                    title="Warmup emails a day running alongside"
                                    className="hidden sm:inline-flex shrink-0 items-center gap-0.5 text-[11px] text-slate-400 tabular-nums"
                                >
                                    <FlameIcon className="w-3 h-3 text-amber-400" />
                                    {s.warmup_per_day}
                                </span>
                            )}
                            <span className="shrink-0 w-[120px] text-right text-[11.5px] text-slate-600 tabular-nums">{capLabel(s, tz)}</span>
                        </li>
                    );
                })}
            </ul>
            {senders.length > limit && (
                <button
                    type="button"
                    onClick={() => setAll((v) => !v)}
                    className="w-full h-8 border-t border-slate-100 text-[11.5px] text-slate-500 hover:text-slate-900 hover:bg-slate-50 transition-colors"
                >
                    {all ? "Show fewer" : `Show all ${total.toLocaleString()}`}
                </button>
            )}
        </div>
    );
}
