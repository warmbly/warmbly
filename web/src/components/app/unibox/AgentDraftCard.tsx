import { useEffect, useMemo, useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { SparklesIcon, SendIcon, Trash2Icon, Loader2Icon } from "lucide-react";
import toast from "react-hot-toast";

import useAgentDrafts from "@/lib/api/hooks/app/unibox/useAgentDrafts";
import { approveAgentDraft, discardAgentDraft } from "@/lib/api/client/app/unibox/agentDrafts";
import { useConfirm } from "@/hooks/context/confirm";

// AgentDraftCard surfaces the inbox agent's suggested reply for the open thread,
// awaiting a human decision. The body is editable inline: "Approve & send" sends
// the (possibly edited) text through the normal reply path; "Discard" dismisses
// it. Nothing is ever sent without one of these explicit actions.
export default function AgentDraftCard({ threadId }: { threadId: string }) {
    const drafts = useAgentDrafts();
    const draft = useMemo(
        () => (drafts.data?.data ?? []).find((d) => d.thread_id === threadId && d.status === "pending"),
        [drafts.data, threadId],
    );

    const [body, setBody] = useState("");
    useEffect(() => {
        // Seed the editor when a draft arrives (or changes).
        if (draft) setBody(draft.body);
    }, [draft?.id]); // eslint-disable-line react-hooks/exhaustive-deps

    const queryClient = useQueryClient();
    const confirm = useConfirm();

    const refresh = () => {
        void queryClient.invalidateQueries({ queryKey: ["unibox", "agent-drafts"] });
        void queryClient.invalidateQueries({ queryKey: ["unibox", "overview"] });
        void queryClient.invalidateQueries({ queryKey: ["unibox", "thread", threadId] });
    };

    const approve = useMutation({
        mutationFn: () => approveAgentDraft(draft!.id, body),
        onSuccess: () => {
            toast.success("Reply sent");
            refresh();
        },
        onError: () => toast.error("Could not send the reply"),
    });

    const discard = useMutation({
        mutationFn: () => discardAgentDraft(draft!.id),
        onSuccess: () => {
            toast.success("Draft discarded");
            refresh();
        },
        onError: () => toast.error("Could not discard the draft"),
    });

    if (!draft) return null;

    const busy = approve.isPending || discard.isPending;

    return (
        <div className="shrink-0 border-t border-slate-200 bg-gradient-to-b from-violet-50/70 to-white px-3 py-2.5">
            <div className="flex items-center gap-1.5">
                <span className="inline-flex size-5 shrink-0 items-center justify-center rounded-md bg-violet-100 text-violet-600 ring-1 ring-violet-200/70">
                    <SparklesIcon className="w-3 h-3" />
                </span>
                <span className="text-[12.5px] font-semibold text-slate-800">AI reply draft</span>
                {draft.intent_class ? (
                    <span className="rounded-full bg-slate-100 px-1.5 py-0.5 text-[10px] font-medium uppercase tracking-[0.08em] text-slate-500">
                        {draft.intent_class}
                    </span>
                ) : null}
                <span className="ml-auto text-[10.5px] text-slate-400">Review before sending</span>
            </div>

            <p className="mt-1 truncate text-[11px] text-slate-500">
                Reply to <span className="font-medium text-slate-700">{draft.to_addr}</span>
            </p>

            <textarea
                value={body}
                onChange={(e) => setBody(e.target.value)}
                rows={5}
                disabled={busy}
                className="mt-2 w-full resize-y rounded-md border border-slate-200 bg-white px-2.5 py-1.5 text-[12.5px] leading-relaxed text-slate-900 outline-none focus:border-sky-400 focus:ring-2 focus:ring-sky-100 disabled:opacity-60"
            />

            <div className="mt-2 flex items-center gap-1.5">
                <button
                    type="button"
                    onClick={() => approve.mutate()}
                    disabled={busy || body.trim() === ""}
                    className="h-7 px-2.5 rounded-md bg-sky-600 hover:bg-sky-700 disabled:opacity-50 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors"
                >
                    {approve.isPending ? <Loader2Icon className="w-3 h-3 animate-spin" /> : <SendIcon className="w-3 h-3" />}
                    Approve &amp; send
                </button>
                <button
                    type="button"
                    onClick={() => confirm.show("Discard this AI draft?", async () => {
                        await discard.mutateAsync();
                    })}
                    disabled={busy}
                    className="h-7 px-2 rounded-md border border-slate-200 hover:border-rose-300 text-slate-600 hover:text-rose-600 text-[12px] inline-flex items-center gap-1.5 transition-colors"
                >
                    <Trash2Icon className="w-3 h-3" />
                    Discard
                </button>
                <span className="ml-auto hidden md:inline text-[10.5px] text-slate-400">Edit the text above before sending.</span>
            </div>
        </div>
    );
}
