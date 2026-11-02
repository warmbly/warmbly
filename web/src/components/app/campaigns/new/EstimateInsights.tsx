// The words around the projection: when it finishes, what holds it back, what
// each mailbox sends with the warmup running beside it, and the pool mailbox
// by mailbox.

import React from "react";
import { CalendarCheckIcon, FlameIcon, GaugeIcon, MinusCircleIcon } from "lucide-react";
import type {
    CampaignEstimateResult,
    CampaignEstimateSender,
    EstimateSenderState,
} from "@/lib/api/client/app/campaigns/estimateCampaign";
import { bottleneckText, scenarioText, wontHelpText } from "./estimateText";
import ProviderLogo from "@/components/app/campaigns/preferences/ProviderLogo";
import { fmtDay, plural } from "./draft";
import { cn } from "@/lib/utils";

// Rounded mean, or 0 for an empty pool.
function mean(total: number, n: number): number {
    return n > 0 ? Math.round(total / n) : 0;
}

export function WarmupNote({ e, className }: { e: CampaignEstimateResult; className?: string }) {
    const w = e.warmup;
    if (w.mailboxes === 0) return null;
    const now = mean(w.per_day, w.mailboxes);
    const without = mean(w.per_day_without ?? w.per_day, w.mailboxes);
    return (
        <p className={cn("flex items-start gap-2 text-[11.5px] text-slate-600 leading-relaxed", className)}>
            <FlameIcon className="w-3.5 h-3.5 text-amber-500 shrink-0 mt-0.5" />
            <span>
                Warmup keeps running on {plural(w.mailboxes, "mailbox", "mailboxes")}
                {without > now ? `, down from about ${without} to ${now} a day each` : `, about ${now} a day each`} while this runs.
                {w.reply_rate ? " Pool partners prioritize replies to it within their own reply settings." : ""}
            </span>
        </p>
    );
}

