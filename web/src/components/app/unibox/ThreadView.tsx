// Thread reader : right pane of the unibox.
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
  ClockIcon,
  CornerUpLeftIcon,
  ForwardIcon,
  Loader2Icon,
  MailCheckIcon,
  MoonIcon,
  MoreVerticalIcon,
  SendIcon,
  TrashIcon,
  UserIcon,
  XIcon,
} from "lucide-react";

import { MessageBubble } from "./MessageBubble";
import { ReplyComposer, type ReplyMode } from "./ReplyComposer";
import ResourceViewers from "@/components/app/presence/ResourceViewers";
import { DateTimePicker } from "@/components/ui/DateTimePicker";
import { usePresenceResource } from "@/hooks/PresenceProvider";
import { ThreadLabelMenu } from "./ThreadLabelMenu";
import ContactContextPanel from "./ContactContextPanel";
import BookACallButton from "@/components/app/integrations/BookACallButton";
import { CategoryChip } from "@/components/app/contacts/CategoryPicker";
import { SectionBar } from "@/components/layout/Page";
import useThread from "@/lib/api/hooks/app/unibox/useThread";
import useMarkSeen from "@/lib/api/hooks/app/unibox/useMarkSeen";
import useThreadLabels from "@/lib/api/hooks/app/unibox/useThreadLabels";
import useThreadScheduled from "@/lib/api/hooks/app/unibox/useThreadScheduled";
import cancelScheduled from "@/lib/api/client/app/unibox/cancelScheduled";
import { useAppStore } from "@/stores";
import {
  PopoverMenu,
  PopoverMenuContent,
  PopoverMenuItem,
  PopoverMenuLabel,
  PopoverMenuTrigger,
} from "@/components/ui/popover-menu";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import {
  snoozeThread,
  unsnoozeThread,
} from "@/lib/api/client/app/unibox/snoozeThread";
import type UniboxEmail from "@/lib/api/models/app/unibox/UniboxEmail";
import type UniboxScheduledItem from "@/lib/api/models/app/unibox/UniboxScheduled";
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
  const delta = (1 - dow + 7) % 7 || 7;
  d.setDate(d.getDate() + delta);
  d.setHours(9, 0, 0, 0);
  return d;
}

// Local datetime → ISO string. The native <input type="datetime-local">
// hands back "YYYY-MM-DDTHH:mm" (no zone), interpreted as the user's
// local clock : perfectly fine here since we round-trip to UTC on send.
function toLocalInput(d: Date): string {
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
}
function defaultCustomSnoozeValue(): string {
  return toLocalInput(offsetHours(2));
}

