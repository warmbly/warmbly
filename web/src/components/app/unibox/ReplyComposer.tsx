// Reply composer — pinned to the bottom of the thread pane.
//
// Surfaces everything you need to see before hitting Send:
//   - From mailbox (with name + email + chip)
//   - Editable To / Cc / Bcc recipient chips
//   - Editable Subject (defaults to "Re: …")
//   - Body textarea
//   - Live signature preview (read from the mailbox the reply is going
//     out from — same one the server's task handler will append at
//     send-time when account.signature_sync is on)
//   - Template picker (loads the org's saved templates and drops the
//     selected body into the textarea)
//   - Schedule picker (instant / preset / pick-a-time, capped at the
//     server's 29-day Cloud Tasks horizon)
//
// ⌘+Enter sends instantly. The schedule preset path also sends —
// every preset calls /unibox/reply with send_mode="scheduled" plus a
// concrete scheduled_at.

import React from "react";
import { AnimatePresence, motion } from "framer-motion";
import {
    CheckIcon,
    ChevronDownIcon,
    ClockIcon,
    FileTextIcon,
    Loader2Icon,
    SendIcon,
    XIcon,
} from "lucide-react";
import toast from "react-hot-toast";
import sendReply from "@/lib/api/client/app/unibox/sendReply";
import useTemplates from "@/lib/api/hooks/app/templates/useTemplates";
import useUniboxOverview from "@/lib/api/hooks/app/unibox/useUniboxOverview";
import { useAppStore } from "@/stores";
import type UniboxEmail from "@/lib/api/models/app/unibox/UniboxEmail";
import {
    PopoverMenu,
    PopoverMenuContent,
    PopoverMenuItem,
    PopoverMenuLabel,
    PopoverMenuTrigger,
    PopoverMenuSeparator,
} from "@/components/ui/popover-menu";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";

interface ReplyComposerProps {
    threadId: string;
    threadEmails: UniboxEmail[];
}

const SCHEDULE_PRESETS: { label: string; at: () => Date }[] = [
    { label: "In 1 hour", at: () => offsetHours(1) },
    { label: "In 3 hours", at: () => offsetHours(3) },
    { label: "Tomorrow 9:00", at: () => atHour(1, 9) },
    { label: "Tomorrow 17:00", at: () => atHour(1, 17) },
    { label: "Monday 9:00", at: () => nextMonday9() },
];

// Body length cap — matches the textarea counter. Generous; real
// replies rarely come close.
const MAX_BODY_LEN = 4000;

// Server cap (GCP Cloud Tasks 30-day ceiling, minus 1 day of
// clock-skew headroom). Mirrored client-side so the user gets a
// useful error instead of a 400 from the API.
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

// Light-touch email syntax check used only for chip validation. Loose
// on purpose — the SMTP layer is the source of truth.
function looksLikeEmail(s: string): boolean {
    return /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(s.trim());
}

// Convert the incoming-thread context into reply-composer defaults.
// We default `to` to the last sender, subject to "Re: <original>", and
// thread_id to the current thread so the reply lands in the same
// conversation.
function deriveDefaults(threadEmails: UniboxEmail[]) {
    const latest = threadEmails[threadEmails.length - 1];
    const subjectBase = latest?.subject?.trim() || "Re:";
    const subject = /^re:/i.test(subjectBase) ? subjectBase : `Re: ${subjectBase}`;
    const to = latest?.from?.trim() ? [latest.from.trim()] : [];
    return { latest, to, subject };
}

