// Single message in a thread.
//
// Header row holds sender (avatar + name + email), recipient(s), and
// timestamp. Body sits below in regular prose with light styling, no
// containing card, just hairlines between messages.
//
// Bodies are fetched per expanded message: the thread endpoint carries only a
// preview line each, so rendering that as the message showed the first ~100
// characters of a ten-line email as the whole thing. Collapsed messages keep
// showing the preview, Gmail-style.
//
// Per-message Reply / Forward affordances surface on hover (and stay
// visible on touch via the md: breakpoint) so the user can choose
// which specific message in the thread their reply targets.

import React from "react";
import { AlertCircleIcon, CornerUpLeftIcon, ForwardIcon, Loader2Icon } from "lucide-react";
import EmailBody from "./EmailBody";
import useUniboxEmail from "@/lib/api/hooks/app/unibox/useUniboxEmail";
import type UniboxEmail from "@/lib/api/models/app/unibox/UniboxEmail";

interface MessageBubbleProps {
    email: UniboxEmail;
    /** Expanded on mount. The newest message and anything unread open by default. */
    defaultExpanded?: boolean;
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

export function MessageBubble({
    email,
    defaultExpanded = false,
    onReply,
    onForward,
}: MessageBubbleProps) {
    const [expanded, setExpanded] = React.useState(defaultExpanded);
    const body = useUniboxEmail(email.id, expanded);

    const date = new Date(email.date);
    const dateStr = date.toLocaleString(undefined, {
        month: "short",
        day: "numeric",
        hour: "2-digit",
        minute: "2-digit",
    });

    const name = fromName(email.from);
    const addr = fromAddr(email.from);
    const snippet = email.snippet ?? "";

    return (
        <article className="group px-3 sm:px-5 py-4">
            {/* Not a <button>: the reply/forward controls live inside it. */}
            <header
                role="button"
                tabIndex={0}
                aria-expanded={expanded}
                className="flex items-start gap-3 mb-3 cursor-pointer"
                onClick={() => setExpanded((v) => !v)}
                onKeyDown={(e) => {
                    if (e.key === "Enter" || e.key === " ") {
                        e.preventDefault();
                        setExpanded((v) => !v);
                    }
                }}
            >
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
                                onClick={(e) => {
                                    e.stopPropagation();
                                    onReply();
                                }}
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
                                onClick={(e) => {
                                    e.stopPropagation();
                                    onForward();
                                }}
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

            {!expanded ? (
                <button
                    type="button"
                    onClick={() => setExpanded(true)}
                    className="w-full text-left text-[13px] text-slate-500 truncate hover:text-slate-700 transition-colors"
                    title="Show this message"
                >
                    {snippet || "Show this message"}
                </button>
            ) : body.isPending ? (
                <div className="flex items-center gap-2 text-[12px] text-slate-400">
                    <Loader2Icon className="w-3.5 h-3.5 animate-spin" />
                    Loading message…
                </div>
            ) : body.isError ? (
                <div className="text-[12.5px] text-slate-600">
                    {/* Falling back to the preview beats an empty message pane. */}
                    <p className="whitespace-pre-wrap break-words">{snippet}</p>
                    <p className="mt-1.5 flex items-center gap-1.5 text-[11.5px] text-amber-700">
                        <AlertCircleIcon className="w-3.5 h-3.5 shrink-0" />
                        Couldn't load the full message.
                        <button
                            type="button"
                            onClick={() => body.refetch()}
                            className="underline underline-offset-2 hover:text-amber-800"
                        >
                            Try again
                        </button>
                    </p>
                </div>
            ) : (
                <>
                    <EmailBody html={body.data?.body_html} plain={body.data?.body_plain} />
                    {body.data?.body_truncated && (
                        <p className="mt-2 flex items-center gap-1.5 text-[11.5px] text-amber-700">
                            <AlertCircleIcon className="w-3.5 h-3.5 shrink-0" />
                            Only a preview of this message is stored, so the rest isn't
                            shown here. Open it in the mailbox to read it in full.
                        </p>
                    )}
                </>
            )}
        </article>
    );
}
