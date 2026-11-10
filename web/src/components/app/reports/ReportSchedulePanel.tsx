// Emails the report to up to ten people on a weekly or monthly cadence.

import React from "react";
import { CalendarClockIcon, Loader2Icon, XIcon } from "lucide-react";
import toast from "react-hot-toast/headless";

import { FieldError, Label, NumberInput } from "@/components/ui/field";
import { SelectMenu } from "@/components/ui/select-menu";
import { Segmented, Toggle } from "@/components/app/campaigns/preferences/components/CampaignPreferenceBoolBox";
import { useConfirm } from "@/hooks/context/confirm";
import { useUserProfile } from "@/hooks/context/user";
import type { WriteGuard } from "@/hooks/usePermission";
import useCurrentOrganization from "@/lib/api/hooks/app/organizations/useCurrentOrganization";
import { useDeleteReportSchedule, usePauseReportSchedule, useSetReportSchedule } from "@/lib/api/hooks/app/reports";
import type ClientReport from "@/lib/api/models/app/reports/ClientReport";
import type { ClientReportCadence, ClientReportScheduleInput } from "@/lib/api/models/app/reports/ClientReport";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import { defaultScheduleTimezone, timezoneOptions } from "@/lib/timezone";
import { cn } from "@/lib/utils";
import { formatDateTime, hourLabel, rangeLabel, scheduleSummary, WEEKDAYS } from "./reportText";