export function ReplyComposer({ threadId, threadEmails }: ReplyComposerProps) {
    const accounts = useAppStore((s) => s.emails);

    const initial = React.useMemo(() => deriveDefaults(threadEmails), [threadEmails]);

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

    // Reset to defaults whenever the thread changes — otherwise stale
    // recipients/subjects bleed across thread navigation.
    React.useEffect(() => {
        setSubject(initial.subject);
        setTo(initial.to);
        setCc([]);
        setBcc([]);
        setShowCc(false);
        setShowBcc(false);
        setBody("");
    }, [threadId, initial.subject, initial.to]);

    // Resolve the sending mailbox from the latest message's account_id.
    // We look it up in the global emails store so we have the full
    // Inbox record (signature_html, signature_plain, etc) — UniboxEmail
    // only carries the id.
    const latest = initial.latest;
    const accountId = latest?.account_id ?? "";
    const mailbox = accounts.find((a) => a.id === accountId);

    // Templates: lazy-fetched the first time the picker opens.
    const templatesQuery = useTemplates();

    // Scheduled-sends pending-cap. We disable the Schedule button at
    // 100% so the user can't queue a send that would 429 server-side.
    // The overview hook is already mounted by the page, so this is a
    // cached read with no extra network cost.
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
                subject: subject.trim() || "Re:",
                body_plain: trimmedBody,
                body_html: trimmedBody.replace(/\n/g, "<br />"),
                thread_id: threadId,
                ...(scheduledAt
                    ? {
                          send_mode: "scheduled" as const,
                          scheduled_at: scheduledAt.toISOString(),
                      }
                    : { send_mode: "instant" as const }),
            });
            setBody("");
            toast.success(
                scheduledAt
                    ? `Scheduled for ${formatFriendly(scheduledAt)}`
                    : "Reply queued",
            );
            setScheduleOpen(false);
            setCustomMode(false);
        } catch {
            toast.error("Failed to send reply");
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
        // the user keeps what they already typed.
        if (!body.trim()) {
            setBody(plain);
        } else {
            setBody((b) => `${b.trimEnd()}\n\n${plain}`);
        }
        // Only overwrite subject if the user hasn't customised it past
        // the default "Re:" prefix.
        if (subj && /^re:\s*$/i.test(subject.trim())) {
            setSubject(subj);
        }
        setTemplateOpen(false);
        toast.success(`Inserted "${name}"`);
    };

    // Signature preview — what the server will append at send time
    // when account.signature_sync is on. We render it inline below the
    // textarea so the user sees the final-looking message without
    // mutating their actual body.
    const signaturePreview = mailbox?.signature_sync && mailbox?.signature_plain
        ? mailbox.signature_plain
        : "";

    return (
        <div className="border-t border-slate-200 bg-white shrink-0 flex flex-col">
            {/* Header strip — From / To / Cc / Bcc + Subject */}
            <div className="px-4 py-2 border-b border-slate-200/70 space-y-1.5 bg-slate-50/50">
                <HeaderRow label="From">
                    {mailbox ? (
                        <div className="inline-flex items-center gap-1.5 min-w-0">
                            <span className="text-[12px] text-slate-900 font-medium truncate">
                                {mailbox.name || mailbox.email}
                            </span>
                            <span className="font-mono text-[10.5px] text-slate-500 bg-white px-1.5 h-4 inline-flex items-center rounded border border-slate-200 shrink-0">
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
                                className="h-5 px-1.5 rounded text-[10.5px] text-slate-500 hover:text-slate-900 hover:bg-slate-200/60 transition-colors"
                            >
                                + Cc
                            </button>
                        )}
                        {!showBcc && (
                            <button
                                type="button"
                                onClick={() => setShowBcc(true)}
                                className="h-5 px-1.5 rounded text-[10.5px] text-slate-500 hover:text-slate-900 hover:bg-slate-200/60 transition-colors"
                            >
                                + Bcc
                            </button>
                        )}
                    </div>
                </HeaderRow>

                <HeaderRow label="To">
                    <RecipientField value={to} onChange={setTo} placeholder="Add recipient…" />
                </HeaderRow>

                {showCc && (
                    <HeaderRow label="Cc" onRemove={() => { setCc([]); setShowCc(false); }}>
                        <RecipientField value={cc} onChange={setCc} placeholder="Cc…" />
                    </HeaderRow>
                )}
                {showBcc && (
                    <HeaderRow label="Bcc" onRemove={() => { setBcc([]); setShowBcc(false); }}>
                        <RecipientField value={bcc} onChange={setBcc} placeholder="Bcc…" />
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
                placeholder="Type a reply… (⌘ + Enter to send)"
                onKeyDown={(e) => {
                    if ((e.metaKey || e.ctrlKey) && e.key === "Enter") {
                        e.preventDefault();
                        if (canSend) handleInstant();
                    }
                }}
                className="w-full min-h-[110px] max-h-72 px-5 py-3 text-[13px] text-slate-800 placeholder:text-slate-400 bg-transparent resize-y focus:outline-none"
            />

            {/* Signature preview — what the server will append. Visible
                so the user can see the final message shape. */}
            {signaturePreview && (
                <div className="mx-5 mb-2 px-3 py-2 rounded-md bg-slate-50 border border-slate-200/70 text-[11.5px] text-slate-500 whitespace-pre-wrap leading-relaxed">
                    <div className="text-[9.5px] uppercase tracking-[0.14em] text-slate-400 font-medium mb-1">
                        Signature
                    </div>
                    {signaturePreview}
                </div>
            )}

            {/* Action bar */}
            <div className="px-3 py-2 border-t border-slate-200/60 flex items-center gap-1.5">
                <button
                    type="button"
                    onClick={handleInstant}
                    disabled={!canSend}
                    className="h-7 px-2.5 rounded-md bg-sky-600 hover:bg-sky-700 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
                >
                    {isSending ? <Loader2Icon className="w-3 h-3 animate-spin" /> : <SendIcon className="w-3 h-3" />}
                    {isSending ? "Sending…" : "Send"}
                </button>

                {/* Schedule picker */}
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
                        <Tooltip>
                            <TooltipTrigger asChild>
                                <button
                                    type="button"
                                    disabled={!canSend || scheduleAtCap}
                                    className="h-7 px-2 rounded-md border border-slate-200 hover:border-slate-300 text-slate-700 hover:text-slate-900 text-[12px] inline-flex items-center gap-1 transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
                                >
                                    <ClockIcon className="w-3 h-3" />
                                    Schedule
                                    <ChevronDownIcon className="w-3 h-3 text-slate-400" />
                                </button>
                            </TooltipTrigger>
                            <TooltipContent side="top">
                                {scheduleAtCap
                                    ? `Schedule queue full (${scheduledUsed}/${scheduledCap}). Cancel some from the Scheduled view.`
                                    : "Send later — capped at 29 days"}
                            </TooltipContent>
                        </Tooltip>
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
                                    <input
                                        type="datetime-local"
                                        value={customValue}
                                        onChange={(e) => setCustomValue(e.target.value)}
                                        min={toLocalInput(new Date(Date.now() + 60_000))}
                                        max={toLocalInput(new Date(Date.now() + MAX_SCHEDULE_MS))}
                                        autoFocus
                                        className="w-full h-8 px-2 mt-1 rounded-md border border-slate-200 text-[12.5px] text-slate-900 outline-none focus:border-sky-400 focus:ring-2 focus:ring-sky-100 tabular-nums"
                                    />
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
                                        Pick a time…
                                    </PopoverMenuItem>
                                </motion.div>
                            )}
                        </AnimatePresence>
                    </PopoverMenuContent>
                </PopoverMenu>

                {/* Template picker */}
                <PopoverMenu
                    align="start"
                    side="top"
                    open={templateOpen}
                    onOpenChange={setTemplateOpen}
                >
                    <PopoverMenuTrigger asChild>
                        <Tooltip>
                            <TooltipTrigger asChild>
                                <button
                                    type="button"
                                    className="h-7 px-2 rounded-md border border-slate-200 hover:border-slate-300 text-slate-700 hover:text-slate-900 text-[12px] inline-flex items-center gap-1 transition-colors"
                                >
                                    <FileTextIcon className="w-3 h-3" />
                                    Template
                                    <ChevronDownIcon className="w-3 h-3 text-slate-400" />
                                </button>
                            </TooltipTrigger>
                            <TooltipContent side="top">
                                Drop a saved reply into the body
                            </TooltipContent>
                        </Tooltip>
                    </PopoverMenuTrigger>
                    <PopoverMenuContent minWidth={260}>
                        <PopoverMenuLabel>Templates</PopoverMenuLabel>
                        {templatesQuery.isPending ? (
                            <div className="px-3 py-2 text-[11.5px] text-slate-400 inline-flex items-center gap-1.5">
                                <Loader2Icon className="w-3 h-3 animate-spin" />
                                Loading…
                            </div>
                        ) : templatesQuery.isError ? (
                            <div className="px-3 py-2 text-[11.5px] text-rose-500">
                                Couldn't load templates
                            </div>
                        ) : !templatesQuery.data || templatesQuery.data.length === 0 ? (
                            <div className="px-3 py-2 text-[11.5px] text-slate-400">
                                No templates yet. Create them in Settings.
                            </div>
                        ) : (
                            <div className="max-h-72 overflow-y-auto">
                                {templatesQuery.data.map((t) => (
                                    <PopoverMenuItem
                                        key={t.id}
                                        onSelect={() => applyTemplate(t.name, t.body_plain, t.subject)}
                                    >
                                        <div className="flex flex-col min-w-0 w-full">
                                            <span className="text-[12px] font-medium text-slate-900 truncate">
                                                {t.name}
                                            </span>
                                            {t.body_plain && (
                                                <span className="text-[10.5px] text-slate-500 truncate">
                                                    {t.body_plain.replace(/\s+/g, " ").slice(0, 90)}
                                                </span>
                                            )}
                                        </div>
                                    </PopoverMenuItem>
                                ))}
                            </div>
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

                <span className="ml-auto font-mono text-[10px] text-slate-400 tabular-nums">
                    {body.length}/{MAX_BODY_LEN}
                </span>
            </div>
        </div>
    );
}

// HeaderRow — one labeled inline field in the compact composer header.
// Keeps every row to a single line so the composer doesn't push the
// thread off-screen.
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
        <div className="flex items-center gap-2 min-h-[22px]">
            <span className="w-12 shrink-0 text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">
                {label}
            </span>
            <div className="flex-1 min-w-0 flex items-center gap-1.5">{children}</div>
            {onRemove && (
                <button
                    type="button"
                    onClick={onRemove}
                    aria-label={`Remove ${label}`}
                    className="size-5 inline-flex items-center justify-center rounded text-slate-400 hover:text-slate-700 hover:bg-slate-200/60 transition-colors shrink-0"
                >
                    <XIcon className="w-3 h-3" />
                </button>
            )}
        </div>
    );
}

// RecipientField — chip-based editor. Enter / "," / Tab / blur commits
// the in-progress text as a chip. Backspace on an empty input removes
// the last chip.
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
                        "inline-flex items-center gap-1 h-5 pl-1.5 pr-0.5 rounded text-[11px] font-medium shrink-0",
                        looksLikeEmail(v)
                            ? "bg-sky-100 text-sky-800"
                            : "bg-rose-100 text-rose-800",
                    )}
                    title={v}
                >
                    <span className="font-mono">{v}</span>
                    <button
                        type="button"
                        onClick={() => onChange(value.filter((x) => x !== v))}
                        aria-label={`Remove ${v}`}
                        className="size-4 inline-flex items-center justify-center rounded hover:bg-black/10"
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
                        const parts = text.split(/[,\s]+/).map((s) => s.trim()).filter(Boolean);
                        const fresh = [...value];
                        for (const p of parts) {
                            if (!fresh.includes(p)) fresh.push(p);
                        }
                        onChange(fresh);
                    }
                }}
                className="flex-1 min-w-[10ch] h-5 bg-transparent text-[11.5px] text-slate-900 placeholder:text-slate-400 outline-none font-mono"
            />
        </>
    );
}