// What one mailbox sends on a sending day with and without this campaign, and
// what happens to its warmup while the campaign runs.
export function MailboxMix({ e, limit, className }: { e: CampaignEstimateResult; limit: number; className?: string }) {
    const w = e.warmup;
    const sending = e.senders.filter((s) => s.steady_cap > 0);
    if (sending.length === 0) return null;
    const firstDay = sending.reduce((n, s) => n + s.first_day_cap, 0);
    const campaign = mean(firstDay, sending.length);
    // Senders stops at 200 mailboxes; scale the listed ones up to the pool.
    const poolCampaign = Math.round((firstDay * e.mailboxes) / e.senders.length);
    const warming = w.mailboxes > 0;
    const now = mean(w.per_day, w.mailboxes);
    const without = mean(w.per_day_without ?? w.per_day, w.mailboxes);
    const ramping = e.bottleneck === "warmup_graduation" || e.ramping > 0;

    return (
        <div className={cn("rounded-md border border-slate-200 bg-white", className)}>
            <p className="px-3 pt-2.5 text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">Each mailbox, per sending day</p>
            <table className="w-full mt-1 text-[12px] tabular-nums">
                <thead>
                    <tr className="text-[11px] text-slate-400">
                        <th className="px-3 py-1 text-left font-normal" />
                        <th className="px-3 py-1 text-right font-normal w-28">No campaign</th>
                        <th className="px-3 py-1 text-right font-normal w-28">While this runs</th>
                    </tr>
                </thead>
                <tbody className="text-slate-700">
                    <tr>
                        <td className="px-3 py-1">Campaign emails</td>
                        <td className="px-3 py-1 text-right text-slate-400">0</td>
                        <td className="px-3 py-1 text-right">
                            {campaign}
                            {ramping && campaign < limit && <span className="text-slate-400"> to {limit}</span>}
                        </td>
                    </tr>
                    {warming && (
                        <tr>
                            <td className="px-3 py-1">Warmup emails</td>
                            <td className="px-3 py-1 text-right">{without}</td>
                            <td className="px-3 py-1 text-right">{now}</td>
                        </tr>
                    )}
                    <tr className="border-t border-slate-100 text-slate-900 font-medium">
                        <td className="px-3 py-1.5">Sent in total</td>
                        <td className="px-3 py-1.5 text-right">{warming ? without : 0}</td>
                        <td className="px-3 py-1.5 text-right">{campaign + (warming ? now : 0)}</td>
                    </tr>
                </tbody>
            </table>
            <p className="px-3 pb-2.5 pt-1 text-[11px] text-slate-500 leading-relaxed">
                All {plural(e.mailboxes, "mailbox", "mailboxes")} together: about {(poolCampaign + w.per_day).toLocaleString()} a day,{" "}
                {poolCampaign.toLocaleString()} for the campaign{warming ? ` and ${w.per_day.toLocaleString()} warmup` : ""}.
                {ramping ? " Campaign volume rises as leads reply." : ""}
            </p>
            {warming && (
                <ul className="border-t border-slate-100 px-3 py-2.5 space-y-1.5 text-[11.5px] text-slate-600 leading-relaxed">
                    <Point>
                        {without > now
                            ? `Warmup drops from about ${without} to ${now} a day per mailbox while this runs, so your outreach is most of what each mailbox sends.`
                            : `Warmup stays at about ${now} a day per mailbox while this runs.`}
                    </Point>
                    {!!w.reply_rate && (
                        <Point>
                            Pool partners answer that warmup before other warmup mail (within each partner's own reply rate) and read, rescue from spam
                            and mark more of it important, so each mailbox keeps getting replies while most cold leads do not answer. Those replies use
                            the partner's own warmup sends, so they add no volume.
                        </Point>
                    )}
                    <Point>
                        {now} a day keeps inbox placement measured: a placement reading needs 20 delivered warmup emails in 7 days at Google,
                        Microsoft and Yahoo.
                    </Point>
                    <Point>Warmup shares the gap between sends with the campaign, and this estimate counts it.</Point>
                    <Point>When no campaign uses the mailbox any more, warmup goes back to its own schedule.</Point>
                </ul>
            )}
        </div>
    );
}

function Point({ children }: { children: React.ReactNode }) {
    return (
        <li className="flex items-start gap-2">
            <FlameIcon className="w-3 h-3 text-amber-400 shrink-0 mt-[3px]" />
            <span>{children}</span>
        </li>
    );
}

export function BottleneckNote({ e, limit, className }: { e: CampaignEstimateResult; limit?: number; className?: string }) {
    const text = bottleneckText(e, limit);
    if (!text) return null;
    return (
        <p className={cn("flex items-start gap-2 text-[11.5px] text-slate-600 leading-relaxed", className)}>
            <GaugeIcon className="w-3.5 h-3.5 text-slate-400 shrink-0 mt-0.5" />
            <span>{text}</span>
        </p>
    );
}

// Whole calendar days from a to b.
function daysBetween(a: Date | string, b: Date | string): number {
    return Math.round((new Date(b).getTime() - new Date(a).getTime()) / 86_400_000);
}

// The changes that would finish sooner, each with the date the same simulation gives.
export function FinishSooner({ e, tz, className }: { e: CampaignEstimateResult; tz?: string; className?: string }) {
    const scenarios = (e.scenarios ?? []).filter((s) => s.finish_at);
    const wont = wontHelpText(e);
    if (scenarios.length === 0 && !wont) return null;
    const finish = e.estimated_finish_at;
    return (
        <div className={className}>
            <p className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium mb-1.5">To finish sooner</p>
            <ul className="space-y-2">
                {scenarios.map((s) => {
                    const t = scenarioText(s);
                    const sooner = finish && s.finish_at ? daysBetween(s.finish_at, finish) : null;
                    return (
                        <li key={s.kind} className="flex items-start gap-2 text-[11.5px] leading-relaxed">
                            <CalendarCheckIcon className="w-3.5 h-3.5 text-emerald-500 shrink-0 mt-0.5" />
                            <span className="min-w-0 flex-1">
                                <span className="text-slate-800 font-medium">{t.title}</span>
                                <span className="block text-slate-500">{t.detail}</span>
                            </span>
                            <span className="shrink-0 text-right tabular-nums">
                                <span className="block text-slate-900 font-medium">{fmtDay(s.finish_at, tz)}</span>
                                {sooner !== null && sooner > 0 && (
                                    <span className="block text-[10.5px] text-emerald-700">{plural(sooner, "day")} sooner</span>
                                )}
                            </span>
                        </li>
                    );
                })}
                {wont && (
                    <li className="flex items-start gap-2 text-[11.5px] text-slate-500 leading-relaxed">
                        <MinusCircleIcon className="w-3.5 h-3.5 text-slate-300 shrink-0 mt-0.5" />
                        <span>{wont}</span>
                    </li>
                )}
            </ul>
        </div>
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
