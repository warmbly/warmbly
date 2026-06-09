// Multi-step new-campaign wizard.
//
// Replaces the 2-field modal. Lets the user configure everything for a
// campaign — basics, schedule, sender pool, the first sequence + follow-ups,
// tracking — in one continuous flow, then submits a single atomic create
// call. The user can skip any optional step and still get a valid campaign:
// only `name` is enforced. The detail page picks up the rest from a live
// channel subscription after navigation.

import React from "react";
import { AnimatePresence, motion } from "framer-motion";
import {
    CalendarClockIcon,
    CheckIcon,
    ChevronLeftIcon,
    ChevronRightIcon,
    Loader2Icon,
    MegaphoneIcon,
    PlusIcon,
    RocketIcon,
    SendIcon,
    Trash2Icon,
    UsersIcon,
    XIcon,
} from "lucide-react";
import toast from "react-hot-toast";
import { useNavigate } from "react-router-dom";
import useCreateCampaign from "@/lib/api/hooks/app/campaigns/useCreateCampaign";
import { Label, TextInput } from "@/components/ui/field";
import { SelectMenu, type SelectOption } from "@/components/ui/select-menu";
import WeekdayBitmask from "@/components/app/campaigns/schedule/WeekdayBitmask";
import TagSelector from "@/components/app/popup/select/TagSelector";
import { useUserProfile } from "@/hooks/context/user";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";

interface Props {
    open: boolean;
    onClose: () => void;
}

type SequenceDraft = {
    name: string;
    subject: string;
    body_plain: string;
    wait_after: number;
};

const WEEKDAYS = ["Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"];
const DEFAULT_DAYS = 0b0011111; // Mon–Fri

type Step = 0 | 1 | 2 | 3;
const STEPS: { key: Step; label: string; icon: React.ComponentType<{ className?: string }> }[] = [
    { key: 0, label: "Basics", icon: MegaphoneIcon },
    { key: 1, label: "Schedule", icon: CalendarClockIcon },
    { key: 2, label: "Sender pool", icon: UsersIcon },
    { key: 3, label: "First email", icon: SendIcon },
];

const initialSequence = (): SequenceDraft => ({
    name: "Step 1",
    subject: "",
    body_plain: "",
    wait_after: 0,
});

