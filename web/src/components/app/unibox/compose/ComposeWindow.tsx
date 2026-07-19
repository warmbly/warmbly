// ComposeWindow — the global "new email" surface. A Gmail-style docked window
// (bottom-right on desktop, near-fullscreen on phones) opened from the unibox
// header, the n shortcut, or any surface that calls useComposeStore.
//
// What makes it more than a form:
//   - From defaults to Auto: the backend scores every mailbox for the current
//     recipient (conversation affinity, remaining budget, auth health) and the
//     picker shows the pick with its reason before anything sends.
//   - Typing a recipient searches the CRM; a known contact surfaces inline and
//     unlocks the History panel: every conversation with them, a Sent tab, and
//     subject search, without leaving the draft.
//   - The same in-composer AI as replies: ⌘J at the caret to write or draft
//     the whole email, selection editing, the sheen + typewriter draft bar.
//   - Suppressed recipients are flagged before send, not bounced after.

import React from "react";
import { AnimatePresence, motion } from "framer-motion";
import toast from "react-hot-toast";
import {
    CheckIcon,
    ChevronDownIcon,
    ClockIcon,
    HistoryIcon,
    Loader2Icon,
    OctagonAlertIcon,
    PenLineIcon,
    SendIcon,
    XIcon,
} from "lucide-react";
import { useComposeStore } from "@/hooks/useComposeStore";
import { useConfirm } from "@/hooks/context/confirm";
import { useAppStore } from "@/stores";
import useComposeCandidates from "@/lib/api/hooks/app/unibox/useComposeCandidates";
import useComposeSend from "@/lib/api/hooks/app/unibox/useComposeSend";
import useGenerateWrite from "@/lib/api/hooks/app/generation/useGenerateWrite";
import AIDraftBar, { useAIDraft } from "@/components/app/ai/AIDraftBar";
import TextareaAIEdit from "@/components/app/ai/TextareaAIEdit";
import TextareaAICaret from "@/components/app/ai/TextareaAICaret";
import ContactRecipientField from "./ContactRecipientField";
import MailboxPicker from "./MailboxPicker";
import ComposeHistoryPanel from "./ComposeHistoryPanel";
import { DateTimePicker } from "@/components/ui/DateTimePicker";
import { Kbd } from "@/components/ui/shortcut-tooltip";
import {
    PopoverMenu,
    PopoverMenuContent,
    PopoverMenuItem,
    PopoverMenuLabel,
    PopoverMenuSeparator,
    PopoverMenuTrigger,
} from "@/components/ui/popover-menu";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import { cn } from "@/lib/utils";

const MAX_BODY_LEN = 4000;
const MAX_SCHEDULE_MS = 29 * 24 * 60 * 60 * 1000;

function looksLikeEmail(s: string): boolean {
    return /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(s.trim());
}

