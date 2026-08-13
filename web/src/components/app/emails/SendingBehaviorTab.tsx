// Sending behaviour — the mailbox's human schedule.
//
// The editor sets RANGES; the mailbox rolls one workday out of them per local
// day and keeps to it. The panel leads with that rolled day, because "why did
// nothing send at 4pm" is answered by today's plan, not by the ranges that
// produced it.
//
// Owns its own save (PUT /emails/:id/behavior) rather than joining the drawer's
// shared mailbox form: it is a separate resource with its own validation.

import React from "react";
import { AnimatePresence, motion } from "framer-motion";
import {
    ClockIcon,
    CoffeeIcon,
    GaugeIcon,
    InfoIcon,
    SunriseIcon,
    SunsetIcon,
    TimerIcon,
} from "lucide-react";
import toast from "react-hot-toast";

import { Toggle } from "@/components/app/campaigns/preferences/components/CampaignPreferenceBoolBox";
import WeekdayBitmask from "@/components/app/campaigns/schedule/WeekdayBitmask";
import { Loading } from "@/components/loader";
import { NumberInput, TextInput } from "@/components/ui/field";
import type { AppError } from "@/lib/api/client/normalizeError";
import useSendingBehavior from "@/lib/api/hooks/app/emails/useSendingBehavior";
import useSendingPlan from "@/lib/api/hooks/app/emails/useSendingPlan";
import useUpdateSendingBehavior from "@/lib/api/hooks/app/emails/useUpdateSendingBehavior";
import type SendingBehavior from "@/lib/api/models/app/emails/SendingBehavior";
import {
    DEFAULT_SENDING_BEHAVIOR,
    WEEKDAY_LABELS,
    clockToMinutes,
    minutesToClock,
    secondsToLabel,
    validateSendingBehavior,
    type SendingBehaviorPatch,
} from "@/lib/api/models/app/emails/SendingBehavior";
import buildError from "@/lib/helper/buildError";
import { cn } from "@/lib/utils";

const Eyebrow = ({ children }: { children: React.ReactNode }) => (
    <div className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">{children}</div>
);

function FieldShell({ label, hint, children }: { label: string; hint?: string; children: React.ReactNode }) {
    return (
        <div>
            <label className="block text-[12px] font-medium text-slate-700 mb-1">{label}</label>
            {children}
            {hint && <p className="text-[10.5px] text-slate-400 mt-1 leading-relaxed">{hint}</p>}
        </div>
    );
}

// Minute-precision clock field. Deliberately not the shared 30-minute
// TimeSelect: the whole point of this panel is that a mailbox starts at 09:14
// rather than on the half hour.
function ClockField({ value, onChange }: { value: number; onChange: (v: number) => void }) {
    const [draft, setDraft] = React.useState(() => minutesToClock(value));

    React.useEffect(() => {
        setDraft(minutesToClock(value));
    }, [value]);

    const commit = (raw: string) => {
        const parsed = clockToMinutes(raw);
        if (parsed === null) {
            setDraft(minutesToClock(value)); // reject: snap back to the stored value
            return;
        }
        onChange(parsed);
    };

    return (
        <TextInput
            value={draft}
            placeholder="09:00"
            onChange={setDraft}
            onBlur={() => commit(draft)}
            className="w-full h-9 tabular-nums"
        />
    );
}

function RangeRow({
    icon,
    label,
    hint,
    children,
}: {
    icon: React.ReactNode;
    label: string;
    hint?: string;
    children: React.ReactNode;
}) {
    return (
        <div>
            <div className="flex items-center gap-1.5 mb-1">
                <span className="text-slate-400">{icon}</span>
                <label className="text-[12px] font-medium text-slate-700">{label}</label>
            </div>
            {children}
            {hint && <p className="text-[10.5px] text-slate-400 mt-1 leading-relaxed">{hint}</p>}
        </div>
    );
}

// A min/max pair rendered as one control, since every range in this panel is
// "somewhere between these two".
function PairField({
    min,
    max,
    onMin,
    onMax,
    suffix,
    step = 1,
    lowerBound = 0,
}: {
    min: number;
    max: number;
    onMin: (v: number) => void;
    onMax: (v: number) => void;
    suffix?: string;
    step?: number;
    lowerBound?: number;
}) {
    return (
        <div className="flex items-center gap-2">
            <NumberInput value={min} onChange={onMin} min={lowerBound} step={step} align="right" className="w-full h-9" />
            <span className="text-[11.5px] text-slate-400 shrink-0">to</span>
            <NumberInput value={max} onChange={onMax} min={lowerBound} step={step} suffix={suffix} align="right" className="w-full h-9" />
        </div>
    );
}

