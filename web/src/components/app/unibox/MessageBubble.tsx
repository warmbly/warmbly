// Single message in a thread.
//
// Header row holds sender (avatar + name + email), recipient(s), and
// timestamp. Body sits below in regular prose with light styling, no
// containing card, just hairlines between messages.
//
// Per-message Reply / Forward affordances surface on hover (and stay
// visible on touch via the md: breakpoint) so the user can choose
// which specific message in the thread their reply targets.

import { CornerUpLeftIcon, ForwardIcon } from "lucide-react";
import type UniboxEmail from "@/lib/api/models/app/unibox/UniboxEmail";

interface MessageBubbleProps {
    email: UniboxEmail;
    onReply?: () => void;
    onForward?: () => void;
}

function fromName(s: string): string {
    const m = s.match(/^"?([^"<]+)"?\s*<.+>$/);
    if (m) return m[1].trim();
    return s.replace(/<.+>/, "").trim() || s;
}

function fromAddr(s: string): string | null {
    const m = s.match(/<([^>]+)>/);
    if (m) return m[1].trim();
    return null;
}

function initials(s: string): string {
    const name = fromName(s);
    const parts = name.split(/\s+/).filter(Boolean);
    if (parts.length >= 2) return (parts[0][0] + parts[1][0]).toUpperCase();
    return (parts[0]?.slice(0, 2) ?? "??").toUpperCase();
}

export function MessageBubble({ email, onReply, onForward }: MessageBubbleProps) {
    const date = new Date(email.date);
    const dateStr = date.toLocaleString(undefined, {
        month: "short",
        day: "numeric",
        hour: "2-digit",
        minute: "2-digit",
    });

    const name = fromName(email.from);
    const addr = fromAddr(email.from);

    return (
        <article className="group px-3 sm:px-5 py-4">
            <header className="flex items-start gap-3 mb-3">
                <div className="size-7 rounded-full bg-slate-100 text-slate-600 flex items-center justify-center text-[10px] font-semibold shrink-0">
                    {initials(email.from)}
                </div>
                <div className="min-w-0 flex-1">
                    <div className="flex items-baseline gap-2">
                        <span className="text-[12.5px] font-semibold text-slate-900 truncate">
                            {name}
                        </span>
                        {addr && (
                            <span className="font-mono text-[10.5px] text-slate-400 truncate">
                                {addr}
                            </span>
                        )}
                    </div>
                    <div className="text-[11px] text-slate-500 mt-0.5 flex items-center gap-1.5">
                        <span className="truncate min-w-0">to {email.to}</span>
                    </div>
                </div>
                <div className="flex items-center gap-1 shrink-0">
                    <div className="flex items-center gap-0.5 opacity-100 md:opacity-0 md:group-hover:opacity-100 transition-opacity">
                        {onReply && (
                            <button
                                type="button"
                                onClick={onReply}
                                aria-label="Reply to this message"
                                title="Reply to this message"
                                className="size-6 rounded text-slate-500 hover:text-sky-700 hover:bg-sky-50 inline-flex items-center justify-center transition-colors"
                            >
                                <CornerUpLeftIcon className="w-3 h-3" />
                            </button>
                        )}
                        {onForward && (
                            <button
                                type="button"
                                onClick={onForward}
                                aria-label="Forward this message"
                                title="Forward this message"
                                className="size-6 rounded text-slate-500 hover:text-violet-700 hover:bg-violet-50 inline-flex items-center justify-center transition-colors"
                            >
                                <ForwardIcon className="w-3 h-3" />
                            </button>
                        )}
                    </div>
                    <span className="font-mono text-[10.5px] text-slate-400 tabular-nums">
                        {dateStr}
                    </span>
                </div>
            </header>
            <div
                className="text-[13px] text-slate-800 leading-relaxed prose prose-sm max-w-none break-words prose-p:my-2 prose-a:text-sky-600"
                dangerouslySetInnerHTML={{ __html: email.body }}
            />
        </article>
    );
}
