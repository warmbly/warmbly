// Thread reader : right pane of the unibox.
//
// Fetches the thread via /unibox/thread. The header is the subject over one
// meta line (count, mailbox, labels, who else is here) with icon-only actions
// on the right, each under a tooltip. Snooze has presets and a custom "pick a
// time" path.

import React from "react";
import { useParams } from "react-router-dom";
import { AnimatePresence, motion } from "framer-motion";
import { useQueryClient, useMutation } from "@tanstack/react-query";
import toast from "react-hot-toast";
import {
  AlertCircleIcon,
  ArchiveIcon,
  CheckIcon,
  ClockIcon,
  CornerUpLeftIcon,
  ForwardIcon,
  InboxIcon,
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
import { ReplyComposer, type ReplyMode, type ReplySeed } from "./ReplyComposer";
import { loadReplyDraft, replyDraftKey } from "@/lib/unibox/replyDraft";
import { useOutboxStore } from "@/hooks/useOutboxStore";
import AgentDraftCard from "./AgentDraftCard";
import ResourceViewers from "@/components/app/presence/ResourceViewers";
import { DateTimePicker } from "@/components/ui/DateTimePicker";
import { usePresenceResource } from "@/hooks/PresenceProvider";
import { useMediaQuery, LG_QUERY } from "@/hooks/useMediaQuery";
import { useShortcutActions } from "@/hooks/useShortcutActions";
import { ThreadLabelMenu } from "./ThreadLabelMenu";
import ContactContextPanel from "./ContactContextPanel";
import { CategoryChip } from "@/components/app/contacts/CategoryPicker";
import useThread from "@/lib/api/hooks/app/unibox/useThread";
import useMarkSeen from "@/lib/api/hooks/app/unibox/useMarkSeen";
import { useConversationActions } from "@/hooks/useConversationActions";
import { SNOOZE_PRESETS, offsetHours } from "@/lib/unibox/snooze";
import type { FilableFolder } from "@/lib/api/client/app/unibox/moveFolder";
import { bareEmail, nameFromAddr, wrappedEmail } from "@/lib/helper/emailAddress";
import useThreadLabels from "@/lib/api/hooks/app/unibox/useThreadLabels";
import useThreadScheduled from "@/lib/api/hooks/app/unibox/useThreadScheduled";
import cancelScheduled from "@/lib/api/client/app/unibox/cancelScheduled";
import { useAppStore } from "@/stores";
import { cn } from "@/lib/utils";
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
    recipients: m.to_addr ?? [],
    subject: m.subject,
    snippet: m.snippet,
    date: new Date(m.internal_date),
    is_seen: m.seen,
    thread_id: m.thread_id,
    account_id: m.email_id,
  };
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
  const userId = useAppStore((s) => s.user?.id);
  const orgId = useAppStore((s) => s.currentOrganization?.id);
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

  // Desktop visibility is remembered; mobile drawers start closed.
  const isWide = useMediaQuery(LG_QUERY);
  const railPref = useAppStore((s) => s.uniboxContactRailOpen);
  const setRailPref = useAppStore((s) => s.setUniboxContactRailOpen);
  const [overlayOpen, setOverlayOpen] = React.useState(false);
  const crmOpen = isWide ? railPref : overlayOpen;

  // Widening drops the overlay state on the floor, so clear it: otherwise an
  // overlay opened while narrow is still "open" on the way back down and the
  // drawer plus its backdrop reappear over the thread with nobody asking. This
  // is the invariant the removed matchMedia effect used to hold.
  React.useEffect(() => {
    if (isWide) setOverlayOpen(false);
  }, [isWide]);
  const setCrmOpen = React.useCallback(
    (open: boolean) => {
      if (isWide) setRailPref(open);
      else setOverlayOpen(open);
    },
    [isWide, setRailPref],
  );

  // `c` labels the open conversation. The key itself is declared in the global
  // shortcut registry; registering the action here is what makes the row live
  // (and shown in the `?` modal) only while a thread is actually open.
  useShortcutActions({ labelThread: () => setLabelMenuOpen(true) });

  // Composer is opt-in. Default: no reply UI mounted. The user has
  // to click Reply (per-message or the footer CTA) before any blank
  // composer appears. Cleared on thread change so navigating to a
  // different conversation doesn't carry a half-written reply with
  // it.
  const [replyState, setReplyState] = React.useState<{
    messageId: string;
    mode: ReplyMode;
  } | null>(null);
  // Restored content for a cancelled undo-send reply; only set alongside
  // replyState by the restore effect below, cleared on any manual open.
  const [replySeed, setReplySeed] = React.useState<ReplySeed | null>(null);
  const openReply = React.useCallback(
    (messageId: string, mode: ReplyMode, seed: ReplySeed | null = null) => {
      setReplySeed(seed);
      setReplyState({ messageId, mode });
    },
    [],
  );
  React.useEffect(() => {
    setReplyState(null);
    setReplySeed(null);
  }, [threadId]);

  // Restore once on arrival, including drafts targeting an older message.
  const reopenedFor = React.useRef<string | null>(null);
  React.useEffect(() => {
    const msgs = q.data?.data ?? [];
    if (!msgs.length || !userId || !orgId) return;
    const identity = JSON.stringify([userId, orgId, threadId]);
    if (reopenedFor.current === identity) return;
    reopenedFor.current = identity;
    for (const message of [...msgs].reverse()) {
      for (const mode of ["reply", "forward"] as const) {
        if (loadReplyDraft(replyDraftKey(userId, orgId, threadId, message.id, mode))) {
          openReply(message.id, mode);
          return;
        }
      }
    }
  }, [threadId, q.data, userId, orgId, openReply]);

  // A cancelled undo-send reply for this thread reopens the composer with
  // the exact content that was about to go out.
  const pendingRestore = useOutboxStore((s) => s.pendingReplyRestore);
  React.useEffect(() => {
    if (!pendingRestore || pendingRestore.threadId !== threadId) return;
    useOutboxStore.getState().setReplyRestore(null);
    openReply(pendingRestore.messageId, pendingRestore.mode, {
      to: pendingRestore.to,
      cc: pendingRestore.cc,
      bcc: pendingRestore.bcc,
      subject: pendingRestore.subject,
      body: pendingRestore.body,
      email_account_id: pendingRestore.emailAccountId,
    });
  }, [pendingRestore, threadId, openReply]);

  // Collaboration: claim this thread while it's open so teammates see
  // "X is viewing" (and "replying" once a composer is mounted) instead of
  // double-handling the same conversation.
  usePresenceResource(
    threadId ? `thread:${threadId}` : null,
    replyState ? "replying" : "viewing",
  );

  // Opening a thread marks its unseen messages as read. The hook writes the
  // flip straight into the cached list and thread and refetches only the
  // counters, so the conversation list the user came from does not re-order
  // under them. Passing threadId is what lets it find the row. Once everything
  // is seen the id list is empty and this no-ops, so it self-terminates.
  const markSeen = useMarkSeen();
  const markSeenMutate = markSeen.mutate;
  React.useEffect(() => {
    const unseenIds = (q.data?.data ?? [])
      .filter((m) => !m.seen)
      .map((m) => m.id);
    if (unseenIds.length === 0) return;
    markSeenMutate({ ids: unseenIds, threadIds: [threadId] });
  }, [threadId, q.data, markSeenMutate]);

  // Header actions. Each one closes the thread: the effect above would
  // otherwise re-mark an "unread" thread as seen on the next refetch, and a
  // filed thread has left the list the reader is looking at.
  const actions = useConversationActions();
  const setSelectedThreadId = useAppStore((s) => s.setSelectedThreadId);
  const threadIds = () => (q.data?.data ?? []).map((m) => m.id);
  const markUnread = () => {
    markSeenMutate({ ids: threadIds(), seen: false, threadIds: [threadId] });
    setSelectedThreadId(null);
  };

  // Filing is store-side: the message keeps its place at the provider, and
  // the sync knows not to undo this (migration 000146).
  // The row leaves the list and the reader closes at once; the request runs
  // behind the toast, and a failure re-reads the list, which brings it back.
  // The copy, the undo and the cache handling are shared with the list row.
  const fileThread = async (folder: FilableFolder) => {
    if (actions.filing) return;
    setSelectedThreadId(null);
    await actions.file([threadId], folder, threadIds());
  };

  // Restoring is only offered where the user can see what they are restoring.
  const { scope: urlScope } = useParams<{ scope?: string }>();
  const filed = urlScope === "trash" || urlScope === "archive";

  const snooze = useMutation({
    mutationFn: (until: Date) => actions.snooze([threadId], until),
    onSettled: () => {
      setSnoozeOpen(false);
      setCustomMode(false);
    },
  });

  const unsnooze = useMutation({
    mutationFn: () => actions.unsnooze([threadId]),
    onSettled: () => setSnoozeOpen(false),
  });

  // Built once per fetch, not once per render. Every consumer holds these
  // objects by identity, so rebuilding them on each render hands each one a
  // value that looks new while the conversation has not changed. That is what
  // let a thread re-render reset the reply composer; keep it memoised, and
  // above the early returns below so the hook order stays fixed.
  const messages = React.useMemo(() => (q.data?.data ?? []).map(toUniboxEmail), [q.data]);

  if (q.isPending) {
    return <ThreadSkeleton />;
  }

  if (q.isError) {
    return (
      <div className="flex-1 flex items-center justify-center px-6">
        <div className="text-center max-w-sm">
          <AlertCircleIcon className="w-5 h-5 text-rose-500 mx-auto mb-2" />
          <p className="text-[12.5px] font-medium text-slate-900 mb-1">
            Couldn't load this conversation
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

  if (messages.length === 0) {
    return (
      <div className="flex-1 flex items-center justify-center text-[12px] text-slate-400">
        This conversation is empty.
      </div>
    );
  }

  const subject = messages[0]?.subject || "(no subject)";
  const mailbox = accounts.find((a) => a.id === messages[0]?.account_id);

  // The external party of the thread = the first message address that isn't
  // our own mailbox. Headers arrive in all three shapes lib/helper/emailAddress
  // parses; reduce to the bare address so the comparison + the lookup work.
  const mailboxEmail = mailbox?.email?.toLowerCase();
  const contactFrom =
    messages
      .map((m) => m.from)
      .find((f) => {
        const e = bareEmail(f);
        return e && e.toLowerCase() !== mailboxEmail;
      }) ?? (messages[0]?.from ?? "");
  const contactEmail = bareEmail(contactFrom);
  // Display name from the From header, so an "Add as contact" from the
  // panel does not create a nameless row. Empty when the header is bare.
  const contactName =
    wrappedEmail(contactFrom) && nameFromAddr(contactFrom) !== contactEmail
      ? nameFromAddr(contactFrom)
      : "";

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
      <div className="min-h-12 px-4 sm:px-5 py-2 border-b border-slate-200 flex items-center gap-3 shrink-0 bg-white">
        <div className="min-w-0 flex-1">
          <h1 className="text-[13.5px] text-slate-900 font-semibold truncate leading-5">
            {subject}
          </h1>
          <div className="flex items-center gap-1.5 min-w-0 text-[11px] text-slate-400 leading-4">
            <span className="shrink-0">
              {messages.length} {messages.length === 1 ? "message" : "messages"}
            </span>
            {mailbox && (
              <>
                <span aria-hidden className="text-slate-300">&middot;</span>
                <span className="truncate min-w-0">{mailbox.email}</span>
              </>
            )}
            {(threadLabels.data ?? []).length > 0 && (
              <span className="hidden md:inline-flex items-center gap-1 shrink-0 ml-1">
                {(threadLabels.data ?? []).slice(0, 3).map((c) => (
                  <CategoryChip key={c.id} category={c} compact />
                ))}
              </span>
            )}
            <ResourceViewers resource={`thread:${threadId}`} className="shrink-0 ml-1" />
          </div>
        </div>
        <div className="flex items-center gap-0.5 shrink-0">
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
                title="Snooze"
                className={cn(
                  "size-7 rounded-md inline-flex items-center justify-center transition-colors",
                  snoozeOpen
                    ? "bg-slate-100 text-slate-900"
                    : "text-slate-500 hover:text-slate-900 hover:bg-slate-100",
                )}
                disabled={snooze.isPending || unsnooze.isPending}
              >
                {snooze.isPending || unsnooze.isPending ? (
                  <Loader2Icon className="w-3.5 h-3.5 animate-spin" />
                ) : (
                  <MoonIcon className="w-[15px] h-[15px]" />
                )}
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

          <div className="hidden sm:flex items-center gap-0.5">
            <IconAction
              label="Mark as unread"
              icon={<MailCheckIcon className="w-[15px] h-[15px]" />}
              onClick={markUnread}
            />
            {filed ? (
              <IconAction
                label="Move to inbox"
                icon={<InboxIcon className="w-[15px] h-[15px]" />}
                disabled={actions.filing}
                onClick={() => fileThread("inbox")}
              />
            ) : (
              <IconAction
                label="Archive thread"
                icon={<ArchiveIcon className="w-[15px] h-[15px]" />}
                disabled={actions.filing}
                onClick={() => fileThread("archive")}
              />
            )}
            {urlScope !== "trash" && (
              <IconAction
                label="Delete thread"
                danger
                icon={<TrashIcon className="w-[15px] h-[15px]" />}
                disabled={actions.filing}
                onClick={() => fileThread("trash")}
              />
            )}
          </div>
          <span aria-hidden className="hidden sm:block h-4 w-px bg-slate-200 mx-1" />
          <Tooltip>
            <TooltipTrigger asChild>
              <button
                type="button"
                onClick={() => setCrmOpen(!crmOpen)}
                aria-label={crmOpen ? "Hide contact panel" : "Show contact panel"}
                className={cn(
                  "size-7 rounded-md inline-flex items-center justify-center transition-colors",
                  crmOpen
                    ? "text-sky-700 bg-sky-50"
                    : "text-slate-500 hover:text-slate-900 hover:bg-slate-100",
                )}
              >
                <UserIcon className="w-[15px] h-[15px]" />
              </button>
            </TooltipTrigger>
            <TooltipContent side="bottom">{crmOpen ? "Hide contact" : "Show contact"}</TooltipContent>
          </Tooltip>
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
                onSelect={markUnread}
              >
                Mark as unread
              </PopoverMenuItem>
              {filed ? (
                <PopoverMenuItem
                  icon={<InboxIcon className="w-3.5 h-3.5" />}
                  disabled={actions.filing}
                  onSelect={() => fileThread("inbox")}
                >
                  Move to inbox
                </PopoverMenuItem>
              ) : (
                <PopoverMenuItem
                  icon={<ArchiveIcon className="w-3.5 h-3.5" />}
                  disabled={actions.filing}
                  onSelect={() => fileThread("archive")}
                >
                  Archive thread
                </PopoverMenuItem>
              )}
              {urlScope !== "trash" && (
                <PopoverMenuItem
                  danger
                  icon={<TrashIcon className="w-3.5 h-3.5" />}
                  disabled={actions.filing}
                  onSelect={() => fileThread("trash")}
                >
                  Delete thread
                </PopoverMenuItem>
              )}
            </PopoverMenuContent>
          </PopoverMenu>
        </div>
      </div>

      <motion.div
        // The thread is keyed on its id, so this runs once per conversation
        // opened: a short fade in place of the messages popping into an
        // otherwise settled pane.
        initial={{ opacity: 0 }}
        animate={{ opacity: 1 }}
        transition={{ duration: 0.16, ease: [0.16, 1, 0.3, 1] }}
        className="flex-1 overflow-y-auto divide-y divide-slate-100"
      >
        {messages.map((email, i) => (
          <MessageBubble
            key={email.id}
            email={email}
            // Open what the reader came for: the newest message, plus anything
            // still unread. Older read messages stay collapsed so a long
            // thread does not fetch every body at once.
            defaultExpanded={i === messages.length - 1 || !email.is_seen}
            outbound={
              !!mailboxEmail && bareEmail(email.from).toLowerCase() === mailboxEmail
            }
            onReply={() => openReply(email.id, "reply")}
            onForward={() => openReply(email.id, "forward")}
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
      </motion.div>

      <AgentDraftCard threadId={threadId} />

      <AnimatePresence mode="wait" initial={false}>
        {replyState ? (
          (() => {
            const target =
              messages.find((m) => m.id === replyState.messageId) ??
              messages[messages.length - 1];
            return target ? (
              <ReplyComposer
                key={`${userId}-${orgId}-${replyState.messageId}-${replyState.mode}`}
                threadId={threadId}
                replyTo={target}
                mode={replyState.mode}
                seed={replySeed ?? undefined}
                onClose={() => {
                  setReplyState(null);
                  setReplySeed(null);
                }}
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
            className="border-t border-slate-200 bg-white px-4 sm:px-5 py-2.5 flex items-center gap-1.5 shrink-0"
          >
            <button
              type="button"
              onClick={() => {
                const last = messages[messages.length - 1];
                if (!last) return;
                openReply(last.id, "reply");
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
                openReply(last.id, "forward");
              }}
              className="h-7 px-2 rounded-md border border-slate-200 hover:border-slate-300 text-slate-700 hover:text-slate-900 text-[12px] inline-flex items-center gap-1.5 transition-colors"
            >
              <ForwardIcon className="w-3 h-3" />
              Forward
            </button>
          </motion.div>
        )}
      </AnimatePresence>
      </div>

      {crmOpen && (
        <ContactContextPanel
          email={contactEmail}
          name={contactName}
          mailboxId={mailbox?.id}
          onClose={() => setCrmOpen(false)}
        />
      )}
    </div>
  );
}

// The pane's own shape while the thread loads: a header block, then two
// messages. Nothing is centred and nothing spins, so the layout the messages
// land in is already on screen.
function ThreadSkeleton() {
  return (
    <div className="flex h-full min-h-0" aria-busy aria-label="Loading conversation">
      <div className="flex-1 flex flex-col min-w-0 bg-white">
        <div className="min-h-12 px-4 sm:px-5 py-2 border-b border-slate-200 flex items-center gap-3 shrink-0">
          <div className="min-w-0 flex-1 space-y-2">
            <div className="h-3 w-2/3 max-w-[320px] rounded bg-slate-100 animate-pulse" />
            <div className="h-2.5 w-40 rounded bg-slate-100/80 animate-pulse" />
          </div>
          <div className="flex items-center gap-1.5 shrink-0">
            {Array.from({ length: 5 }).map((_, i) => (
              <div key={i} className="size-7 rounded-md bg-slate-50" />
            ))}
          </div>
        </div>
        <div className="flex-1 divide-y divide-slate-100">
          <div className="px-4 sm:px-5 py-2.5 flex items-center gap-3">
            <div className="size-7 rounded-full bg-slate-100 animate-pulse shrink-0" />
            <div className="h-2.5 w-28 rounded bg-slate-100 animate-pulse" />
            <div className="h-2.5 flex-1 max-w-[280px] rounded bg-slate-100/70 animate-pulse" />
          </div>
          <div className="px-4 sm:px-5 py-4">
            <div className="flex items-start gap-3">
              <div className="size-7 rounded-full bg-slate-100 animate-pulse shrink-0" />
              <div className="flex-1 space-y-2 pt-1">
                <div className="h-2.5 w-36 rounded bg-slate-100 animate-pulse" />
                <div className="h-2.5 w-24 rounded bg-slate-100/80 animate-pulse" />
              </div>
            </div>
            <div className="sm:pl-10 mt-4 space-y-2.5">
              <div className="h-2.5 w-[92%] rounded bg-slate-100 animate-pulse" />
              <div className="h-2.5 w-[84%] rounded bg-slate-100 animate-pulse" />
              <div className="h-2.5 w-[60%] rounded bg-slate-100 animate-pulse" />
              <div className="h-2.5 w-[76%] rounded bg-slate-100/80 animate-pulse mt-4" />
              <div className="h-2.5 w-[40%] rounded bg-slate-100/80 animate-pulse" />
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}

function IconAction({
  label,
  icon,
  danger,
  disabled,
  onClick,
}: {
  label: string;
  icon: React.ReactNode;
  danger?: boolean;
  disabled?: boolean;
  onClick?: () => void;
}) {
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <button
          type="button"
          onClick={onClick}
          disabled={disabled}
          aria-label={label}
          className={
            "size-7 rounded-md inline-flex items-center justify-center transition-colors disabled:opacity-40 disabled:pointer-events-none " +
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
