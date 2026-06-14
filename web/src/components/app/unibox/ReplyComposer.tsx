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
    CalendarPlusIcon,
    CheckIcon,
    ChevronDownIcon,
    ClockIcon,
    CornerUpLeftIcon,
    FileTextIcon,
    InfoIcon,
    Loader2Icon,
    PenLineIcon,
    SearchIcon,
    SendIcon,
    SettingsIcon,
    XIcon,
} from "lucide-react";
import { Link } from "react-router-dom";
import toast from "react-hot-toast";
import sendReply from "@/lib/api/client/app/unibox/sendReply";
import { DateTimePicker } from "@/components/ui/DateTimePicker";
import useTemplates from "@/lib/api/hooks/app/templates/useTemplates";
import useUniboxOverview from "@/lib/api/hooks/app/unibox/useUniboxOverview";
import useIntegrationConnections from "@/lib/api/hooks/app/integrations/useIntegrationConnections";
import { bookingURL, prefilledBookingURL } from "@/lib/api/models/app/integrations/Integration";
import { useAppStore } from "@/stores";
import type Template from "@/lib/api/models/app/templates/Template";
import WriteWithAI from "@/components/app/campaigns/sequences/WriteWithAI";
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

// InsertBookingLink drops the org's scheduling link (prefilled with the
// recipient's email) into the reply body. Renders only when a Calendly /
// Cal.com link is configured, so the action never shows as a dead button.
function InsertBookingLink({
    email,
    onInsert,
}: {
    email?: string;
    onInsert: (text: string) => void;
}) {
    const { data } = useIntegrationConnections();
    const url = (data?.connections ?? [])
        .map((c) => bookingURL(c))
        .find((u): u is string => !!u);
    if (!url) return null;

    const cleanEmail = email ? bareEmail(email) : undefined;
    return (
        <button
            type="button"
            title="Insert your booking link, prefilled for this contact"
            onClick={() => {
                onInsert(prefilledBookingURL(url, cleanEmail));
                toast.success("Booking link added");
            }}
            className="h-7 px-2 rounded-md border border-slate-200 hover:border-slate-300 text-slate-700 hover:text-slate-900 text-[12px] inline-flex items-center gap-1 transition-colors"
        >
            <CalendarPlusIcon className="w-3 h-3" />
            Booking link
        </button>
    );
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
            {/* Target message preview : the message being replied to /
                forwarded, plus a close handle to dismiss the composer
                entirely. */}
            <div className="px-4 pt-3 pb-2 flex items-start gap-3 bg-slate-50/60 border-b border-slate-200/70">
                <span
                    className={cn(
                        "size-7 rounded-full flex items-center justify-center text-[10px] font-semibold shrink-0",
                        mode === "forward"
                            ? "bg-violet-100 text-violet-700"
                            : "bg-sky-100 text-sky-700",
                    )}
                    aria-hidden
                >
                    <CornerUpLeftIcon
                        className={cn("w-3.5 h-3.5", mode === "forward" && "rotate-180")}
                    />
                </span>
                <div className="min-w-0 flex-1">
                    <div className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">
                        {mode === "forward" ? "Forwarding" : "Replying to"}
                    </div>
                    <div className="flex items-baseline gap-2 mt-0.5 min-w-0">
                        <span className="text-[12.5px] font-semibold text-slate-900 truncate">
                            {replyToName}
                        </span>
                        {replyToAddr && (
                            <span className="font-mono text-[10.5px] text-slate-500 truncate">
                                {replyToAddr}
                            </span>
                        )}
                    </div>
                    <div className="text-[11.5px] text-slate-500 mt-0.5 truncate">
                        {replyTargetSubject}
                    </div>
                </div>
                <button
                    type="button"
                    onClick={onClose}
                    aria-label="Close composer"
                    title="Close composer"
                    className="size-7 inline-flex items-center justify-center rounded-md text-slate-400 hover:text-slate-900 hover:bg-slate-200/60 transition-colors shrink-0"
                >
                    <XIcon className="w-3.5 h-3.5" />
                </button>
            </div>

            {/* Header strip : From / To / Cc / Bcc / Subject. Labels
                sit in a wider, hairline-divided column so values get
                proper breathing room. */}
            <div className="px-4 py-2.5 border-b border-slate-200/70 divide-y divide-slate-200/40 bg-white">
                <HeaderRow label="From">
                    {mailbox ? (
                        <div className="inline-flex items-center gap-2 min-w-0">
                            <span className="text-[12.5px] text-slate-900 font-medium truncate">
                                {mailbox.name || mailbox.email}
                            </span>
                            <span
                                className="font-mono text-[10.5px] text-slate-500 bg-slate-50 px-1.5 h-4 inline-flex items-center rounded border border-slate-200 min-w-0 truncate"
                                title={mailbox.email}
                            >
                                {mailbox.email}
                            </span>
                        </div>
                    ) : (
                        <span className="text-[12px] text-amber-700">
                            No sending mailbox resolved
                        </span>
                    )}
                    <div className="ml-auto flex items-center gap-1">
                        {!showCc && (
                            <button
                                type="button"
                                onClick={() => setShowCc(true)}
                                className="h-5 px-1.5 rounded text-[10.5px] text-slate-500 hover:text-slate-900 hover:bg-slate-100 transition-colors"
                            >
                                + Cc
                            </button>
                        )}
                        {!showBcc && (
                            <button
                                type="button"
                                onClick={() => setShowBcc(true)}
                                className="h-5 px-1.5 rounded text-[10.5px] text-slate-500 hover:text-slate-900 hover:bg-slate-100 transition-colors"
                            >
                                + Bcc
                            </button>
                        )}
                    </div>
                </HeaderRow>

                <HeaderRow label="To">
                    <RecipientField value={to} onChange={setTo} placeholder="name@example.com" />
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

                <HeaderRow label="Subject">
                    <input
                        type="text"
                        value={subject}
                        onChange={(e) => setSubject(e.target.value)}
                        placeholder="Subject"
                        className="flex-1 min-w-0 h-6 bg-transparent text-[12.5px] text-slate-900 placeholder:text-slate-400 outline-none"
                    />
                </HeaderRow>
            </div>

            {/* Body */}
            <textarea
                value={body}
                onChange={(e) => setBody(e.target.value.slice(0, MAX_BODY_LEN))}
                placeholder={
                    mode === "forward"
                        ? "Add a note (optional). ⌘ + Enter to send."
                        : "Write your reply. ⌘ + Enter to send."
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
                className="w-full min-h-[120px] max-h-72 px-5 py-3 text-[13px] text-slate-800 placeholder:text-slate-400 bg-transparent resize-y focus:outline-none"
            />

            {/* Signature preview / status. Three branches so the user
                always knows what will (or will not) appear at the
                bottom of their reply on send. */}
            {signatureState.kind === "on" && (
                <div className="mx-3 sm:mx-5 mb-2 rounded-md border border-emerald-200/60 bg-emerald-50/40 overflow-hidden">
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
                <div className="mx-3 sm:mx-5 mb-2 px-3 py-2 rounded-md border border-amber-200/60 bg-amber-50/50 flex items-start gap-2 text-[11.5px] text-amber-900">
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
                <div className="mx-3 sm:mx-5 mb-2 px-3 py-1.5 rounded-md border border-dashed border-slate-200 text-[11px] text-slate-400 flex items-center gap-1.5">
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

                <WriteWithAI
                    onInsert={(text) =>
                        setBody((b) => (b.trim() ? `${b.trimEnd()}\n\n${text}` : text).slice(0, MAX_BODY_LEN))
                    }
                />

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

// HeaderRow : one labelled inline field in the composer header.
// Wider label column + hairline divider gives the value its own
// visual lane. Items inside wrap so many recipient chips don't push
// the field off-screen.
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
        <div className="flex items-start gap-3 py-1.5">
            <span className="w-14 shrink-0 pt-[3px] text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">
                {label}
            </span>
            <span className="self-stretch w-px bg-slate-200 shrink-0" aria-hidden />
            <div className="flex-1 min-w-0 flex flex-wrap items-center gap-1.5">{children}</div>
            {onRemove && (
                <button
                    type="button"
                    onClick={onRemove}
                    aria-label={`Remove ${label}`}
                    className="size-5 inline-flex items-center justify-center rounded text-slate-400 hover:text-slate-700 hover:bg-slate-100 transition-colors shrink-0"
                >
                    <XIcon className="w-3 h-3" />
                </button>
            )}
        </div>
    );
}

// TemplatePickerContent : rich template list rendered inside the
// PopoverMenu. Custom rows (not PopoverMenuItem) so each entry can
// run two lines, show a badge, and adopt a hover accent. Includes
// an inline search when the user has more than a handful of
// templates, plus skeleton + empty + error states.
function TemplatePickerContent({
    query,
    onPick,
    onClose,
}: {
    query: ReturnType<typeof useTemplates>;
    onPick: (t: Template) => void;
    onClose: () => void;
}) {
    const [search, setSearch] = React.useState("");
    const all = React.useMemo(() => query.data ?? [], [query.data]);
    const showSearch = all.length > 5;
    const filtered = React.useMemo(() => {
        const q = search.trim().toLowerCase();
        if (!q) return all;
        return all.filter((t) => {
            const hay = `${t.name} ${t.subject} ${t.body_plain}`.toLowerCase();
            return hay.includes(q);
        });
    }, [all, search]);

    return (
        <div className="w-[340px] max-w-[92vw]">
            <div className="px-3 pt-2.5 pb-1 flex items-center justify-between gap-2">
                <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-semibold">
                    Templates
                </span>
                {all.length > 0 && (
                    <span className="font-mono text-[10px] text-slate-400 tabular-nums">
                        {filtered.length === all.length
                            ? all.length
                            : `${filtered.length}/${all.length}`}
                    </span>
                )}
            </div>

            {showSearch && (
                <div className="px-2 pb-2">
                    <div className="flex items-center gap-1.5 px-2 h-7 rounded-md border border-slate-200 bg-slate-50 focus-within:bg-white focus-within:border-sky-400 focus-within:ring-2 focus-within:ring-sky-100 transition-colors">
                        <SearchIcon className="w-3 h-3 text-slate-400 shrink-0" />
                        <input
                            type="text"
                            value={search}
                            onChange={(e) => setSearch(e.target.value)}
                            placeholder="Search templates"
                            autoFocus
                            className="flex-1 min-w-0 h-6 bg-transparent text-[12px] text-slate-900 placeholder:text-slate-400 outline-none"
                        />
                        {search && (
                            <button
                                type="button"
                                onClick={() => setSearch("")}
                                className="size-4 inline-flex items-center justify-center rounded text-slate-400 hover:text-slate-700 hover:bg-slate-200/60 shrink-0"
                                aria-label="Clear search"
                            >
                                <XIcon className="w-2.5 h-2.5" />
                            </button>
                        )}
                    </div>
                </div>
            )}

            {query.isPending ? (
                <div className="px-2 pb-2 space-y-1">
                    {[0, 1, 2].map((i) => (
                        <div
                            key={i}
                            className="h-12 rounded-md bg-slate-100/70 animate-pulse"
                        />
                    ))}
                </div>
            ) : query.isError ? (
                <div className="px-3 py-3 flex items-start gap-2 text-[11.5px] text-rose-600 bg-rose-50/60 mx-2 mb-2 rounded-md border border-rose-200/60">
                    <span>Couldn't load templates. Try again in a moment.</span>
                </div>
            ) : all.length === 0 ? (
                <TemplatePickerEmpty onClose={onClose} />
            ) : filtered.length === 0 ? (
                <div className="px-3 py-6 text-center">
                    <p className="text-[12px] text-slate-500">
                        No templates match &ldquo;{search}&rdquo;.
                    </p>
                    <button
                        type="button"
                        onClick={() => setSearch("")}
                        className="mt-2 text-[11.5px] text-sky-700 hover:text-sky-900 font-medium"
                    >
                        Clear search
                    </button>
                </div>
            ) : (
                <div className="max-h-[300px] overflow-y-auto px-1 pb-1 space-y-0.5">
                    {filtered.map((t) => (
                        <TemplateRow
                            key={t.id}
                            template={t}
                            onPick={() => {
                                onPick(t);
                                onClose();
                            }}
                        />
                    ))}
                </div>
            )}

            {all.length > 0 && (
                <div className="border-t border-slate-200/70 px-2 py-1.5 flex items-center justify-between">
                    <Link
                        to="/app/templates"
                        onClick={onClose}
                        className="inline-flex items-center gap-1.5 h-6 px-1.5 rounded text-[11px] text-slate-500 hover:text-slate-900 hover:bg-slate-100 transition-colors"
                    >
                        <SettingsIcon className="w-3 h-3" />
                        Manage templates
                    </Link>
                    <span className="font-mono text-[9.5px] uppercase tracking-[0.14em] text-slate-400">
                        Click to insert
                    </span>
                </div>
            )}
        </div>
    );
}

// TemplateRow : one template entry. Two compact lines, no icon, no
// left accent bar : just name on top, single-line body preview
// underneath. Hover and active states are conveyed by the row
// background alone.
function TemplateRow({
    template,
    onPick,
}: {
    template: Template;
    onPick: () => void;
}) {
    const bodyPreview = (template.body_plain ?? "")
        .replace(/\s+/g, " ")
        .trim()
        .slice(0, 120);

    return (
        <button
            type="button"
            onClick={onPick}
            className="w-full text-left rounded-md px-2.5 py-1.5 flex flex-col gap-0.5 hover:bg-slate-50 active:bg-slate-100 transition-colors focus:outline-none focus-visible:ring-2 focus-visible:ring-sky-200"
        >
            <span className="text-[12.5px] font-medium text-slate-900 truncate">
                {template.name}
            </span>
            {bodyPreview && (
                <span className="text-[11px] text-slate-400 truncate leading-snug">
                    {bodyPreview}
                </span>
            )}
        </button>
    );
}

// TemplatePickerEmpty : the zero-state surface. A blank list is a
// teaching moment, not a dead-end, so we link straight to the
// templates settings page where the user can create their first one.
function TemplatePickerEmpty({ onClose }: { onClose: () => void }) {
    return (
        <div className="px-4 pb-4 pt-2 text-center">
            <div className="size-9 rounded-lg bg-slate-100 text-slate-400 inline-flex items-center justify-center mb-2">
                <FileTextIcon className="w-4 h-4" />
            </div>
            <p className="text-[12.5px] font-medium text-slate-900">
                No templates yet
            </p>
            <p className="text-[11px] text-slate-500 mt-1 leading-relaxed max-w-[34ch] mx-auto">
                Save your most-used replies once and drop them into any
                conversation with two clicks.
            </p>
            <Link
                to="/app/templates"
                onClick={onClose}
                className="inline-flex items-center gap-1.5 h-7 px-2.5 mt-3 rounded-md bg-sky-600 hover:bg-sky-700 text-white text-[12px] font-medium transition-colors"
            >
                <SettingsIcon className="w-3 h-3" />
                Create a template
            </Link>
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
