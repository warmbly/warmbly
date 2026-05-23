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

interface ConversationItemProps {
    email: UniboxEmail;
}

export function ConversationItem({ email }: ConversationItemProps) {
    const selectedThreadId = useAppStore((s) => s.selectedThreadId);
    const setSelectedThreadId = useAppStore((s) => s.setSelectedThreadId);

    const threadId = email.thread_id || email.id;
    const isSelected = selectedThreadId === threadId;
    const date = new Date(email.date);
    const unread = !email.is_seen;
    const preview = email.body.replace(/<[^>]*>/g, "").replace(/\s+/g, " ").slice(0, 100);

    return (
        <button
            onClick={() => setSelectedThreadId(threadId)}
            className={cn(
                "group w-full text-left px-3 py-2.5 transition-colors flex items-start gap-2.5 relative",
                isSelected ? "bg-sky-50/80" : "hover:bg-slate-50/80",
            )}
        >
            {unread && (
                <span
                    aria-hidden
                    className="absolute left-0 top-2.5 bottom-2.5 w-[3px] rounded-r bg-sky-500"
                />
            )}
            <div
                className={cn(
                    "size-7 rounded-full flex items-center justify-center shrink-0 text-[10px] font-semibold",
                    isSelected ? "bg-sky-100 text-sky-700" : "bg-slate-100 text-slate-600",
                )}
            >
                {initials(email.from)}
            </div>
            <div className="min-w-0 flex-1">
                <div className="flex items-baseline justify-between gap-2">
                    <span
                        className={cn(
                            "text-[12.5px] truncate",
                            unread ? "text-slate-900 font-semibold" : "text-slate-800 font-medium",
                        )}
                    >
                        {fromName(email.from)}
                    </span>
                    <span className="font-mono text-[10px] text-slate-400 tabular-nums shrink-0">
                        {relative(date)}
                    </span>
                </div>
                <div className="text-[12px] text-slate-700 truncate mt-0.5">
                    {email.subject || "(no subject)"}
                </div>
                <div className="text-[11px] text-slate-400 truncate mt-0.5">
                    {preview}
                </div>
            </div>
        </button>
    );
}
