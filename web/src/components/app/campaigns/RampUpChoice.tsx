// How a campaign's per-mailbox volume grows: the one picker the new-campaign
// wizard and the campaign settings share, over the two server fields it maps to.

import React from "react";
import { AnimatePresence, motion } from "framer-motion";
import { AlertCircleIcon, GaugeIcon, MessageSquareReplyIcon, TrendingUpIcon } from "lucide-react";
import { Label, NumberInput } from "@/components/ui/field";
import { ChoiceCard } from "./new/fields";
import { rampUpInvalid, rampUpMode, type RampUp } from "./rampUp";

const MODE_FIELDS = {
    replies: { warmup_graduation: true, ramp_enabled: false },
    schedule: { warmup_graduation: false, ramp_enabled: true },
    full: { warmup_graduation: false, ramp_enabled: false },
} as const satisfies Record<ReturnType<typeof rampUpMode>, Pick<RampUp, "warmup_graduation" | "ramp_enabled">>;

export default function RampUpChoice({
    value,
    onChange,
    level,
}: {
    value: RampUp;
    onChange: (patch: Partial<RampUp>) => void;
    // Today's persisted ramp level on a saved campaign.
    level?: number;
}) {
    const mode = rampUpMode(value);
    const invalid = rampUpInvalid(value);
    const id = React.useId();
    return (
        <div>
            <div role="radiogroup" aria-label="Ramp-up" className="grid grid-cols-1 sm:grid-cols-3 gap-2">
                <ChoiceCard
                    selected={mode === "replies"}
                    icon={MessageSquareReplyIcon}
                    title="As replies come in"
                    description="Mailboxes leaving warmup start at 5 a day and gain one a day per lead who replied this week."
                    onSelect={() => onChange(MODE_FIELDS.replies)}
                />
                <ChoiceCard
                    selected={mode === "schedule"}
                    icon={TrendingUpIcon}
                    title="On a schedule"
                    description="Start low and add a fixed amount each sending day, up to a ceiling you pick."
                    onSelect={() => onChange(MODE_FIELDS.schedule)}
                />
                <ChoiceCard
                    selected={mode === "full"}
                    icon={GaugeIcon}
                    title="Full volume"
                    description="Every mailbox sends up to its daily limit from the first day."
                    onSelect={() => onChange(MODE_FIELDS.full)}
                />
            </div>
            <AnimatePresence initial={false} mode="wait">
                <motion.div
                    key={mode}
                    initial={{ opacity: 0, height: 0 }}
                    animate={{ opacity: 1, height: "auto" }}
                    exit={{ opacity: 0, height: 0 }}
                    transition={{ duration: 0.16 }}
                    className="overflow-hidden"
                >
                    {mode === "schedule" ? (
                        <div className="mt-3 rounded-md border border-slate-200 bg-slate-50/40 p-3.5 space-y-3.5">
                            <div className="flex flex-wrap items-end gap-4">
                                <div>
                                    <Label htmlFor={`${id}-start`}>Start</Label>
                                    <NumberInput
                                        id={`${id}-start`}
                                        value={value.ramp_start}
                                        min={1}
                                        max={5000}
                                        onChange={(v) => onChange({ ramp_start: v })}
                                        suffix="/ day"
                                        className="w-32"
                                    />
                                </div>
                                <div>
                                    <Label htmlFor={`${id}-increment`}>Increase by</Label>
                                    <NumberInput
                                        id={`${id}-increment`}
                                        value={value.ramp_increment}
                                        min={0}
                                        max={100}
                                        onChange={(v) => onChange({ ramp_increment: v })}
                                        suffix="/ day"
                                        className="w-32"
                                    />
                                </div>
                                <div>
                                    <Label htmlFor={`${id}-ceiling`}>Ceiling</Label>
                                    <NumberInput
                                        id={`${id}-ceiling`}
                                        value={value.ramp_ceiling}
                                        min={1}
                                        max={5000}
                                        onChange={(v) => onChange({ ramp_ceiling: v })}
                                        suffix="/ day"
                                        className="w-32"
                                    />
                                </div>
                                {level !== undefined && level > 0 && (
                                    <span className="inline-flex items-center gap-1.5 h-7 px-2.5 rounded-md bg-sky-50 text-sky-700 text-[11.5px] font-medium">
                                        Today&apos;s cap
                                        <span className="font-mono tabular-nums">{level}</span>
                                    </span>
                                )}
                            </div>
                            {invalid ? (
                                <p className="flex items-center gap-1.5 text-[11px] text-rose-500">
                                    <AlertCircleIcon className="w-3.5 h-3.5 shrink-0" />
                                    {invalid}
                                </p>
                            ) : (
                                <RampPreview
                                    start={value.ramp_start}
                                    increment={value.ramp_increment}
                                    ceiling={value.ramp_ceiling}
                                    level={level ?? 0}
                                />
                            )}
                            <p className="text-[11px] text-slate-500 leading-relaxed">
                                Per mailbox. The daily limit and each mailbox&apos;s own cap still apply, and the climb
                                does not pause on a bounce or a spam placement, so watch the campaign&apos;s health while it rises.
                            </p>
                            {value.warmup_graduation && (
                                <p className="text-[11px] text-amber-700 leading-relaxed">
                                    Mailboxes leaving warmup also wait for replies in this campaign. Choose On a schedule
                                    again to send them on this schedule only.
                                </p>
                            )}
                        </div>
                    ) : (
                        <p className="mt-2 text-[11px] text-slate-500 leading-relaxed">
                            {mode === "replies"
                                ? "A bounce pauses the climb and a spam complaint resets it. With a typical reply rate this stays close to 5 a day, so pick On a schedule if you want a predictable climb. Mailboxes that never warmed, or whose warmup is stopped, send at their own limit."
                                : "Each mailbox's daily limit and minimum gap still apply. Going straight to full volume is the fastest and the riskiest choice for mailboxes that have not sent cold mail before."}
                        </p>
                    )}
                </motion.div>
            </AnimatePresence>
        </div>
    );
}