function ClockPair({
    min,
    max,
    onMin,
    onMax,
}: {
    min: number;
    max: number;
    onMin: (v: number) => void;
    onMax: (v: number) => void;
}) {
    return (
        <div className="flex items-center gap-2">
            <ClockField value={min} onChange={onMin} />
            <span className="text-[11.5px] text-slate-400 shrink-0">to</span>
            <ClockField value={max} onChange={onMax} />
        </div>
    );
}

/* ── today's rolled workday ─────────────────────── */

function TodayCard({ mailboxId, enabled }: { mailboxId: string; enabled: boolean }) {
    const plan = useSendingPlan(mailboxId, enabled);

    if (!enabled) return null;
    if (plan.isPending) {
        return (
            <div className="px-5 py-6 flex items-center justify-center">
                <Loading className="!w-4 h-4" />
            </div>
        );
    }
    if (!plan.data) return null;

    const p = plan.data;

    if (!p.is_working_day) {
        return (
            <div className="px-5 py-4">
                <Eyebrow>Today</Eyebrow>
                <p className="mt-2 text-[12.5px] text-slate-700">
                    Not a sending day for this mailbox. The next working day picks up its own hours.
                </p>
            </div>
        );
    }

    const used = Math.min(100, (p.sent_today / Math.max(1, p.daily_limit)) * 100);

    return (
        <div className="px-5 py-4">
            <div className="flex items-center justify-between">
                <Eyebrow>Today&apos;s workday</Eyebrow>
                <span className="text-[10.5px] text-slate-400 font-mono">{p.timezone}</span>
            </div>

            <div className="mt-2 flex flex-wrap items-center gap-x-4 gap-y-1.5 text-[12.5px] text-slate-700">
                <span className="inline-flex items-center gap-1.5">
                    <ClockIcon className="w-3.5 h-3.5 text-slate-400" />
                    <b className="text-slate-900 tabular-nums">{minutesToClock(p.work_start_minute)}</b>
                    <span className="text-slate-400">–</span>
                    <b className="text-slate-900 tabular-nums">{minutesToClock(p.work_end_minute)}</b>
                </span>
                {p.lunch_start_minute !== null && p.lunch_end_minute !== null && (
                    <span className="inline-flex items-center gap-1.5">
                        <CoffeeIcon className="w-3.5 h-3.5 text-slate-400" />
                        <span className="tabular-nums">
                            {minutesToClock(p.lunch_start_minute)}–{minutesToClock(p.lunch_end_minute)}
                        </span>
                    </span>
                )}
                <span className="inline-flex items-center gap-1.5">
                    <TimerIcon className="w-3.5 h-3.5 text-slate-400" />
                    <span>{secondsToLabel(p.gap_min_seconds)}–{secondsToLabel(p.gap_max_seconds)} apart</span>
                </span>
                <span className="inline-flex items-center gap-1.5">
                    <GaugeIcon className="w-3.5 h-3.5 text-slate-400" />
                    <span>max {p.hourly_limit}/hour</span>
                </span>
            </div>

            <div className="mt-3 flex items-center justify-between text-[12px] text-slate-600">
                <span>
                    <b className="text-slate-900 tabular-nums">{p.sent_today}</b> of{" "}
                    <b className="text-slate-900 tabular-nums">{p.daily_limit}</b> sent
                </span>
                <span className="text-slate-400 tabular-nums">{p.remaining_today} left</span>
            </div>
            <div className="mt-1.5 h-1.5 w-full rounded-full bg-slate-100 overflow-hidden">
                <div className="h-full rounded-full bg-sky-500 transition-all" style={{ width: `${used}%` }} />
            </div>
        </div>
    );
}

/* ── the tab ─────────────────────── */

