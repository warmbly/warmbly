// Thread reader — right pane of the unibox.
//
// Fetches the thread via /unibox/thread. Header actions are wrapped
// in radix tooltips so hover reveals the intent + (where it exists) a
// keyboard hint. Snooze has both presets and a custom "pick a time"
// path with a native datetime input.

import React from "react";
import { AnimatePresence, motion } from "framer-motion";
import { useQueryClient, useMutation } from "@tanstack/react-query";
import toast from "react-hot-toast";
import {
    AlertCircleIcon,
    ArchiveIcon,
    CheckIcon,
    ChevronDownIcon,
    Loader2Icon,
    MailCheckIcon,
    MoonIcon,
    TrashIcon,
} from "lucide-react";

import { MessageBubble } from "./MessageBubble";
import { ReplyComposer } from "./ReplyComposer";
import { SectionBar } from "@/components/layout/Page";
import useThread from "@/lib/api/hooks/app/unibox/useThread";
import { useAppStore } from "@/stores";
import {
    PopoverMenu,
    PopoverMenuContent,
    PopoverMenuItem,
    PopoverMenuLabel,
    PopoverMenuTrigger,
} from "@/components/ui/popover-menu";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { snoozeThread, unsnoozeThread } from "@/lib/api/client/app/unibox/snoozeThread";
import type UniboxEmail from "@/lib/api/models/app/unibox/UniboxEmail";
import type { UniboxThreadMessage } from "@/lib/api/models/app/unibox/UniboxThread";

interface ThreadViewProps {
    threadId: string;
    emailId?: string;
}

function toUniboxEmail(m: UniboxThreadMessage): UniboxEmail {
    return {
        id: m.id,
        from: m.from_addr?.[0] ?? "",
        to: m.to_addr?.[0] ?? "",
        subject: m.subject,
        body: m.snippet ? `<p>${escapeHtml(m.snippet)}</p>` : "",
        date: new Date(m.internal_date),
        is_seen: m.seen,
        thread_id: m.thread_id,
        account_id: m.email_id,
    };
}

