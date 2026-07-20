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
import { Link, useLocation } from "react-router-dom";
import toast from "react-hot-toast";
import {
    CheckIcon,
    ChevronDownIcon,
    ChevronUpIcon,
    ClockIcon,
    FileTextIcon,
    HistoryIcon,
    Loader2Icon,
    MailPlusIcon,
    MinusIcon,
    OctagonAlertIcon,
    PenLineIcon,
    SendIcon,
    XIcon,
} from "lucide-react";
import { useComposeStore } from "@/hooks/useComposeStore";
import { resolveSendAt, useOutboxStore } from "@/hooks/useOutboxStore";
import { useUserProfile } from "@/hooks/context/user";
import { useAppStore } from "@/stores";
import useComposeCandidates from "@/lib/api/hooks/app/unibox/useComposeCandidates";
import useComposeSend from "@/lib/api/hooks/app/unibox/useComposeSend";
import useGenerateWrite from "@/lib/api/hooks/app/generation/useGenerateWrite";
import useComposeDraft from "@/lib/api/hooks/app/unibox/useComposeDraft";
import { useDeleteComposeDraft, useSaveComposeDraft } from "@/lib/api/hooks/app/unibox/useComposeDrafts";
import type { ComposeDraft } from "@/lib/api/client/app/unibox/composeDrafts";
import useTemplates from "@/lib/api/hooks/app/templates/useTemplates";
import type Template from "@/lib/api/models/app/templates/Template";
import TemplatePickerContent from "../TemplatePicker";
import InsertBookingLink from "../InsertBookingLink";
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

// draftSnapshot serializes the savable fields so autosave can cheaply tell
// "changed since last save" without deep comparisons.
function draftSnapshot(d: {
    to: string[];
    cc: string[];
    bcc: string[];
    subject: string;
    body: string;
    email_account_id?: string | null;
}): string {
    return JSON.stringify([d.to, d.cc, d.bcc, d.subject, d.body, d.email_account_id ?? "auto"]);
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
    const seed = useComposeStore((s) => s.seed);
    const session = useComposeStore((s) => s.session);

    // Navigation collapses the window to the corner bar; the draft follows you.
    const { pathname } = useLocation();
    const lastPath = React.useRef(pathname);
    React.useEffect(() => {
        if (lastPath.current === pathname) return;
        lastPath.current = pathname;
        const s = useComposeStore.getState();
        if (s.open && !s.minimized) s.setMinimized(true);
    }, [pathname]);

    return (
        <AnimatePresence>
            {open && <ComposeWindowInner key={session} prefillTo={prefillTo} seed={seed} />}
        </AnimatePresence>
    );
}