export function ThreadView({ threadId, emailId }: ThreadViewProps) {
  const q = useThread(threadId, emailId);
  const scheduledQ = useThreadScheduled(threadId);
  const accounts = useAppStore((s) => s.emails);
  const queryClient = useQueryClient();

  const cancel = useMutation({
    mutationFn: (taskId: string) => cancelScheduled(taskId),
    onSuccess: () => {
      toast.success("Scheduled send cancelled");
      // Three caches to refresh: the per-thread list (this view),
      // the global scheduled list (Scheduled scope), and the
      // overview that powers the scope-rail counter.
      queryClient.invalidateQueries({
        queryKey: ["unibox", "scheduled", "thread", threadId],
      });
      queryClient.invalidateQueries({ queryKey: ["unibox", "scheduled"] });
      queryClient.invalidateQueries({ queryKey: ["unibox", "overview"] });
    },
    onError: () => toast.error("Couldn't cancel that send"),
  });

  const [snoozeOpen, setSnoozeOpen] = React.useState(false);
  const [customValue, setCustomValue] = React.useState(
    defaultCustomSnoozeValue,
  );
  const [customMode, setCustomMode] = React.useState(false);

  // Conversation labels (read for the header chips; the menu writes).
  const threadLabels = useThreadLabels(threadId);
  const [labelMenuOpen, setLabelMenuOpen] = React.useState(false);

  // CRM context rail (right side). Open by default on wide screens (lg+),
  // where it renders as a static rail. Below lg it renders as an overlay
  // drawer, so it starts closed and is opened from the header toggle.
  const [crmOpen, setCrmOpen] = React.useState(
    () =>
      typeof window !== "undefined" &&
      window.matchMedia("(min-width: 1024px)").matches,
  );

  // `c` opens the label menu while a thread is open — ignored while
  // typing into the composer / any input so it never eats keystrokes.
  React.useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if (e.metaKey || e.ctrlKey || e.altKey) return;
      const t = e.target as HTMLElement | null;
      if (t) {
        const tag = t.tagName;
        if (tag === "INPUT" || tag === "TEXTAREA" || t.isContentEditable)
          return;
      }
      if (e.key === "c") {
        setLabelMenuOpen(true);
        e.preventDefault();
      }
    };
    document.addEventListener("keydown", handler);
    return () => document.removeEventListener("keydown", handler);
  }, []);

  // Composer is opt-in. Default: no reply UI mounted. The user has
  // to click Reply (per-message or the footer CTA) before any blank
  // composer appears. Cleared on thread change so navigating to a
  // different conversation doesn't carry a half-written reply with
  // it.
  const [replyState, setReplyState] = React.useState<{
    messageId: string;
    mode: ReplyMode;
  } | null>(null);
  React.useEffect(() => {
    setReplyState(null);
  }, [threadId]);

  // Collaboration: claim this thread while it's open so teammates see
  // "X is viewing" (and "replying" once a composer is mounted) instead of
  // double-handling the same conversation.
  usePresenceResource(
    threadId ? `thread:${threadId}` : null,
    replyState ? "replying" : "viewing",
  );

  // Opening a thread marks its unseen messages as read. The hook invalidates
  // ["unibox"], so the unread badge, the collapsed list, and the overview all
  // refresh. Once everything is seen the id list is empty and this no-ops, so
  // it self-terminates after the post-mark refetch (no loop).
  const markSeen = useMarkSeen();
  const markSeenMutate = markSeen.mutate;
  React.useEffect(() => {
    const unseenIds = (q.data?.data ?? [])
      .filter((m) => !m.seen)
      .map((m) => m.id);
    if (unseenIds.length === 0) return;
    markSeenMutate({ ids: unseenIds });
  }, [threadId, q.data, markSeenMutate]);

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

  // The external party of the thread = the first message address that isn't
  // our own mailbox. Addresses arrive as "Name <addr>" or bare "addr"; reduce
  // to the bare address so the comparison + the CRM panel lookup both work.
  const mailboxEmail = mailbox?.email?.toLowerCase();
  const bareAddr = (s: string) => {
    const m = s.match(/<([^>]+)>/);
    return (m ? m[1] : s).trim();
  };
  const contactEmail =
    messages
      .map((m) => bareAddr(m.from))
      .find((e) => e && e.toLowerCase() !== mailboxEmail) ??
    bareAddr(messages[0]?.from ?? "");

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
    <div className="flex h-full min-h-0">
      <div className="flex-1 flex flex-col min-w-0 bg-white">
      <div className="h-12 px-3 sm:px-5 border-b border-slate-200 flex items-center gap-2 sm:gap-3 shrink-0 bg-white">
        <span className="hidden sm:inline text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">
          Thread
        </span>
        <div className="hidden sm:block h-4 w-px bg-slate-200" />
        <span className="text-[12.5px] text-slate-900 font-medium truncate min-w-0">
          {subject}
        </span>
        {mailbox && (
          <span className="ml-1 hidden md:inline-flex items-center gap-1 h-5 px-1.5 rounded bg-slate-100 text-slate-600 text-[10.5px] font-medium font-mono shrink-0">
            {mailbox.email}
          </span>
        )}
        {(threadLabels.data ?? []).length > 0 && (
          <span className="hidden md:inline-flex items-center gap-1 shrink-0">
            {(threadLabels.data ?? []).slice(0, 3).map((c) => (
              <CategoryChip key={c.id} category={c} compact />
            ))}
          </span>
        )}
        <ResourceViewers resource={`thread:${threadId}`} className="shrink-0" />
        <div className="ml-auto flex items-center gap-1">
          <button
            type="button"
            onClick={() => setCrmOpen((o) => !o)}
            aria-label={crmOpen ? "Hide contact panel" : "Show contact panel"}
            className={
              "inline-flex size-7 rounded-md items-center justify-center transition-colors " +
              (crmOpen
                ? "text-sky-700 bg-sky-50"
                : "text-slate-500 hover:text-slate-900 hover:bg-slate-100")
            }
          >
            <UserIcon className="w-3.5 h-3.5" />
          </button>
          <BookACallButton email={contactEmail} className="hidden sm:inline-flex" />
          <ThreadLabelMenu
            threadId={threadId}
            open={labelMenuOpen}
            onOpenChange={setLabelMenuOpen}
          />
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
                <span className="hidden sm:inline">Snooze</span>
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
                    <div className="mt-1">
                      <DateTimePicker value={customValue} onChange={setCustomValue} stepMinutes={15} />
                    </div>
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

          <div className="hidden sm:flex items-center gap-1">
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
          <PopoverMenu align="end" side="bottom">
            <PopoverMenuTrigger asChild>
              <button
                type="button"
                aria-label="More thread actions"
                className="sm:hidden size-7 rounded-md inline-flex items-center justify-center text-slate-500 hover:text-slate-900 hover:bg-slate-100 transition-colors"
              >
                <MoreVerticalIcon className="w-3.5 h-3.5" />
              </button>
            </PopoverMenuTrigger>
            <PopoverMenuContent>
              <PopoverMenuItem
                icon={<MailCheckIcon className="w-3.5 h-3.5" />}
              >
                Mark as unread
              </PopoverMenuItem>
              <PopoverMenuItem icon={<ArchiveIcon className="w-3.5 h-3.5" />}>
                Archive thread
              </PopoverMenuItem>
              <PopoverMenuItem
                danger
                icon={<TrashIcon className="w-3.5 h-3.5" />}
              >
                Delete thread
              </PopoverMenuItem>
            </PopoverMenuContent>
          </PopoverMenu>
        </div>
      </div>

      <SectionBar
        label={`${messages.length} ${messages.length === 1 ? "message" : "messages"}`}
        count={participants.size}
      />

      <div className="flex-1 overflow-y-auto divide-y divide-slate-200/60">
        {messages.map((email) => (
          <MessageBubble
            key={email.id}
            email={email}
            onReply={() =>
              setReplyState({ messageId: email.id, mode: "reply" })
            }
            onForward={() =>
              setReplyState({ messageId: email.id, mode: "forward" })
            }
          />
        ))}
        {(scheduledQ.data?.data ?? []).map((item) => (
          <ScheduledMessageBubble
            key={item.task_id}
            item={item}
            cancelling={cancel.isPending && cancel.variables === item.task_id}
            onCancel={() => cancel.mutate(item.task_id)}
          />
        ))}
      </div>

      <AnimatePresence mode="wait" initial={false}>
        {replyState ? (
          (() => {
            const target =
              messages.find((m) => m.id === replyState.messageId) ??
              messages[messages.length - 1];
            return target ? (
              <ReplyComposer
                key={`${replyState.messageId}-${replyState.mode}`}
                threadId={threadId}
                replyTo={target}
                mode={replyState.mode}
                onClose={() => setReplyState(null)}
              />
            ) : null;
          })()
        ) : (
          <motion.div
            key="reply-rail"
            initial={{ opacity: 0, y: 6 }}
            animate={{ opacity: 1, y: 0 }}
            exit={{ opacity: 0, y: 6 }}
            transition={{ duration: 0.16, ease: [0.16, 1, 0.3, 1] }}
            className="border-t border-slate-200 bg-white px-3 py-2 flex items-center gap-1.5 shrink-0"
          >
            <button
              type="button"
              onClick={() => {
                const last = messages[messages.length - 1];
                if (!last) return;
                setReplyState({ messageId: last.id, mode: "reply" });
              }}
              className="h-7 px-2.5 rounded-md bg-sky-600 hover:bg-sky-700 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors"
            >
              <CornerUpLeftIcon className="w-3 h-3" />
              Reply
            </button>
            <button
              type="button"
              onClick={() => {
                const last = messages[messages.length - 1];
                if (!last) return;
                setReplyState({ messageId: last.id, mode: "forward" });
              }}
              className="h-7 px-2 rounded-md border border-slate-200 hover:border-slate-300 text-slate-700 hover:text-slate-900 text-[12px] inline-flex items-center gap-1.5 transition-colors"
            >
              <ForwardIcon className="w-3 h-3" />
              Forward
            </button>
            <span className="ml-auto hidden md:inline text-[10.5px] text-slate-400">
              Hover any message to reply to it directly.
            </span>
          </motion.div>
        )}
      </AnimatePresence>
      </div>

      {crmOpen && (
        <ContactContextPanel
          email={contactEmail}
          mailboxId={mailbox?.id}
          onClose={() => setCrmOpen(false)}
        />
      )}
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

