// Multi-step new-campaign wizard.
//
// Four steps (basics, schedule, sending, first email) with directional slide
// transitions and a numbered stepper, ending in a single atomic create call.
// Only the name and the first email are enforced; every other step ships a
// safe default and can be changed later on the campaign detail page.

import React from "react";
import { AnimatePresence, motion } from "framer-motion";
import {
    AlertCircleIcon,
    CalendarClockIcon,
    CheckIcon,
    ChevronLeftIcon,
    ChevronRightIcon,
    Loader2Icon,
    MailIcon,
    MegaphoneIcon,
    PencilLineIcon,
    PlusIcon,
    SendIcon,
    Trash2Icon,
    XIcon,
} from "lucide-react";
import toast from "react-hot-toast";
import { useNavigate } from "react-router-dom";
import useCreateCampaign from "@/lib/api/hooks/app/campaigns/useCreateCampaign";
import { Label, NumberInput, TextInput } from "@/components/ui/field";
import { SelectMenu, type SelectOption } from "@/components/ui/select-menu";
import { TimePicker } from "@/components/ui/TimePicker";
import WeekdayBitmask from "@/components/app/campaigns/schedule/WeekdayBitmask";
import TagSelector from "@/components/app/popup/select/TagSelector";
import { Toggle } from "@/components/app/campaigns/preferences/components/CampaignPreferenceBoolBox";
import { useUserProfile } from "@/hooks/context/user";
import { useConfirm } from "@/hooks/context/confirm";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import { cn } from "@/lib/utils";

interface Props {
    open: boolean;
    onClose: () => void;
}

type SequenceDraft = {
    id: string;
    subject: string;
    body_plain: string;
    wait_after: number;
};

const WEEKDAYS = ["Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday", "Sunday"];
const WEEKDAYS_MASK = 0b0011111;
const EVERY_DAY_MASK = 0b1111111;

const NAME_MIN = 3;
const NAME_MAX = 50;

const STEPS = [
    { label: "Basics", icon: MegaphoneIcon },
    { label: "Schedule", icon: CalendarClockIcon },
    { label: "Sending", icon: SendIcon },
    { label: "First email", icon: MailIcon },
] as const;
type Step = 0 | 1 | 2 | 3;
const LAST_STEP = (STEPS.length - 1) as Step;

let seqCounter = 0;
const newSequence = (wait: number): SequenceDraft => ({
    id: `seq-${++seqCounter}`,
    subject: "",
    body_plain: "",
    wait_after: wait,
});

type Draft = {
    name: string;
    description: string;
    timezone: string;
    days: number;
    startTime: string;
    endTime: string;
    emailTagIds: string[];
    dailyLimit: number;
    stopOnReply: boolean;
    openTracking: boolean;
    linkTracking: boolean;
    unsubHeader: boolean;
    sequences: SequenceDraft[];
};

const initialDraft = (timezone: string): Draft => ({
    name: "",
    description: "",
    timezone,
    days: WEEKDAYS_MASK,
    startTime: "08:00",
    endTime: "18:00",
    emailTagIds: [],
    dailyLimit: 50,
    stopOnReply: true,
    openTracking: true,
    linkTracking: true,
    unsubHeader: true,
    sequences: [newSequence(0)],
});

// One human-readable reason a step cannot be left yet, or null when it can.
function stepIssue(step: Step, d: Draft): string | null {
    if (step === 0) {
        const n = d.name.trim().length;
        if (n < NAME_MIN) return `Name needs at least ${NAME_MIN} characters`;
        if (n > NAME_MAX) return `Name is ${NAME_MAX} characters max`;
        return null;
    }
    if (step === 1) {
        if (d.days === 0) return "Pick at least one sending day";
        if (d.startTime && d.endTime && d.startTime >= d.endTime) return "End time must be after the start time";
        return null;
    }
    if (step === 3) {
        // The first email may be skipped entirely (written later on the Steps
        // tab), but a half-written one must be finished.
        const first = d.sequences[0];
        if (!first) return null;
        const hasSubject = first.subject.trim().length > 0;
        const hasBody = first.body_plain.trim().length > 0;
        if (hasBody && !hasSubject) return "Give the first email a subject line";
        if (hasSubject && !hasBody) return "Write the first email's body";
        return null;
    }
    return null;
}