const MAX_RECIPIENTS = 10;
const EMAIL = /^[^\s@<>(),;:"]+@[^\s@<>(),;:"]+\.[^\s@<>(),;:"]+$/;

const CADENCE_OPTIONS: { value: ClientReportCadence; label: string }[] = [
    { value: "weekly", label: "Weekly" },
    { value: "monthly", label: "Monthly" },
];
const WEEKDAY_OPTIONS = WEEKDAYS.map((d, i) => ({ value: String(i), label: d }));
const HOUR_OPTIONS = Array.from({ length: 24 }, (_, h) => ({ value: String(h), label: hourLabel(h) }));

type Draft = ClientReportScheduleInput;

function sameDraft(a: Draft, b: Draft) {
    return (
        a.cadence === b.cadence &&
        a.weekday === b.weekday &&
        a.month_day === b.month_day &&
        a.hour === b.hour &&
        a.timezone === b.timezone &&
        a.paused === b.paused &&
        a.recipients.length === b.recipients.length &&
        a.recipients.every((r, i) => r === b.recipients[i])
    );
}

export default function ReportSchedulePanel({ report, write }: { report: ClientReport; write: WriteGuard }) {
    const s = report.schedule;
    // Remounts the editor when the saved schedule changes underneath it, but not on a pause flip.
    const key = s ? [s.recipients.join(","), s.cadence, s.weekday, s.month_day, s.hour, s.timezone].join("|") : "none";
    return <ScheduleEditor key={key} report={report} write={write} />;
}

function ScheduleEditor({ report, write }: { report: ClientReport; write: WriteGuard }) {
    const confirm = useConfirm();
    const profile = useUserProfile();
    const org = useCurrentOrganization();
    const save = useSetReportSchedule();
    const pause = usePauseReportSchedule();
    const remove = useDeleteReportSchedule();
    const saved = report.schedule;

    const start = React.useMemo<Draft>(
        () =>
            saved
                ? {
                      recipients: saved.recipients,
                      cadence: saved.cadence,
                      weekday: saved.weekday,
                      month_day: saved.month_day,
                      hour: saved.hour,
                      timezone: saved.timezone,
                      paused: saved.paused,
                  }
                : {
                      recipients: [],
                      cadence: "weekly",
                      weekday: 1,
                      month_day: 1,
                      hour: 9,
                      timezone: defaultScheduleTimezone(org.data?.timezone),
                      paused: false,
                  },
        // The draft seeds once per mount; a saved change remounts through the key.
        // eslint-disable-next-line react-hooks/exhaustive-deps
        [],
    );
    const [draft, setDraft] = React.useState<Draft>(start);
    const [entry, setEntry] = React.useState("");
    const [entryError, setEntryError] = React.useState<string | null>(null);
    const [problem, setProblem] = React.useState<string | null>(null);
    const [showErrors, setShowErrors] = React.useState(false);

    // A saved schedule's pause is applied on the spot, so it is never part of the draft.
    const paused = saved ? saved.paused : draft.paused;
    const dirty = !sameDraft({ ...draft, paused }, { ...start, paused: saved ? saved.paused : start.paused }) || entry.trim() !== "";

    const tzOptions = React.useMemo(() => timezoneOptions(profile?.timezones, draft.timezone), [profile?.timezones, draft.timezone]);
    const set = (patch: Partial<Draft>) => {
        setProblem(null);
        setDraft((d) => ({ ...d, ...patch }));
    };

    // Adds every address in the text, keeping what could not be read in the box with a reason.
    function commitEntry(raw = entry): { recipients: string[]; clean: boolean } {
        const parts = raw.split(/[\s,;]+/).map((p) => p.trim()).filter(Boolean);
        if (parts.length === 0) return { recipients: draft.recipients, clean: true };
        const next = [...draft.recipients];
        const rejected: string[] = [];
        let full = false;
        for (const p of parts) {
            const addr = p.replace(/^<|>$/g, "").toLowerCase();
            if (!EMAIL.test(addr)) {
                rejected.push(p);
                continue;
            }
            if (next.includes(addr)) continue;
            if (next.length >= MAX_RECIPIENTS) {
                full = true;
                rejected.push(p);
                continue;
            }
            next.push(addr);
        }
        set({ recipients: next });
        setEntry(rejected.join(", "));
        setEntryError(full ? `Up to ${MAX_RECIPIENTS} recipients.` : rejected.length ? "Enter a plain email address, like name@client.com." : null);
        return { recipients: next, clean: rejected.length === 0 };
    }

    async function submit() {
        const { recipients, clean } = commitEntry();
        if (!clean) return;
        if (recipients.length === 0) {
            setShowErrors(true);
            return;
        }
        setProblem(null);
        try {
            await save.mutateAsync({ id: report.id, input: { ...draft, recipients, paused } });
            toast.success(saved ? "Schedule saved" : "Schedule turned on");
        } catch (err) {
            const e = err as AppError;
            if (e.code === "invalid_report_schedule" || e.code === "report_emails_unavailable") setProblem(e.message);
            else toast.error(buildError(e));
        }
    }

    async function togglePause(next: boolean) {
        if (!saved) {
            set({ paused: next });
            return;
        }
        try {
            await pause.mutateAsync({ report, paused: next });
        } catch (err) {
            toast.error(buildError(err as AppError));
        }
    }

    function askRemove() {
        confirm.show("Stop emailing this report? Recipients get no further emails.", async () => {
            try {
                await remove.mutateAsync(report.id);
                toast.success("Schedule removed");
            } catch (err) {
                toast.error(buildError(err as AppError));
            }
        });
    }

    const disabled = !write.allowed;

    return (
        <div className="flex flex-col gap-4 max-w-[640px]">
            <div className="flex items-start gap-3">
                <div className="size-8 rounded-md bg-slate-100 flex items-center justify-center shrink-0">
                    <CalendarClockIcon className="w-3.5 h-3.5 text-slate-500" />
                </div>
                <div className="min-w-0 flex-1">
                    <div className="flex items-center gap-2">
                        <span className="text-[13px] font-medium text-slate-900">Scheduled email</span>
                        {saved && (
                            <span
                                className={cn(
                                    "inline-flex items-center h-5 px-1.5 rounded border text-[10.5px] font-medium",
                                    saved.paused ? "bg-amber-50 text-amber-700 border-amber-100" : "bg-emerald-50 text-emerald-700 border-emerald-100",
                                )}
                            >
                                {saved.paused ? "Paused" : "On"}
                            </span>
                        )}
                    </div>
                    <p className="text-[11.5px] text-slate-500 leading-relaxed">
                        Each email covers the report&apos;s default range ({rangeLabel(report.date_range).toLowerCase()}) and links to the shared page while sharing is on.
                    </p>
                    {saved && (
                        <p className="mt-1 text-[11.5px] text-slate-500">
                            {scheduleSummary(saved)} ({saved.timezone}).{" "}
                            {saved.paused ? "Paused, nothing is sent." : `Next email ${formatDateTime(saved.next_run_at)}.`}{" "}
                            {saved.last_sent_at ? `Last sent ${formatDateTime(saved.last_sent_at)}.` : "Not sent yet."}
                        </p>
                    )}
                </div>
            </div>

            <div>
                <Label>Recipients</Label>
                <div
                    className={cn(
                        "rounded-md border bg-white min-h-[34px] px-2 py-1 flex flex-wrap items-center gap-1 transition-colors focus-within:border-sky-400 focus-within:ring-2 focus-within:ring-sky-100",
                        entryError || (showErrors && draft.recipients.length === 0) ? "border-red-300" : "border-slate-200",
                        disabled && "bg-slate-50",
                    )}
                >
                    {draft.recipients.map((r) => (
                        <span key={r} className="inline-flex items-center gap-1 h-5 max-w-full min-w-0 px-1.5 rounded bg-slate-100 text-[11.5px] text-slate-700">
                            <span className="truncate">{r}</span>
                            {!disabled && (
                                <button
                                    type="button"
                                    onClick={() => set({ recipients: draft.recipients.filter((x) => x !== r) })}
                                    aria-label={`Remove ${r}`}
                                    className="opacity-60 hover:opacity-100 shrink-0"
                                >
                                    <XIcon className="w-2.5 h-2.5" />
                                </button>
                            )}
                        </span>
                    ))}
                    {draft.recipients.length < MAX_RECIPIENTS && (
                        <input
                            value={entry}
                            disabled={disabled}
                            onChange={(e) => {
                                setEntryError(null);
                                const v = e.target.value;
                                if (/[,;\s]$/.test(v)) commitEntry(v);
                                else setEntry(v);
                            }}
                            onKeyDown={(e) => {
                                if (e.key === "Enter") {
                                    e.preventDefault();
                                    commitEntry();
                                } else if (e.key === "Backspace" && entry === "" && draft.recipients.length > 0) {
                                    set({ recipients: draft.recipients.slice(0, -1) });
                                }
                            }}
                            onPaste={(e) => {
                                const text = e.clipboardData.getData("text");
                                if (/[,;\s]/.test(text.trim())) {
                                    e.preventDefault();
                                    commitEntry(`${entry} ${text}`);
                                }
                            }}
                            onBlur={() => commitEntry()}
                            placeholder={draft.recipients.length === 0 ? "name@client.com" : "Add another"}
                            aria-label="Add recipient"
                            className="flex-1 min-w-[140px] h-6 bg-transparent outline-none text-[16px] md:text-[12.5px] text-slate-900 placeholder:text-slate-400"
                        />
                    )}
                </div>
                <FieldError message={entryError ?? (showErrors && draft.recipients.length === 0 ? "Add at least one recipient." : null)} />
                <p className="mt-1 text-[11px] text-slate-400">
                    Up to {MAX_RECIPIENTS} addresses. {draft.recipients.length}/{MAX_RECIPIENTS} added.
                </p>
            </div>

            <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
                <div>
                    <Label>Cadence</Label>
                    <Segmented value={draft.cadence} onChange={(cadence) => !disabled && set({ cadence })} options={CADENCE_OPTIONS} />
                </div>
                {draft.cadence === "weekly" ? (
                    <div>
                        <Label>Day</Label>
                        <SelectMenu
                            value={String(draft.weekday)}
                            onChange={(v) => set({ weekday: Number(v) })}
                            options={WEEKDAY_OPTIONS}
                            fullWidth
                            disabled={disabled}
                            aria-label="Day of the week"
                        />
                    </div>
                ) : (
                    <div>
                        <Label>Day of month</Label>
                        <NumberInput
                            value={draft.month_day}
                            onChange={(month_day) => set({ month_day })}
                            min={1}
                            max={28}
                            disabled={disabled}
                            className="w-full"
                        />
                        <p className="mt-1 text-[11px] text-slate-400">1 to 28, so every month has the day.</p>
                    </div>
                )}
                <div>
                    <Label>Time</Label>
                    <SelectMenu
                        value={String(draft.hour)}
                        onChange={(v) => set({ hour: Number(v) })}
                        options={HOUR_OPTIONS}
                        fullWidth
                        disabled={disabled}
                        aria-label="Hour"
                    />
                </div>
                <div>
                    <Label>Timezone</Label>
                    <SelectMenu
                        value={draft.timezone}
                        onChange={(timezone) => set({ timezone })}
                        options={tzOptions}
                        fullWidth
                        disabled={disabled}
                        placeholder="Select a timezone"
                        aria-label="Timezone"
                    />
                </div>
            </div>

            <div className="rounded-md border border-slate-200 px-3 py-2 flex items-center gap-3">
                <div className="min-w-0 flex-1">
                    <div className="text-[12.5px] text-slate-900">Paused</div>
                    <div className="text-[11px] text-slate-500">Keep the schedule but send nothing until you turn this off.</div>
                </div>
                <Toggle value={paused} onChange={(v) => void togglePause(v)} disabled={disabled || pause.isPending} ariaLabel="Pause schedule" />
            </div>

            {problem && <p className="text-[11.5px] text-red-600">{problem}</p>}

            {!disabled && (
                <div className="flex items-center gap-2">
                    <button
                        type="button"
                        onClick={write.guard(() => void submit())}
                        disabled={save.isPending || (!!saved && !dirty)}
                        className="h-7 px-2.5 rounded-md bg-sky-600 hover:bg-sky-700 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors disabled:opacity-60"
                    >
                        {save.isPending && <Loader2Icon className="w-3 h-3 animate-spin" />}
                        {saved ? "Save schedule" : "Turn on schedule"}
                    </button>
                    {saved && (
                        <button
                            type="button"
                            onClick={write.guard(askRemove)}
                            disabled={remove.isPending}
                            className="ml-auto h-7 px-2.5 rounded-md text-rose-600 hover:bg-rose-50 text-[12px] font-medium transition-colors disabled:opacity-60"
                        >
                            Remove schedule
                        </button>
                    )}
                </div>
            )}
        </div>
    );
}