// Friendly relative-or-absolute time used for scheduled cards.
// Examples: "in 12 min", "in 3 h", "tomorrow, 09:00", "Mar 5, 17:00".
function formatScheduled(iso: string): string {
  const d = new Date(iso);
  if (!Number.isFinite(d.getTime())) return iso;
  const now = new Date();
  const diffMs = d.getTime() - now.getTime();
  const diffMin = Math.round(diffMs / 60_000);
  const timeStr = d.toLocaleTimeString(undefined, {
    hour: "2-digit",
    minute: "2-digit",
  });

  if (diffMin > 0 && diffMin < 60) {
    return `in ${diffMin} min`;
  }
  const sameDay =
    d.getFullYear() === now.getFullYear() &&
    d.getMonth() === now.getMonth() &&
    d.getDate() === now.getDate();
  if (sameDay) return `today, ${timeStr}`;

  const tomorrow = new Date(now);
  tomorrow.setDate(now.getDate() + 1);
  const isTomorrow =
    d.getFullYear() === tomorrow.getFullYear() &&
    d.getMonth() === tomorrow.getMonth() &&
    d.getDate() === tomorrow.getDate();
  if (isTomorrow) return `tomorrow, ${timeStr}`;

  return d.toLocaleString(undefined, {
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  });
}