function bareEmail(s: string): string {
    const m = s.match(/<([^>]+)>/);
    if (m) return m[1].trim();
    return s.trim();
}

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
function toLocalInput(d: Date): string {
    const pad = (n: number) => String(n).padStart(2, "0");
    return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
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

const SCHEDULE_PRESETS: { label: string; at: () => Date }[] = [
    { label: "In 1 hour", at: () => offsetHours(1) },
    { label: "In 3 hours", at: () => offsetHours(3) },
    { label: "Tomorrow 9:00", at: () => atHour(1, 9) },
    { label: "Tomorrow 17:00", at: () => atHour(1, 17) },
];

export default function ComposeWindow() {
    const open = useComposeStore((s) => s.open);
    const prefillTo = useComposeStore((s) => s.prefillTo);

    return (
        <AnimatePresence>
            {open && <ComposeWindowInner key="compose" prefillTo={prefillTo} />}
        </AnimatePresence>
    );
}

function ComposeWindowInner({ prefillTo }: { prefillTo: string | null }) {
    const closeCompose = useComposeStore((s) => s.closeCompose);
    const confirm = useConfirm();
    const accounts = useAppStore((s) => s.emails);

    const [to, setTo] = React.useState<string[]>(prefillTo ? [prefillTo] : []);
    const [cc, setCc] = React.useState<string[]>([]);
    const [bcc, setBcc] = React.useState<string[]>([]);
    const [showCc, setShowCc] = React.useState(false);
    const [showBcc, setShowBcc] = React.useState(false);
    const [subject, setSubject] = React.useState("");
    const [body, setBody] = React.useState("");
    const [accountSel, setAccountSel] = React.useState("auto");
    const [historyOpen, setHistoryOpen] = React.useState(false);
    const [isSending, setIsSending] = React.useState(false);
    const [scheduleOpen, setScheduleOpen] = React.useState(false);
    const [customMode, setCustomMode] = React.useState(false);
    const [customValue, setCustomValue] = React.useState(() => toLocalInput(offsetHours(2)));

    const bodyRef = React.useRef<HTMLTextAreaElement>(null);

    // Candidate scoring keys off the first recipient. Empty address still
    // lists the mailboxes (no affinity signal yet).
    const primary = to.length > 0 ? bareEmail(to[0]) : "";
    const candidatesQ = useComposeCandidates(primary);
    const candidates = candidatesQ.data;

    const contact = candidates?.contact ?? null;
    const contactDisplay = contact
        ? `${contact.first_name ?? ""} ${contact.last_name ?? ""}`.trim() || contact.email
        : "";
    const suppressed = !!candidates?.suppression && to.length > 0;

    // The mailbox that would actually send (explicit pick, or Auto's
    // resolution) — drives the signature chip.
    const resolvedAccount = React.useMemo(() => {
        const list = candidates?.accounts ?? [];
        const picked =
            accountSel === "auto" ? (list.find((a) => a.recommended) ?? list[0]) : list.find((a) => a.id === accountSel);
        if (!picked) return undefined;
        return accounts.find((a) => a.id === picked.id);
    }, [accounts, accountSel, candidates]);

    const affinity = React.useMemo(() => {
        const withHistory = (candidates?.accounts ?? []).filter((a) => a.history_messages > 0);
        if (withHistory.length === 0) return undefined;
        const top = withHistory.reduce((a, b) => (b.history_messages > a.history_messages ? b : a));
        return `Usually from ${top.email}`;
    }, [candidates]);

    // AI draft of the whole email, grounded in whatever we know about the
    // recipient. Same draft-bar review flow as replies.
    const writeMut = useGenerateWrite();
    const buildDraftPrompt = React.useCallback(
        (instruction?: string) => {
            const lines = [
                instruction?.trim() ||
                    "Write a short, specific outbound email that opens a conversation.",
                "",
                "You are writing a complete, ready-to-send email body. Return ONLY the body text: no subject line, no signature, and no placeholders like [Name] — if a detail is unknown, write around it.",
            ];
            const ctx: string[] = [];
            if (to.length) ctx.push(`Recipient: ${to.join(", ")}`);
            if (contactDisplay) ctx.push(`Recipient name: ${contactDisplay}`);
            if (contact?.company) ctx.push(`Recipient company: ${contact.company}`);
            if (subject.trim()) ctx.push(`Subject: ${subject.trim()}`);
            if (ctx.length) lines.push("", ...ctx);
            return lines.join("\n");
        },
        [contact?.company, contactDisplay, subject, to],
    );
    const generateDraft = React.useCallback(
        (instruction?: string) => writeMut.mutateAsync({ prompt: buildDraftPrompt(instruction) }),
        [buildDraftPrompt, writeMut],
    );
    const aiDraft = useAIDraft({
        value: body,
        onChange: setBody,
        generate: generateDraft,
        maxLen: MAX_BODY_LEN,
    });

    // Subject suggestion: one cheap generation over the drafted body.
    const subjectMut = useGenerateWrite();
    const suggestSubject = async () => {
        if (!body.trim() || subjectMut.isPending) return;
        try {
            const res = await subjectMut.mutateAsync({
                prompt: `Write ONE short email subject line (max 8 words) for the email below. Return only the subject text, no quotes.\n\n${body.trim().slice(0, 2000)}`,
            });
            const line = res.text.trim().split("\n")[0].replace(/^["']|["']$/g, "");
            if (line) setSubject(line);
        } catch (e) {
            const err = e as AppError;
            toast.error(err?.status === 402 ? "You're out of AI credits." : buildError(err));
        }
    };

    const sendMut = useComposeSend();
    const trimmedBody = body.trim();
    const canSend =
        to.length > 0 &&
        to.every(looksLikeEmail) &&
        !!subject.trim() &&
        !!trimmedBody &&
        !suppressed &&
        !isSending;

    const dirty = to.length > 0 || cc.length > 0 || bcc.length > 0 || !!subject.trim() || !!trimmedBody;
    const requestClose = React.useCallback(() => {
        if (dirty) {
            confirm.show("Discard this draft?", async () => closeCompose());
        } else {
            closeCompose();
        }
    }, [closeCompose, confirm, dirty]);

    const send = async (scheduledAt?: Date) => {
        if (!canSend) {
            if (to.length === 0) toast.error("Add a recipient");
            else if (!to.every(looksLikeEmail)) toast.error("Recipient email looks invalid");
            else if (suppressed) toast.error("This recipient is suppressed");
            else if (!subject.trim()) toast.error("Add a subject");
            else if (!trimmedBody) toast.error("Body is empty");
            return;
        }
        setIsSending(true);
        try {
            const res = await sendMut.mutateAsync({
                email_account_id: accountSel === "auto" ? undefined : accountSel,
                to,
                cc: cc.length ? cc : undefined,
                bcc: bcc.length ? bcc : undefined,
                subject: subject.trim(),
                body_plain: trimmedBody,
                body_html: trimmedBody.replace(/\n/g, "<br />"),
                ...(scheduledAt
                    ? { send_mode: "scheduled" as const, scheduled_at: scheduledAt.toISOString() }
                    : { send_mode: "instant" as const }),
            });
            toast.success(
                scheduledAt
                    ? `Scheduled for ${formatFriendly(scheduledAt)} from ${res.account_email}`
                    : `Sent from ${res.account_email}${res.auto ? " · picked automatically" : ""}`,
            );
            closeCompose();
        } catch (e) {
            toast.error(buildError(e as AppError));
        } finally {
            setIsSending(false);
        }
    };

    const handleSchedule = (d: Date) => {
        if (!Number.isFinite(d.getTime()) || d.getTime() <= Date.now() + 5_000) {
            toast.error("Pick a future time (a few seconds out, please)");
            return;
        }
        if (d.getTime() - Date.now() > MAX_SCHEDULE_MS) {
            toast.error("Scheduled send can't be more than 29 days out");
            return;
        }
        setScheduleOpen(false);
        setCustomMode(false);
        void send(d);
    };

    const showHistory = historyOpen && !!primary;

    return (
        <motion.div
            initial={{ opacity: 0, y: 24, scale: 0.98 }}
            animate={{ opacity: 1, y: 0, scale: 1 }}
            exit={{ opacity: 0, y: 16, scale: 0.98 }}
            transition={{ type: "spring", stiffness: 480, damping: 38 }}
            className="fixed z-[70] inset-x-2 bottom-2 sm:inset-x-auto sm:right-4 sm:bottom-4 flex items-stretch rounded-xl border border-slate-200 bg-white shadow-2xl overflow-hidden max-h-[min(660px,calc(100dvh-1rem))]"
            onKeyDown={(e) => {
                if (e.key === "Escape") {
                    // Floating AI layers portal to <body> and stop their own
                    // Escape; reaching here means nothing else claimed it.
                    e.stopPropagation();
                    requestClose();
                }
            }}
        >
            {/* ── Main column ─────────────────────────────────────────── */}
            <div className="flex flex-col w-full sm:w-[540px] min-h-0">
                <div className="shrink-0 h-9 pl-3.5 pr-1.5 flex items-center gap-2 bg-slate-900">
                    <span className="text-[12px] font-medium text-white">New email</span>
                    {contact && (
                        <span className="hidden sm:inline text-[11px] text-white/50 truncate min-w-0">
                            · {contactDisplay}
                            {contact.company ? `, ${contact.company}` : ""}
                        </span>
                    )}
                    <div className="ml-auto flex items-center gap-0.5 shrink-0">
                        <button
                            type="button"
                            onClick={() => setHistoryOpen((o) => !o)}
                            disabled={!primary}
                            title={
                                primary
                                    ? "Conversations and sent mail with this recipient"
                                    : "Add a recipient to see your history with them"
                            }
                            className={cn(
                                "hidden sm:inline-flex size-6 rounded-md items-center justify-center transition-colors disabled:opacity-30",
                                showHistory
                                    ? "bg-white/20 text-white"
                                    : "text-white/60 hover:text-white hover:bg-white/10",
                            )}
                        >
                            <HistoryIcon className="w-3.5 h-3.5" />
                        </button>
                        <button
                            type="button"
                            onClick={requestClose}
                            aria-label="Close composer"
                            className="size-6 inline-flex items-center justify-center rounded-md text-white/60 hover:text-white hover:bg-white/10 transition-colors"
                        >
                            <XIcon className="w-3.5 h-3.5" />
                        </button>
                    </div>
                </div>

                {/* Header rows. Plain labelled lines, Gmail-style: hairline
                    between rows, no label lane or divider column, subject
                    unlabelled and slightly heavier. */}
                <div className="shrink-0">
                    <ComposeRow label="To">
                        <ContactRecipientField
                            value={to}
                            onChange={setTo}
                            placeholder="Search contacts or type an email"
                            autoFocus={!prefillTo}
                        />
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
                    </ComposeRow>

                    {showCc && (
                        <ComposeRow
                            label="Cc"
                            onRemove={() => {
                                setCc([]);
                                setShowCc(false);
                            }}
                        >
                            <ContactRecipientField value={cc} onChange={setCc} placeholder="Add Cc recipients" />
                        </ComposeRow>
                    )}
                    {showBcc && (
                        <ComposeRow
                            label="Bcc"
                            onRemove={() => {
                                setBcc([]);
                                setShowBcc(false);
                            }}
                        >
                            <ContactRecipientField value={bcc} onChange={setBcc} placeholder="Add Bcc recipients" />
                        </ComposeRow>
                    )}

                    <ComposeRow label="From">
                        <MailboxPicker
                            value={accountSel}
                            onChange={setAccountSel}
                            candidates={candidates}
                            loading={candidatesQ.isPending}
                        />
                    </ComposeRow>

                    <div className="flex items-center gap-2 px-3.5 border-b border-slate-100">
                        <input
                            type="text"
                            value={subject}
                            onChange={(e) => setSubject(e.target.value)}
                            placeholder="Subject"
                            className="flex-1 min-w-0 h-9 bg-transparent text-[13px] font-medium text-slate-900 placeholder:text-slate-400 placeholder:font-normal outline-none"
                        />
                        {body.trim() && !subject.trim() && (
                            <button
                                type="button"
                                onClick={suggestSubject}
                                disabled={subjectMut.isPending}
                                title="Suggest a subject from the body (from 1 credit)"
                                className="shrink-0 h-5 px-1.5 rounded inline-flex items-center gap-1 text-[10.5px] text-slate-400 hover:text-slate-700 hover:bg-slate-100 transition-colors disabled:opacity-50"
                            >
                                {subjectMut.isPending && (
                                    <Loader2Icon className="w-2.5 h-2.5 animate-spin" />
                                )}
                                Suggest
                            </button>
                        )}
                    </div>
                </div>

                {suppressed && (
                    <div className="shrink-0 mx-3.5 mt-2 px-2.5 py-1.5 rounded-md border border-rose-200 bg-rose-50 flex items-start gap-1.5 text-[11.5px] text-rose-800">
                        <OctagonAlertIcon className="w-3 h-3 mt-0.5 shrink-0" />
                        <span className="leading-snug">
                            {primary} is suppressed for this workspace
                            {candidates?.suppression?.reason ? ` (${candidates.suppression.reason})` : ""}. Sending is blocked to protect your reputation.
                        </span>
                    </div>
                )}

                {/* Body + in-composer AI */}
                <div className="relative flex-1 min-h-0 flex flex-col">
                    <textarea
                        ref={bodyRef}
                        value={body}
                        onChange={(e) => setBody(e.target.value.slice(0, MAX_BODY_LEN))}
                        placeholder="Write your email…"
                        onKeyDown={(e) => {
                            if ((e.metaKey || e.ctrlKey) && e.key === "Enter") {
                                e.preventDefault();
                                void send();
                            }
                        }}
                        className="w-full flex-1 min-h-[160px] px-4 py-3 text-[13px] text-slate-800 placeholder:text-slate-400 bg-transparent resize-none focus:outline-none"
                    />
                    {aiDraft.phase === "busy" && (
                        <div className="ai-sheen pointer-events-none absolute inset-0" aria-hidden />
                    )}
                    <AIDraftBar
                        ctrl={aiDraft}
                        busyLabels={[
                            contactDisplay
                                ? `Thinking about ${contactDisplay}…`
                                : "Thinking about your recipient…",
                            "Writing your email…",
                            "Polishing…",
                        ]}
                    />
                </div>
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
                    draftLabel="Draft this email with AI"
                    draftCost="from 1 credit"
                    contextHint={`It is a new outbound email${contactDisplay ? ` to ${contactDisplay}` : ""}${subject.trim() ? ` with the subject "${subject.trim()}"` : ""}.`}
                    maxLen={MAX_BODY_LEN}
                />

                {/* Action bar */}
                <div className="shrink-0 px-3 py-2 border-t border-slate-200/60 flex flex-wrap items-center gap-1.5">
                    <button
                        type="button"
                        onClick={() => void send()}
                        disabled={!canSend}
                        title="Send now (⌘Enter)"
                        className="h-7 px-2.5 rounded-md bg-sky-600 hover:bg-sky-700 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
                    >
                        {isSending ? (
                            <Loader2Icon className="w-3 h-3 animate-spin" />
                        ) : (
                            <SendIcon className="w-3 h-3" />
                        )}
                        {isSending ? "Sending" : "Send"}
                        {!isSending && <Kbd combo="mod+enter" />}
                    </button>

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
                                disabled={!canSend}
                                title="Send later, up to 29 days out"
                                className="h-7 px-2 rounded-md border border-slate-200 hover:border-slate-300 text-slate-700 hover:text-slate-900 text-[12px] inline-flex items-center gap-1 transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
                            >
                                <ClockIcon className="w-3 h-3" />
                                Schedule
                                <ChevronDownIcon className="w-3 h-3 text-slate-400" />
                            </button>
                        </PopoverMenuTrigger>
                        <PopoverMenuContent minWidth={240}>
                            {customMode ? (
                                <div className="px-1 py-1 w-[260px]">
                                    <PopoverMenuLabel>Send at</PopoverMenuLabel>
                                    <div className="mt-1">
                                        <DateTimePicker value={customValue} onChange={setCustomValue} stepMinutes={15} />
                                    </div>
                                    <div className="mt-2 flex items-center gap-1.5">
                                        <button
                                            type="button"
                                            onClick={() => handleSchedule(new Date(customValue))}
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
                                </div>
                            ) : (
                                <>
                                    <PopoverMenuLabel>Send at</PopoverMenuLabel>
                                    {SCHEDULE_PRESETS.map((p) => (
                                        <PopoverMenuItem key={p.label} onSelect={() => handleSchedule(p.at())}>
                                            {p.label}
                                        </PopoverMenuItem>
                                    ))}
                                    <PopoverMenuSeparator />
                                    <PopoverMenuItem onSelect={() => setCustomMode(true)} closeOnSelect={false}>
                                        Pick a time
                                    </PopoverMenuItem>
                                </>
                            )}
                        </PopoverMenuContent>
                    </PopoverMenu>

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
                            resolvedAccount?.signature_sync && resolvedAccount.signature_plain?.trim()
                                ? "bg-emerald-50 text-emerald-700"
                                : "bg-slate-100 text-slate-500",
                        )}
                        title={
                            resolvedAccount?.signature_sync && resolvedAccount.signature_plain?.trim()
                                ? `The ${resolvedAccount.email} signature is appended on send.`
                                : "No signature will be appended for the sending mailbox."
                        }
                    >
                        <PenLineIcon className="w-2.5 h-2.5" />
                        {resolvedAccount?.signature_sync && resolvedAccount.signature_plain?.trim()
                            ? "Signature on"
                            : "No signature"}
                    </span>
                    <span className="font-mono text-[10px] text-slate-400 tabular-nums">
                        {body.length}/{MAX_BODY_LEN}
                    </span>
                </div>
            </div>

            {/* ── History panel ───────────────────────────────────────── */}
            <AnimatePresence>
                {showHistory && (
                    <motion.div
                        initial={{ width: 0, opacity: 0 }}
                        animate={{ width: 300, opacity: 1 }}
                        exit={{ width: 0, opacity: 0 }}
                        transition={{ duration: 0.2, ease: [0.16, 1, 0.3, 1] }}
                        className="hidden sm:block border-l border-slate-200 bg-slate-50/40 overflow-hidden"
                    >
                        <div className="w-[300px] h-full">
                            <ComposeHistoryPanel
                                address={primary}
                                displayName={contactDisplay || undefined}
                                affinityLine={affinity}
                            />
                        </div>
                    </motion.div>
                )}
            </AnimatePresence>
        </motion.div>
    );
}

// ComposeRow — one plain labelled line in the compose header: quiet inline
// label, hairline underneath, nothing else.
function ComposeRow({
    label,
    onRemove,
    children,
}: {
    label: string;
    onRemove?: () => void;
    children: React.ReactNode;
}) {
    return (
        <div className="flex items-start gap-2 px-3.5 py-[7px] border-b border-slate-100">
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
