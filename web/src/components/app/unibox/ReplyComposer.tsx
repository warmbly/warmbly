// Reply composer.
//
// Mounted on demand by ThreadView when the user clicks "Reply" on a
// specific message. Not pinned to the bottom by default, and it does
// not pre-populate body text. The user only sees this when they have
// already committed to writing a reply to a particular message.
//
// Layout: a target-message preview card at the top (sender + subject
// + dismiss), followed by labelled header rows (From, To, Cc, Bcc,
// Subject), the body textarea, the optional signature preview, and
// the action bar (Send / Schedule / Template / Discard).
//
// ⌘+Enter sends instantly. Each schedule preset calls /unibox/reply
// with send_mode="scheduled" plus the concrete scheduled_at.

import React from "react";
import { AnimatePresence, motion } from "framer-motion";
import {
    CheckIcon,
    ChevronDownIcon,
    ClockIcon,
    CornerUpLeftIcon,
    FileTextIcon,
    InfoIcon,
    Loader2Icon,
    PenLineIcon,
    SendIcon,
    XIcon,
} from "lucide-react";
import toast from "react-hot-toast";
import sendReply from "@/lib/api/client/app/unibox/sendReply";
import { DateTimePicker } from "@/components/ui/DateTimePicker";
import useTemplates from "@/lib/api/hooks/app/templates/useTemplates";
import TemplatePickerContent from "./TemplatePicker";
import InsertBookingLink from "./InsertBookingLink";
import useUniboxOverview from "@/lib/api/hooks/app/unibox/useUniboxOverview";
import { useAppStore } from "@/stores";
import useDraftReply from "@/lib/api/hooks/app/unibox/useDraftReply";
import AIDraftBar, { useAIDraft } from "@/components/app/ai/AIDraftBar";
import TextareaAIEdit from "@/components/app/ai/TextareaAIEdit";
import TextareaAICaret from "@/components/app/ai/TextareaAICaret";
import type UniboxEmail from "@/lib/api/models/app/unibox/UniboxEmail";
import {
    PopoverMenu,
    PopoverMenuContent,
    PopoverMenuItem,
    PopoverMenuLabel,
    PopoverMenuTrigger,
    PopoverMenuSeparator,
} from "@/components/ui/popover-menu";
import { cn } from "@/lib/utils";

export type ReplyMode = "reply" | "forward";

interface ReplyComposerProps {
    threadId: string;
    replyTo: UniboxEmail;
    mode: ReplyMode;
    onClose: () => void;
}

const SCHEDULE_PRESETS: { label: string; at: () => Date }[] = [
    { label: "In 1 hour", at: () => offsetHours(1) },
    { label: "In 3 hours", at: () => offsetHours(3) },
    { label: "Tomorrow 9:00", at: () => atHour(1, 9) },
    { label: "Tomorrow 17:00", at: () => atHour(1, 17) },
    { label: "Monday 9:00", at: () => nextMonday9() },
];

// Body length cap. Generous; real replies rarely come close.
const MAX_BODY_LEN = 4000;

// Server cap (GCP Cloud Tasks 30-day ceiling, minus a day of
// clock-skew headroom). Mirrored client-side so users get an inline
// error instead of a 400 from the API.
const MAX_SCHEDULE_MS = 29 * 24 * 60 * 60 * 1000;

function offsetHours(h: number): Date {
    const d = new Date();
    d.setHours(d.getHours() + h);
    return d;
}
function atHour(dayOffset: number, hour: number): Date {
    const d = new Date();
    d.setDate(d.getDate() + dayOffset);
    d.setHours(hour, 0, 0, 0);
    return d;
}
function nextMonday9(): Date {
    const d = new Date();
    const dow = d.getDay();
    const delta = ((1 - dow + 7) % 7) || 7;
    d.setDate(d.getDate() + delta);
    d.setHours(9, 0, 0, 0);
    return d;
}

function toLocalInput(d: Date): string {
    const pad = (n: number) => String(n).padStart(2, "0");
    return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
}
function defaultCustomScheduleValue(): string {
    return toLocalInput(offsetHours(2));
}