// ScheduledMessageBubble : a queued send rendered inline at the
// bottom of the thread. Visually distinct from sent messages
// (dashed border, sky tint, ClockIcon) so the user can tell at a
// glance that this hasn't fired yet. Cancel flips the row to
// 'cancelled' in Postgres; the queued Cloud Task either gets a
// best-effort DeleteTask or fires as a no-op (the worker handler
// short-circuits on non-pending status).
function ScheduledMessageBubble({
  item,
  cancelling,
  onCancel,
}: {
  item: UniboxScheduledItem;
  cancelling: boolean;
  onCancel: () => void;
}) {
  const when = formatScheduled(item.scheduled_at);
  const recipients = [...item.to, ...(item.cc ?? []), ...(item.bcc ?? [])];
  const recipientLine =
    recipients.slice(0, 3).join(", ") +
    (recipients.length > 3 ? ` +${recipients.length - 3}` : "");

  return (
    <article className="px-3 sm:px-5 py-3">
      <div className="rounded-lg border border-dashed border-sky-300 bg-sky-50/40 px-3 sm:px-4 py-3">
        <header className="flex items-start gap-3">
          <div className="size-7 rounded-full bg-sky-100 text-sky-700 flex items-center justify-center shrink-0">
            <ClockIcon className="w-3.5 h-3.5" />
          </div>
          <div className="min-w-0 flex-1">
            <div className="flex items-baseline gap-2 flex-wrap">
              <span className="text-[10px] uppercase tracking-[0.14em] text-sky-700 font-semibold">
                Scheduled
              </span>
              <span className="text-[12.5px] font-semibold text-slate-900">
                {when}
              </span>
            </div>
            <div className="text-[11px] text-slate-500 mt-0.5 flex items-center gap-1.5 min-w-0">
              <span className="truncate">from {item.account_email}</span>
              <span aria-hidden className="text-slate-300">
                &middot;
              </span>
              <span className="truncate">
                to {recipientLine || "(no recipient)"}
              </span>
            </div>
          </div>
          <button
            type="button"
            onClick={onCancel}
            disabled={cancelling}
            title="Cancel this scheduled send"
            className="shrink-0 inline-flex items-center gap-1 h-6 px-1.5 rounded-md border border-sky-200 bg-white text-sky-700 hover:text-rose-700 hover:border-rose-300 hover:bg-rose-50 text-[11px] font-medium transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
          >
            {cancelling ? (
              <Loader2Icon className="w-3 h-3 animate-spin" />
            ) : (
              <XIcon className="w-3 h-3" />
            )}
            {cancelling ? "Cancelling" : "Cancel"}
          </button>
        </header>
        <div className="mt-2.5 ml-10">
          {item.subject && (
            <div className="text-[12.5px] font-medium text-slate-900 truncate">
              {item.subject}
            </div>
          )}
          {item.snippet && (
            <p className="text-[12px] text-slate-600 mt-1 leading-relaxed line-clamp-3">
              {item.snippet}
            </p>
          )}
          <div className="mt-2 inline-flex items-center gap-1 h-5 px-1.5 rounded bg-white border border-sky-200 text-[10px] text-sky-700 font-medium">
            <SendIcon className="w-2.5 h-2.5" />
            Will send {when}
          </div>
        </div>
      </div>
    </article>
  );
}