export function NewCampaignDialog({ open, onClose }: Props) {
    const navigate = useNavigate();
    const profile = useUserProfile();
    const create = useCreateCampaign();

    const [step, setStep] = React.useState<Step>(0);

    // Step 1 — basics
    const [name, setName] = React.useState("");
    const [description, setDescription] = React.useState("");

    // Step 2 — schedule
    const [timezone, setTimezone] = React.useState<string>(
        profile?.timezones?.[0]?.name || "Europe/London",
    );
    const [days, setDays] = React.useState<number>(DEFAULT_DAYS);
    const [startTime, setStartTime] = React.useState<string>("08:00");
    const [endTime, setEndTime] = React.useState<string>("18:00");

    // Step 3 — sender pool + tracking
    const [emailTagIds, setEmailTagIds] = React.useState<string[]>([]);
    const [dailyLimit, setDailyLimit] = React.useState<number>(50);
    const [stopOnReply, setStopOnReply] = React.useState<boolean>(true);
    const [openTracking, setOpenTracking] = React.useState<boolean>(true);
    const [linkTracking, setLinkTracking] = React.useState<boolean>(true);
    const [unsubHeader, setUnsubHeader] = React.useState<boolean>(true);

    // Step 4 — sequences
    const [sequences, setSequences] = React.useState<SequenceDraft[]>([initialSequence()]);
    const [launchImmediately, setLaunchImmediately] = React.useState<boolean>(false);

    React.useEffect(() => {
        if (!open) {
            setStep(0);
            setName("");
            setDescription("");
            setTimezone(profile?.timezones?.[0]?.name || "Europe/London");
            setDays(DEFAULT_DAYS);
            setStartTime("08:00");
            setEndTime("18:00");
            setEmailTagIds([]);
            setDailyLimit(50);
            setStopOnReply(true);
            setOpenTracking(true);
            setLinkTracking(true);
            setUnsubHeader(true);
            setSequences([initialSequence()]);
            setLaunchImmediately(false);
        }
    }, [open, profile?.timezones]);

    const canAdvance = React.useMemo(() => {
        if (step === 0) return name.trim().length >= 3 && name.trim().length <= 50;
        if (step === 3) {
            const first = sequences[0];
            if (!first) return false;
            return first.subject.trim().length > 0 && first.body_plain.trim().length > 0;
        }
        return true;
    }, [step, name, sequences]);

    async function submit() {
        if (create.isPending) return;
        const trimmedName = name.trim();
        if (trimmedName.length < 3) {
            toast.error("Name must be at least 3 characters");
            setStep(0);
            return;
        }
        const cleanSequences = sequences
            .filter((s) => s.subject.trim().length > 0 || s.body_plain.trim().length > 0)
            .map((s, i) => ({
                name: s.name.trim() || `Step ${i + 1}`,
                subject: s.subject.trim(),
                body_plain: s.body_plain,
                body_html: `<div>${escapeHtml(s.body_plain).replace(/\n/g, "<br/>")}</div>`,
                wait_after: i === 0 ? 0 : Math.max(0, s.wait_after),
            }));
        try {
            const created = await create.mutateAsync({
                name: trimmedName,
                description: description.trim(),
                timezone,
                days,
                start_time: startTime,
                end_time: endTime,
                daily_limit: dailyLimit,
                stop_on_reply: stopOnReply,
                open_tracking: openTracking,
                link_tracking: linkTracking,
                unsubscribe_header: unsubHeader,
                email_tag_ids: emailTagIds,
                sequences: cleanSequences,
            });
            toast.success(launchImmediately ? "Campaign created — open to launch" : "Campaign created");
            onClose();
            if (created?.id) navigate(`/app/campaigns/${created.id}`);
        } catch (err) {
            toast.error(buildError(err as AppError));
        }
    }

    return (
        <AnimatePresence>
            {open && (
                <motion.div
                    key="overlay"
                    initial={{ opacity: 0 }}
                    animate={{ opacity: 1 }}
                    exit={{ opacity: 0 }}
                    transition={{ duration: 0.15 }}
                    onClick={onClose}
                    className="fixed inset-0 z-[110] flex items-center justify-center bg-slate-900/30 backdrop-blur-[2px] px-4"
                >
                    <motion.div
                        key="card"
                        initial={{ y: 8, opacity: 0 }}
                        animate={{ y: 0, opacity: 1 }}
                        exit={{ y: 8, opacity: 0 }}
                        transition={{ duration: 0.16 }}
                        onClick={(e) => e.stopPropagation()}
                        className="w-full max-w-[720px] rounded-lg bg-white border border-slate-200 shadow-[0_24px_48px_-12px_rgba(15,23,42,0.18),0_8px_16px_-8px_rgba(15,23,42,0.1)] overflow-hidden flex flex-col max-h-[88vh]"
                    >
                        <Header onClose={onClose} step={step} />

                        <StepRail step={step} setStep={(s) => setStep(s)} />

                        <div className="flex-1 min-h-0 overflow-y-auto px-5 py-5">
                            {step === 0 && (
                                <BasicsStep
                                    name={name}
                                    setName={setName}
                                    description={description}
                                    setDescription={setDescription}
                                />
                            )}
                            {step === 1 && (
                                <ScheduleStep
                                    timezone={timezone}
                                    setTimezone={setTimezone}
                                    days={days}
                                    setDays={setDays}
                                    startTime={startTime}
                                    setStartTime={setStartTime}
                                    endTime={endTime}
                                    setEndTime={setEndTime}
                                />
                            )}
                            {step === 2 && (
                                <SenderStep
                                    emailTagIds={emailTagIds}
                                    setEmailTagIds={setEmailTagIds}
                                    dailyLimit={dailyLimit}
                                    setDailyLimit={setDailyLimit}
                                    stopOnReply={stopOnReply}
                                    setStopOnReply={setStopOnReply}
                                    openTracking={openTracking}
                                    setOpenTracking={setOpenTracking}
                                    linkTracking={linkTracking}
                                    setLinkTracking={setLinkTracking}
                                    unsubHeader={unsubHeader}
                                    setUnsubHeader={setUnsubHeader}
                                />
                            )}
                            {step === 3 && (
                                <SequencesStep
                                    sequences={sequences}
                                    setSequences={setSequences}
                                />
                            )}
                        </div>

                        <Footer
                            step={step}
                            setStep={(s) => setStep(s)}
                            canAdvance={canAdvance}
                            submit={submit}
                            isPending={create.isPending}
                            launchImmediately={launchImmediately}
                            setLaunchImmediately={setLaunchImmediately}
                        />
                    </motion.div>
                </motion.div>
            )}
        </AnimatePresence>
    );
}

