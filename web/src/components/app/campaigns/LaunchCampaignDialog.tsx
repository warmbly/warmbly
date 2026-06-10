// Launch campaign dialog.
//
// A purpose-built, animated launch experience that replaces the generic
// "Start X?" confirm. It shows a short pre-flight summary (daily cap,
// schedule, tracking, steps) derived from the campaign, then runs through
// idle → launching → live with motion: the launching state reuses the same
// dot-grid loader as the running indicator, and success springs a check in
// before auto-closing. Errors surface inline so a failed start (e.g. a
// template error) is readable instead of a toast that vanishes.

import React from "react";
import { AnimatePresence, motion } from "framer-motion";
import {
    AlertTriangleIcon,
    CalendarClockIcon,
    CheckIcon,
    EyeIcon,
    GaugeIcon,
    ListChecksIcon,
    RocketIcon,
    XIcon,
} from "lucide-react";
import type Campaign from "@/lib/api/models/app/campaigns/Campaign";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import useCampaign from "@/lib/api/hooks/app/campaigns/useCampaign";

type Phase = "idle" | "launching" | "done";

function bitCount(n: number): number {
    let c = 0;
    let v = n & 0xff;
    while (v) {
        v &= v - 1;
        c++;
    }
    return c;
}

function scheduleSummary(c: Campaign): string {
    const hasWindows =
        Array.isArray(c.schedule_windows) &&
        c.schedule_windows.some((d) => Array.isArray(d) && d.length > 0);
    if (hasWindows) return "Custom windows";
    const days = bitCount(c.days ?? 0);
    const time =
        c.start_time && c.end_time ? `${c.start_time}–${c.end_time}` : "all day";
    return `${days || 7} day${days === 1 ? "" : "s"} · ${time}`;
}

function trackingSummary(c: Campaign): string {
    if (c.open_tracking && c.link_tracking) return "Opens + links";
    if (c.open_tracking) return "Opens";
    if (c.link_tracking) return "Links";
    return "Off";
}

function SummaryChip({
    icon,
    label,
    value,
}: {
    icon: React.ReactNode;
    label: string;
    value: string;
}) {
    return (
        <div className="flex items-center gap-2.5 px-3 py-2.5 rounded-md border border-slate-200 bg-slate-50/60">
            <span className="shrink-0 text-slate-400">{icon}</span>
            <div className="min-w-0">
                <div className="text-[10px] uppercase tracking-[0.12em] text-slate-400 font-medium">
                    {label}
                </div>
                <div className="text-[12.5px] text-slate-900 font-medium truncate">
                    {value}
                </div>
            </div>
        </div>
    );
}

