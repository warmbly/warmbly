// Writing a campaign email with AI: the whole email from a short brief, and
// subject-line ideas. Both know where the step sits in the sequence, so a
// follow-up reads the emails already sent instead of pitching from scratch.

import React from "react";
import { Link } from "@tanstack/react-router";
import { ArrowUpIcon, RefreshCwIcon, SparklesIcon } from "lucide-react";
import toast from "react-hot-toast/headless";
import { motion } from "framer-motion";
import useWritingStream, { isAbort } from "@/components/app/ai/useWritingStream";
import useCurrentOrganization from "@/lib/api/hooks/app/organizations/useCurrentOrganization";
import { usePermission } from "@/hooks/usePermission";
import useAiMetered from "@/hooks/useAiMetered";
import AgentMark from "@/components/app/agent/AgentMark";
import { PopoverMenu, PopoverMenuContent, PopoverMenuTrigger } from "@/components/ui/popover-menu";
import { shortcutLabel } from "@/components/ui/shortcut-tooltip";
import useFocusOnOpen from "@/hooks/useFocusOnOpen";
import useCustomFieldKeys from "@/lib/api/hooks/app/contacts/useCustomFieldKeys";
import { FIELD_TOKEN_RE, isStandardKey, tokenLabel } from "@/lib/templateVars";
import { renderPreview } from "./emailPreview";
import { type EmailAIContext, SUBJECT_MOVES, parseSubjects, stepLabel, subjectsPrompt } from "./emailAIPrompt";
import type { EmailAIWriter } from "./useEmailAIWriter";
import { reportAIError } from "@/components/app/ai/reportAIError";
import { fmtUsdAbout } from "@/lib/money";
import { AI_ESTIMATE_USD } from "@/lib/aiEstimates";

// Briefs offered as one-click starting points. A follow-up gets the moves that
// earn a reply after silence; a first email gets a rewrite when it has a draft.
const FOLLOW_UP_BRIEFS = [
    "Try a different angle on their problem",
    "Share one useful tip they can use today",
    "Short, friendly nudge with an easier ask",
    "Last email: give them an easy way out",
];
const FIRST_BRIEFS = [
    "Lead with a problem their business likely has",
    "Offer something small and useful, no call yet",
];