function Header({ onClose, step }: { onClose: () => void; step: Step }) {
    const stepLabel = STEPS[step]?.label || "Campaign";
    return (
        <div className="h-12 px-4 border-b border-slate-200 flex items-center gap-2.5 shrink-0">
            <div className="size-5 rounded bg-slate-100 text-slate-600 flex items-center justify-center">
                <MegaphoneIcon className="w-3 h-3" />
            </div>
            <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">
                New
            </span>
            <div className="h-4 w-px bg-slate-200" />
            <span className="text-[12.5px] text-slate-900 font-medium">{stepLabel}</span>
            <span className="text-[11px] text-slate-400 ml-1">
                step {step + 1} of {STEPS.length}
            </span>
            <button
                type="button"
                onClick={onClose}
                aria-label="Close"
                className="ml-auto size-7 rounded-md text-slate-500 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center justify-center transition-colors"
            >
                <XIcon className="w-3.5 h-3.5" />
            </button>
        </div>
    );
}

function StepRail({ step, setStep }: { step: Step; setStep: (s: Step) => void }) {
    return (
        <div className="px-4 h-9 border-b border-slate-100 flex items-center gap-1 shrink-0">
            {STEPS.map((s, i) => {
                const Icon = s.icon;
                const active = s.key === step;
                const done = s.key < step;
                return (
                    <button
                        key={s.key}
                        type="button"
                        onClick={() => setStep(s.key)}
                        className={`h-6 px-2 rounded-md text-[11.5px] font-medium inline-flex items-center gap-1.5 transition-colors ${
                            active
                                ? "bg-slate-900 text-white"
                                : done
                                  ? "text-slate-700 hover:bg-slate-100"
                                  : "text-slate-400 hover:bg-slate-50 hover:text-slate-600"
                        }`}
                    >
                        {done ? (
                            <CheckIcon className="w-3 h-3" />
                        ) : (
                            <Icon className="w-3 h-3" />
                        )}
                        <span>{s.label}</span>
                        {i < STEPS.length - 1 && (
                            <span className="ml-1 text-slate-300">›</span>
                        )}
                    </button>
                );
            })}
        </div>
    );
}

function Footer({
    step,
    setStep,
    canAdvance,
    submit,
    isPending,
    launchImmediately,
    setLaunchImmediately,
}: {
    step: Step;
    setStep: (s: Step) => void;
    canAdvance: boolean;
    submit: () => void;
    isPending: boolean;
    launchImmediately: boolean;
    setLaunchImmediately: (v: boolean) => void;
}) {
    const isLast = step === STEPS.length - 1;
    return (
        <div className="px-3 h-12 border-t border-slate-200 flex items-center gap-1.5 shrink-0">
            {step > 0 && (
                <button
                    type="button"
                    onClick={() => setStep((step - 1) as Step)}
                    className="h-7 px-2.5 rounded-md text-[12px] text-slate-700 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center gap-1 transition-colors"
                >
                    <ChevronLeftIcon className="w-3 h-3" />
                    Back
                </button>
            )}
            {isLast && (
                <label className="text-[11.5px] text-slate-500 inline-flex items-center gap-1.5 ml-1 select-none cursor-pointer">
                    <input
                        type="checkbox"
                        checked={launchImmediately}
                        onChange={(e) => setLaunchImmediately(e.target.checked)}
                        className="size-3 rounded border-slate-300 text-slate-900 focus:ring-slate-400"
                    />
                    Open & launch after create
                </label>
            )}
            <div className="ml-auto flex items-center gap-1.5">
                {!isLast ? (
                    <button
                        type="button"
                        onClick={() => setStep(Math.min(STEPS.length - 1, step + 1) as Step)}
                        disabled={!canAdvance}
                        className="h-7 px-2.5 rounded-md bg-slate-900 hover:bg-slate-800 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors disabled:opacity-50"
                    >
                        Continue
                        <ChevronRightIcon className="w-3 h-3" />
                    </button>
                ) : (
                    <button
                        type="button"
                        onClick={submit}
                        disabled={isPending || !canAdvance}
                        className="h-7 px-2.5 rounded-md bg-slate-900 hover:bg-slate-800 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors disabled:opacity-60"
                    >
                        {isPending ? (
                            <Loader2Icon className="w-3 h-3 animate-spin" />
                        ) : (
                            <RocketIcon className="w-3 h-3" />
                        )}
                        Create campaign
                    </button>
                )}
            </div>
        </div>
    );
}

