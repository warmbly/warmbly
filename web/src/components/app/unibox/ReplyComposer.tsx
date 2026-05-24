// Reply composer — pinned to the bottom of the thread pane.
//
// Slim chrome: a textarea with a hairline border on top of an action
// bar (send + cancel + schedule placeholder). ⌘+Enter sends; Esc
// clears focus. Reads as a quick reply, not a full editor.

import { useState } from "react";
import { ChevronDownIcon, SendIcon } from "lucide-react";
import toast from "react-hot-toast";
import sendReply from "@/lib/api/client/app/unibox/sendReply";
import type UniboxEmail from "@/lib/api/models/app/unibox/UniboxEmail";
import {
    PopoverMenu,
    PopoverMenuContent,
    PopoverMenuItem,
    PopoverMenuLabel,
    PopoverMenuTrigger,
} from "@/components/ui/popover-menu";

interface ReplyComposerProps {
    threadId: string;
    threadEmails: UniboxEmail[];
}

export function ReplyComposer({ threadId, threadEmails }: ReplyComposerProps) {
    const [reply, setReply] = useState("");
    const [isSending, setIsSending] = useState(false);

    const handleSend = async () => {
        if (!reply.trim()) return;

        const latestEmail = threadEmails[threadEmails.length - 1];
        if (!latestEmail?.account_id) {
            toast.error("Cannot determine sender account for this thread");
            return;
        }

        const replyTo = latestEmail.from?.trim();
        if (!replyTo) {
            toast.error("Cannot determine recipient for this reply");
            return;
        }

        const subjectBase = latestEmail.subject?.trim() || "Re:";
        const subject = /^re:/i.test(subjectBase) ? subjectBase : `Re: ${subjectBase}`;

        setIsSending(true);
        try {
            await sendReply({
                email_account_id: latestEmail.account_id,
                to: [replyTo],
                subject,
                body_plain: reply.trim(),
                body_html: reply.trim().replace(/\n/g, "<br />"),
                thread_id: threadId,
                send_mode: "instant",
            });
            setReply("");
            toast.success("Reply queued");
        } catch {
            toast.error("Failed to send reply");
        } finally {
            setIsSending(false);
        }
    };

    return (
        <div className="border-t border-slate-200 bg-white shrink-0">
            <textarea
                value={reply}
                onChange={(e) => setReply(e.target.value)}
                placeholder="Type a reply… (⌘ + Enter to send)"
                onKeyDown={(e) => {
                    if ((e.metaKey || e.ctrlKey) && e.key === "Enter") {
                        e.preventDefault();
                        handleSend();
                    }
                }}
                className="w-full min-h-[80px] max-h-60 px-5 py-3 text-[13px] text-slate-800 placeholder:text-slate-400 bg-transparent resize-y focus:outline-none"
            />
            <div className="px-3 py-2 border-t border-slate-200/60 flex items-center gap-1.5">
                <button
                    type="button"
                    onClick={handleSend}
                    disabled={!reply.trim() || isSending}
                    className="h-7 px-2.5 rounded-md bg-sky-600 hover:bg-sky-700 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
                >
                    <SendIcon className="w-3 h-3" />
                    {isSending ? "Sending…" : "Send"}
                </button>

                <PopoverMenu align="start" side="top">
                    <PopoverMenuTrigger asChild>
                        <button
                            type="button"
                            className="h-7 px-2 rounded-md border border-slate-200 hover:border-slate-300 text-slate-700 hover:text-slate-900 text-[12px] inline-flex items-center gap-1 transition-colors"
                        >
                            Schedule
                            <ChevronDownIcon className="w-3 h-3 text-slate-400" />
                        </button>
                    </PopoverMenuTrigger>
                    <PopoverMenuContent>
                        <PopoverMenuLabel>Send at</PopoverMenuLabel>
                        <PopoverMenuItem>In 1 hour</PopoverMenuItem>
                        <PopoverMenuItem>Tomorrow 9:00</PopoverMenuItem>
                        <PopoverMenuItem>Next Monday 9:00</PopoverMenuItem>
                    </PopoverMenuContent>
                </PopoverMenu>

                {reply && (
                    <button
                        type="button"
                        onClick={() => setReply("")}
                        className="h-7 px-2 rounded-md text-slate-500 hover:text-slate-900 text-[12px] transition-colors"
                    >
                        Discard
                    </button>
                )}

                <span className="ml-auto font-mono text-[10px] text-slate-400 tabular-nums">
                    {reply.length}/{4000}
                </span>
            </div>
        </div>
    );
}
