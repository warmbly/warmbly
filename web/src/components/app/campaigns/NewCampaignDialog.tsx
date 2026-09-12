// Multi-step new-campaign wizard.
//
// Two flows share one dialog. A sequence (basics, schedule, sending, first
// email) ends in a single atomic create call and leaves a draft. A one-time
// email (basics, email, audience, sending, send) creates the campaign, links
// the chosen segments so their members become leads, and starts it, either
// now or on the scheduled date. Both use directional slide transitions and a
// numbered stepper; a step cannot be left until it is complete, and the
// reason shows in the footer instead of a silently disabled button.

import React from "react";
import { AnimatePresence, motion } from "framer-motion";
import {
    AlertCircleIcon,
    CalendarClockIcon,
    CheckIcon,
    ChevronLeftIcon,
    ChevronRightIcon,
    ListChecksIcon,
    Loader2Icon,
    MailIcon,
    MegaphoneIcon,
    PencilLineIcon,
    PlusIcon,
    SendIcon,
    Trash2Icon,
    UsersIcon,
    XIcon,
} from "lucide-react";
import toast from "react-hot-toast";
import { useNavigate } from "react-router-dom";
import useCreateCampaign from "@/lib/api/hooks/app/campaigns/useCreateCampaign";
import useStartCampaign from "@/lib/api/hooks/app/campaigns/useStartCampaign";
import useCampaignEstimate from "@/lib/api/hooks/app/campaigns/useCampaignEstimate";
import { useSetCampaignSegments } from "@/lib/api/hooks/app/segments";
import type { CampaignKind } from "@/lib/api/models/app/campaigns/Campaign";
import { Label, NumberInput, TextInput } from "@/components/ui/field";
import { SelectMenu, type SelectOption } from "@/components/ui/select-menu";
import { TimePicker } from "@/components/ui/TimePicker";
import { DateTimePicker } from "@/components/ui/DateTimePicker";
import WeekdayBitmask from "@/components/app/campaigns/schedule/WeekdayBitmask";
import EntryDelayPicker from "@/components/app/campaigns/schedule/EntryDelay";
import { entryDelayLabel } from "@/components/app/campaigns/schedule/entryDelay";
import TagSelector from "@/components/app/popup/select/TagSelector";
import { SegmentMultiPicker } from "@/components/app/segments/SegmentPickers";
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

type StepKey = "basics" | "schedule" | "sending" | "email" | "audience" | "send";
type StepDef = { key: StepKey; label: string; icon: typeof MegaphoneIcon };

const SEQUENCE_STEPS: readonly StepDef[] = [
    { key: "basics", label: "Basics", icon: MegaphoneIcon },
    { key: "schedule", label: "Schedule", icon: CalendarClockIcon },
    { key: "sending", label: "Sending", icon: SendIcon },
    { key: "email", label: "First email", icon: MailIcon },
];

// A one-time email asks for the message before the audience, and ends on the
// send step where the estimate can see everything it depends on.
const ONE_TIME_STEPS: readonly StepDef[] = [
    { key: "basics", label: "Basics", icon: MegaphoneIcon },
    { key: "email", label: "Email", icon: MailIcon },
    { key: "audience", label: "Audience", icon: UsersIcon },
    { key: "sending", label: "Sending", icon: SendIcon },
    { key: "send", label: "Send", icon: CalendarClockIcon },
];

function stepsFor(kind: CampaignKind): readonly StepDef[] {
    return kind === "one_time" ? ONE_TIME_STEPS : SEQUENCE_STEPS;
}

let seqCounter = 0;
const newSequence = (wait: number): SequenceDraft => ({
    id: `seq-${++seqCounter}`,
    subject: "",
    body_plain: "",
    wait_after: wait,
});

type SendMode = "now" | "later";

type Draft = {
    kind: CampaignKind;
    name: string;
    description: string;
    timezone: string;
    days: number;
    startTime: string;
    endTime: string;
    // Minutes to hold a contact's first email after they enter the campaign.
    entryDelayMinutes: number;
    emailTagIds: string[];
    dailyLimit: number;
    stopOnReply: boolean;
    openTracking: boolean;
    linkTracking: boolean;
    utmTracking: boolean;
    unsubHeader: boolean;
    sequences: SequenceDraft[];
    // One-time only.
    segmentIds: string[];
    sendMode: SendMode;
    // Local "yyyy-MM-ddTHH:mm", the DateTimePicker's shape.
    scheduledAt: string;
};