function BasicsStep({
    name,
    setName,
    description,
    setDescription,
}: {
    name: string;
    setName: (v: string) => void;
    description: string;
    setDescription: (v: string) => void;
}) {
    return (
        <div className="space-y-4 max-w-[520px]">
            <div>
                <Label>Campaign name</Label>
                <TextInput
                    value={name}
                    onChange={setName}
                    placeholder="Q1 outbound"
                    autoFocus
                    className="w-full"
                />
                <p className="text-[11px] text-slate-400 mt-1">3–50 characters. Visible only to your team.</p>
            </div>
            <div>
                <Label>Description</Label>
                <TextInput
                    value={description}
                    onChange={setDescription}
                    placeholder="Optional — what this targets"
                    className="w-full"
                />
            </div>
            <div className="border border-slate-200 rounded-md p-3 bg-slate-50/60">
                <p className="text-[11.5px] text-slate-600 leading-relaxed">
                    The next steps configure how and when this campaign sends. You can change
                    anything later from the campaign detail page.
                </p>
            </div>
        </div>
    );
}

function ScheduleStep({
    timezone,
    setTimezone,
    days,
    setDays,
    startTime,
    setStartTime,
    endTime,
    setEndTime,
}: {
    timezone: string;
    setTimezone: (v: string) => void;
    days: number;
    setDays: (v: number) => void;
    startTime: string;
    setStartTime: (v: string) => void;
    endTime: string;
    setEndTime: (v: string) => void;
}) {
    const profile = useUserProfile();
    const timezoneOptions = React.useMemo<SelectOption[]>(
        () =>
            (profile?.timezones || []).map((tz) => ({
                value: tz.name,
                label: tz.display_name,
            })),
        [profile?.timezones],
    );
    return (
        <div className="space-y-5 max-w-[560px]">
            <div>
                <Label>Sending timezone</Label>
                <SelectMenu
                    value={timezone}
                    onChange={(v) => setTimezone(v)}
                    options={timezoneOptions}
                    className="w-full"
                    placeholder="Select a timezone"
                    aria-label="Sending timezone"
                />
                <p className="text-[11px] text-slate-400 mt-1">
                    Sends are scheduled in this zone. Worker IPs spread distribution naturally.
                </p>
            </div>

            <div>
                <Label>Active days</Label>
                <div className="grid grid-cols-7 gap-1.5 mt-1">
                    {WEEKDAYS.map((d, i) => {
                        const mask = 1 << i;
                        const active = (days & mask) !== 0;
                        return (
                            <button
                                key={d}
                                type="button"
                                onClick={() => setDays(days ^ mask)}
                                className={`h-7 rounded-md text-[11.5px] font-medium transition-colors ${
                                    active
                                        ? "bg-slate-900 text-white"
                                        : "bg-slate-50 text-slate-500 hover:bg-slate-100"
                                }`}
                            >
                                {d}
                            </button>
                        );
                    })}
                </div>
                <p className="text-[11px] text-slate-400 mt-1">
                    Default Mon–Fri. Mailbox safety requires at least one day.
                </p>
            </div>

            <div className="grid grid-cols-2 gap-3">
                <div>
                    <Label>Start time</Label>
                    <input
                        type="time"
                        value={startTime}
                        onChange={(e) => setStartTime(e.target.value)}
                        className="w-full h-7 px-2 rounded-md border border-slate-200 bg-white text-[12.5px] text-slate-900 outline-none focus:border-sky-400 focus:ring-2 focus:ring-sky-100"
                    />
                </div>
                <div>
                    <Label>End time</Label>
                    <input
                        type="time"
                        value={endTime}
                        onChange={(e) => setEndTime(e.target.value)}
                        className="w-full h-7 px-2 rounded-md border border-slate-200 bg-white text-[12.5px] text-slate-900 outline-none focus:border-sky-400 focus:ring-2 focus:ring-sky-100"
                    />
                </div>
            </div>
        </div>
    );
}