// WriteEmailWithAI is the brief popover above the subject; the writing itself
// shows on the email (see useEmailAIWriter).
export function WriteEmailWithAI({ writer, ctx, bodyPlain }: { writer: EmailAIWriter; ctx: EmailAIContext; bodyPlain: string }) {
    const canAI = usePermission("USE_AI");
    const metered = useAiMetered();
    const { data: org } = useCurrentOrganization();
    const [open, setOpen] = React.useState(false);
    const [brief, setBrief] = React.useState("");
    const writeBriefRef = React.useRef<HTMLTextAreaElement>(null);
    useFocusOnOpen(open, writeBriefRef);
    const briefs = ctx.earlier.length > 0 ? FOLLOW_UP_BRIEFS : FIRST_BRIEFS;
    const hasDraft = bodyPlain.trim() !== "";
    const knowsProduct = !!org?.product_description?.trim();
    const canRun = brief.trim() !== "" || hasDraft || ctx.earlier.length > 0;
    const busy = writer.phase === "busy";

    function write() {
        if (!canRun || busy) return;
        writer.start(brief);
        setOpen(false);
    }

    if (!canAI) return null;

    return (
        <PopoverMenu open={open} onOpenChange={setOpen}>
            <PopoverMenuTrigger asChild>
                <button
                    type="button"
                    disabled={busy}
                    title="Write this email with AI"
                    className="h-7 px-2.5 inline-flex items-center gap-1.5 rounded-md border border-sky-200 bg-sky-50 text-[12px] font-medium text-sky-700 transition-colors hover:border-sky-300 hover:bg-sky-100 disabled:opacity-60"
                >
                    {busy ? <AgentMark variant="bare" size={13} state="thinking" /> : <SparklesIcon className="w-3.5 h-3.5" />}
                    Write with AI
                </button>
            </PopoverMenuTrigger>
            <PopoverMenuContent minWidth={340} role="dialog" aria-label="Write with AI" className="w-[340px] p-0">
                <div>
                    <div className="px-3 pt-2.5">
                        <div className="flex items-baseline justify-between gap-2">
                            <span className="text-[10px] font-medium uppercase tracking-[0.14em] text-slate-400">
                                {stepLabel(ctx)}
                            </span>
                            {ctx.earlier.length > 0 && (
                                <span className="text-[10.5px] text-slate-400">
                                    Reads the {ctx.earlier.length === 1 ? "earlier email" : `${ctx.earlier.length} earlier emails`}
                                </span>
                            )}
                        </div>
                        <textarea
                            ref={writeBriefRef}
                            value={brief}
                            onChange={(e) => setBrief(e.target.value)}
                            onKeyDown={(e) => {
                                if (e.key === "Enter" && !e.shiftKey) {
                                    e.preventDefault();
                                    write();
                                }
                            }}
                            rows={3}
                            maxLength={1000}
                            placeholder={
                                ctx.earlier.length > 0
                                    ? "What should this follow-up do? Leave blank and AI picks a fresh angle."
                                    : hasDraft
                                      ? "What should change? Leave blank to rewrite your draft."
                                      : "Who is it for and what can you do for them? e.g. Dental clinics in Texas, we fill empty appointment slots"
                            }
                            className="mt-1.5 w-full resize-none rounded-md border border-slate-200 bg-white px-2.5 py-2 text-[12.5px] leading-relaxed text-slate-800 outline-none placeholder:text-slate-400 focus:border-sky-400 focus:ring-2 focus:ring-sky-100"
                        />
                    </div>
                    <div className="flex flex-wrap gap-1 px-3 pt-1.5">
                        {briefs.map((b) => (
                            <button
                                key={b}
                                type="button"
                                onClick={() => setBrief(b)}
                                className="rounded-full border border-slate-200 px-2 py-0.5 text-[11px] text-slate-600 transition-colors hover:border-sky-300 hover:bg-sky-50 hover:text-sky-700"
                            >
                                {b}
                            </button>
                        ))}
                    </div>
                    {!knowsProduct && (
                        <p className="mx-3 mt-2 rounded-md bg-amber-50 px-2 py-1.5 text-[11px] leading-snug text-amber-800">
                            AI does not know what you sell yet, so it writes from your brief alone. Add it in{" "}
                            <Link to="/app/settings/workspace" className="font-medium underline underline-offset-2">
                                Workspace settings
                            </Link>{" "}
                            and every email it writes can say how you help.
                        </p>
                    )}
                    <div className="mt-2 flex items-center gap-2 border-t border-slate-100 px-3 py-2">
                        <span className="mr-auto text-[10.5px] text-slate-400">
                            {hasDraft ? "Replaces the current email. You can undo." : "Your mailbox signature is added for you."}
                            {metered && ` Costs ${fmtUsdAbout(AI_ESTIMATE_USD.writingAssistant)}.`}
                        </span>
                        <button
                            type="button"
                            onClick={write}
                            disabled={!canRun}
                            className="h-7 px-2.5 inline-flex items-center gap-1 rounded-md bg-sky-600 text-[12px] font-medium text-white transition-colors hover:bg-sky-700 disabled:opacity-40"
                        >
                            <ArrowUpIcon className="h-3.5 w-3.5" />
                            Write
                        </button>
                    </div>
                </div>
            </PopoverMenuContent>
        </PopoverMenu>
    );
}