function daysLabel(mask: number): string {
    if (mask === EVERY_DAY_MASK) return "Every day";
    if (mask === WEEKDAYS_MASK) return "Weekdays";
    const on = WEEKDAYS.filter((_, i) => (mask & (1 << i)) !== 0).map((d) => d.slice(0, 3));
    return on.length === 0 ? "No days" : on.join(", ");
}

// "14:30" -> "2:30 PM"
function fmt12(hhmm: string): string {
    const [h, m] = hhmm.split(":").map(Number);
    if (Number.isNaN(h) || Number.isNaN(m)) return hhmm;
    const h12 = h % 12 === 0 ? 12 : h % 12;
    return `${h12}:${String(m).padStart(2, "0")} ${h < 12 ? "AM" : "PM"}`;
}

export function NewCampaignDialog({ open, onClose }: Props) {
    const navigate = useNavigate();
    const profile = useUserProfile();
    const confirm = useConfirm();
    const create = useCreateCampaign();
    const defaultTimezone = profile?.timezones?.[0]?.name || "Europe/London";

    const [step, setStep] = React.useState<Step>(0);
    const [direction, setDirection] = React.useState<1 | -1>(1);
    const [draft, setDraft] = React.useState<Draft>(() => initialDraft(defaultTimezone));
    // Set when the user tries to leave a step that is not ready; shows the reason.
    const [nudged, setNudged] = React.useState(false);

    const patch = React.useCallback((p: Partial<Draft>) => setDraft((d) => ({ ...d, ...p })), []);

    React.useEffect(() => {
        if (!open) {
            setStep(0);
            setDirection(1);
            setNudged(false);
            setDraft(initialDraft(defaultTimezone));
        }
    }, [open, defaultTimezone]);

    const issue = stepIssue(step, draft);
    React.useEffect(() => {
        if (!issue) setNudged(false);
    }, [issue]);

    // A step is reachable when every step before it is complete.
    const canReach = React.useCallback(
        (target: Step) => {
            for (let i = 0; i < target; i++) if (stepIssue(i as Step, draft)) return false;
            return true;
        },
        [draft],
    );

    const goTo = React.useCallback(
        (target: Step) => {
            if (target === step) return;
            if (target > step && !canReach(target)) {
                setNudged(true);
                return;
            }
            setDirection(target > step ? 1 : -1);
            setNudged(false);
            setStep(target);
        },
        [step, canReach],
    );

    const next = React.useCallback(() => {
        if (issue) {
            setNudged(true);
            return;
        }
        if (step < LAST_STEP) goTo((step + 1) as Step);
    }, [issue, step, goTo]);

    const dirty =
        draft.name.trim() !== "" ||
        draft.description.trim() !== "" ||
        draft.emailTagIds.length > 0 ||
        draft.sequences.some((s) => s.subject.trim() !== "" || s.body_plain.trim() !== "");

    const requestClose = React.useCallback(() => {
        if (create.isPending) return;
        if (dirty) {
            confirm.show("Discard this campaign draft?", async () => onClose());
            return;
        }
        onClose();
    }, [create.isPending, dirty, confirm, onClose]);

    React.useEffect(() => {
        if (!open) return;
        const onKey = (e: KeyboardEvent) => {
            if (e.key !== "Escape") return;
            // An open dropdown (timezone, tags) or the discard confirm owns this Escape.
            if (document.querySelector("[data-floating], [role='alertdialog']")) return;
            e.preventDefault();
            requestClose();
        };
        document.addEventListener("keydown", onKey);
        return () => document.removeEventListener("keydown", onKey);
    }, [open, requestClose]);

    async function submit() {
        if (create.isPending) return;
        for (const s of [0, 1, 2, 3] as Step[]) {
            if (stepIssue(s, draft)) {
                setDirection(s > step ? 1 : -1);
                setStep(s);
                setNudged(true);
                return;
            }
        }
        const steps = draft.sequences
            .filter((s) => s.subject.trim().length > 0 || s.body_plain.trim().length > 0)
            .map((s, i) => ({
                name: `Step ${i + 1}`,
                subject: s.subject.trim(),
                body_plain: s.body_plain,
                body_html: `<div>${escapeHtml(s.body_plain).replace(/\n/g, "<br/>")}</div>`,
                wait_after: i === 0 ? 0 : Math.max(0, s.wait_after),
            }));
        try {
            const created = await create.mutateAsync({
                name: draft.name.trim(),
                description: draft.description.trim(),
                timezone: draft.timezone,
                days: draft.days,
                start_time: draft.startTime,
                end_time: draft.endTime,
                daily_limit: draft.dailyLimit,
                stop_on_reply: draft.stopOnReply,
                open_tracking: draft.openTracking,
                link_tracking: draft.linkTracking,
                unsubscribe_header: draft.unsubHeader,
                email_tag_ids: draft.emailTagIds,
                steps,
            });
            toast.success("Campaign created. Add contacts, then launch it.");
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
                    onMouseDown={requestClose}
                    className="fixed inset-0 z-[110] flex items-center justify-center bg-slate-900/30 backdrop-blur-[2px] px-4"
                >
                    <motion.div
                        key="card"
                        role="dialog"
                        aria-modal="true"
                        aria-label="New campaign"
                        initial={{ y: 8, opacity: 0, scale: 0.985 }}
                        animate={{ y: 0, opacity: 1, scale: 1 }}
                        exit={{ y: 8, opacity: 0, scale: 0.985 }}
                        transition={{ duration: 0.18, ease: [0.22, 1, 0.36, 1] }}
                        onMouseDown={(e) => e.stopPropagation()}
                        className="w-full max-w-[720px] rounded-lg bg-white border border-slate-200 shadow-[0_24px_48px_-12px_rgba(15,23,42,0.18),0_8px_16px_-8px_rgba(15,23,42,0.1)] overflow-hidden flex flex-col max-h-[88dvh]"
                    >
                        <Header onClose={requestClose} />
                        <Stepper step={step} canReach={canReach} goTo={goTo} />

                        <div className="flex-1 min-h-0 overflow-y-auto overflow-x-hidden">
                            <AnimatePresence mode="wait" initial={false} custom={direction}>
                                <motion.div
                                    key={step}
                                    custom={direction}
                                    variants={paneVariants}
                                    initial="enter"
                                    animate="center"
                                    exit="exit"
                                    transition={{ duration: 0.18, ease: [0.22, 1, 0.36, 1] }}
                                    className="px-5 py-5 min-h-[340px]"
                                >
                                    {step === 0 && <BasicsStep draft={draft} patch={patch} onEnter={next} />}
                                    {step === 1 && <ScheduleStep draft={draft} patch={patch} />}
                                    {step === 2 && <SendingStep draft={draft} patch={patch} />}
                                    {step === 3 && <EmailsStep draft={draft} patch={patch} goTo={goTo} />}
                                </motion.div>
                            </AnimatePresence>
                        </div>

                        <Footer
                            step={step}
                            issue={nudged ? issue : null}
                            onBack={() => goTo((step - 1) as Step)}
                            onNext={next}
                            onSubmit={submit}
                            isPending={create.isPending}
                        />
                    </motion.div>
                </motion.div>
            )}
        </AnimatePresence>
    );
}