function ComposeWindowInner({
    prefillTo,
    seed,
}: {
    prefillTo: string | null;
    seed: ComposeDraft | null;
}) {
    const closeCompose = useComposeStore((s) => s.closeCompose);
    const minimized = useComposeStore((s) => s.minimized);
    const setMinimized = useComposeStore((s) => s.setMinimized);
    const accounts = useAppStore((s) => s.emails);
    const { user } = useUserProfile();
    const addOutbox = useOutboxStore((s) => s.add);

    const [to, setTo] = React.useState<string[]>(
        seed ? seed.to : prefillTo ? [prefillTo] : [],
    );
    const [cc, setCc] = React.useState<string[]>(seed?.cc ?? []);
    const [bcc, setBcc] = React.useState<string[]>(seed?.bcc ?? []);
    const [showCc, setShowCc] = React.useState((seed?.cc?.length ?? 0) > 0);
    const [showBcc, setShowBcc] = React.useState((seed?.bcc?.length ?? 0) > 0);
    const [subject, setSubject] = React.useState(seed?.subject ?? "");
    const [body, setBody] = React.useState(seed?.body ?? "");
    const [accountSel, setAccountSel] = React.useState(seed?.email_account_id || "auto");
    // Tag scoping the Auto pick ("Auto in Sales"); session-only, not part
    // of the draft payload. Meaningless with an explicit account.
    const [autoTagId, setAutoTagId] = React.useState<string | null>(null);
    const [historyOpen, setHistoryOpen] = React.useState(false);
    const [isSending, setIsSending] = React.useState(false);
    const [scheduleOpen, setScheduleOpen] = React.useState(false);
    const [customMode, setCustomMode] = React.useState(false);
    const [customValue, setCustomValue] = React.useState(() => toLocalInput(offsetHours(2)));
    const [templateOpen, setTemplateOpen] = React.useState(false);

    const templatesQuery = useTemplates();

    // Body empty → replace; otherwise append under a separator. The template
    // subject only fills an empty subject line, never overwrites yours.
    const applyTemplate = (t: Template) => {
        const plain = t.body_plain ?? "";
        setBody((b) => (b.trim() ? `${b.trimEnd()}\n\n${plain}` : plain).slice(0, MAX_BODY_LEN));
        if (t.subject && !subject.trim()) setSubject(t.subject);
        setTemplateOpen(false);
        toast.success(`Inserted "${t.name}"`);
    };

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
        let picked;
        if (accountSel === "auto") {
            // Tag-scoped auto resolves to the best-scored member of the tag
            // (the list arrives score-sorted); unscoped auto keeps the
            // backend's recommendation.
            picked = autoTagId
                ? list.find((a) => (accounts.find((e) => e.id === a.id)?.tags ?? []).includes(autoTagId))
                : (list.find((a) => a.recommended) ?? list[0]);
        } else {
            picked = list.find((a) => a.id === accountSel);
        }
        if (!picked) return undefined;
        return accounts.find((a) => a.id === picked.id);
    }, [accounts, accountSel, autoTagId, candidates]);

    const affinity = React.useMemo(() => {
        const withHistory = (candidates?.accounts ?? []).filter((a) => a.history_messages > 0);
        if (withHistory.length === 0) return undefined;
        const top = withHistory.reduce((a, b) => (b.history_messages > a.history_messages ? b : a));
        return `Usually from ${top.email}`;
    }, [candidates]);

    // AI draft of the whole email, server-grounded like reply drafts: the
    // backend folds in the contact record, the correspondence history with the
    // address, and the org voice profile, and asks a clarifying question when
    // it can't tell what the email is for.
    const draftMut = useComposeDraft();
    const generateDraft = React.useCallback(
        (instruction?: string) =>
            draftMut.mutateAsync({
                to: primary || undefined,
                subject: subject.trim() || undefined,
                instruction,
                idempotency_key: crypto.randomUUID(),
            }),
        [draftMut, primary, subject],
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

    // ── Autosave. The draft id is client-generated (or the resumed seed's),
    // so the debounced PUT is idempotent. Everything the user types survives
    // a close, a reload, or switching to another draft.
    const draftIdRef = React.useRef(seed?.id ?? crypto.randomUUID());
    const everSavedRef = React.useRef(!!seed);
    const lastSavedRef = React.useRef(seed ? draftSnapshot(seed) : "");
    const saveMut = useSaveComposeDraft();
    const deleteMut = useDeleteComposeDraft();

    const currentSnapshot = draftSnapshot({ to, cc, bcc, subject, body, email_account_id: accountSel });
    const saveNow = React.useCallback(() => {
        if (!dirty) return;
        if (currentSnapshot === lastSavedRef.current) return;
        lastSavedRef.current = currentSnapshot;
        everSavedRef.current = true;
        saveMut.mutate({
            id: draftIdRef.current,
            data: {
                email_account_id: accountSel === "auto" ? undefined : accountSel,
                to,
                cc,
                bcc,
                subject,
                body,
            },
        });
    }, [accountSel, bcc, body, cc, currentSnapshot, dirty, saveMut, subject, to]);

    React.useEffect(() => {
        if (!dirty || currentSnapshot === lastSavedRef.current) return;
        const t = window.setTimeout(saveNow, 1200);
        return () => window.clearTimeout(t);
    }, [currentSnapshot, dirty, saveNow]);

    const saved = dirty && currentSnapshot === lastSavedRef.current && !saveMut.isPending;

    // Closing never loses work: dirty content is flushed to Drafts, an
    // emptied-out draft is deleted.
    const requestClose = React.useCallback(() => {
        if (dirty) {
            saveNow();
            toast.success("Saved to Drafts");
        } else if (everSavedRef.current) {
            deleteMut.mutate(draftIdRef.current);
        }
        closeCompose();
    }, [closeCompose, deleteMut, dirty, saveNow]);

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
                from_tag_id: accountSel === "auto" && autoTagId ? autoTagId : undefined,
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
            if (!scheduledAt && res.send_mode === "instant") {
                // Undo window: the send is queued a few seconds out. No
                // "sent" toast; the header pill counts down and can cancel,
                // reopening the composer from this seed.
                const nowIso = new Date().toISOString();
                addOutbox({
                    taskId: res.task_id,
                    scheduledAt: resolveSendAt(res.scheduled_at, user.undo_send_seconds || 30),
                    kind: "compose",
                    to,
                    subject: subject.trim(),
                    seed: {
                        id: crypto.randomUUID(),
                        email_account_id: accountSel === "auto" ? null : accountSel,
                        to,
                        cc,
                        bcc,
                        subject: subject.trim(),
                        body: trimmedBody,
                        updated_at: nowIso,
                        created_at: nowIso,
                    },
                });
            } else {
                toast.success(
                    scheduledAt
                        ? `Scheduled for ${formatFriendly(scheduledAt)} from ${res.account_email}`
                        : `Sent from ${res.account_email}${res.auto ? " · picked automatically" : ""}`,
                );
            }
            // The email is on its way; its draft is done.
            if (everSavedRef.current) deleteMut.mutate(draftIdRef.current);
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

    const showHistory = historyOpen && !!primary && !minimized;

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
            <div className={cn("flex flex-col w-full min-h-0", minimized ? "sm:w-[280px]" : "sm:w-[540px]")}>
                <div
                    className={cn(
                        "shrink-0 h-8 pl-3 pr-1.5 flex items-center gap-2 bg-slate-100/80 select-none",
                        minimized ? "cursor-pointer hover:bg-slate-100" : "border-b border-slate-200",
                    )}
                    onClick={minimized ? () => setMinimized(false) : undefined}
                >
                    <MailPlusIcon className="w-3.5 h-3.5 text-slate-500 shrink-0" />
                    <span className="min-w-0 flex-1 text-[11.5px] text-slate-500 truncate">
                        <span className="font-semibold text-slate-800">New email</span>
                        {contact ? (
                            <>
                                {" "}
                                to {contactDisplay}
                                {contact.company ? `, ${contact.company}` : ""}
                            </>
                        ) : primary ? (
                            <> to {primary}</>
                        ) : null}
                    </span>
                    <div className="flex items-center gap-0.5 shrink-0">
                        {!minimized && dirty && (
                            <span className="mr-1 text-[10px] text-slate-400 select-none">
                                {saveMut.isPending ? "Saving…" : saved ? "Saved" : ""}
                            </span>
                        )}
                        {!minimized && (
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
                                        ? "bg-slate-200 text-slate-700"
                                        : "text-slate-400 hover:text-slate-700 hover:bg-slate-200/60",
                                )}
                            >
                                <HistoryIcon className="w-3.5 h-3.5" />
                            </button>
                        )}
                        <button
                            type="button"
                            onClick={(e) => {
                                e.stopPropagation();
                                setMinimized(!minimized);
                            }}
                            aria-label={minimized ? "Restore composer" : "Minimize composer"}
                            title={
                                minimized
                                    ? "Restore"
                                    : "Minimize; your draft stays while you work elsewhere"
                            }
                            className="size-6 inline-flex items-center justify-center rounded-md text-slate-400 hover:text-slate-700 hover:bg-slate-200/60 transition-colors"
                        >
                            {minimized ? (
                                <ChevronUpIcon className="w-3.5 h-3.5" />
                            ) : (
                                <MinusIcon className="w-3.5 h-3.5" />
                            )}
                        </button>
                        <button
                            type="button"
                            onClick={(e) => {
                                e.stopPropagation();
                                requestClose();
                            }}
                            aria-label="Close composer"
                            className="size-6 inline-flex items-center justify-center rounded-md text-slate-400 hover:text-slate-700 hover:bg-slate-200/60 transition-colors"
                        >
                            <XIcon className="w-3.5 h-3.5" />
                        </button>
                    </div>
                </div>

                {!minimized && (
                <>
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
                            autoTag={autoTagId}
                            onChange={(next, tag) => {
                                setAccountSel(next);
                                setAutoTagId(next === "auto" ? tag : null);
                            }}
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
                        className="w-full flex-1 min-h-[160px] px-3.5 py-3 text-[13px] text-slate-800 placeholder:text-slate-400 bg-transparent resize-none focus:outline-none"
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
                    draftCost="from 2 credits"
                    contextHint={`It is a new outbound email${contactDisplay ? ` to ${contactDisplay}` : ""}${subject.trim() ? ` with the subject "${subject.trim()}"` : ""}.`}
                    maxLen={MAX_BODY_LEN}
                />

                {/* One-time nudge: drafts came back ungrounded in any product
                    context, so point at the workspace voice profile. */}
                {aiDraft.grounding && !aiDraft.grounding.voice_profile && (
                    <div className="shrink-0 mx-3.5 mb-1.5 px-2.5 py-1.5 rounded-md border border-amber-200/60 bg-amber-50/60 text-[10.5px] text-amber-800 leading-snug">
                        AI doesn&apos;t know your product yet.{" "}
                        <Link
                            to="/app/settings/workspace"
                            className="font-medium underline underline-offset-2 hover:text-amber-950"
                        >
                            Set your voice profile
                        </Link>{" "}
                        (what you sell, who to, how you sound) and drafts will stop being generic.
                    </div>
                )}

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

                    <PopoverMenu align="start" side="top" open={templateOpen} onOpenChange={setTemplateOpen}>
                        <PopoverMenuTrigger asChild>
                            <button
                                type="button"
                                title="Drop a saved template into the email"
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
                                onPick={applyTemplate}
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
                </>
                )}
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