function formatFriendly(d: Date): string {
    const now = new Date();
    const sameDay =
        d.getFullYear() === now.getFullYear() &&
        d.getMonth() === now.getMonth() &&
        d.getDate() === now.getDate();
    const time = d.toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit" });
    if (sameDay) return `today, ${time}`;
    return d.toLocaleString(undefined, {
        month: "short",
        day: "numeric",
        hour: "2-digit",
        minute: "2-digit",
    });
}

function looksLikeEmail(s: string): boolean {
    return /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(s.trim());
}

function nameFromAddr(s: string): string {
    const m = s.match(/^"?([^"<]+)"?\s*<.+>$/);
    if (m) return m[1].trim();
    return s.replace(/<.+>/, "").trim() || s;
}

function bareEmail(s: string): string {
    const m = s.match(/<([^>]+)>/);
    if (m) return m[1].trim();
    return s.trim();
}

// Derive composer defaults from the message the user explicitly chose
// to reply to (or forward). Reply takes the message's "from" as the
// new "to". Forward leaves "to" empty so the user picks the new
// recipient.
function deriveDefaults(replyTo: UniboxEmail, mode: ReplyMode) {
    const subjectBase = replyTo.subject?.trim() || "";
    let subject: string;
    if (mode === "forward") {
        subject = /^fwd:/i.test(subjectBase) ? subjectBase : `Fwd: ${subjectBase || "(no subject)"}`;
    } else {
        subject = /^re:/i.test(subjectBase) ? subjectBase : `Re: ${subjectBase || "(no subject)"}`;
    }
    const fromAddr = replyTo.from ? bareEmail(replyTo.from) : "";
    const to = mode === "reply" && fromAddr ? [fromAddr] : [];
    return { to, subject };
}

