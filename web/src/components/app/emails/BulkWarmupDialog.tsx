import { useState } from "react";
import { AnimatePresence, motion } from "framer-motion";
import { FlameIcon, XIcon } from "lucide-react";
import toast from "react-hot-toast";
import { useQueryClient } from "@tanstack/react-query";
import { Label, NumberInput } from "@/components/ui/field";
import TimeSelect from "@/components/ui/TimeSelect";
import WeekdayBitmask from "@/components/app/campaigns/schedule/WeekdayBitmask";
import { Loading } from "@/components/loader";
import updateEmail from "@/lib/api/client/app/emails/updateEmail";
import warmupLifecycle from "@/lib/api/client/app/emails/warmupLifecycle";
import type Inbox from "@/lib/api/models/app/emails/Inbox";

const WEEKDAYS = ["Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday", "Sunday"];

// Bulk warmup-start dialog. Lets the user optionally apply one set of warmup
// settings to every selected mailbox before starting — useful when onboarding a
// batch of new inboxes that should all ramp the same way.
export default function BulkWarmupDialog({
    open,
    ids,
    onClose,
    onComplete,
}: {
    open: boolean;
    ids: string[];
    onClose: () => void;
    onComplete: () => void;
}) {
    const queryClient = useQueryClient();
    const [customize, setCustomize] = useState(true);
    const [busy, setBusy] = useState(false);

    const [base, setBase] = useState(10);
    const [increase, setIncrease] = useState(1);
    const [max, setMax] = useState(40);
    const [replyRate, setReplyRate] = useState(30);
    const [startTime, setStartTime] = useState("08:00");
    const [endTime, setEndTime] = useState("20:00");
    const [days, setDays] = useState(0);

    const n = ids.length;
    const baseOverMax = base > max;

    const start = async () => {
        if (busy || n === 0) return;
        if (customize && baseOverMax) {
            toast.error("Starting volume can't exceed the maximum.");
            return;
        }
        setBusy(true);
        const patch: Partial<Inbox> = {
            warmup_base: base,
            warmup_increase: increase,
            warmup_max: max,
            warmup_reply_rate: replyRate,
            warmup_start_time: startTime,
            warmup_end_time: endTime,
            warmup_days: days,
        };
        // Apply settings (if customizing) then start, per mailbox. allSettled so
        // one failure doesn't abort the rest.
        const results = await Promise.allSettled(
            ids.map(async (id) => {
                if (customize) await updateEmail(id, patch);
                await warmupLifecycle(id, "start");
            }),
        );
        const failed = results.filter((r) => r.status === "rejected").length;
        await queryClient.invalidateQueries({ queryKey: ["emails"] });
        await queryClient.invalidateQueries({ queryKey: ["analytics", "accounts"] });
        setBusy(false);
        if (failed > 0) {
            toast.error(`${failed} of ${n} mailbox${n > 1 ? "es" : ""} couldn't start`);
        } else {
            toast.success(`Warmup started for ${n} mailbox${n > 1 ? "es" : ""}`);
        }
        onComplete();
        onClose();
    };

    return (
        <AnimatePresence>
            {open && (
                <>
                    <motion.div
                        initial={{ opacity: 0 }}
                        animate={{ opacity: 1 }}
                        exit={{ opacity: 0 }}
                        transition={{ duration: 0.18 }}
                        className="fixed inset-0 z-50 bg-slate-900/40"
                        onClick={busy ? undefined : onClose}
                    />
                    <div className="fixed inset-0 z-50 flex items-center justify-center p-4 pointer-events-none">
                        <motion.div
                            initial={{ opacity: 0, scale: 0.97, y: 8 }}
                            animate={{ opacity: 1, scale: 1, y: 0 }}
                            exit={{ opacity: 0, scale: 0.97, y: 8 }}
                            transition={{ duration: 0.2 }}
                            className="pointer-events-auto w-full max-w-[440px] max-h-[88dvh] overflow-y-auto rounded-lg border border-slate-200 bg-white shadow-[0_24px_60px_-12px_rgba(15,23,42,0.35)]"
                        >
                            {/* Header */}
                            <div className="px-5 h-14 flex items-center gap-3 border-b border-slate-200">
                                <div className="w-8 h-8 rounded-lg bg-orange-50 text-orange-600 flex items-center justify-center shrink-0">
                                    <FlameIcon className="w-4 h-4" />
                                </div>
                                <div className="min-w-0 flex-1">
                                    <div className="text-[13px] font-medium text-slate-900">Start warmup</div>
                                    <div className="text-[11px] text-slate-400">
                                        {n} mailbox{n > 1 ? "es" : ""} selected
                                    </div>
                                </div>
                                <button
                                    onClick={onClose}
                                    disabled={busy}
                                    aria-label="Close"
                                    className="w-7 h-7 rounded-md flex items-center justify-center text-slate-400 hover:text-slate-900 hover:bg-slate-100 transition-colors disabled:opacity-50"
                                >
                                    <XIcon className="w-4 h-4" />
                                </button>
                            </div>

                            {/* Body */}
                            <div className="px-5 py-4 space-y-4">
                                <label className="flex items-center gap-2.5 cursor-pointer select-none">
                                    <Toggle on={customize} onChange={setCustomize} />
                                    <span className="text-[12.5px] text-slate-700">
                                        Apply these settings to all selected mailboxes
                                    </span>
                                </label>

                                <div className={customize ? "space-y-4" : "space-y-4 opacity-40 pointer-events-none"}>
                                    <div className="grid grid-cols-1 sm:grid-cols-3 gap-3">
                                        <div>
                                            <Label>Start</Label>
                                            <NumberInput value={base} min={1} max={500} onChange={setBase} suffix="/day" />
                                        </div>
                                        <div>
                                            <Label>+ / day</Label>
                                            <NumberInput value={increase} min={0} max={100} onChange={setIncrease} />
                                        </div>
                                        <div>
                                            <Label>Max</Label>
                                            <NumberInput value={max} min={1} max={500} onChange={setMax} suffix="/day" />
                                        </div>
                                    </div>
                                    {baseOverMax && (
                                        <p className="text-[11px] text-rose-500 -mt-1.5">
                                            Starting volume can't exceed the maximum.
                                        </p>
                                    )}
                                    <div>
                                        <Label>Reply rate</Label>
                                        <NumberInput value={replyRate} min={0} max={100} onChange={setReplyRate} suffix="%" className="w-32" />
                                    </div>
                                    <div>
                                        <Label>Sending window</Label>
                                        <div className="grid grid-cols-2 gap-3 max-w-[320px]">
                                            <TimeSelect value={startTime} onChange={setStartTime} />
                                            <TimeSelect value={endTime} onChange={setEndTime} />
                                        </div>
                                    </div>
                                    <div>
                                        <Label>Sending days</Label>
                                        <WeekdayBitmask weekdays={WEEKDAYS} value={days} setValue={setDays} />
                                        <p className="text-[11px] text-slate-400 mt-1.5">
                                            Leave all unselected to send every day.
                                        </p>
                                    </div>
                                </div>
                            </div>

                            {/* Footer */}
                            <div className="px-5 h-14 flex items-center justify-end gap-2 border-t border-slate-200 bg-slate-50/60">
                                <button
                                    onClick={onClose}
                                    disabled={busy}
                                    className="h-8 px-3 rounded-md border border-slate-200 hover:border-slate-300 text-[12px] text-slate-700 hover:text-slate-900 transition-colors disabled:opacity-50"
                                >
                                    Cancel
                                </button>
                                <button
                                    onClick={start}
                                    disabled={busy || n === 0}
                                    className="h-8 px-3.5 rounded-md bg-sky-600 hover:bg-sky-700 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors disabled:opacity-60"
                                >
                                    {busy && <Loading className="!w-3.5 h-3.5 text-white" />}
                                    Start warmup
                                </button>
                            </div>
                        </motion.div>
                    </div>
                </>
            )}
        </AnimatePresence>
    );
}

function Toggle({ on, onChange }: { on: boolean; onChange: (v: boolean) => void }) {
    return (
        <button
            type="button"
            role="switch"
            aria-checked={on}
            onClick={() => onChange(!on)}
            className={`relative h-5 w-9 rounded-full transition-colors shrink-0 ${on ? "bg-sky-600" : "bg-slate-200"}`}
        >
            <span className={`absolute top-0.5 left-0.5 h-4 w-4 rounded-full bg-white shadow-sm transition-transform ${on ? "translate-x-4" : ""}`} />
        </button>
    );
}
