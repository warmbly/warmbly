// Thread view — right pane of the unibox.
//
// Top: subject + meta + actions. Body: message stream — each message is
// a clean block with hairline header, no card containers. Bottom: pinned
// ReplyComposer.

import { MessageBubble } from "./MessageBubble";
import { ReplyComposer } from "./ReplyComposer";
import { useAppStore } from "@/stores";
import { ArchiveIcon, MailCheckIcon, TrashIcon } from "lucide-react";
import { SectionBar } from "@/components/layout/Page";

interface ThreadViewProps {
    threadId: string;
}

export function ThreadView({ threadId }: ThreadViewProps) {
    const emails = useAppStore((s) => s.uniboxEmails);
    const threadEmails = emails
        .filter((e) => e.thread_id === threadId || e.id === threadId)
        .sort((a, b) => new Date(a.date).getTime() - new Date(b.date).getTime());

    if (threadEmails.length === 0) {
        return (
            <div className="flex-1 flex items-center justify-center text-[12px] text-slate-400">
                Loading thread…
            </div>
        );
    }

    const subject = threadEmails[0]?.subject || "(no subject)";
    const participants = new Set(threadEmails.map((e) => e.from));

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
                <div className="ml-auto flex items-center gap-1">
                    <button
                        aria-label="Mark unread"
                        className="size-7 rounded-md text-slate-500 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center justify-center transition-colors"
                    >
                        <MailCheckIcon className="w-3.5 h-3.5" />
                    </button>
                    <button
                        aria-label="Archive"
                        className="size-7 rounded-md text-slate-500 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center justify-center transition-colors"
                    >
                        <ArchiveIcon className="w-3.5 h-3.5" />
                    </button>
                    <button
                        aria-label="Delete"
                        className="size-7 rounded-md text-slate-500 hover:text-red-600 hover:bg-red-50 inline-flex items-center justify-center transition-colors"
                    >
                        <TrashIcon className="w-3.5 h-3.5" />
                    </button>
                </div>
            </div>

            <SectionBar
                label={`${threadEmails.length} ${threadEmails.length === 1 ? "message" : "messages"}`}
                count={participants.size}
            />

            <div className="flex-1 overflow-y-auto divide-y divide-slate-200/60">
                {threadEmails.map((email) => (
                    <MessageBubble key={email.id} email={email} />
                ))}
            </div>

            <ReplyComposer threadId={threadId} threadEmails={threadEmails} />
        </div>
    );
}
