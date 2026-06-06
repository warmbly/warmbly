// One row in the conversation list.
//
// Dense by design: sender + relative time on top, subject in the
// middle, snippet at the bottom, mailbox chip + tag color dots as the
// last meta row. Unread shows both as a left bar AND a font-weight
// change so it's scannable from across the room.

import type UniboxEmail from "@/lib/api/models/app/unibox/UniboxEmail";
import { useAppStore } from "@/stores";
import { cn } from "@/lib/utils";

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
  const m = s.match(/^"?([^"<]+)"?\s*<.+>$/);
  if (m) return m[1].trim();
  return s.replace(/<.+>/, "").trim() || s;
}

function initials(s: string): string {
  const name = fromName(s);
  const parts = name.split(/\s+/).filter(Boolean);
  if (parts.length >= 2) return (parts[0][0] + parts[1][0]).toUpperCase();
  return (parts[0]?.slice(0, 2) ?? "??").toUpperCase();
}

// Stable colour per sender so the eye learns to spot them.
function hueFor(s: string): number {
  let h = 0;
  for (let i = 0; i < s.length; i++) h = (h * 31 + s.charCodeAt(i)) >>> 0;
  return h % 360;
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
  const preview = email.body
    .replace(/<[^>]*>/g, "")
    .replace(/\s+/g, " ")
    .slice(0, 140);

  const mailbox = accounts.find((a) => a.id === email.account_id);
  const sender = fromName(email.from);
  const hue = hueFor(sender);

  // Thread-stacking: count of messages in the conversation behind this
  // row, and the conversation's assigned labels (categories).
  const messageCount = email.message_count ?? 1;
  const labels = email.labels ?? [];

  return (
    <button
      onClick={() => {
        setSelectedThreadId(threadId);
        setSelectedAccountId(email.account_id ?? null);
      }}
      className={cn(
        "group w-full text-left px-3 py-2 transition-colors flex items-start gap-2.5 relative",
        isSelected ? "bg-sky-50/80" : "hover:bg-slate-50/80",
      )}
    >
      {unread && (
        <span
          aria-hidden
          className="absolute left-0 top-2 bottom-2 w-[3px] rounded-r bg-sky-500"
        />
      )}
      <div
        className={cn(
          "size-7 rounded-full flex items-center justify-center shrink-0 text-[10px] font-semibold",
        )}
        style={
          isSelected
            ? { backgroundColor: "rgb(224 242 254)", color: "rgb(2 132 199)" }
            : {
                backgroundColor: `hsl(${hue} 70% 94%)`,
                color: `hsl(${hue} 55% 35%)`,
              }
        }
      >
        {initials(email.from)}
      </div>
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-2 min-w-0">
          <span
            className={cn(
              "text-[12.5px] truncate min-w-0",
              unread
                ? "text-slate-900 font-semibold"
                : "text-slate-800 font-medium",
            )}
          >
            {sender}
          </span>
          {messageCount > 1 && (
            <span
              className="shrink-0 font-mono tabular-nums text-[10px] text-slate-500 bg-slate-100 rounded-full px-1.5 h-4 inline-flex items-center"
              title={`${messageCount} messages in this conversation`}
            >
              {messageCount}
            </span>
          )}
          <span className="font-mono text-[10px] text-slate-400 tabular-nums shrink-0 ml-auto">
            {relative(date)}
          </span>
        </div>
        <div
          className={cn(
            "text-[12px] truncate mt-0.5",
            unread ? "text-slate-800 font-medium" : "text-slate-600",
          )}
        >
          {email.subject || "(no subject)"}
        </div>
        <div className="text-[11px] text-slate-400 truncate mt-0.5">
          {preview || "(no preview)"}
        </div>
        {(mailbox || labels.length > 0) && (
          <div className="mt-1 flex items-center gap-1 min-w-0 flex-wrap">
            {/* Conversation labels: colored dot + name chips, the
                            per-thread categories the user assigned. */}
            {labels.slice(0, 3).map((l) => (
              <TagChip key={l.id} title={l.title} color={l.color} />
            ))}
            {labels.length > 3 && (
              <span
                className="text-[9.5px] text-slate-400 font-mono"
                title={labels
                  .slice(3)
                  .map((l) => l.title)
                  .join(", ")}
              >
                +{labels.length - 3}
              </span>
            )}
            {mailbox && (
              <span className="inline-flex items-center h-4 px-1.5 rounded-sm bg-slate-100 text-slate-500 text-[9.5px] font-mono truncate max-w-[160px]">
                {mailbox.email}
              </span>
            )}
          </div>
        )}
      </div>
    </button>
  );
}

function TagChip({ title, color }: { title: string; color: string }) {
  // Tint a slim chip background from the tag's own color so two
  // chips read as visually distinct without needing to read the
  // text. We don't render solid coloured chips (too loud); the dot
  // carries the colour, the chip carries the name.
  return (
    <span
      className="inline-flex items-center gap-1 h-4 pl-1 pr-1.5 rounded-sm border bg-white text-[10px] font-medium text-slate-700 truncate max-w-[120px]"
      style={{
        borderColor: color ? `${color}60` : "rgb(226 232 240)",
        backgroundColor: color ? `${color}12` : "white",
      }}
      title={title}
    >
      <span
        aria-hidden
        className="block size-2 rounded-full shrink-0"
        style={{ backgroundColor: color || "#94a3b8" }}
      />
      {title}
    </span>
  );
}