const paneVariants = {
    enter: (dir: 1 | -1) => ({ x: dir * 28, opacity: 0 }),
    center: { x: 0, opacity: 1 },
    exit: (dir: 1 | -1) => ({ x: dir * -28, opacity: 0 }),
};

function Header({ onClose }: { onClose: () => void }) {
    return (
        <div className="h-12 px-4 border-b border-slate-200 flex items-center gap-2.5 shrink-0">
            <div className="size-5 rounded bg-slate-100 text-slate-600 flex items-center justify-center">
                <MegaphoneIcon className="w-3 h-3" />
            </div>
            <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">New</span>
            <div className="h-4 w-px bg-slate-200" />
            <span className="text-[12.5px] text-slate-900 font-medium">Campaign</span>
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

function Stepper({
    step,
    canReach,
    goTo,
}: {
    step: Step;
    canReach: (s: Step) => boolean;
    goTo: (s: Step) => void;
}) {
    return (
        <div className="px-4 sm:px-5 h-11 border-b border-slate-100 flex items-center shrink-0 bg-slate-50/40">
            {STEPS.map((s, i) => {
                const idx = i as Step;
                const active = idx === step;
                const done = idx < step;
                const reachable = idx <= step || canReach(idx);
                return (
                    <React.Fragment key={s.label}>
                        <button
                            type="button"
                            onClick={() => goTo(idx)}
                            disabled={!reachable}
                            aria-current={active ? "step" : undefined}
                            className={cn(
                                "group inline-flex items-center gap-2 h-7 pl-1 pr-2 rounded-md shrink-0 transition-colors outline-none focus-visible:ring-2 focus-visible:ring-sky-100",
                                reachable && !active ? "hover:bg-slate-100" : "",
                                !reachable ? "cursor-default" : "",
                            )}
                        >
                            <span
                                className={cn(
                                    "relative size-5 rounded-full inline-flex items-center justify-center text-[10.5px] font-semibold tabular-nums transition-colors",
                                    done
                                        ? "bg-sky-600 text-white"
                                        : active
                                          ? "bg-white text-sky-700 ring-1 ring-inset ring-sky-600"
                                          : "bg-white text-slate-400 ring-1 ring-inset ring-slate-200",
                                )}
                            >
                                <AnimatePresence mode="wait" initial={false}>
                                    {done ? (
                                        <motion.span
                                            key="check"
                                            initial={{ scale: 0.4, opacity: 0 }}
                                            animate={{ scale: 1, opacity: 1 }}
                                            exit={{ scale: 0.4, opacity: 0 }}
                                            transition={{ duration: 0.16 }}
                                            className="inline-flex"
                                        >
                                            <CheckIcon className="w-3 h-3" strokeWidth={3} />
                                        </motion.span>
                                    ) : (
                                        <motion.span
                                            key="num"
                                            initial={{ scale: 0.4, opacity: 0 }}
                                            animate={{ scale: 1, opacity: 1 }}
                                            exit={{ scale: 0.4, opacity: 0 }}
                                            transition={{ duration: 0.16 }}
                                        >
                                            {i + 1}
                                        </motion.span>
                                    )}
                                </AnimatePresence>
                            </span>
                            <span
                                className={cn(
                                    "text-[11.5px] font-medium whitespace-nowrap",
                                    active ? "text-slate-900" : done ? "text-slate-600" : "text-slate-400",
                                    active ? "inline" : "hidden sm:inline",
                                )}
                            >
                                {s.label}
                            </span>
                        </button>
                        {i < STEPS.length - 1 && (
                            <span className="relative flex-1 h-px mx-1 sm:mx-2 bg-slate-200 min-w-3 overflow-hidden">
                                <motion.span
                                    initial={false}
                                    animate={{ scaleX: done ? 1 : 0 }}
                                    transition={{ duration: 0.25, ease: [0.22, 1, 0.36, 1] }}
                                    style={{ originX: 0 }}
                                    className="absolute inset-0 bg-sky-600"
                                />
                            </span>
                        )}
                    </React.Fragment>
                );
            })}
        </div>
    );
}

function Footer({
    step,
    issue,
    onBack,
    onNext,
    onSubmit,
    isPending,
}: {
    step: Step;
    issue: string | null;
    onBack: () => void;
    onNext: () => void;
    onSubmit: () => void;
    isPending: boolean;
}) {
    const isLast = step === LAST_STEP;
    return (
        <div className="px-3 min-h-12 py-1.5 sm:py-0 sm:h-12 border-t border-slate-200 flex items-center gap-1.5 shrink-0 bg-slate-50/30">
            {step > 0 ? (
                <button
                    type="button"
                    onClick={onBack}
                    disabled={isPending}
                    className="h-7 px-2.5 rounded-md text-[12px] text-slate-700 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center gap-1 transition-colors disabled:opacity-50"
                >
                    <ChevronLeftIcon className="w-3 h-3" />
                    Back
                </button>
            ) : (
                <span className="text-[11px] text-slate-400 pl-1 hidden sm:inline">
                    Everything here can be changed later.
                </span>
            )}

            <div className="ml-auto flex items-center gap-2.5 min-w-0">
                <AnimatePresence initial={false}>
                    {issue && (
                        <motion.span
                            key={issue}
                            initial={{ opacity: 0, x: 6 }}
                            animate={{ opacity: 1, x: 0 }}
                            exit={{ opacity: 0, x: 6 }}
                            transition={{ duration: 0.14 }}
                            role="status"
                            className="text-[11.5px] text-amber-700 inline-flex items-center gap-1 min-w-0"
                        >
                            <AlertCircleIcon className="w-3 h-3 shrink-0" />
                            <span className="truncate">{issue}</span>
                        </motion.span>
                    )}
                </AnimatePresence>
                {!isLast ? (
                    <button
                        type="button"
                        onClick={onNext}
                        className="h-7 px-2.5 rounded-md bg-slate-900 hover:bg-slate-800 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors shrink-0"
                    >
                        Continue
                        <ChevronRightIcon className="w-3 h-3" />
                    </button>
                ) : (
                    <button
                        type="button"
                        onClick={onSubmit}
                        disabled={isPending}
                        className="h-7 px-2.5 rounded-md bg-sky-600 hover:bg-sky-700 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors disabled:opacity-60 shrink-0"
                    >
                        {isPending ? <Loader2Icon className="w-3 h-3 animate-spin" /> : <PlusIcon className="w-3 h-3" />}
                        Create campaign
                    </button>
                )}
            </div>
        </div>
    );
}

function StepIntro({ title, hint }: { title: string; hint: string }) {
    return (
        <div className="mb-4">
            <p className="text-[13.5px] text-slate-900 font-semibold">{title}</p>
            <p className="text-[11.5px] text-slate-500 mt-0.5 leading-relaxed">{hint}</p>
        </div>
    );
}

function BasicsStep({
    draft,
    patch,
    onEnter,
}: {
    draft: Draft;
    patch: (p: Partial<Draft>) => void;
    onEnter: () => void;
}) {
    const len = draft.name.trim().length;
    return (
        <div className="max-w-[520px]">
            <StepIntro title="What is this campaign?" hint="A name your team will recognise in the list. Only the name is required." />
            <div className="space-y-4">
                <div>
                    <div className="flex items-baseline justify-between">
                        <Label>Campaign name</Label>
                        <span
                            className={cn(
                                "text-[10.5px] tabular-nums",
                                len > NAME_MAX ? "text-rose-600" : "text-slate-400",
                            )}
                        >
                            {len}/{NAME_MAX}
                        </span>
                    </div>
                    <TextInput
                        value={draft.name}
                        onChange={(v) => patch({ name: v })}
                        onKeyDown={(e) => {
                            if (e.key === "Enter") {
                                e.preventDefault();
                                onEnter();
                            }
                        }}
                        placeholder="Q3 outbound, SaaS founders"
                        autoFocus
                        className="w-full"
                    />
                </div>
                <div>
                    <Label>Description</Label>
                    <TextInput
                        value={draft.description}
                        onChange={(v) => patch({ description: v })}
                        placeholder="Optional. Who this targets and why."
                        className="w-full"
                    />
                </div>
            </div>
        </div>
    );
}

function ScheduleStep({ draft, patch }: { draft: Draft; patch: (p: Partial<Draft>) => void }) {
    const profile = useUserProfile();
    const timezoneOptions = React.useMemo<SelectOption[]>(
        () => (profile?.timezones || []).map((tz) => ({ value: tz.name, label: tz.display_name })),
        [profile?.timezones],
    );
    const windowInvalid = draft.startTime >= draft.endTime;
    return (
        <div className="max-w-[560px]">
            <StepIntro
                title="When should it send?"
                hint="Sends stay inside this window in the campaign timezone and spread out across it."
            />
            <div className="space-y-5">
                <div>
                    <Label>Timezone</Label>
                    <SelectMenu
                        value={draft.timezone}
                        onChange={(v) => patch({ timezone: v })}
                        options={timezoneOptions}
                        fullWidth
                        placeholder="Select a timezone"
                        aria-label="Sending timezone"
                    />
                </div>

                <div>
                    <div className="flex items-baseline justify-between">
                        <Label>Sending days</Label>
                        <div className="flex items-center gap-1 text-[10.5px]">
                            <button
                                type="button"
                                onClick={() => patch({ days: WEEKDAYS_MASK })}
                                className={cn(
                                    "px-1.5 h-5 rounded transition-colors",
                                    draft.days === WEEKDAYS_MASK
                                        ? "bg-sky-50 text-sky-700"
                                        : "text-slate-400 hover:text-slate-700 hover:bg-slate-100",
                                )}
                            >
                                Weekdays
                            </button>
                            <button
                                type="button"
                                onClick={() => patch({ days: EVERY_DAY_MASK })}
                                className={cn(
                                    "px-1.5 h-5 rounded transition-colors",
                                    draft.days === EVERY_DAY_MASK
                                        ? "bg-sky-50 text-sky-700"
                                        : "text-slate-400 hover:text-slate-700 hover:bg-slate-100",
                                )}
                            >
                                Every day
                            </button>
                        </div>
                    </div>
                    <div className="mt-1">
                        <WeekdayBitmask weekdays={WEEKDAYS} value={draft.days} setValue={(v) => patch({ days: v })} />
                    </div>
                </div>

                <div className="grid grid-cols-2 gap-3">
                    <div>
                        <Label>From</Label>
                        <TimePicker
                            value={draft.startTime}
                            onChange={(v) => patch({ startTime: v })}
                            stepMinutes={30}
                            fullWidth
                            placeholder="Start"
                        />
                    </div>
                    <div>
                        <Label>Until</Label>
                        <TimePicker
                            value={draft.endTime}
                            onChange={(v) => patch({ endTime: v })}
                            stepMinutes={30}
                            fullWidth
                            placeholder="End"
                        />
                    </div>
                </div>

                <p className={cn("text-[11.5px] leading-relaxed", windowInvalid ? "text-amber-700" : "text-slate-500")}>
                    {windowInvalid
                        ? "The window ends before it starts. Pick an end time after the start time."
                        : `${daysLabel(draft.days)}, ${fmt12(draft.startTime)} to ${fmt12(draft.endTime)}.`}
                </p>
            </div>
        </div>
    );
}

function SendingStep({ draft, patch }: { draft: Draft; patch: (p: Partial<Draft>) => void }) {
    return (
        <div className="max-w-[560px]">
            <StepIntro
                title="Who sends it, and how?"
                hint="Volume is split across every mailbox in the pool so no single sender carries the campaign."
            />
            <div className="space-y-5">
                <div>
                    <Label>Sender pool</Label>
                    <TagSelector
                        selected={draft.emailTagIds}
                        onAdd={(t) => patch({ emailTagIds: [...draft.emailTagIds, t] })}
                        onRemove={(t) => patch({ emailTagIds: draft.emailTagIds.filter((id) => id !== t) })}
                    />
                    <p className="text-[11px] text-slate-400 mt-1">
                        Mailbox tags this campaign rotates through. Leave empty to use every active mailbox.
                    </p>
                </div>

                <div className="flex items-start justify-between gap-5">
                    <div className="min-w-0">
                        <p className="text-[12.5px] text-slate-900 font-medium">Daily limit per mailbox</p>
                        <p className="text-[11px] text-slate-500 mt-0.5 leading-relaxed">
                            3 to 100. Stay near 50 until the mailboxes have proven their reputation.
                        </p>
                    </div>
                    <NumberInput
                        value={draft.dailyLimit}
                        min={3}
                        max={100}
                        onChange={(v) => patch({ dailyLimit: v })}
                        className="w-24 shrink-0"
                    />
                </div>

                <div className="border border-slate-200 rounded-md divide-y divide-slate-100 overflow-hidden">
                    <SwitchRow
                        label="Stop on reply"
                        description="Pause follow-ups for a contact once they respond."
                        value={draft.stopOnReply}
                        onChange={(v) => patch({ stopOnReply: v })}
                    />
                    <SwitchRow
                        label="Track opens"
                        description="Insert a transparent pixel to measure inbox impressions."
                        value={draft.openTracking}
                        onChange={(v) => patch({ openTracking: v })}
                    />
                    <SwitchRow
                        label="Track clicks"
                        description="Wrap links so click activity appears in your live feed."
                        value={draft.linkTracking}
                        onChange={(v) => patch({ linkTracking: v })}
                    />
                    <SwitchRow
                        label="Unsubscribe header"
                        description="Add List-Unsubscribe, which most providers require for bulk mail."
                        value={draft.unsubHeader}
                        onChange={(v) => patch({ unsubHeader: v })}
                    />
                </div>
            </div>
        </div>
    );
}

function SwitchRow({
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
        // The row is the click target; the switch stops propagation so a click
        // on it does not toggle twice.
        <div
            onClick={() => onChange(!value)}
            className="w-full px-3 py-2.5 flex items-start justify-between gap-4 cursor-pointer select-none hover:bg-slate-50 transition-colors"
        >
            <div className="min-w-0">
                <p className="text-[12.5px] text-slate-900 font-medium">{label}</p>
                <p className="text-[11px] text-slate-500 mt-0.5 leading-relaxed">{description}</p>
            </div>
            <span onClick={(e) => e.stopPropagation()} className="shrink-0 mt-0.5 inline-flex">
                <Toggle value={value} onChange={onChange} />
            </span>
        </div>
    );
}

function EmailsStep({
    draft,
    patch,
    goTo,
}: {
    draft: Draft;
    patch: (p: Partial<Draft>) => void;
    goTo: (s: Step) => void;
}) {
    const update = (i: number, p: Partial<SequenceDraft>) =>
        patch({ sequences: draft.sequences.map((s, idx) => (idx === i ? { ...s, ...p } : s)) });

    return (
        <div className="max-w-[640px]">
            <StepIntro
                title="What does the first email say?"
                hint="Optional. Skip this to write it in the full editor on the Steps tab afterwards."
            />

            <div className="mb-4 flex flex-wrap items-center gap-x-1.5 gap-y-1 text-[11px] text-slate-500">
                <button type="button" onClick={() => goTo(1)} className="hover:text-slate-900 hover:underline underline-offset-2">
                    {daysLabel(draft.days)}, {fmt12(draft.startTime)} to {fmt12(draft.endTime)}
                </button>
                <span className="text-slate-300">·</span>
                <button type="button" onClick={() => goTo(2)} className="hover:text-slate-900 hover:underline underline-offset-2">
                    {draft.dailyLimit}/day per mailbox
                </button>
                <span className="text-slate-300">·</span>
                <button type="button" onClick={() => goTo(2)} className="hover:text-slate-900 hover:underline underline-offset-2">
                    {draft.emailTagIds.length === 0
                        ? "all mailboxes"
                        : `${draft.emailTagIds.length} tag${draft.emailTagIds.length === 1 ? "" : "s"}`}
                </button>
                <PencilLineIcon className="w-3 h-3 text-slate-300 ml-0.5" />
            </div>

            <div className="space-y-3">
                <AnimatePresence initial={false}>
                    {draft.sequences.map((seq, i) => (
                        <motion.div
                            key={seq.id}
                            layout
                            initial={{ opacity: 0, y: 6 }}
                            animate={{ opacity: 1, y: 0 }}
                            exit={{ opacity: 0, y: -6 }}
                            transition={{ duration: 0.16 }}
                            className="border border-slate-200 rounded-md overflow-hidden"
                        >
                            <div className="h-9 px-3 flex items-center gap-2 bg-slate-50/60 border-b border-slate-100">
                                <span className="size-5 rounded-full bg-white ring-1 ring-inset ring-slate-200 text-[10.5px] font-semibold text-slate-600 inline-flex items-center justify-center tabular-nums">
                                    {i + 1}
                                </span>
                                <span className="text-[12px] text-slate-900 font-medium">
                                    {i === 0 ? "First email" : `Follow-up ${i}`}
                                </span>
                                {i > 0 && (
                                    <div className="flex items-center gap-1.5 ml-1">
                                        <span className="text-[11px] text-slate-500">after</span>
                                        <NumberInput
                                            value={seq.wait_after}
                                            min={0}
                                            max={60}
                                            onChange={(v) => update(i, { wait_after: v })}
                                            className="w-20"
                                        />
                                        <span className="text-[11px] text-slate-500">
                                            day{seq.wait_after === 1 ? "" : "s"}
                                        </span>
                                    </div>
                                )}
                                {i > 0 && (
                                    <button
                                        type="button"
                                        onClick={() =>
                                            patch({ sequences: draft.sequences.filter((_, idx) => idx !== i) })
                                        }
                                        aria-label="Remove follow-up"
                                        className="ml-auto size-6 rounded text-slate-400 hover:text-rose-600 hover:bg-rose-50 inline-flex items-center justify-center transition-colors"
                                    >
                                        <Trash2Icon className="w-3 h-3" />
                                    </button>
                                )}
                            </div>
                            <div className="p-3 space-y-2">
                                <TextInput
                                    value={seq.subject}
                                    onChange={(v) => update(i, { subject: v })}
                                    placeholder={
                                        i === 0
                                            ? "Subject, e.g. quick idea for {{.Company}}"
                                            : "Subject (leave blank to reply in the same thread)"
                                    }
                                    className="w-full"
                                />
                                <textarea
                                    value={seq.body_plain}
                                    onChange={(e) => update(i, { body_plain: e.target.value })}
                                    placeholder={
                                        i === 0
                                            ? "Hi {{.FirstName}},\n\nNoticed {{.Company}} is ..."
                                            : "Just bumping this up in case it slipped past."
                                    }
                                    rows={i === 0 ? 7 : 4}
                                    className="w-full px-2.5 py-2 rounded-md border border-slate-200 bg-white text-[12.5px] text-slate-900 placeholder:text-slate-400 outline-none focus:border-sky-400 focus:ring-2 focus:ring-sky-100 resize-y leading-relaxed"
                                />
                            </div>
                        </motion.div>
                    ))}
                </AnimatePresence>

                <button
                    type="button"
                    onClick={() => patch({ sequences: [...draft.sequences, newSequence(3)] })}
                    className="w-full h-8 rounded-md border border-dashed border-slate-200 text-[12px] text-slate-500 hover:text-slate-900 hover:border-slate-300 hover:bg-slate-50 inline-flex items-center justify-center gap-1.5 transition-colors"
                >
                    <PlusIcon className="w-3 h-3" />
                    Add a follow-up
                </button>

                <p className="text-[10.5px] text-slate-400 leading-relaxed">
                    Personalise with <code className="font-mono">{"{{.FirstName}}"}</code>,{" "}
                    <code className="font-mono">{"{{.Company}}"}</code>, custom fields like{" "}
                    <code className="font-mono">{"{{.role}}"}</code>, and conditionals like{" "}
                    <code className="font-mono">{"{{if .Company}}...{{end}}"}</code>. HTML is generated for you.
                </p>
            </div>
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