function SenderStep({
    emailTagIds,
    setEmailTagIds,
    dailyLimit,
    setDailyLimit,
    stopOnReply,
    setStopOnReply,
    openTracking,
    setOpenTracking,
    linkTracking,
    setLinkTracking,
    unsubHeader,
    setUnsubHeader,
}: {
    emailTagIds: string[];
    setEmailTagIds: React.Dispatch<React.SetStateAction<string[]>>;
    dailyLimit: number;
    setDailyLimit: (v: number) => void;
    stopOnReply: boolean;
    setStopOnReply: (v: boolean) => void;
    openTracking: boolean;
    setOpenTracking: (v: boolean) => void;
    linkTracking: boolean;
    setLinkTracking: (v: boolean) => void;
    unsubHeader: boolean;
    setUnsubHeader: (v: boolean) => void;
}) {
    return (
        <div className="space-y-5 max-w-[560px]">
            <div>
                <Label>Sender pool</Label>
                <TagSelector
                    selected={emailTagIds}
                    onAdd={(t) => setEmailTagIds((bef) => [...bef, t])}
                    onRemove={(t) => setEmailTagIds((bef) => bef.filter((id) => id !== t))}
                />
                <p className="text-[11px] text-slate-400 mt-1">
                    Pick the email-account tags this campaign should rotate through. Volume
                    is split across every connected mailbox to protect deliverability.
                </p>
            </div>

            <div>
                <Label>Daily limit per mailbox</Label>
                <input
                    type="number"
                    min={3}
                    max={100}
                    value={dailyLimit}
                    onChange={(e) => setDailyLimit(Number(e.target.value))}
                    className="w-32 h-7 px-2 rounded-md border border-slate-200 bg-white text-[12.5px] text-slate-900 outline-none focus:border-sky-400 focus:ring-2 focus:ring-sky-100"
                />
                <p className="text-[11px] text-slate-400 mt-1">
                    3–100. Default 50. Stay conservative until reputation is proven.
                </p>
            </div>

            <div className="border border-slate-200 rounded-md divide-y divide-slate-100">
                <ToggleRow
                    label="Stop on reply"
                    description="Pause follow-ups for a contact once they respond."
                    value={stopOnReply}
                    onChange={setStopOnReply}
                />
                <ToggleRow
                    label="Track opens"
                    description="Insert a transparent pixel to measure inbox impressions."
                    value={openTracking}
                    onChange={setOpenTracking}
                />
                <ToggleRow
                    label="Track clicks"
                    description="Wrap links so click activity appears in your live feed."
                    value={linkTracking}
                    onChange={setLinkTracking}
                />
                <ToggleRow
                    label="Unsubscribe header"
                    description="Add List-Unsubscribe — required by most providers for bulk mail."
                    value={unsubHeader}
                    onChange={setUnsubHeader}
                />
            </div>
        </div>
    );
}

function ToggleRow({
    label,
    description,
    value,
    onChange,
}: {
    label: string;
    description: string;
    value: boolean;
    onChange: (v: boolean) => void;
}) {
    return (
        <button
            type="button"
            onClick={() => onChange(!value)}
            className="w-full px-3 py-2.5 text-left flex items-start gap-3 hover:bg-slate-50 transition-colors"
        >
            <div
                className={`mt-0.5 size-7 rounded-full p-0.5 transition-colors shrink-0 ${
                    value ? "bg-slate-900" : "bg-slate-200"
                }`}
            >
                <div
                    className={`size-6 rounded-full bg-white shadow-sm transition-transform ${
                        value ? "translate-x-3" : ""
                    }`}
                />
            </div>
            <div className="flex-1 min-w-0">
                <p className="text-[12.5px] text-slate-900 font-medium">{label}</p>
                <p className="text-[11px] text-slate-500 mt-0.5 leading-relaxed">
                    {description}
                </p>
            </div>
        </button>
    );
}