export default function SendingBehaviorTab({ mailboxId, timezone }: { mailboxId: string; timezone?: string }) {
    const query = useSendingBehavior(mailboxId);
    const mutation = useUpdateSendingBehavior(mailboxId);

    const [form, setForm] = React.useState<SendingBehavior | null>(null);
    React.useEffect(() => {
        if (query.data) setForm(query.data);
    }, [query.data]);

    const update = (patch: Partial<SendingBehavior>) => setForm((f) => (f ? { ...f, ...patch } : f));

    const dirty = React.useMemo(
        () => !!form && !!query.data && JSON.stringify(form) !== JSON.stringify(query.data),
        [form, query.data],
    );
    const error = React.useMemo(() => (form ? validateSendingBehavior(form) : null), [form]);

    if (query.isPending || !form) {
        return (
            <div className="px-5 py-10 flex items-center justify-center">
                <Loading className="!w-4 h-4" />
            </div>
        );
    }

    if (query.isError) {
        return (
            <div className="px-5 py-6 text-[12.5px] text-slate-500">
                Sending behaviour is unavailable for this mailbox right now.
            </div>
        );
    }

    const save = async () => {
        if (error) {
            toast.error(error);
            return;
        }
        const patch: SendingBehaviorPatch = {};
        const stored = query.data as SendingBehavior;
        for (const key of Object.keys(DEFAULT_SENDING_BEHAVIOR) as (keyof SendingBehaviorPatch)[]) {
            if (form[key] !== stored[key]) {
                (patch as Record<string, unknown>)[key] = form[key];
            }
        }
        try {
            await mutation.mutateAsync(patch);
            toast.success("Sending behaviour updated");
        } catch (e) {
            toast.error(buildError(e as AppError));
        }
    };

    const tz = form.timezone || timezone;

    return (
        // The drawer owns the scroll container, so the save row is sticky
        // rather than a flex footer — otherwise it would sit below the fold on
        // a panel this tall.
        <div>
            <div className="divide-y divide-slate-200/60">
                {/* On/off */}
                <div className="px-5 py-4 flex items-center justify-between gap-3">
                    <div className="flex items-center gap-2.5 min-w-0">
                        <div
                            className={cn(
                                "w-8 h-8 rounded-lg flex items-center justify-center shrink-0",
                                form.enabled ? "bg-sky-50 text-sky-600" : "bg-slate-100 text-slate-400",
                            )}
                        >
                            <ClockIcon className="w-4 h-4" />
                        </div>
                        <div className="min-w-0">
                            <div className="text-[12.5px] font-medium text-slate-900">
                                {form.enabled ? "Sending like a person" : "Fixed schedule"}
                            </div>
                            <div className="text-[11px] text-slate-400">
                                {form.enabled
                                    ? "Hours, volume and spacing are re-rolled each day"
                                    : "Uses the mailbox daily cap and minimum gap"}
                            </div>
                        </div>
                    </div>
                    <Toggle value={form.enabled} onChange={(v) => update({ enabled: v })} />
                </div>

                <TodayCard mailboxId={mailboxId} enabled={form.enabled && query.data?.enabled === true} />

                {/* Working days */}
                <div className="px-5 py-5 space-y-2">
                    <Eyebrow>Working days</Eyebrow>
                    <WeekdayBitmask
                        weekdays={[...WEEKDAY_LABELS]}
                        value={form.weekdays}
                        setValue={(v) => update({ weekdays: v })}
                    />
                    <p className="text-[10.5px] text-slate-400 leading-relaxed">
                        Days this mailbox sends anything at all, cold outreach and warmup alike.
                    </p>
                </div>

                {/* Workday */}
                <div className="px-5 py-5 space-y-5">
                    <Eyebrow>Workday</Eyebrow>
                    <RangeRow
                        icon={<SunriseIcon className="w-3.5 h-3.5" />}
                        label="Starts somewhere between"
                        hint="A different start time each day, so the mailbox never opens on the same minute twice."
                    >
                        <ClockPair
                            min={form.work_start_min}
                            max={form.work_start_max}
                            onMin={(v) => update({ work_start_min: v })}
                            onMax={(v) => update({ work_start_max: v })}
                        />
                    </RangeRow>
                    <RangeRow
                        icon={<SunsetIcon className="w-3.5 h-3.5" />}
                        label="Finishes somewhere between"
                        hint="Nothing is scheduled after the day's rolled finish time."
                    >
                        <ClockPair
                            min={form.work_end_min}
                            max={form.work_end_max}
                            onMin={(v) => update({ work_end_min: v })}
                            onMax={(v) => update({ work_end_max: v })}
                        />
                    </RangeRow>
                </div>

                {/* Lunch */}
                <div className="px-5 py-5 space-y-5">
                    <div className="flex items-center justify-between gap-3">
                        <Eyebrow>Lunch break</Eyebrow>
                        <Toggle value={form.lunch_enabled} onChange={(v) => update({ lunch_enabled: v })} />
                    </div>
                    {form.lunch_enabled && (
                        <>
                            <RangeRow icon={<CoffeeIcon className="w-3.5 h-3.5" />} label="Starts somewhere between">
                                <ClockPair
                                    min={form.lunch_earliest}
                                    max={form.lunch_latest}
                                    onMin={(v) => update({ lunch_earliest: v })}
                                    onMax={(v) => update({ lunch_latest: v })}
                                />
                            </RangeRow>
                            <FieldShell label="Lasts" hint="A quiet gap in the middle of the day, like any real inbox has.">
                                <PairField
                                    min={form.lunch_min_minutes}
                                    max={form.lunch_max_minutes}
                                    onMin={(v) => update({ lunch_min_minutes: v })}
                                    onMax={(v) => update({ lunch_max_minutes: v })}
                                    suffix="minutes"
                                />
                            </FieldShell>
                        </>
                    )}
                </div>

                {/* Volume + spacing */}
                <div className="px-5 py-5 space-y-5">
                    <Eyebrow>Volume and spacing</Eyebrow>
                    <FieldShell
                        label="Cold emails per day"
                        hint="Rolled once a day inside this range. It can only lower the mailbox's daily cap, never raise it."
                    >
                        <PairField
                            min={form.daily_limit_min}
                            max={form.daily_limit_max}
                            onMin={(v) => update({ daily_limit_min: v })}
                            onMax={(v) => update({ daily_limit_max: v })}
                            suffix="/ day"
                            lowerBound={1}
                        />
                    </FieldShell>
                    <FieldShell
                        label="Cold emails per hour"
                        hint="Stops a whole day's allowance landing in one burst before lunch."
                    >
                        <PairField
                            min={form.hourly_limit_min}
                            max={form.hourly_limit_max}
                            onMin={(v) => update({ hourly_limit_min: v })}
                            onMax={(v) => update({ hourly_limit_max: v })}
                            suffix="/ hour"
                            lowerBound={1}
                        />
                    </FieldShell>
                    <FieldShell
                        label="Delay between emails"
                        hint={`Drawn fresh for every send, so the intervals stay irregular. Currently ${secondsToLabel(form.gap_min_seconds)} to ${secondsToLabel(form.gap_max_seconds)}.`}
                    >
                        <PairField
                            min={form.gap_min_seconds}
                            max={form.gap_max_seconds}
                            onMin={(v) => update({ gap_min_seconds: v })}
                            onMax={(v) => update({ gap_max_seconds: v })}
                            suffix="seconds"
                            step={15}
                            lowerBound={30}
                        />
                    </FieldShell>
                </div>

                {/* Timezone note */}
                <div className="px-5 py-4">
                    <div className="rounded-md border border-sky-100 bg-sky-50/70 px-3 py-2.5 flex gap-2.5">
                        <InfoIcon className="w-4 h-4 text-sky-600 shrink-0 mt-0.5" />
                        <p className="text-[11.5px] text-sky-900/90 leading-relaxed">
                            Every time here is local to <b>{tz || "UTC"}</b>, this mailbox&apos;s own timezone. Mailboxes in
                            other regions keep their own working hours on the same campaign. Change the timezone on the
                            mailbox to move the whole schedule.
                        </p>
                    </div>
                </div>

                {error && (
                    <div className="px-5 py-3">
                        <p className="text-[11.5px] text-rose-600">{error}</p>
                    </div>
                )}
            </div>

            <AnimatePresence>
                {dirty && (
                    <motion.div
                        initial={{ y: 60 }}
                        animate={{ y: 0 }}
                        exit={{ y: 60 }}
                        transition={{ duration: 0.2 }}
                        className="sticky bottom-0 z-10 h-14 px-5 flex items-center gap-2 border-t border-slate-200 bg-slate-50/95 backdrop-blur-sm"
                    >
                        <span className="text-[11.5px] text-slate-500">Unsaved changes</span>
                        <div className="ml-auto flex items-center gap-2">
                            <button
                                onClick={() => query.data && setForm(query.data)}
                                className="h-8 px-3 rounded-md border border-slate-200 hover:border-slate-300 text-[12px] text-slate-700 hover:text-slate-900 transition-colors"
                            >
                                Discard
                            </button>
                            <button
                                onClick={save}
                                disabled={mutation.isPending || !!error}
                                className="h-8 px-3.5 rounded-md bg-sky-600 hover:bg-sky-700 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors disabled:opacity-60"
                            >
                                {mutation.isPending && <Loading className="!w-3.5 h-3.5 text-white" />}
                                Save behaviour
                            </button>
                        </div>
                    </motion.div>
                )}
            </AnimatePresence>
        </div>
    );
}
