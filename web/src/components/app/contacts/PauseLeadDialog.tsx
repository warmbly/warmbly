// Pause one lead in one campaign: until a date, or until someone resumes it.
// The contact stays subscribed and keeps their place in the sequence.

import React from "react";
import { AnimatePresence, motion } from "framer-motion";
import { Loader2Icon, PauseIcon, XIcon } from "lucide-react";
import toast from "react-hot-toast";

import { DatePicker } from "@/components/ui/DatePicker";
import { Label, TextInput } from "@/components/ui/field";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import { usePauseLead } from "@/lib/api/hooks/app/campaigns/useLeadHold";

interface Props {
    open: boolean;
    onClose: () => void;
    campaign: { id: string; name: string };
    // The lead being paused; `name` is only for the copy on screen.
    lead: { id: string; name: string } | null;
}

// A date picker hands back "yyyy-MM-dd". The hold has to be an instant, and
// the useful one is the END of the chosen day in the member's own timezone:
// "pause until the 8th" means the 8th is still covered.
function endOfLocalDay(iso: string): string | null {
    const [y, m, d] = iso.split("-").map(Number);
    if (!y || !m || !d) return null;
    return new Date(y, m - 1, d, 23, 59, 59, 0).toISOString();
}

function defaultUntil(days: number): string {
    const d = new Date();
    d.setDate(d.getDate() + days);
    return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;
}