export function ReplyComposer({ threadId, replyTo, mode, onClose }: ReplyComposerProps) {
    const accounts = useAppStore((s) => s.emails);

    const initial = React.useMemo(() => deriveDefaults(replyTo, mode), [replyTo, mode]);

    const [body, setBody] = React.useState("");
    const [subject, setSubject] = React.useState(initial.subject);
    const [to, setTo] = React.useState<string[]>(initial.to);
    const [cc, setCc] = React.useState<string[]>([]);
    const [bcc, setBcc] = React.useState<string[]>([]);
    const [showCc, setShowCc] = React.useState(false);
    const [showBcc, setShowBcc] = React.useState(false);
    const [isSending, setIsSending] = React.useState(false);

    const [scheduleOpen, setScheduleOpen] = React.useState(false);
    const [customMode, setCustomMode] = React.useState(false);
    const [customValue, setCustomValue] = React.useState(defaultCustomScheduleValue);
    const [templateOpen, setTemplateOpen] = React.useState(false);

    // Context-grounded AI reply draft. Types itself into the composer through
    // the draft bar (Keep / Adjust / Retry / Discard); the human sends.
    const draftReplyMut = useDraftReply();
    const bodyRef = React.useRef<HTMLTextAreaElement>(null);
    const generateDraft = React.useCallback(
        (instruction?: string) =>
            draftReplyMut.mutateAsync({
                thread_id: threadId,
                instruction,
                idempotency_key: crypto.randomUUID(),
            }),
        [draftReplyMut, threadId],
    );
    const aiDraft = useAIDraft({
        value: body,
        onChange: setBody,
        generate: generateDraft,
        maxLen: MAX_BODY_LEN,
    });

    // Reset whenever the user picks a different target message or
    // switches between reply and forward. Without this the body, chips,
    // and subject would persist across separate compose sessions.
    React.useEffect(() => {
        setSubject(initial.subject);
        setTo(initial.to);
        setCc([]);
        setBcc([]);
        setShowCc(false);
        setShowBcc(false);
        setBody("");
    }, [initial.subject, initial.to, replyTo.id, mode]);

    // Resolve the sending mailbox from the target message's
    // account_id. We look it up in the global emails store so we have
    // the full Inbox record (signature_html, signature_plain, etc).
    const accountId = replyTo.account_id ?? "";
    const mailbox = accounts.find((a) => a.id === accountId);

    const templatesQuery = useTemplates();

    // Scheduled-sends pending cap. Disable the Schedule button at 100%
    // so users cannot queue a send that would 429 server-side. Cached
    // read; the page already mounts useUniboxOverview.
    const overview = useUniboxOverview();
    const scheduledUsed = overview.data?.scheduled_pending ?? 0;
    const scheduledCap = overview.data?.scheduled_pending_max ?? 0;
    const scheduleAtCap = scheduledCap > 0 && scheduledUsed >= scheduledCap;

    const trimmedBody = body.trim();
    const canSend = !!trimmedBody && to.length > 0 && to.every(looksLikeEmail) && !!accountId && !isSending;

    const send = async (scheduledAt?: Date) => {
        if (!canSend && !isSending) {
            if (!trimmedBody) {
                toast.error("Body is empty");
                return;
            }
            if (to.length === 0) {
                toast.error("Add at least one recipient");
                return;
            }
            if (!to.every(looksLikeEmail)) {
                toast.error("Recipient email looks invalid");
                return;
            }
            if (!accountId) {
                toast.error("Couldn't resolve the sending mailbox");
                return;
            }
        }

        setIsSending(true);
        try {
            await sendReply({
                email_account_id: accountId,
                to,
                cc: cc.length ? cc : undefined,
                bcc: bcc.length ? bcc : undefined,
                subject: subject.trim() || (mode === "forward" ? "Fwd:" : "Re:"),
                body_plain: trimmedBody,
                body_html: trimmedBody.replace(/\n/g, "<br />"),
                thread_id: mode === "reply" ? threadId : undefined,
                ...(scheduledAt
                    ? {
                          send_mode: "scheduled" as const,
                          scheduled_at: scheduledAt.toISOString(),
                      }
                    : { send_mode: "instant" as const }),
            });
            toast.success(
                scheduledAt
                    ? `Scheduled for ${formatFriendly(scheduledAt)}`
                    : mode === "forward"
                      ? "Forward queued"
                      : "Reply queued",
            );
            setScheduleOpen(false);
            setCustomMode(false);
            onClose();
        } catch {
            toast.error(mode === "forward" ? "Failed to forward" : "Failed to send reply");
        } finally {
            setIsSending(false);
        }
    };

    const handleInstant = () => send();
    const handleSchedule = (d: Date) => {
        if (!Number.isFinite(d.getTime()) || d.getTime() <= Date.now() + 5_000) {
            toast.error("Pick a future time (a few seconds out, please)");
            return;
        }
        if (d.getTime() - Date.now() > MAX_SCHEDULE_MS) {
            toast.error("Scheduled send can't be more than 29 days out");
            return;
        }
        send(d);
    };
    const handleCustom = () => {
        if (!customValue) {
            toast.error("Pick a time first");
            return;
        }
        handleSchedule(new Date(customValue));
    };

    const applyTemplate = (name: string, plain: string, subj: string) => {
        // Replace body if empty; otherwise append under a separator so
        // the user keeps whatever they already typed.
        if (!body.trim()) {
            setBody(plain);
        } else {
            setBody((b) => `${b.trimEnd()}\n\n${plain}`);
        }
        // Only overwrite subject when the user hasn't customised it
        // past the default "Re:" / "Fwd:" prefix.
        const subj0 = subject.trim();
        if (subj && (/^re:\s*$/i.test(subj0) || /^fwd:\s*$/i.test(subj0))) {
            setSubject(subj);
        }
        setTemplateOpen(false);
        toast.success(`Inserted "${name}"`);
    };

    // Three signature states the user might be in. Surfacing all
    // three (not just the "on" case) so it's never a surprise when a
    // signature shows up at send-time, AND never a surprise when it
    // doesn't because sync is disabled in mailbox settings.
    //
    //   on   : signature_sync is true AND a signature is configured.
    //          We'll append it on send, so we render the preview and
    //          a confirming pill in the action bar.
    //   off  : a signature exists but sync is off in settings. We
    //          tell the user so they don't assume it'll be sent.
    //   none : no signature configured for this mailbox.
    type SignatureState =
        | { kind: "on"; preview: string }
        | { kind: "off"; preview: string }
        | { kind: "none" };
    const signatureState: SignatureState = React.useMemo(() => {
        const plain = (mailbox?.signature_plain ?? "").trim();
        if (!plain) return { kind: "none" };
        if (mailbox?.signature_sync) return { kind: "on", preview: plain };
        return { kind: "off", preview: plain };
    }, [mailbox?.signature_plain, mailbox?.signature_sync]);

    const replyToName = replyTo.from ? nameFromAddr(replyTo.from) : "(unknown sender)";
    const replyToAddr = replyTo.from ? bareEmail(replyTo.from) : "";
    const replyTargetSubject = replyTo.subject?.trim() || "(no subject)";

    const scheduleTooltip = scheduleAtCap
        ? `Schedule queue full (${scheduledUsed}/${scheduledCap}). Cancel some from the Scheduled view.`
        : "Send later, up to 29 days out";

    return (
        <motion.div
            initial={{ opacity: 0, y: 12 }}
            animate={{ opacity: 1, y: 0 }}
            exit={{ opacity: 0, y: 8 }}
            transition={{ duration: 0.2, ease: [0.16, 1, 0.3, 1] }}
            className="border-t border-slate-200 bg-white shrink-0 flex flex-col shadow-[0_-6px_24px_-12px_rgba(15,23,42,0.10)]"
        >
            {/* Target strip: one quiet line naming what this composer is
                doing (same visual language as the compose window's header),
                plus the close handle. */}
            <div className="h-8 pl-3.5 pr-1.5 flex items-center gap-2 bg-slate-100/80 border-b border-slate-200 select-none">
                <CornerUpLeftIcon
                    className={cn(
                        "w-3.5 h-3.5 shrink-0",
                        mode === "forward" ? "rotate-180 text-violet-500" : "text-slate-500",
                    )}
                    aria-hidden
                />
                <span
                    className="min-w-0 flex-1 text-[11.5px] text-slate-500 truncate"
                    title={replyToAddr ? `${replyToName} <${replyToAddr}>` : replyToName}
                >
                    <span className="font-semibold text-slate-800">
                        {mode === "forward" ? "Forward" : "Reply"}
                    </span>{" "}
                    to {replyToName}
                    <span className="text-slate-400"> · {replyTargetSubject}</span>
                </span>
                <button
                    type="button"
                    onClick={onClose}
                    aria-label="Close composer"
                    title="Close composer"
                    className="size-6 inline-flex items-center justify-center rounded-md text-slate-400 hover:text-slate-700 hover:bg-slate-200/60 transition-colors shrink-0"
                >
                    <XIcon className="w-3.5 h-3.5" />
                </button>
            </div>

            {/* Header rows. Plain labelled lines matching the compose window:
                quiet inline label, hairline between rows, no label lane or
                divider column. */}
            <div className="shrink-0 bg-white">
                <HeaderRow label="To">
                    <RecipientField value={to} onChange={setTo} placeholder="name@example.com" />
                    {(!showCc || !showBcc) && (
                        <div className="ml-auto flex items-center gap-0.5 shrink-0 self-start pt-px">
                            {!showCc && (
                                <button
                                    type="button"
                                    onClick={() => setShowCc(true)}
                                    className="h-5 px-1 rounded text-[11px] text-slate-400 hover:text-slate-700 transition-colors"
                                >
                                    Cc
                                </button>
                            )}
                            {!showBcc && (
                                <button
                                    type="button"
                                    onClick={() => setShowBcc(true)}
                                    className="h-5 px-1 rounded text-[11px] text-slate-400 hover:text-slate-700 transition-colors"
                                >
                                    Bcc
                                </button>
                            )}
                        </div>
                    )}
                </HeaderRow>

                {showCc && (
                    <HeaderRow
                        label="Cc"
                        onRemove={() => {
                            setCc([]);
                            setShowCc(false);
                        }}
                    >
                        <RecipientField
                            value={cc}
                            onChange={setCc}
                            placeholder="Add Cc recipients"
                        />
                    </HeaderRow>
                )}
                {showBcc && (
                    <HeaderRow
                        label="Bcc"
                        onRemove={() => {
                            setBcc([]);
                            setShowBcc(false);
                        }}
                    >
                        <RecipientField
                            value={bcc}
                            onChange={setBcc}
                            placeholder="Add Bcc recipients"
                        />
                    </HeaderRow>
                )}

                <HeaderRow label="From">
                    {mailbox ? (
                        <div className="inline-flex items-center gap-2 min-w-0">
                            <span className="text-[12.5px] text-slate-800 truncate">
                                {mailbox.name || mailbox.email}
                            </span>
                            <span className="font-mono text-[10.5px] text-slate-400 min-w-0 truncate" title={mailbox.email}>
                                {mailbox.email}
                            </span>
                        </div>
                    ) : (
                        <span className="text-[12px] text-amber-700">
                            No sending mailbox resolved
                        </span>
                    )}
                </HeaderRow>

                <div className="flex items-center gap-2 px-4 border-b border-slate-100">
                    <input
                        type="text"
                        value={subject}
                        onChange={(e) => setSubject(e.target.value)}
                        placeholder="Subject"
                        className="flex-1 min-w-0 h-9 bg-transparent text-[13px] font-medium text-slate-900 placeholder:text-slate-400 placeholder:font-normal outline-none"
                    />
                </div>
            </div>

            {/* Body. AI drafting overlays it instead of pushing layout: a
                light sheen sweeps the textarea while generating and the
                status/review card floats over the bottom edge. */}
            <div className="relative">
            <textarea
                ref={bodyRef}
                value={body}
                onChange={(e) => setBody(e.target.value.slice(0, MAX_BODY_LEN))}
                placeholder={
                    mode === "forward"
                        ? "Add a note (optional). ⌘J for AI, ⌘Enter to send."
                        : "Write your reply. ⌘J for AI, ⌘Enter to send."
                }
                onKeyDown={(e) => {
                    if ((e.metaKey || e.ctrlKey) && e.key === "Enter") {
                        e.preventDefault();
                        if (canSend) handleInstant();
                    } else if (e.key === "Escape") {
                        e.preventDefault();
                        onClose();
                    }
                }}
                className="w-full min-h-[120px] max-h-72 px-4 py-3 text-[13px] text-slate-800 placeholder:text-slate-400 bg-transparent resize-y focus:outline-none"
            />
            {aiDraft.phase === "busy" && (
                <div className="ai-sheen pointer-events-none absolute inset-0" aria-hidden />
            )}
            <AIDraftBar
                ctrl={aiDraft}
                busyLabels={[
                    "Reading the thread…",
                    mode === "forward" ? "Writing your note…" : "Writing your reply…",
                    "Polishing…",
                ]}
            />
            </div>

            {/* Inline AI, where you type: select text and an "Edit with AI"
                pill floats over the selection; with just a caret, a faint
                sparkle rides the current line (or ⌘J) and opens the write
                menu at the cursor — ask AI to write, draft a full reply from
                the thread, or continue the draft. */}
            <TextareaAIEdit
                textareaRef={bodyRef}
                value={body}
                onChange={(next) => setBody(next.slice(0, MAX_BODY_LEN))}
                getContext={() => `Subject: ${subject}\n\n${body}`}
                maxLen={MAX_BODY_LEN}
            />
            <TextareaAICaret
                textareaRef={bodyRef}
                value={body}
                onChange={(next) => setBody(next.slice(0, MAX_BODY_LEN))}
                onDraftReply={() => aiDraft.start()}
                contextHint={`It is a ${mode === "forward" ? "forward note" : "reply"} with the subject "${subject}".`}
                maxLen={MAX_BODY_LEN}
            />

            {/* Signature preview / status. Three branches so the user
                always knows what will (or will not) appear at the
                bottom of their reply on send. */}
            {signatureState.kind === "on" && (
                <div className="mx-4 mb-2 rounded-md border border-emerald-200/60 bg-emerald-50/40 overflow-hidden">
                    <div className="px-3 py-1.5 flex items-center gap-1.5 border-b border-emerald-200/40 bg-emerald-50/60">
                        <PenLineIcon className="w-3 h-3 text-emerald-700" />
                        <span className="text-[10px] uppercase tracking-[0.14em] text-emerald-800 font-semibold">
                            Signature appended on send
                        </span>
                        <span
                            className="ml-auto inline-flex items-center gap-1 text-[10px] text-emerald-700/80"
                            title="Manage this in mailbox settings"
                        >
                            <InfoIcon className="w-2.5 h-2.5" />
                            from {mailbox?.email ?? "this mailbox"}
                        </span>
                    </div>
                    <pre className="px-3 py-2 m-0 font-sans text-[11.5px] text-slate-700 whitespace-pre-wrap leading-relaxed max-h-28 overflow-y-auto md:max-h-none">
                        {signatureState.preview}
                    </pre>
                </div>
            )}
            {signatureState.kind === "off" && (
                <div className="mx-4 mb-2 px-3 py-2 rounded-md border border-amber-200/60 bg-amber-50/50 flex items-start gap-2 text-[11.5px] text-amber-900">
                    <InfoIcon className="w-3 h-3 mt-0.5 shrink-0 text-amber-700" />
                    <span className="leading-snug">
                        A signature is saved for this mailbox but signature
                        sync is off, so it will <strong>not</strong> be
                        appended on send. Turn sync on in mailbox settings
                        to include it automatically.
                    </span>
                </div>
            )}
            {signatureState.kind === "none" && (
                <div className="mx-4 mb-2 px-3 py-1.5 rounded-md border border-dashed border-slate-200 text-[11px] text-slate-400 flex items-center gap-1.5">
                    <PenLineIcon className="w-3 h-3" />
                    No signature on this mailbox. Type one inline or set
                    one in mailbox settings.
                </div>
            )}

            {/* Action bar. flex-wrap so the Send + Schedule + Template
                + Discard chain doesn't overflow on a 360px-wide phone;
                the counter (ml-auto) keeps the right edge whether it
                wraps to a second line or stays on the first. */}
            <div className="px-3 py-2 border-t border-slate-200/60 flex flex-wrap items-center gap-1.5">
                <button
                    type="button"
                    onClick={handleInstant}
                    disabled={!canSend}
                    className="h-7 px-2.5 rounded-md bg-sky-600 hover:bg-sky-700 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
                >
                    {isSending ? (
                        <Loader2Icon className="w-3 h-3 animate-spin" />
                    ) : (
                        <SendIcon className="w-3 h-3" />
                    )}
                    {isSending ? "Sending" : "Send"}
                </button>

                {/* Schedule picker. Direct button trigger (no Tooltip
                    wrapper) so PopoverMenuTrigger's asChild ref cloning
                    actually attaches to a real DOM element. */}
                <PopoverMenu
                    align="start"
                    side="top"
                    open={scheduleOpen}
                    onOpenChange={(o) => {
                        setScheduleOpen(o);
                        if (!o) setCustomMode(false);
                    }}
                >
                    <PopoverMenuTrigger asChild>
                        <button
                            type="button"
                            disabled={!canSend || scheduleAtCap}
                            title={scheduleTooltip}
                            className="h-7 px-2 rounded-md border border-slate-200 hover:border-slate-300 text-slate-700 hover:text-slate-900 text-[12px] inline-flex items-center gap-1 transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
                        >
                            <ClockIcon className="w-3 h-3" />
                            Schedule
                            <ChevronDownIcon className="w-3 h-3 text-slate-400" />
                        </button>
                    </PopoverMenuTrigger>
                    <PopoverMenuContent minWidth={240}>
                        <AnimatePresence mode="wait" initial={false}>
                            {customMode ? (
                                <motion.div
                                    key="custom"
                                    initial={{ opacity: 0 }}
                                    animate={{ opacity: 1 }}
                                    exit={{ opacity: 0 }}
                                    transition={{ duration: 0.12, ease: [0.16, 1, 0.3, 1] }}
                                    className="px-1 py-1 w-[260px]"
                                >
                                    <PopoverMenuLabel>Send at</PopoverMenuLabel>
                                    <div className="mt-1">
                                        <DateTimePicker value={customValue} onChange={setCustomValue} stepMinutes={15} />
                                    </div>
                                    <div className="mt-2 flex items-center gap-1.5">
                                        <button
                                            type="button"
                                            onClick={handleCustom}
                                            disabled={isSending}
                                            className="h-7 px-2.5 rounded-md bg-sky-600 hover:bg-sky-700 text-white text-[12px] font-medium inline-flex items-center gap-1 transition-colors disabled:opacity-50"
                                        >
                                            <CheckIcon className="w-3 h-3" />
                                            Schedule
                                        </button>
                                        <button
                                            type="button"
                                            onClick={() => setCustomMode(false)}
                                            className="h-7 px-2 rounded-md text-slate-500 hover:text-slate-900 hover:bg-slate-100 text-[12px] transition-colors"
                                        >
                                            Back
                                        </button>
                                    </div>
                                </motion.div>
                            ) : (
                                <motion.div
                                    key="presets"
                                    initial={{ opacity: 0 }}
                                    animate={{ opacity: 1 }}
                                    exit={{ opacity: 0 }}
                                    transition={{ duration: 0.12, ease: [0.16, 1, 0.3, 1] }}
                                >
                                    <PopoverMenuLabel>Send at</PopoverMenuLabel>
                                    {SCHEDULE_PRESETS.map((p) => (
                                        <PopoverMenuItem
                                            key={p.label}
                                            onSelect={() => handleSchedule(p.at())}
                                        >
                                            {p.label}
                                        </PopoverMenuItem>
                                    ))}
                                    <PopoverMenuSeparator />
                                    <PopoverMenuItem
                                        onSelect={() => setCustomMode(true)}
                                        closeOnSelect={false}
                                    >
                                        Pick a time
                                    </PopoverMenuItem>
                                </motion.div>
                            )}
                        </AnimatePresence>
                    </PopoverMenuContent>
                </PopoverMenu>

                {/* Template picker. Custom rich rows (not
                    PopoverMenuItem) so each row can run two lines
                    without the wrapper truncating them. */}
                <PopoverMenu
                    align="start"
                    side="top"
                    open={templateOpen}
                    onOpenChange={setTemplateOpen}
                >
                    <PopoverMenuTrigger asChild>
                        <button
                            type="button"
                            title="Drop a saved reply into the body"
                            className="h-7 px-2 rounded-md border border-slate-200 hover:border-slate-300 text-slate-700 hover:text-slate-900 text-[12px] inline-flex items-center gap-1 transition-colors"
                        >
                            <FileTextIcon className="w-3 h-3" />
                            Template
                            <ChevronDownIcon className="w-3 h-3 text-slate-400" />
                        </button>
                    </PopoverMenuTrigger>
                    <PopoverMenuContent minWidth={340} className="max-w-[92vw]">
                        <TemplatePickerContent
                            query={templatesQuery}
                            onPick={(t) => applyTemplate(t.name, t.body_plain, t.subject)}
                            onClose={() => setTemplateOpen(false)}
                        />
                    </PopoverMenuContent>
                </PopoverMenu>

                <InsertBookingLink
                    email={to[0]}
                    onInsert={(text) =>
                        setBody((b) => (b.trim() ? `${b.trimEnd()}\n\n${text}` : text).slice(0, MAX_BODY_LEN))
                    }
                />

                {body && (
                    <button
                        type="button"
                        onClick={() => setBody("")}
                        className="h-7 px-2 rounded-md text-slate-500 hover:text-slate-900 hover:bg-slate-100 text-[12px] transition-colors"
                    >
                        Discard
                    </button>
                )}

                <span
                    className={cn(
                        "ml-auto inline-flex items-center gap-1 h-5 px-1.5 rounded text-[10px] font-medium",
                        signatureState.kind === "on" &&
                            "bg-emerald-50 text-emerald-700",
                        signatureState.kind === "off" &&
                            "bg-amber-50 text-amber-800",
                        signatureState.kind === "none" &&
                            "bg-slate-100 text-slate-500",
                    )}
                    title={
                        signatureState.kind === "on"
                            ? "Your mailbox signature will be appended to this reply on send."
                            : signatureState.kind === "off"
                              ? "A signature exists but sync is off, so it will not be appended."
                              : "No signature configured for this mailbox."
                    }
                >
                    <PenLineIcon className="w-2.5 h-2.5" />
                    {signatureState.kind === "on" && "Signature on"}
                    {signatureState.kind === "off" && "Signature off"}
                    {signatureState.kind === "none" && "No signature"}
                </span>

                <span className="font-mono text-[10px] text-slate-400 tabular-nums">
                    {body.length}/{MAX_BODY_LEN}
                </span>
            </div>
        </motion.div>
    );
}