/** A day-by-day bar chart of the per-mailbox ramp; the bar at today's level is highlighted. */
export function RampPreview({
    start,
    increment,
    ceiling,
    level,
}: {
    start: number;
    increment: number;
    ceiling: number;
    level: number;
}) {
    if (start <= 0 || ceiling <= 0 || start > ceiling) return null;
    // The climb reaches its ceiling on steadyDay; the chart shows its first MAX_BARS days.
    const steadyDay = increment > 0 ? 1 + Math.ceil((ceiling - start) / increment) : 1;
    const steady = increment > 0 ? ceiling : start;
    const MAX_BARS = 14;
    const shown = Array.from({ length: Math.min(steadyDay, MAX_BARS) }, (_, i) => Math.min(ceiling, start + i * increment));
    const more = steadyDay - shown.length;
    const todayIdx = level > 0 ? shown.findIndex((d) => d >= level) : -1;

    return (
        <div>
            <div className="flex items-end gap-1 h-14">
                {shown.map((val, i) => (
                    <div
                        key={i}
                        title={`Day ${i + 1}: ${val}/day`}
                        className={`flex-1 min-w-[4px] rounded-t-sm ${i === todayIdx ? "bg-sky-500" : "bg-sky-200"}`}
                        style={{ height: `${Math.max(10, (val / ceiling) * 100)}%` }}
                    />
                ))}
            </div>
            <div className="flex items-center justify-between mt-1.5 text-[10px] text-slate-400">
                <span>Day 1 · {start}/day</span>
                <span>
                    {more > 0 ? `+${more} more · ` : ""}Day {steadyDay} · {steady}/day, then steady
                </span>
            </div>
        </div>
    );
}