// SubjectAI: subject ideas from the body, a short brief, or a rewrite of the
// current subject. Opened from the sparkle at the end of the subject field or
// with ⌘J in it; picking an idea replaces the subject.
export function SubjectAI({
    ctx,
    bodyPlain,
    subject,
    open,
    onOpenChange,
    onPick,
}: {
    ctx: EmailAIContext;
    bodyPlain: string;
    subject: string;
    open: boolean;
    onOpenChange: (open: boolean) => void;
    onPick: (subject: string) => void;
}) {
    const stream = useWritingStream();
    const [pending, setPending] = React.useState(false);
    const { data: customKeys = [] } = useCustomFieldKeys();
    const [brief, setBrief] = React.useState("");
    const [ideas, setIdeas] = React.useState<string[]>([]);
    const [last, setLast] = React.useState<{ goal: string; brief: string } | null>(null);
    const briefRef = React.useRef<HTMLInputElement>(null);
    useFocusOnOpen(open, briefRef);
    const hasBody = bodyPlain.trim() !== "";
    const hasSubject = subject.trim() !== "";
    const canStart = hasBody || ctx.earlier.length > 0;

    // Ideas appear as each line finishes.
    async function generate(goal: string, text: string) {
        setLast({ goal, brief: text });
        setIdeas([]);
        setPending(true);
        try {
            const prompt = subjectsPrompt({ ctx, body: bodyPlain, brief: text, current: subject, goal, fields: customKeys.filter((k) => !isStandardKey(k)) });
            const res = await stream.run(
                { path: "/generation/write", body: { prompt } },
                { onText: (shown) => setIdeas(parseSubjects(shown.slice(0, shown.lastIndexOf("\n") + 1))) },
            );
            const next = parseSubjects(res.text);
            setIdeas(next);
            if (next.length === 0) toast.error("No ideas came back. Try again.");
        } catch (e) {
            if (!isAbort(e)) reportAIError(e);
        } finally {
            setPending(false);
        }
    }

    // Opening offers ideas straight away, however it was opened (⌘J included);
    // closing, picking an idea included, stops a stream still writing.
    const onOpen = React.useRef(() => {});
    onOpen.current = () => {
        if (canStart && ideas.length === 0 && !pending) void generate("", brief);
    };
    const { stop } = stream;
    React.useEffect(() => {
        if (open) onOpen.current();
        else stop();
    }, [open, stop]);

    return (
        <PopoverMenu
            open={open}
            align="end"
            onOpenChange={onOpenChange}
        >
            <PopoverMenuTrigger asChild>
                <button
                    type="button"
                    onMouseDown={(e) => e.preventDefault()}
                    title={`Subject ideas with AI (${shortcutLabel("mod+J")})`}
                    aria-label="Subject ideas with AI"
                    className="h-7 px-1.5 inline-flex items-center gap-1 rounded text-sky-600 transition-colors hover:bg-sky-50 hover:text-sky-700"
                >
                    <SparklesIcon className="h-3.5 w-3.5" />
                    <kbd className="hidden sm:inline font-sans text-[10px] text-sky-500/80">{shortcutLabel("mod+J")}</kbd>
                </button>
            </PopoverMenuTrigger>
            <PopoverMenuContent minWidth={340} role="dialog" aria-label="Subject ideas" className="w-[360px] max-w-[calc(100vw-24px)] p-0">
                <div className="flex items-center gap-1.5 border-b border-slate-100 px-3 py-2">
                    <SparklesIcon className="h-3.5 w-3.5 text-sky-600" />
                    <span className="text-[12px] font-medium text-slate-800">Subject ideas</span>
                    <span className="ml-auto text-[10.5px] text-slate-400">{stepLabel(ctx)}</span>
                </div>
                <form
                    className="flex items-center gap-1 border-b border-slate-100 px-2 py-1.5"
                    onSubmit={(e) => {
                        e.preventDefault();
                        if (!pending) void generate("", brief);
                    }}
                >
                    <input
                        ref={briefRef}
                        value={brief}
                        onChange={(e) => setBrief(e.target.value)}
                        placeholder={hasBody ? "Steer it (optional): mention their hiring…" : "What is the email about?"}
                        className="h-7 min-w-0 flex-1 rounded px-1.5 text-[12px] text-slate-800 outline-none placeholder:text-slate-400"
                    />
                    <button
                        type="submit"
                        disabled={pending || (!canStart && !brief.trim())}
                        aria-label="Suggest subjects"
                        className="inline-flex h-6 w-6 items-center justify-center rounded bg-sky-600 text-white transition-colors hover:bg-sky-700 disabled:opacity-40"
                    >
                        <ArrowUpIcon className="h-3.5 w-3.5" />
                    </button>
                </form>
                {hasSubject && (
                    <div className="flex flex-wrap gap-1 border-b border-slate-100 px-2 py-1.5">
                        {SUBJECT_MOVES.map((m) => (
                            <button
                                key={m.label}
                                type="button"
                                disabled={pending}
                                onClick={() => void generate(m.goal, brief)}
                                className="h-6 rounded-md border border-slate-200 px-2 text-[11px] text-slate-600 transition-colors hover:border-sky-300 hover:bg-sky-50 hover:text-sky-700 disabled:opacity-50"
                            >
                                {m.label}
                            </button>
                        ))}
                    </div>
                )}
                <div className="max-h-[300px] overflow-y-auto p-1">
                    {ideas.length === 0 && !pending ? (
                        <p className="px-2.5 py-3 text-[11.5px] leading-snug text-slate-500">
                            {canStart
                                ? "Ask for ideas above, or pick a move to rewrite the current subject."
                                : "Write the body first, or say in a few words what the email is about."}
                        </p>
                    ) : (
                        ideas.map((idea) => (
                            <motion.button
                                key={idea}
                                initial={{ opacity: 0, y: 3 }}
                                animate={{ opacity: 1, y: 0 }}
                                transition={{ duration: 0.18 }}
                                type="button"
                                onClick={() => {
                                    onPick(idea);
                                    onOpenChange(false);
                                }}
                                className="block w-full rounded px-2.5 py-1.5 text-left hover:bg-slate-100"
                            >
                                <TokenText text={idea} />
                                {idea !== renderPreview(idea) && (
                                    <span className="mt-0.5 block truncate text-[11px] text-slate-400">{renderPreview(idea)}</span>
                                )}
                            </motion.button>
                        ))
                    )}
                    {pending && (
                        <div className="flex items-center gap-2 px-2.5 py-2.5">
                            <AgentMark variant="bare" size={14} state="thinking" />
                            <span className="ai-shimmer-text text-[12px] font-medium">
                                {ideas.length ? "Writing more…" : "Thinking of subjects…"}
                            </span>
                        </div>
                    )}
                </div>
                {ideas.length > 0 && !pending && last && (
                    <div className="border-t border-slate-100 p-1">
                        <button
                            type="button"
                            onClick={() => void generate(last.goal, last.brief)}
                            className="flex h-7 w-full items-center gap-1.5 rounded px-2.5 text-[11.5px] text-slate-600 hover:bg-slate-50 hover:text-slate-900"
                        >
                            <RefreshCwIcon className="h-3 w-3" />
                            More ideas
                        </button>
                    </div>
                )}
            </PopoverMenuContent>
        </PopoverMenu>
    );
}

// TokenText shows a subject with its merge fields as small chips.
function TokenText({ text }: { text: string }) {
    const parts: React.ReactNode[] = [];
    let last = 0;
    for (const m of text.matchAll(new RegExp(FIELD_TOKEN_RE.source, "g"))) {
        const at = m.index ?? 0;
        if (at > last) parts.push(text.slice(last, at));
        parts.push(
            <span key={at} className="mx-px rounded bg-sky-50 px-1 text-[11.5px] text-sky-700">
                {tokenLabel(m[0].replace(/\s*\|[^}]*/, ""))}
            </span>,
        );
        last = at + m[0].length;
    }
    if (last < text.length) parts.push(text.slice(last));
    return <span className="block text-[12.5px] leading-snug text-slate-800">{parts}</span>;
}