function SequencesStep({
    sequences,
    setSequences,
}: {
    sequences: SequenceDraft[];
    setSequences: React.Dispatch<React.SetStateAction<SequenceDraft[]>>;
}) {
    const update = (i: number, patch: Partial<SequenceDraft>) =>
        setSequences((bef) => bef.map((s, idx) => (idx === i ? { ...s, ...patch } : s)));

    return (
        <div className="space-y-4 max-w-[640px]">
            {sequences.map((seq, i) => (
                <div
                    key={i}
                    className="border border-slate-200 rounded-md p-3 space-y-2.5"
                >
                    <div className="flex items-center gap-2">
                        <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">
                            Step {i + 1}
                        </span>
                        {i > 0 && (
                            <>
                                <div className="h-3 w-px bg-slate-200" />
                                <span className="text-[11px] text-slate-500">Wait</span>
                                <input
                                    type="number"
                                    min={0}
                                    max={60}
                                    value={seq.wait_after}
                                    onChange={(e) =>
                                        update(i, { wait_after: Number(e.target.value) })
                                    }
                                    className="w-16 h-6 px-1.5 rounded border border-slate-200 bg-white text-[11.5px] text-slate-900 outline-none focus:border-sky-400 focus:ring-2 focus:ring-sky-100"
                                />
                                <span className="text-[11px] text-slate-500">days after previous</span>
                            </>
                        )}
                        {sequences.length > 1 && (
                            <button
                                type="button"
                                onClick={() =>
                                    setSequences((bef) => bef.filter((_, idx) => idx !== i))
                                }
                                aria-label="Remove step"
                                className="ml-auto size-6 rounded text-slate-400 hover:text-red-600 hover:bg-red-50 inline-flex items-center justify-center transition-colors"
                            >
                                <Trash2Icon className="w-3 h-3" />
                            </button>
                        )}
                    </div>
                    <div>
                        <TextInput
                            value={seq.subject}
                            onChange={(v) => update(i, { subject: v })}
                            placeholder={
                                i === 0
                                    ? "Subject line — e.g. quick idea for {{company}}"
                                    : "Follow-up subject (leave blank to reuse the thread)"
                            }
                            className="w-full"
                        />
                    </div>
                    <div>
                        <textarea
                            value={seq.body_plain}
                            onChange={(e) => update(i, { body_plain: e.target.value })}
                            placeholder={
                                i === 0
                                    ? "Hi {{.FirstName}},\n\nNoticed {{.Company}} is …"
                                    : "Just bumping this up in case it slipped past."
                            }
                            rows={6}
                            className="w-full px-2.5 py-1.5 rounded-md border border-slate-200 bg-white text-[12.5px] text-slate-900 placeholder:text-slate-400 outline-none focus:border-sky-400 focus:ring-2 focus:ring-sky-100 resize-y leading-relaxed"
                        />
                        <p className="text-[10.5px] text-slate-400 mt-1">
                            Use <code className="font-mono">{`{{.FirstName}}`}</code>,{" "}
                            <code className="font-mono">{`{{.Company}}`}</code>, custom fields like{" "}
                            <code className="font-mono">{`{{.role}}`}</code>, and conditionals like{" "}
                            <code className="font-mono">{`{{if .Company}}…{{end}}`}</code>. HTML is generated automatically.
                        </p>
                    </div>
                </div>
            ))}
            <button
                type="button"
                onClick={() =>
                    setSequences((bef) => [
                        ...bef,
                        {
                            name: `Step ${bef.length + 1}`,
                            subject: "",
                            body_plain: "",
                            wait_after: 3,
                        },
                    ])
                }
                className="w-full h-8 rounded-md border border-dashed border-slate-200 text-[12px] text-slate-500 hover:text-slate-900 hover:border-slate-300 hover:bg-slate-50 inline-flex items-center justify-center gap-1.5 transition-colors"
            >
                <PlusIcon className="w-3 h-3" />
                Add follow-up step
            </button>
        </div>
    );
}

function escapeHtml(s: string): string {
    return s
        .replace(/&/g, "&amp;")
        .replace(/</g, "&lt;")
        .replace(/>/g, "&gt;")
        .replace(/"/g, "&quot;")
        .replace(/'/g, "&#39;");
}