// HeaderRow : one plain labelled line in the composer header, matching the
// compose window: quiet inline label, hairline underneath, nothing else.
function HeaderRow({
    label,
    onRemove,
    children,
}: {
    label: string;
    onRemove?: () => void;
    children: React.ReactNode;
}) {
    return (
        <div className="flex items-start gap-2 px-4 py-[7px] border-b border-slate-100">
            <span className="w-9 shrink-0 pt-[3px] text-[11px] text-slate-400">{label}</span>
            <div className="flex-1 min-w-0 flex flex-wrap items-center gap-1.5">{children}</div>
            {onRemove && (
                <button
                    type="button"
                    onClick={onRemove}
                    aria-label={`Remove ${label}`}
                    className="size-5 inline-flex items-center justify-center rounded text-slate-300 hover:text-slate-600 hover:bg-slate-100 transition-colors shrink-0"
                >
                    <XIcon className="w-3 h-3" />
                </button>
            )}
        </div>
    );
}

// RecipientField : chip-based editor. Enter / "," / Tab / blur commits
// the in-progress text as a chip. Backspace on empty input removes the
// last chip. Chips wrap to multiple lines via the parent's
// `flex-wrap`, so Cc/Bcc with many recipients stays readable.
function RecipientField({
    value,
    onChange,
    placeholder,
}: {
    value: string[];
    onChange: (next: string[]) => void;
    placeholder: string;
}) {
    const [input, setInput] = React.useState("");

    const commit = (raw: string) => {
        const trimmed = raw.trim().replace(/,$/, "").trim();
        if (!trimmed) return;
        if (value.includes(trimmed)) {
            setInput("");
            return;
        }
        onChange([...value, trimmed]);
        setInput("");
    };

    return (
        <>
            {value.map((v) => (
                <span
                    key={v}
                    className={cn(
                        "inline-flex items-center gap-1 h-5 pl-1.5 pr-0.5 rounded-md text-[11px] font-medium max-w-full min-w-0 border",
                        looksLikeEmail(v)
                            ? "bg-sky-50 text-sky-800 border-sky-200"
                            : "bg-rose-50 text-rose-800 border-rose-200",
                    )}
                    title={v}
                >
                    <span className="font-mono truncate">{v}</span>
                    <button
                        type="button"
                        onClick={() => onChange(value.filter((x) => x !== v))}
                        aria-label={`Remove ${v}`}
                        className="size-4 shrink-0 inline-flex items-center justify-center rounded hover:bg-black/10"
                    >
                        <XIcon className="w-2.5 h-2.5" />
                    </button>
                </span>
            ))}
            <input
                type="email"
                value={input}
                onChange={(e) => setInput(e.target.value)}
                placeholder={value.length === 0 ? placeholder : ""}
                onKeyDown={(e) => {
                    if (e.key === "Enter" || e.key === "," || e.key === "Tab") {
                        if (input.trim()) {
                            e.preventDefault();
                            commit(input);
                        }
                    } else if (e.key === "Backspace" && !input && value.length > 0) {
                        e.preventDefault();
                        onChange(value.slice(0, -1));
                    }
                }}
                onBlur={() => commit(input)}
                onPaste={(e) => {
                    const text = e.clipboardData.getData("text");
                    if (text.includes(",") || text.includes(" ")) {
                        e.preventDefault();
                        const parts = text
                            .split(/[,\s]+/)
                            .map((s) => s.trim())
                            .filter(Boolean);
                        const fresh = [...value];
                        for (const p of parts) {
                            if (!fresh.includes(p)) fresh.push(p);
                        }
                        onChange(fresh);
                    }
                }}
                className="flex-1 min-w-[14ch] h-5 bg-transparent text-[11.5px] text-slate-900 placeholder:text-slate-400 outline-none font-mono"
            />
        </>
    );
}