export default function LaunchCampaignDialog({
    campaign,
    onClose,
    onConfirm,
}: {
    campaign: Campaign | null;
    onClose: () => void;
    onConfirm: (id: string) => Promise<unknown>;
}) {
    const [phase, setPhase] = React.useState<Phase>("idle");
    const [error, setError] = React.useState<string | null>(null);
    const timer = React.useRef<number | null>(null);

    // The list passes a campaign whose `sequences` is null (the list endpoint
    // omits them), so fetch the full record to show an accurate step count and
    // settings. Falls back to the passed campaign until it resolves.
    const full = useCampaign(campaign?.id ?? "");
    const c = full.data ?? campaign;

    // Reset to a clean state whenever a new campaign is opened.
    React.useEffect(() => {
        if (campaign) {
            setPhase("idle");
            setError(null);
        }
    }, [campaign]);

    React.useEffect(() => {
        return () => {
            if (timer.current) window.clearTimeout(timer.current);
        };
    }, []);

    // Esc closes (unless mid-launch).
    React.useEffect(() => {
        if (!campaign) return;
        const onKey = (e: KeyboardEvent) => {
            if (e.key === "Escape" && phase !== "launching") onClose();
        };
        document.addEventListener("keydown", onKey);
        return () => document.removeEventListener("keydown", onKey);
    }, [campaign, phase, onClose]);

    async function launch() {
        if (!campaign || phase === "launching") return;
        setError(null);
        setPhase("launching");
        try {
            await onConfirm(campaign.id);
            setPhase("done");
            timer.current = window.setTimeout(onClose, 1200);
        } catch (e) {
            setError(buildError(e as unknown as AppError));
            setPhase("idle");
        }
    }

    return (
        <AnimatePresence>
            {c && (
                <motion.div
                    key="launch-overlay"
                    initial={{ opacity: 0 }}
                    animate={{ opacity: 1 }}
                    exit={{ opacity: 0 }}
                    transition={{ duration: 0.15 }}
                    onClick={() => phase !== "launching" && onClose()}
                    className="fixed inset-0 z-[120] flex items-center justify-center bg-slate-900/30 backdrop-blur-[2px] px-4"
                >
                    <motion.div
                        key="launch-card"
                        initial={{ y: 8, opacity: 0, scale: 0.98 }}
                        animate={{ y: 0, opacity: 1, scale: 1 }}
                        exit={{ y: 8, opacity: 0, scale: 0.98 }}
                        transition={{ duration: 0.18, ease: [0.16, 1, 0.3, 1] }}
                        onClick={(e) => e.stopPropagation()}
                        className="w-full max-w-[440px] rounded-lg bg-white border border-slate-200 shadow-[0_24px_48px_-12px_rgba(15,23,42,0.18),0_8px_16px_-8px_rgba(15,23,42,0.1)] overflow-hidden max-h-[90dvh] overflow-y-auto"
                    >
                        <AnimatePresence mode="wait">
                            {phase === "done" ? (
                                <motion.div
                                    key="success"
                                    initial={{ opacity: 0 }}
                                    animate={{ opacity: 1 }}
                                    exit={{ opacity: 0 }}
                                    className="px-6 py-10 flex flex-col items-center text-center"
                                >
                                    <motion.div
                                        initial={{ scale: 0.4, opacity: 0 }}
                                        animate={{ scale: 1, opacity: 1 }}
                                        transition={{ type: "spring", stiffness: 320, damping: 18 }}
                                        className="size-12 rounded-full bg-emerald-100 text-emerald-600 flex items-center justify-center"
                                    >
                                        <CheckIcon className="w-6 h-6" strokeWidth={2.6} />
                                    </motion.div>
                                    <p className="mt-3 text-[14px] font-semibold text-slate-900">
                                        You're live
                                    </p>
                                    <p className="mt-0.5 text-[12px] text-slate-500 truncate max-w-full">
                                        {c.name} is now sending
                                    </p>
                                </motion.div>
                            ) : (
                                <motion.div key="form" exit={{ opacity: 0 }}>
                                    {/* Header */}
                                    <div className="px-5 pt-5 pb-4 flex items-start gap-3">
                                        <span className="shrink-0 size-9 rounded-md bg-sky-50 text-sky-600 flex items-center justify-center">
                                            <RocketIcon className="w-[18px] h-[18px]" />
                                        </span>
                                        <div className="min-w-0 flex-1">
                                            <h2 className="text-[14px] font-semibold text-slate-900 leading-tight">
                                                Launch campaign
                                            </h2>
                                            <p className="text-[12px] text-slate-500 truncate">
                                                {c.name}
                                            </p>
                                        </div>
                                        <button
                                            type="button"
                                            onClick={onClose}
                                            disabled={phase === "launching"}
                                            aria-label="Close"
                                            className="shrink-0 size-7 -mt-1 -mr-1 rounded text-slate-400 hover:text-slate-700 hover:bg-slate-100 flex items-center justify-center transition-colors disabled:opacity-40"
                                        >
                                            <XIcon className="w-4 h-4" />
                                        </button>
                                    </div>

                                    {/* Pre-flight summary */}
                                    <div className="px-5 grid grid-cols-2 gap-2">
                                        <SummaryChip
                                            icon={<GaugeIcon className="w-4 h-4" />}
                                            label="Daily cap"
                                            value={`${c.daily_limit}/mailbox`}
                                        />
                                        <SummaryChip
                                            icon={<CalendarClockIcon className="w-4 h-4" />}
                                            label="Schedule"
                                            value={scheduleSummary(c)}
                                        />
                                        <SummaryChip
                                            icon={<EyeIcon className="w-4 h-4" />}
                                            label="Tracking"
                                            value={trackingSummary(c)}
                                        />
                                        <SummaryChip
                                            icon={<ListChecksIcon className="w-4 h-4" />}
                                            label="Steps"
                                            value={
                                                full.isLoading && !c.sequences
                                                    ? "…"
                                                    : c.sequences
                                                        ? `${c.sequences.length} step${c.sequences.length === 1 ? "" : "s"}`
                                                        : "0 steps"
                                            }
                                        />
                                    </div>

                                    <p className="px-5 pt-3 text-[11.5px] text-slate-500 leading-relaxed">
                                        Sending begins immediately, paced to the schedule and your
                                        mailbox guardrails. You can pause anytime.
                                    </p>

                                    {error && (
                                        <div className="mx-5 mt-3 flex items-start gap-2 rounded-md border border-rose-200 bg-rose-50 px-3 py-2">
                                            <AlertTriangleIcon className="w-3.5 h-3.5 text-rose-500 shrink-0 mt-0.5" />
                                            <p className="text-[12px] text-rose-700 leading-snug">
                                                {error}
                                            </p>
                                        </div>
                                    )}

                                    {/* Footer */}
                                    <div className="px-5 py-4 mt-4 flex items-center justify-end gap-2 border-t border-slate-200 bg-slate-50/50">
                                        <button
                                            type="button"
                                            onClick={onClose}
                                            disabled={phase === "launching"}
                                            className="h-8 px-3 rounded-md border border-slate-200 hover:border-slate-300 text-slate-700 hover:text-slate-900 bg-white text-[12.5px] font-medium transition-colors disabled:opacity-50"
                                        >
                                            Cancel
                                        </button>
                                        <button
                                            type="button"
                                            onClick={launch}
                                            disabled={phase === "launching"}
                                            className="h-8 px-3.5 rounded-md bg-sky-600 hover:bg-sky-700 text-white text-[12.5px] font-medium inline-flex items-center gap-2 transition-colors disabled:opacity-80"
                                        >
                                            {phase === "launching" ? (
                                                <>
                                                    <span className="campaign-grid text-white" aria-hidden />
                                                    Launching…
                                                </>
                                            ) : (
                                                <>
                                                    <RocketIcon className="w-3.5 h-3.5" />
                                                    Launch now
                                                </>
                                            )}
                                        </button>
                                    </div>
                                </motion.div>
                            )}
                        </AnimatePresence>
                    </motion.div>
                </motion.div>
            )}
        </AnimatePresence>
    );
}