export default function PauseLeadDialog({ open, onClose, campaign, lead }: Props) {
    const [mode, setMode] = React.useState<"date" | "open">("date");
    const [until, setUntil] = React.useState(() => defaultUntil(7));
    const [reason, setReason] = React.useState("");
    const pause = usePauseLead();

    React.useEffect(() => {
        if (!open) return;
        setMode("date");
        setUntil(defaultUntil(7));
        setReason("");
    }, [open]);

    // Escape closes the innermost layer only: while the date picker's calendar
    // (or a confirm) is on screen, it belongs to that.
    React.useEffect(() => {
        if (!open) return;
        function onKey(e: KeyboardEvent) {
            if (e.key !== "Escape") return;
            if (document.querySelector("[data-floating], [role='alertdialog']")) return;
            onClose();
        }
        window.addEventListener("keydown", onKey);
        return () => window.removeEventListener("keydown", onKey);
    }, [open, onClose]);

    async function submit() {
        if (!lead) return;
        let untilISO: string | null = null;
        if (mode === "date") {
            untilISO = endOfLocalDay(until);
            if (!untilISO) {
                toast.error("Pick the day they are back");
                return;
            }
            if (new Date(untilISO).getTime() <= Date.now()) {
                toast.error("Pick a day in the future");
                return;
            }
        }
        try {
            await toast.promise(
                pause.mutateAsync({
                    campaignId: campaign.id,
                    contactId: lead.id,
                    until: untilISO,
                    reason: reason.trim(),
                }),
                {
                    loading: "Pausing lead…",
                    success:
                        mode === "date"
                            ? `Paused until ${new Date(untilISO as string).toLocaleDateString()}`
                            : "Paused until you resume it",
                    error: (err: AppError) => buildError(err),
                },
            );
            onClose();
        } catch {
            /* toast.promise already surfaced it */
        }
    }

    return (
        <AnimatePresence>
            {open && lead && (
                <motion.div
                    key="overlay"
                    initial={{ opacity: 0 }}
                    animate={{ opacity: 1 }}
                    exit={{ opacity: 0 }}
                    transition={{ duration: 0.15 }}
                    // mousedown, and only on the backdrop itself: a click whose
                    // press started inside the card and ended out here would
                    // otherwise discard the draft. The card does NOT stop
                    // propagation, so the date picker's own click-away still
                    // sees every press inside the dialog.
                    onMouseDown={(e) => {
                        if (e.target === e.currentTarget) onClose();
                    }}
                    className="fixed inset-0 z-[110] flex items-center justify-center bg-slate-900/30 backdrop-blur-[2px] px-4"
                >
                    <motion.div
                        key="card"
                        initial={{ y: 8, opacity: 0 }}
                        animate={{ y: 0, opacity: 1 }}
                        exit={{ y: 8, opacity: 0 }}
                        transition={{ duration: 0.16 }}
                        role="dialog"
                        aria-modal="true"
                        aria-label={`Pause ${lead.name} in ${campaign.name}`}
                        className="w-full max-w-[420px] rounded-lg bg-white border border-slate-200 shadow-[0_24px_48px_-12px_rgba(15,23,42,0.18),0_8px_16px_-8px_rgba(15,23,42,0.1)] overflow-hidden flex flex-col max-h-[calc(100dvh-2rem)]"
                    >
                        <div className="h-12 px-4 border-b border-slate-200 flex items-center gap-2.5 shrink-0">
                            <div className="size-5 rounded bg-slate-100 text-slate-600 flex items-center justify-center">
                                <PauseIcon className="w-3 h-3" />
                            </div>
                            <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">
                                Pause
                            </span>
                            <div className="h-4 w-px bg-slate-200" />
                            <span className="text-[12.5px] text-slate-900 font-medium truncate">{lead.name}</span>
                            <button
                                type="button"
                                onClick={onClose}
                                aria-label="Close"
                                className="ml-auto size-7 rounded-md text-slate-500 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center justify-center transition-colors"
                            >
                                <XIcon className="w-3.5 h-3.5" />
                            </button>
                        </div>

                        <div className="px-4 py-4 space-y-3 flex-1 min-h-0 overflow-y-auto">
                            <p className="text-[11.5px] text-slate-500 leading-relaxed">
                                Nothing goes out to this contact in {campaign.name} while the pause
                                lasts. They stay subscribed and keep their place in the sequence, so
                                the flow picks up where it stopped.
                            </p>
                            <div className="flex items-center gap-1.5">
                                <ModeTab active={mode === "date"} onClick={() => setMode("date")} autoFocus>
                                    Until a date
                                </ModeTab>
                                <ModeTab active={mode === "open"} onClick={() => setMode("open")}>
                                    Until I resume it
                                </ModeTab>
                            </div>
                            {mode === "date" && (
                                <div>
                                    <Label>Back on</Label>
                                    <DatePicker
                                        value={until}
                                        onChange={setUntil}
                                        clearable={false}
                                        className="w-full"
                                    />
                                </div>
                            )}
                            <div>
                                <Label>Note (optional)</Label>
                                <TextInput
                                    value={reason}
                                    onChange={setReason}
                                    placeholder="On holiday, asked to follow up later…"
                                    className="w-full"
                                />
                            </div>
                        </div>

                        <div className="px-3 h-12 border-t border-slate-200 flex items-center gap-1.5 shrink-0">
                            <button
                                type="button"
                                onClick={onClose}
                                className="ml-auto h-7 px-2.5 rounded-md text-[12px] text-slate-700 hover:text-slate-900 hover:bg-slate-100 transition-colors"
                            >
                                Cancel
                            </button>
                            <button
                                type="button"
                                onClick={submit}
                                disabled={pause.isPending}
                                className="h-7 px-2.5 rounded-md bg-slate-900 hover:bg-slate-800 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors disabled:opacity-60"
                            >
                                {pause.isPending && <Loader2Icon className="w-3 h-3 animate-spin" />}
                                Pause lead
                            </button>
                        </div>
                    </motion.div>
                </motion.div>
            )}
        </AnimatePresence>
    );
}

function ModeTab({
    active,
    onClick,
    children,
    autoFocus = false,
}: {
    active: boolean;
    onClick: () => void;
    children: React.ReactNode;
    autoFocus?: boolean;
}) {
    return (
        <button
            type="button"
            aria-pressed={active}
            autoFocus={autoFocus}
            onClick={onClick}
            className={`h-7 px-2.5 rounded-md border text-[12px] transition-colors ${
                active
                    ? "border-sky-200 bg-sky-50 text-sky-700 font-medium"
                    : "border-slate-200 text-slate-600 hover:bg-slate-50"
            }`}
        >
            {children}
        </button>
    );
}