const initialDraft = (timezone: string): Draft => ({
    kind: "sequence",
    name: "",
    description: "",
    timezone,
    days: WEEKDAYS_MASK,
    startTime: "08:00",
    endTime: "18:00",
    entryDelayMinutes: 0,
    emailTagIds: [],
    dailyLimit: 50,
    stopOnReply: true,
    openTracking: true,
    linkTracking: true,
    utmTracking: true,
    unsubHeader: true,
    sequences: [newSequence(0)],
    segmentIds: [],
    sendMode: "now",
    scheduledAt: "",
});

function scheduledDate(d: Draft): Date | null {
    if (d.sendMode !== "later" || !d.scheduledAt) return null;
    const date = new Date(d.scheduledAt);
    return Number.isNaN(date.getTime()) ? null : date;
}

// One human-readable reason a step cannot be left yet, or null when it can.
function stepIssue(key: StepKey, d: Draft): string | null {
    switch (key) {
        case "basics": {
            const n = d.name.trim().length;
            if (n < NAME_MIN) return `Name needs at least ${NAME_MIN} characters`;
            if (n > NAME_MAX) return `Name is ${NAME_MAX} characters max`;
            return null;
        }
        case "schedule":
            if (d.days === 0) return "Pick at least one sending day";
            if (d.startTime && d.endTime && d.startTime >= d.endTime) return "End time must be after the start time";
            return null;
        case "email": {
            const first = d.sequences[0];
            if (!first) return null;
            const hasSubject = first.subject.trim().length > 0;
            const hasBody = first.body_plain.trim().length > 0;
            // A one-time email is the whole campaign, so it cannot be skipped.
            if (d.kind === "one_time") {
                if (!hasSubject) return "Give the email a subject line";
                if (!hasBody) return "Write the email's body";
                return null;
            }
            // A sequence's first email may be skipped entirely (written later
            // on the Steps tab), but a half-written one must be finished.
            if (hasBody && !hasSubject) return "Give the first email a subject line";
            if (hasSubject && !hasBody) return "Write the first email's body";
            return null;
        }
        case "audience":
            if (d.segmentIds.length === 0) return "Pick at least one segment";
            return null;
        case "send": {
            if (d.days === 0) return "Pick at least one sending day";
            if (d.startTime && d.endTime && d.startTime >= d.endTime) return "End time must be after the start time";
            if (d.sendMode === "later") {
                const at = scheduledDate(d);
                if (!at) return "Pick a date and time to send";
                if (at.getTime() < Date.now()) return "The scheduled time has already passed";
            }
            return null;
        }
        default:
            return null;
    }
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

function fmtDate(d: Date): string {
    return d.toLocaleDateString("en-US", { month: "short", day: "numeric" });
}

function fmtDateTime(d: Date): string {
    return d.toLocaleString("en-US", { month: "short", day: "numeric", hour: "numeric", minute: "2-digit" });
}

export function NewCampaignDialog({ open, onClose }: Props) {
    const navigate = useNavigate();
    const profile = useUserProfile();
    const confirm = useConfirm();
    const create = useCreateCampaign();
    const linkSegments = useSetCampaignSegments();
    const start = useStartCampaign();
    const defaultTimezone = profile?.timezones?.[0]?.name || "Europe/London";

    const [step, setStep] = React.useState(0);
    const [direction, setDirection] = React.useState<1 | -1>(1);
    const [draft, setDraft] = React.useState<Draft>(() => initialDraft(defaultTimezone));
    // Set when the user tries to leave a step that is not ready; shows the reason.
    const [nudged, setNudged] = React.useState(false);
    // The one-time flow is three calls; the footer stays busy across all of them.
    const [submitting, setSubmitting] = React.useState(false);

    const steps = stepsFor(draft.kind);
    const lastStep = steps.length - 1;
    const current = steps[Math.min(step, lastStep)];

    const patch = React.useCallback((p: Partial<Draft>) => setDraft((d) => ({ ...d, ...p })), []);

    const setKind = React.useCallback(
        (kind: CampaignKind) =>
            setDraft((d) => {
                if (d.kind === kind) return d;
                return {
                    ...d,
                    kind,
                    // A one-time email is exactly one message.
                    sequences: kind === "one_time" ? d.sequences.slice(0, 1) : d.sequences,
                };
            }),
        [],
    );

    React.useEffect(() => {
        if (!open) {
            setStep(0);
            setDirection(1);
            setNudged(false);
            setSubmitting(false);
            setDraft(initialDraft(defaultTimezone));
        }
    }, [open, defaultTimezone]);

    const issue = stepIssue(current.key, draft);
    React.useEffect(() => {
        if (!issue) setNudged(false);
    }, [issue]);

    // A step is reachable when every step before it is complete.
    const canReach = React.useCallback(
        (target: number) => {
            for (let i = 0; i < target; i++) if (stepIssue(steps[i].key, draft)) return false;
            return true;
        },
        [draft, steps],
    );

    const goTo = React.useCallback(
        (target: number) => {
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

    const goToKey = React.useCallback(
        (key: StepKey) => {
            const idx = steps.findIndex((s) => s.key === key);
            if (idx >= 0) goTo(idx);
        },
        [steps, goTo],
    );

    const next = React.useCallback(() => {
        if (issue) {
            setNudged(true);
            return;
        }
        if (step < lastStep) goTo(step + 1);
    }, [issue, step, lastStep, goTo]);

    const dirty =
        draft.name.trim() !== "" ||
        draft.description.trim() !== "" ||
        draft.emailTagIds.length > 0 ||
        draft.segmentIds.length > 0 ||
        !draft.stopOnReply ||
        !draft.openTracking ||
        !draft.linkTracking ||
        !draft.utmTracking ||
        !draft.unsubHeader ||
        draft.sequences.some((s) => s.subject.trim() !== "" || s.body_plain.trim() !== "");

    const isPending = create.isPending || submitting;

    const requestClose = React.useCallback(() => {
        if (isPending) return;
        if (dirty) {
            confirm.show("Discard this campaign draft?", async () => onClose());
            return;
        }
        onClose();
    }, [isPending, dirty, confirm, onClose]);

    React.useEffect(() => {
        if (!open) return;
        const onKey = (e: KeyboardEvent) => {
            if (e.key !== "Escape") return;
            // An open dropdown (timezone, tags, segments, date) or the discard confirm owns this Escape.
            if (document.querySelector("[data-floating], [role='alertdialog']")) return;
            e.preventDefault();
            requestClose();
        };
        document.addEventListener("keydown", onKey);
        return () => document.removeEventListener("keydown", onKey);
    }, [open, requestClose]);

    // The wizard writes plain text, so it sends plain text. The backend renders
    // the HTML part from it: it used to be built here with an escapeHtml that
    // turned the quotes in a conditional ({{if eq .Company "Acme"}}) into
    // entities, which makes the template fail to parse at send time and ships
    // the literal {{if}} to the recipient. The server's version also links bare
    // URLs, so a wizard-written step gets click tracking like any other.
    function buildSteps() {
        return draft.sequences
            .filter((s) => s.subject.trim().length > 0 || s.body_plain.trim().length > 0)
            .map((s, i) => ({
                name: draft.kind === "one_time" ? "Email" : `Step ${i + 1}`,
                subject: s.subject.trim(),
                body_plain: s.body_plain,
                wait_after: i === 0 ? 0 : Math.max(0, s.wait_after),
            }));
    }

    async function submit() {
        if (isPending) return;
        for (let i = 0; i < steps.length; i++) {
            if (stepIssue(steps[i].key, draft)) {
                setDirection(i > step ? 1 : -1);
                setStep(i);
                setNudged(true);
                return;
            }
        }
        const base = {
            name: draft.name.trim(),
            description: draft.description.trim(),
            timezone: draft.timezone,
            days: draft.days,
            start_time: draft.startTime,
            end_time: draft.endTime,
            entry_delay_minutes: draft.entryDelayMinutes,
            daily_limit: draft.dailyLimit,
            open_tracking: draft.openTracking,
            link_tracking: draft.linkTracking,
            utm_tracking: draft.utmTracking,
            unsubscribe_header: draft.unsubHeader,
            email_tag_ids: draft.emailTagIds,
            steps: buildSteps(),
        };

        if (draft.kind === "sequence") {
            try {
                const created = await create.mutateAsync({ ...base, kind: "sequence", stop_on_reply: draft.stopOnReply });
                toast.success("Campaign created. Add contacts, then launch it.");
                onClose();
                if (created?.id) navigate(`/app/campaigns/${created.id}`);
            } catch (err) {
                toast.error(buildError(err as AppError));
            }
            return;
        }

        // One-time: create, link the audience (which enrols its members as
        // leads), then start. A scheduled start date parks the campaign until
        // then; the scheduler does the waiting, not the browser.
        const at = scheduledDate(draft);
        setSubmitting(true);
        let createdID: string | null = null;
        try {
            const created = await create.mutateAsync({
                ...base,
                kind: "one_time",
                // No follow-ups exist to stop, and a reply should still not
                // re-send the one message, so the flag stays on.
                stop_on_reply: true,
                start_date: at ? at.toISOString() : undefined,
            });
            createdID = created.id;
            await linkSegments.mutateAsync({ campaignId: created.id, segmentIds: draft.segmentIds });
            await start.mutateAsync({ id: created.id });
            toast.success(at ? `Scheduled for ${fmtDateTime(at)}.` : "Sending now.");
            onClose();
            navigate(`/app/campaigns/${created.id}`);
        } catch (err) {
            // Past the create call the campaign exists as a draft: say so and
            // hand over to the campaign page, where the launch dialog can
            // explain a refused start (an empty segment, a risky list).
            const message = buildError(err as AppError);
            if (createdID) {
                toast.error(`${message} The email was saved as a draft; start it from its page.`);
                onClose();
                navigate(`/app/campaigns/${createdID}`);
            } else {
                toast.error(message);
            }
        } finally {
            setSubmitting(false);
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
                        aria-label={draft.kind === "one_time" ? "New one-time email" : "New campaign"}
                        initial={{ y: 8, opacity: 0, scale: 0.985 }}
                        animate={{ y: 0, opacity: 1, scale: 1 }}
                        exit={{ y: 8, opacity: 0, scale: 0.985 }}
                        transition={{ duration: 0.18, ease: [0.22, 1, 0.36, 1] }}
                        onMouseDown={(e) => e.stopPropagation()}
                        className="w-full max-w-[720px] rounded-lg bg-white border border-slate-200 shadow-[0_24px_48px_-12px_rgba(15,23,42,0.18),0_8px_16px_-8px_rgba(15,23,42,0.1)] overflow-hidden flex flex-col max-h-[88dvh]"
                    >
                        <Header kind={draft.kind} onClose={requestClose} />
                        <Stepper steps={steps} step={step} canReach={canReach} goTo={goTo} />

                        <div className="flex-1 min-h-0 overflow-y-auto overflow-x-hidden">
                            <AnimatePresence mode="wait" initial={false} custom={direction}>
                                <motion.div
                                    key={`${draft.kind}-${current.key}`}
                                    custom={direction}
                                    variants={paneVariants}
                                    initial="enter"
                                    animate="center"
                                    exit="exit"
                                    transition={{ duration: 0.18, ease: [0.22, 1, 0.36, 1] }}
                                    className="px-5 py-5 min-h-[340px]"
                                >
                                    {current.key === "basics" && (
                                        <BasicsStep draft={draft} patch={patch} setKind={setKind} onEnter={next} />
                                    )}
                                    {current.key === "schedule" && <ScheduleStep draft={draft} patch={patch} />}
                                    {current.key === "sending" && <SendingStep draft={draft} patch={patch} />}
                                    {current.key === "email" && <EmailsStep draft={draft} patch={patch} goToKey={goToKey} />}
                                    {current.key === "audience" && <AudienceStep draft={draft} patch={patch} />}
                                    {current.key === "send" && <SendStep draft={draft} patch={patch} goToKey={goToKey} />}
                                </motion.div>
                            </AnimatePresence>
                        </div>

                        <Footer
                            step={step}
                            lastStep={lastStep}
                            submitLabel={
                                draft.kind === "one_time"
                                    ? draft.sendMode === "later"
                                        ? "Schedule"
                                        : "Send now"
                                    : "Create campaign"
                            }
                            submitIcon={
                                draft.kind === "one_time"
                                    ? draft.sendMode === "later"
                                        ? CalendarClockIcon
                                        : SendIcon
                                    : PlusIcon
                            }
                            issue={nudged ? issue : null}
                            onBack={() => goTo(step - 1)}
                            onNext={next}
                            onSubmit={submit}
                            isPending={isPending}
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

function Header({ kind, onClose }: { kind: CampaignKind; onClose: () => void }) {
    const Icon = kind === "one_time" ? SendIcon : MegaphoneIcon;
    return (
        <div className="h-12 px-4 border-b border-slate-200 flex items-center gap-2.5 shrink-0">
            <div className="size-5 rounded bg-slate-100 text-slate-600 flex items-center justify-center">
                <Icon className="w-3 h-3" />
            </div>
            <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">New</span>
            <div className="h-4 w-px bg-slate-200" />
            <span className="text-[12.5px] text-slate-900 font-medium">
                {kind === "one_time" ? "One-time email" : "Campaign"}
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

function Stepper({
    steps,
    step,
    canReach,
    goTo,
}: {
    steps: readonly StepDef[];
    step: number;
    canReach: (s: number) => boolean;
    goTo: (s: number) => void;
}) {
    return (
        <div className="px-4 sm:px-5 h-11 border-b border-slate-100 flex items-center shrink-0 bg-slate-50/40">
            {steps.map((s, i) => {
                const active = i === step;
                const done = i < step;
                const reachable = i <= step || canReach(i);
                return (
                    <React.Fragment key={s.key}>
                        <button
                            type="button"
                            onClick={() => goTo(i)}
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
                        {i < steps.length - 1 && (
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
    lastStep,
    submitLabel,
    submitIcon: SubmitIcon,
    issue,
    onBack,
    onNext,
    onSubmit,
    isPending,
}: {
    step: number;
    lastStep: number;
    submitLabel: string;
    submitIcon: typeof PlusIcon;
    issue: string | null;
    onBack: () => void;
    onNext: () => void;
    onSubmit: () => void;
    isPending: boolean;
}) {
    const isLast = step === lastStep;
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
                        {isPending ? <Loader2Icon className="w-3 h-3 animate-spin" /> : <SubmitIcon className="w-3 h-3" />}
                        {submitLabel}
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

function KindCard({
    selected,
    icon: Icon,
    title,
    description,
    onSelect,
}: {
    selected: boolean;
    icon: typeof MegaphoneIcon;
    title: string;
    description: string;
    onSelect: () => void;
}) {
    return (
        <button
            type="button"
            role="radio"
            aria-checked={selected}
            onClick={onSelect}
            className={cn(
                "text-left rounded-md border px-3 py-2.5 flex items-start gap-2.5 transition-colors outline-none focus-visible:ring-2 focus-visible:ring-sky-100",
                selected
                    ? "border-sky-400 bg-sky-50/60 ring-1 ring-inset ring-sky-400"
                    : "border-slate-200 hover:border-slate-300 hover:bg-slate-50",
            )}
        >
            <span
                className={cn(
                    "size-6 rounded-md inline-flex items-center justify-center shrink-0 mt-0.5",
                    selected ? "bg-sky-600 text-white" : "bg-slate-100 text-slate-600",
                )}
            >
                <Icon className="w-3.5 h-3.5" />
            </span>
            <span className="min-w-0">
                <span className="block text-[12.5px] text-slate-900 font-medium">{title}</span>
                <span className="block text-[11px] text-slate-500 mt-0.5 leading-relaxed">{description}</span>
            </span>
        </button>
    );
}

function BasicsStep({
    draft,
    patch,
    setKind,
    onEnter,
}: {
    draft: Draft;
    patch: (p: Partial<Draft>) => void;
    setKind: (k: CampaignKind) => void;
    onEnter: () => void;
}) {
    const len = draft.name.trim().length;
    return (
        <div className="max-w-[560px]">
            <StepIntro
                title="What are you sending?"
                hint="A sequence follows up over days; a one-time email goes out once to a segment. Both send through your mailbox pool at its daily caps."
            />
            <div className="space-y-4">
                <div role="radiogroup" aria-label="Campaign type" className="grid grid-cols-1 sm:grid-cols-2 gap-2">
                    <KindCard
                        selected={draft.kind === "sequence"}
                        icon={ListChecksIcon}
                        title="Sequence"
                        description="Several emails with waits, branches and follow-ups. Add contacts and launch when ready."
                        onSelect={() => setKind("sequence")}
                    />
                    <KindCard
                        selected={draft.kind === "one_time"}
                        icon={SendIcon}
                        title="One-time email"
                        description="One message to a segment, sent now or on a date, with no follow-ups."
                        onSelect={() => setKind("one_time")}
                    />
                </div>
                <div>
                    <div className="flex items-baseline justify-between">
                        <Label>{draft.kind === "one_time" ? "Email name" : "Campaign name"}</Label>
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
                        placeholder={draft.kind === "one_time" ? "September product update" : "Q3 outbound, SaaS founders"}
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

function TimezoneField({ draft, patch }: { draft: Draft; patch: (p: Partial<Draft>) => void }) {
    const profile = useUserProfile();
    const timezoneOptions = React.useMemo<SelectOption[]>(
        () => (profile?.timezones || []).map((tz) => ({ value: tz.name, label: tz.display_name })),
        [profile?.timezones],
    );
    return (
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
    );
}

function SendingWindowFields({ draft, patch }: { draft: Draft; patch: (p: Partial<Draft>) => void }) {
    const windowInvalid = draft.startTime >= draft.endTime;
    return (
        <>
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
        </>
    );
}

function ScheduleStep({ draft, patch }: { draft: Draft; patch: (p: Partial<Draft>) => void }) {
    return (
        <div className="max-w-[560px]">
            <StepIntro
                title="When should it send?"
                hint="Sends stay inside this window in the campaign timezone and spread out across it."
            />
            <div className="space-y-5">
                <TimezoneField draft={draft} patch={patch} />
                <SendingWindowFields draft={draft} patch={patch} />
                <div>
                    <div className="flex items-baseline justify-between">
                        <Label>Wait before the first email</Label>
                        <span className="text-[10.5px] text-slate-400">
                            {entryDelayLabel(draft.entryDelayMinutes)}
                        </span>
                    </div>
                    <div className="mt-1">
                        <EntryDelayPicker
                            value={draft.entryDelayMinutes}
                            onChange={(v) => patch({ entryDelayMinutes: v })}
                        />
                    </div>
                    <p className="mt-2 text-[11.5px] leading-relaxed text-slate-500">
                        Counted from when a contact enters the campaign, so someone who joins a linked segment later
                        waits the same amount from their own start. Follow-up waits are set on the steps.
                    </p>
                </div>
            </div>
        </div>
    );
}

function SendingStep({ draft, patch }: { draft: Draft; patch: (p: Partial<Draft>) => void }) {
    const oneTime = draft.kind === "one_time";
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
                            3 to 5,000. Stay near 50 until the mailboxes have proven their reputation.
                        </p>
                    </div>
                    <NumberInput
                        value={draft.dailyLimit}
                        min={3}
                        max={5000}
                        onChange={(v) => patch({ dailyLimit: v })}
                        className="w-24 shrink-0"
                    />
                </div>

                <div className="border border-slate-200 rounded-md divide-y divide-slate-100 overflow-hidden">
                    {!oneTime && (
                        <SwitchRow
                            label="Stop on reply"
                            description="Pause follow-ups for a contact once they respond."
                            value={draft.stopOnReply}
                            onChange={(v) => patch({ stopOnReply: v })}
                        />
                    )}
                    <SwitchRow
                        label="Track opens"
                        description="Insert a transparent pixel to measure inbox impressions."
                        value={draft.openTracking}
                        onChange={(v) => patch({ openTracking: v })}
                    />
                    <SwitchRow
                        label="Track clicks"
                        description="Wrap links so each click, and which link it was, appears in your live feed and the contact's activity."
                        value={draft.linkTracking}
                        onChange={(v) => patch({ linkTracking: v })}
                    />
                    <SwitchRow
                        label="Add UTM parameters"
                        description="Tag every link with utm_source, utm_medium, utm_campaign and a per-link utm_content for your web analytics. Editable later in settings."
                        value={draft.utmTracking}
                        onChange={(v) => patch({ utmTracking: v })}
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
    goToKey,
}: {
    draft: Draft;
    patch: (p: Partial<Draft>) => void;
    goToKey: (k: StepKey) => void;
}) {
    const oneTime = draft.kind === "one_time";
    const update = (i: number, p: Partial<SequenceDraft>) =>
        patch({ sequences: draft.sequences.map((s, idx) => (idx === i ? { ...s, ...p } : s)) });

    return (
        <div className="max-w-[640px]">
            <StepIntro
                title={oneTime ? "What does the email say?" : "What does the first email say?"}
                hint={
                    oneTime
                        ? "This is the whole send. You can polish it in the full editor on the Steps tab afterwards."
                        : "Optional. Skip this to write it in the full editor on the Steps tab afterwards."
                }
            />

            {!oneTime && (
                <div className="mb-4 flex flex-wrap items-center gap-x-1.5 gap-y-1 text-[11px] text-slate-500">
                    <button type="button" onClick={() => goToKey("schedule")} className="hover:text-slate-900 hover:underline underline-offset-2">
                        {daysLabel(draft.days)}, {fmt12(draft.startTime)} to {fmt12(draft.endTime)}
                        {draft.entryDelayMinutes > 0
                            ? `, first email after ${entryDelayLabel(draft.entryDelayMinutes).toLowerCase()}`
                            : ""}
                    </button>
                    <span className="text-slate-300">·</span>
                    <button type="button" onClick={() => goToKey("sending")} className="hover:text-slate-900 hover:underline underline-offset-2">
                        {draft.dailyLimit}/day per mailbox
                    </button>
                    <span className="text-slate-300">·</span>
                    <button type="button" onClick={() => goToKey("sending")} className="hover:text-slate-900 hover:underline underline-offset-2">
                        {draft.emailTagIds.length === 0
                            ? "all mailboxes"
                            : `${draft.emailTagIds.length} tag${draft.emailTagIds.length === 1 ? "" : "s"}`}
                    </button>
                    <PencilLineIcon className="w-3 h-3 text-slate-300 ml-0.5" />
                </div>
            )}

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
                                    {oneTime ? "Email" : i === 0 ? "First email" : `Follow-up ${i}`}
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

                {oneTime ? (
                    <p className="text-[11px] text-slate-500 leading-relaxed">
                        No follow-ups here by design. Want a reminder a few days later? Create a sequence instead.
                    </p>
                ) : (
                    <button
                        type="button"
                        onClick={() => patch({ sequences: [...draft.sequences, newSequence(3)] })}
                        className="w-full h-8 rounded-md border border-dashed border-slate-200 text-[12px] text-slate-500 hover:text-slate-900 hover:border-slate-300 hover:bg-slate-50 inline-flex items-center justify-center gap-1.5 transition-colors"
                    >
                        <PlusIcon className="w-3 h-3" />
                        Add a follow-up
                    </button>
                )}

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

function AudienceStep({ draft, patch }: { draft: Draft; patch: (p: Partial<Draft>) => void }) {
    const estimate = useCampaignEstimate({ segment_ids: draft.segmentIds });
    const recipients = estimate.data?.recipients;
    return (
        <div className="max-w-[560px]">
            <StepIntro
                title="Who receives it?"
                hint="Every current member of the segments you pick becomes a lead. Suppressed and unsubscribed contacts are skipped at send time."
            />
            <div className="space-y-4">
                <div>
                    <Label>Segments</Label>
                    <SegmentMultiPicker value={draft.segmentIds} onChange={(next) => patch({ segmentIds: next })} />
                    <p className="text-[11px] text-slate-400 mt-1">
                        Contacts in more than one segment are counted, and emailed, once.
                    </p>
                </div>
                <div className="rounded-md border border-slate-200 px-3 py-2.5 flex items-center gap-3">
                    <span className="size-7 rounded-md bg-slate-100 text-slate-600 inline-flex items-center justify-center shrink-0">
                        <UsersIcon className="w-3.5 h-3.5" />
                    </span>
                    <div className="min-w-0">
                        <p className="text-[12.5px] text-slate-900 font-medium tabular-nums">
                            {draft.segmentIds.length === 0
                                ? "No segment picked yet"
                                : estimate.isPending
                                  ? "Counting…"
                                  : estimate.isError
                                    ? "Could not count recipients"
                                    : `${(recipients ?? 0).toLocaleString()} recipient${recipients === 1 ? "" : "s"}`}
                        </p>
                        <p className="text-[11px] text-slate-500 mt-0.5">
                            {recipients === 0 && !estimate.isPending && draft.segmentIds.length > 0
                                ? "These segments have no contacts right now, so there is nothing to send."
                                : "Live membership, as of now."}
                        </p>
                    </div>
                </div>
            </div>
        </div>
    );
}

function SendStep({
    draft,
    patch,
    goToKey,
}: {
    draft: Draft;
    patch: (p: Partial<Draft>) => void;
    goToKey: (k: StepKey) => void;
}) {
    const at = scheduledDate(draft);
    const estimate = useCampaignEstimate({
        segment_ids: draft.segmentIds,
        email_tag_ids: draft.emailTagIds,
        daily_limit: draft.dailyLimit,
        days: draft.days,
        timezone: draft.timezone,
        start_date: at ? at.toISOString() : undefined,
    });
    const e = estimate.data;
    const finish = e?.estimated_finish_at ? new Date(e.estimated_finish_at) : null;

    return (
        <div className="max-w-[560px]">
            <StepIntro
                title="When should it go?"
                hint="Send now starts the moment you confirm. Either way it keeps to the sending window and the daily cap of every mailbox."
            />
            <div className="space-y-5">
                <div role="radiogroup" aria-label="Send timing" className="grid grid-cols-1 sm:grid-cols-2 gap-2">
                    <KindCard
                        selected={draft.sendMode === "now"}
                        icon={SendIcon}
                        title="Send now"
                        description="Start as soon as it is created."
                        onSelect={() => patch({ sendMode: "now" })}
                    />
                    <KindCard
                        selected={draft.sendMode === "later"}
                        icon={CalendarClockIcon}
                        title="Schedule for later"
                        description="Pick the date and time sending begins."
                        onSelect={() => patch({ sendMode: "later" })}
                    />
                </div>

                <AnimatePresence initial={false}>
                    {draft.sendMode === "later" && (
                        <motion.div
                            key="when"
                            initial={{ opacity: 0, height: 0 }}
                            animate={{ opacity: 1, height: "auto" }}
                            exit={{ opacity: 0, height: 0 }}
                            transition={{ duration: 0.16 }}
                            className="overflow-hidden"
                        >
                            <Label>Start sending at</Label>
                            <DateTimePicker
                                value={draft.scheduledAt}
                                onChange={(v) => patch({ scheduledAt: v })}
                                stepMinutes={15}
                                datePlaceholder="Pick a date"
                            />
                            <p className="text-[11px] text-slate-400 mt-1">In your local time.</p>
                        </motion.div>
                    )}
                </AnimatePresence>

                <TimezoneField draft={draft} patch={patch} />
                <SendingWindowFields draft={draft} patch={patch} />

                <EstimatePanel
                    loading={estimate.isPending && draft.segmentIds.length > 0}
                    error={estimate.isError}
                    recipients={e?.recipients ?? 0}
                    mailboxes={e?.mailboxes ?? 0}
                    dailyCapacity={e?.daily_capacity ?? 0}
                    sendingDays={e?.sending_days ?? null}
                    finish={finish}
                    startsAt={at}
                    onEditPool={() => goToKey("sending")}
                    onEditAudience={() => goToKey("audience")}
                />
            </div>
        </div>
    );
}

function EstimatePanel({
    loading,
    error,
    recipients,
    mailboxes,
    dailyCapacity,
    sendingDays,
    finish,
    startsAt,
    onEditPool,
    onEditAudience,
}: {
    loading: boolean;
    error: boolean;
    recipients: number;
    mailboxes: number;
    dailyCapacity: number;
    sendingDays: number | null;
    finish: Date | null;
    startsAt: Date | null;
    onEditPool: () => void;
    onEditAudience: () => void;
}) {
    let headline: string;
    let detail: React.ReactNode;
    let tone: "neutral" | "warn" = "neutral";

    if (loading) {
        headline = "Working out the timing…";
        detail = "Counting the audience against the mailbox pool.";
    } else if (error) {
        headline = "Could not estimate the timing";
        detail = "The send still works; this only affects the preview.";
    } else if (recipients === 0) {
        headline = "Nothing to send yet";
        detail = (
            <>
                The chosen segments have no contacts.{" "}
                <button type="button" onClick={onEditAudience} className="underline underline-offset-2 hover:text-slate-900">
                    Change the audience
                </button>
                .
            </>
        );
        tone = "warn";
    } else if (mailboxes === 0 || dailyCapacity === 0) {
        headline = "No mailbox can send this";
        detail = (
            <>
                The sender pool resolves to no active mailbox.{" "}
                <button type="button" onClick={onEditPool} className="underline underline-offset-2 hover:text-slate-900">
                    Change the pool
                </button>{" "}
                or connect a mailbox first.
            </>
        );
        tone = "warn";
    } else if (sendingDays === null || finish === null) {
        // The backend leaves both null when the audience is not covered
        // inside its two-year horizon, which must not read as "one day".
        headline = "Longer than this estimate can project";
        detail = (
            <>
                {recipients.toLocaleString()} recipients at up to {dailyCapacity.toLocaleString()} a day would take
                years.{" "}
                <button type="button" onClick={onEditPool} className="underline underline-offset-2 hover:text-slate-900">
                    Add mailboxes or raise the daily limit
                </button>
                , or narrow the audience.
            </>
        );
        tone = "warn";
    } else {
        const days = sendingDays;
        headline =
            days <= 1
                ? `Finishes ${startsAt ? "on" : "today,"} ${fmtDate(finish)}`
                : `About ${days} sending days, finishing around ${fmtDate(finish)}`;
        detail = (
            <>
                {recipients.toLocaleString()} recipient{recipients === 1 ? "" : "s"} across {mailboxes} mailbox
                {mailboxes === 1 ? "" : "es"} at up to {dailyCapacity.toLocaleString()} a day.
                {days > 1 && " One-time means one message per contact, not one day: the daily caps still pace it."}
            </>
        );
        if (days > 7) tone = "warn";
    }

    return (
        <div
            className={cn(
                "rounded-md border px-3 py-2.5 flex items-start gap-3",
                tone === "warn" ? "border-amber-200 bg-amber-50/60" : "border-slate-200",
            )}
        >
            <span
                className={cn(
                    "size-7 rounded-md inline-flex items-center justify-center shrink-0",
                    tone === "warn" ? "bg-amber-100 text-amber-700" : "bg-slate-100 text-slate-600",
                )}
            >
                {loading ? <Loader2Icon className="w-3.5 h-3.5 animate-spin" /> : <CalendarClockIcon className="w-3.5 h-3.5" />}
            </span>
            <div className="min-w-0">
                <p className={cn("text-[12.5px] font-medium", tone === "warn" ? "text-amber-900" : "text-slate-900")}>
                    {headline}
                </p>
                <p className={cn("text-[11px] mt-0.5 leading-relaxed", tone === "warn" ? "text-amber-800" : "text-slate-500")}>
                    {detail}
                </p>
            </div>
        </div>
    );
}

