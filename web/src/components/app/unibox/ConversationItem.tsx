// One row in the conversation list.
//
// Three lines and nothing else: sender and time, subject, preview. No avatar,
// so the eye reads down one column of names the way it does in Superhuman or
// Hey. Unread is a dot in the gutter plus weight, which reads from across
// the room without a coloured bar. Labels sit at the end of the subject line
// as small tinted chips; the owning mailbox shows only when the workspace
// has more than one, quietly at the end of the preview.

import type UniboxEmail from "@/lib/api/models/app/unibox/UniboxEmail";
import { useAppStore } from "@/stores";
import { useResourceViewers } from "@/hooks/PresenceProvider";
import { cn } from "@/lib/utils";
import { nameFromAddr } from "@/lib/helper/emailAddress";

function relative(d: Date): string {
  const diff = Date.now() - d.getTime();
  const m = Math.floor(diff / 60_000);
  if (m < 1) return "now";
  if (m < 60) return `${m}m`;
  const h = Math.floor(m / 60);
  if (h < 24) return `${h}h`;
  const days = Math.floor(h / 24);
  if (days < 7) return `${days}d`;
  return d.toLocaleDateString(undefined, { month: "short", day: "numeric" });
}

function fromName(s: string): string {
  if (!s) return "Unknown sender";
  return nameFromAddr(s);
}

interface ConversationItemProps {
  email: UniboxEmail;
}

export function ConversationItem({ email }: ConversationItemProps) {
  const selectedThreadId = useAppStore((s) => s.selectedThreadId);
  const setSelectedThreadId = useAppStore((s) => s.setSelectedThreadId);
  const setSelectedAccountId = useAppStore((s) => s.setSelectedAccountId);
  const accounts = useAppStore((s) => s.emails);

  const threadId = email.thread_id || email.id;
  const isSelected = selectedThreadId === threadId;
  const date = new Date(email.date);
  const unread = !email.is_seen;
  // Snippets are plain text; slice by code point so an emoji at the cut never
  // splits into a replacement character.
  const preview = [...(email.snippet ?? "").replace(/\s+/g, " ")]
    .slice(0, 140)
    .join("");

  const mailbox = accounts.find((a) => a.id === email.account_id);
  const showMailbox = !!mailbox && accounts.length > 1;
  const sender = fromName(email.from);

  const messageCount = email.message_count ?? 1;
  const labels = email.labels ?? [];

  // A teammate already has this conversation open (or is replying):
  // surface it on the row so nobody double-handles the same email.
  const viewers = useResourceViewers(`thread:${threadId}`);
  const replierName = viewers.find((v) => v.action === "replying")?.name;

  return (
    <button
      onClick={() => {
        setSelectedThreadId(threadId);
        setSelectedAccountId(email.account_id ?? null);
      }}
      aria-current={isSelected ? "true" : undefined}
      className={cn(
        "group w-full text-left pl-3 pr-4 py-2.5 flex items-start gap-2 transition-colors",
        isSelected ? "bg-sky-50" : "hover:bg-slate-50",
      )}
    >
      {/* Gutter: the unread dot, or the same space when read so text aligns. */}
      <span className="w-2 shrink-0 flex items-center justify-center h-[18px]">
        {unread && (
          <span aria-hidden className="block size-2 rounded-full bg-sky-500" />
        )}
      </span>
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-2 min-w-0 h-[18px]">
          <span
            className={cn(
              "text-[12.5px] truncate min-w-0",
              unread ? "text-slate-900 font-semibold" : "text-slate-700",
            )}
          >
            {sender}
          </span>
          {messageCount > 1 && (
            <span
              className="shrink-0 tabular-nums text-[11px] text-slate-400"
              title={`${messageCount} messages in this conversation`}
            >
              {messageCount}
            </span>
          )}
          {viewers.length > 0 && (
            <span
              className="shrink-0 relative flex size-2"
              title={
                replierName
                  ? `${replierName} is replying`
                  : `${viewers[0].name ?? "A teammate"} is viewing`
              }
            >
              <span
                className={cn(
                  "absolute inline-flex h-full w-full animate-ping rounded-full opacity-60",
                  replierName ? "bg-amber-400" : "bg-emerald-400",
                )}
              />
              <span
                className={cn(
                  "relative inline-flex size-2 rounded-full",
                  replierName ? "bg-amber-500" : "bg-emerald-500",
                )}
              />
            </span>
          )}
          <span
            className={cn(
              "text-[11px] tabular-nums shrink-0 ml-auto",
              unread ? "text-sky-700 font-medium" : "text-slate-400",
            )}
          >
            {relative(date)}
          </span>
        </div>
        <div className="flex items-center gap-1.5 min-w-0 mt-0.5">
          <span
            className={cn(
              "text-[12.5px] truncate min-w-0",
              unread ? "text-slate-900 font-medium" : "text-slate-600",
            )}
          >
            {email.subject || "(no subject)"}
          </span>
          {labels.length > 0 && (
            <span className="ml-auto shrink-0 inline-flex items-center gap-1">
              {labels.slice(0, 2).map((l) => (
                <LabelChip key={l.id} title={l.title} color={l.color} />
              ))}
              {labels.length > 2 && (
                <span
                  className="text-[10px] text-slate-400 tabular-nums"
                  title={labels
                    .slice(2)
                    .map((l) => l.title)
                    .join(", ")}
                >
                  +{labels.length - 2}
                </span>
              )}
            </span>
          )}
        </div>
        <div className="flex items-center gap-2 min-w-0 mt-0.5">
          <span className="text-[11.5px] text-slate-400 truncate min-w-0">
            {preview || "(no preview)"}
          </span>
          {showMailbox && (
            <span
              className="ml-auto shrink-0 max-w-[40%] truncate text-[10.5px] text-slate-300 group-hover:text-slate-400 transition-colors"
              title={mailbox.email}
            >
              {mailbox.email}
            </span>
          )}
        </div>
      </div>
    </button>
  );
}

function LabelChip({ title, color }: { title: string; color: string }) {
  // The dot carries the colour, the chip carries the name; a solid coloured
  // chip is too loud for a list this dense.
  return (
    <span
      className="inline-flex items-center gap-1 h-4 px-1.5 rounded-sm text-[10px] font-medium overflow-hidden max-w-[110px]"
      style={{
        color: color || "#475569",
        backgroundColor: color ? `${color}14` : "rgb(241 245 249)",
      }}
      title={title}
    >
      <span
        aria-hidden
        className="block size-1.5 rounded-full shrink-0"
        style={{ backgroundColor: color || "#94a3b8" }}
      />
      <span className="truncate">{title}</span>
    </span>
  );
}