function escapeHtml(s: string): string {
    return s
        .replace(/&/g, "&amp;")
        .replace(/</g, "&lt;")
        .replace(/>/g, "&gt;")
        .replace(/"/g, "&quot;")
        .replace(/'/g, "&#39;");
}

const SNOOZE_PRESETS: { label: string; until: () => Date }[] = [
    { label: "In 1 hour", until: () => offsetHours(1) },
    { label: "In 3 hours", until: () => offsetHours(3) },
    { label: "Tomorrow 9:00", until: () => atHour(1, 9) },
    { label: "Monday 9:00", until: () => nextMonday9() },
    { label: "Next week", until: () => offsetDays(7) },
];

function offsetHours(h: number): Date {
    const d = new Date();
    d.setHours(d.getHours() + h);
    return d;
}
function offsetDays(d: number): Date {
    const x = new Date();
    x.setDate(x.getDate() + d);
    return x;
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

// Local datetime → ISO string. The native <input type="datetime-local">
// hands back "YYYY-MM-DDTHH:mm" (no zone), interpreted as the user's
// local clock — perfectly fine here since we round-trip to UTC on send.
function toLocalInput(d: Date): string {
    const pad = (n: number) => String(n).padStart(2, "0");
    return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
}
function defaultCustomSnoozeValue(): string {
    return toLocalInput(offsetHours(2));
}

export function ThreadView({ threadId, emailId }: ThreadViewProps) {
    const q = useThread(threadId, emailId);
    const accounts = useAppStore((s) => s.emails);
    const queryClient = useQueryClient();

    const [snoozeOpen, setSnoozeOpen] = React.useState(false);
    const [customValue, setCustomValue] = React.useState(defaultCustomSnoozeValue);
    const [customMode, setCustomMode] = React.useState(false);

    const snooze = useMutation({
        mutationFn: (until: Date) =>
            snoozeThread({ thread_id: threadId, snoozed_until: until.toISOString() }),
        onSuccess: () => {
            toast.success("Snoozed");
            queryClient.invalidateQueries({ queryKey: ["unibox", "search"] });
            queryClient.invalidateQueries({ queryKey: ["unibox", "overview"] });
            queryClient.invalidateQueries({ queryKey: ["unibox", "count"] });
            setSnoozeOpen(false);
            setCustomMode(false);
        },
        onError: () => toast.error("Couldn't snooze this thread"),
    });

    const unsnooze = useMutation({
        mutationFn: () => unsnoozeThread(threadId),
        onSuccess: () => {
            toast.success("Un-snoozed");
            queryClient.invalidateQueries({ queryKey: ["unibox", "search"] });
            queryClient.invalidateQueries({ queryKey: ["unibox", "overview"] });
            setSnoozeOpen(false);
        },
        onError: () => toast.error("Couldn't un-snooze"),
    });

    if (q.isPending) {
        return (
            <div className="flex-1 flex items-center justify-center gap-2 text-[12px] text-slate-400">
                <Loader2Icon className="w-3.5 h-3.5 animate-spin" />
                Loading thread…
            </div>
        );
    }

    if (q.isError) {
        return (
            <div className="flex-1 flex items-center justify-center px-6">
                <div className="text-center max-w-sm">
                    <AlertCircleIcon className="w-5 h-5 text-rose-500 mx-auto mb-2" />
                    <p className="text-[12.5px] font-medium text-slate-900 mb-1">
                        Couldn't load this thread
                    </p>
                    <p className="text-[11.5px] text-slate-500 mb-3">
                        {q.error?.message ?? "Request failed"}
                    </p>
                    <button
                        type="button"
                        onClick={() => q.refetch()}
                        className="h-7 px-2.5 rounded-md bg-slate-900 hover:bg-slate-800 text-white text-[12px] font-medium transition-colors"
                    >
                        Try again
                    </button>
                </div>
            </div>
        );
    }

    const messages = (q.data?.data ?? []).map(toUniboxEmail);

    if (messages.length === 0) {
        return (
            <div className="flex-1 flex items-center justify-center text-[12px] text-slate-400">
                This thread is empty.
            </div>
        );
    }

    const subject = messages[0]?.subject || "(no subject)";
    const participants = new Set(
        messages.map((m) => m.from).filter((f): f is string => Boolean(f)),
    );
    const mailbox = accounts.find((a) => a.id === messages[0]?.account_id);

    const submitCustomSnooze = () => {
        if (!customValue) return;
        const d = new Date(customValue);
        // Server caps at 90 days for snooze (matches SnoozeMaxHorizon
        // in internal/app/unibox/config.go). Reject early with a
        // useful message instead of letting the API 400.
        const MAX_SNOOZE_MS = 90 * 24 * 60 * 60 * 1000;
        if (Number.isNaN(d.getTime()) || d.getTime() <= Date.now() + 5_000) {
            toast.error("Pick a future time (a few seconds out, please)");
            return;
        }
        if (d.getTime() - Date.now() > MAX_SNOOZE_MS) {
            toast.error("Snooze can't be more than 90 days out");
            return;
        }
        snooze.mutate(d);
    };

    return (
        <div className="flex flex-col h-full bg-white">
            <div className="h-12 px-5 border-b border-slate-200 flex items-center gap-3 shrink-0 bg-white">
                <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">
                    Thread
                </span>
                <div className="h-4 w-px bg-slate-200" />
                <span className="text-[12.5px] text-slate-900 font-medium truncate">
                    {subject}
                </span>
                {mailbox && (
                    <span className="ml-1 inline-flex items-center gap-1 h-5 px-1.5 rounded bg-slate-100 text-slate-600 text-[10.5px] font-medium font-mono shrink-0">
                        {mailbox.email}
                    </span>
                )}
                <div className="ml-auto flex items-center gap-1">
                    <PopoverMenu
                        align="end"
                        side="bottom"
                        open={snoozeOpen}
                        onOpenChange={(o) => {
                            setSnoozeOpen(o);
                            if (!o) setCustomMode(false);
                        }}
                    >
                        <PopoverMenuTrigger asChild>
                            <button
                                aria-label="Snooze this thread"
                                className="h-7 px-2 rounded-md text-slate-500 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center gap-1 transition-colors text-[12px]"
                                disabled={snooze.isPending || unsnooze.isPending}
                            >
                                {snooze.isPending || unsnooze.isPending ? (
                                    <Loader2Icon className="w-3 h-3 animate-spin" />
                                ) : (
                                    <MoonIcon className="w-3.5 h-3.5" />
                                )}
                                Snooze
                                <ChevronDownIcon className="w-3 h-3 text-slate-400" />
                            </button>
                        </PopoverMenuTrigger>
                        <PopoverMenuContent>
                            <AnimatePresence mode="wait" initial={false}>
                                {customMode ? (
                                    <motion.div
                                        key="custom"
                                        initial={{ opacity: 0 }}
                                        animate={{ opacity: 1 }}
                                        exit={{ opacity: 0 }}
                                        transition={{ duration: 0.12, ease: [0.16, 1, 0.3, 1] }}
                                        className="px-1 py-1 w-[240px]"
                                    >
                                        <PopoverMenuLabel>Pick a date &amp; time</PopoverMenuLabel>
                                        <input
                                            type="datetime-local"
                                            value={customValue}
                                            onChange={(e) => setCustomValue(e.target.value)}
                                            min={toLocalInput(new Date(Date.now() + 60_000))}
                                            max={toLocalInput(new Date(Date.now() + 90 * 24 * 60 * 60 * 1000))}
                                            autoFocus
                                            className="w-full h-8 px-2 mt-1 rounded-md border border-slate-200 text-[12.5px] text-slate-900 outline-none focus:border-sky-400 focus:ring-2 focus:ring-sky-100 tabular-nums"
                                        />
                                        <div className="mt-2 flex items-center gap-1.5">
                                            <button
                                                type="button"
                                                onClick={submitCustomSnooze}
                                                className="h-7 px-2.5 rounded-md bg-sky-600 hover:bg-sky-700 text-white text-[12px] font-medium inline-flex items-center gap-1 transition-colors"
                                            >
                                                <CheckIcon className="w-3 h-3" />
                                                Snooze
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
                                        <PopoverMenuLabel>Snooze until</PopoverMenuLabel>
                                        {SNOOZE_PRESETS.map((p) => (
                                            <PopoverMenuItem
                                                key={p.label}
                                                onSelect={() => snooze.mutate(p.until())}
                                            >
                                                {p.label}
                                            </PopoverMenuItem>
                                        ))}
                                        <PopoverMenuItem
                                            onSelect={() => setCustomMode(true)}
                                            closeOnSelect={false}
                                        >
                                            Pick a time…
                                        </PopoverMenuItem>
                                        <PopoverMenuItem onSelect={() => unsnooze.mutate()}>
                                            Un-snooze now
                                        </PopoverMenuItem>
                                    </motion.div>
                                )}
                            </AnimatePresence>
                        </PopoverMenuContent>
                    </PopoverMenu>

                    <IconAction
                        label="Mark as unread"
                        icon={<MailCheckIcon className="w-3.5 h-3.5" />}
                    />
                    <IconAction
                        label="Archive thread"
                        icon={<ArchiveIcon className="w-3.5 h-3.5" />}
                    />
                    <IconAction
                        label="Delete thread"
                        danger
                        icon={<TrashIcon className="w-3.5 h-3.5" />}
                    />
                </div>
            </div>

            <SectionBar
                label={`${messages.length} ${messages.length === 1 ? "message" : "messages"}`}
                count={participants.size}
            />

            <div className="flex-1 overflow-y-auto divide-y divide-slate-200/60">
                {messages.map((email) => (
                    <MessageBubble key={email.id} email={email} />
                ))}
            </div>

            <ReplyComposer threadId={threadId} threadEmails={messages} />
        </div>
    );
}

function IconAction({
    label,
    icon,
    danger,
    onClick,
}: {
    label: string;
    icon: React.ReactNode;
    danger?: boolean;
    onClick?: () => void;
}) {
    return (
        <Tooltip>
            <TooltipTrigger asChild>
                <button
                    type="button"
                    onClick={onClick}
                    aria-label={label}
                    className={
                        "size-7 rounded-md inline-flex items-center justify-center transition-colors " +
                        (danger
                            ? "text-slate-500 hover:text-red-600 hover:bg-red-50"
                            : "text-slate-500 hover:text-slate-900 hover:bg-slate-100")
                    }
                >
                    {icon}
                </button>
            </TooltipTrigger>
            <TooltipContent side="bottom">{label}</TooltipContent>
        </Tooltip>
    );
}
