// Right-side AI assistant panel. Persistent across routes, opened from the
// header sparkle button or Cmd+I. Streams a tool-using agent over SSE: text
// deltas, collapsible tool steps, inline approval cards for write/send actions,
// draft-artifact deep links, a stop button, and a credits meter in the footer.
//
// Follows the InboxDetails drawer visual language (fixed right drawer, slate/sky
// theme). Sends never happen from AI: draft artifacts open the real editors.

import React from "react";
import { AnimatePresence, motion } from "framer-motion";
import { useLocation, useNavigate } from "react-router-dom";
import {
    SparklesIcon,
    XIcon,
    ArrowUpIcon,
    SquareIcon,
    PlusIcon,
    CheckIcon,
    Loader2Icon,
    WrenchIcon,
    ExternalLinkIcon,
    AlertTriangleIcon,
    ShieldQuestionIcon,
} from "lucide-react";
import { useAppStore } from "@/stores";
import createAgentSession from "@/lib/api/client/app/agent/createAgentSession";
import streamAgentRun from "@/lib/api/client/app/agent/streamAgentRun";
import type { AgentStreamEvent } from "@/lib/api/models/app/agent/Agent";

type ToolStep = {
    id: string;
    tool: string;
    argsSummary?: string;
    result?: string;
    done: boolean;
    entityType?: string;
    entityId?: string;
    openURL?: string;
};

type Pending = {
    toolCallId: string;
    tool: string;
    risk: string;
    argsSummary?: string;
};

type Block =
    | { kind: "text"; text: string }
    | { kind: "tool"; step: ToolStep }
    | { kind: "error"; code?: string; message: string };

type Turn = {
    id: string;
    role: "user" | "assistant";
    blocks: Block[];
};

let mid = 0;
const nextId = () => `m${++mid}`;

export default function AgentPanel() {
    const open = useAppStore((s) => s.aiAssistantOpen);
    const setOpen = useAppStore((s) => s.setAIAssistantOpen);
    const navigate = useNavigate();
    const location = useLocation();

    const [sessionId, setSessionId] = React.useState<string | null>(null);
    const [turns, setTurns] = React.useState<Turn[]>([]);
    const [input, setInput] = React.useState("");
    const [running, setRunning] = React.useState(false);
    const [pending, setPending] = React.useState<Pending | null>(null);
    const [credits, setCredits] = React.useState<number | null>(null);
    const [budget, setBudget] = React.useState<number>(20);
    const [iteration, setIteration] = React.useState<number>(0);
    const abortRef = React.useRef<AbortController | null>(null);
    const scrollRef = React.useRef<HTMLDivElement>(null);

    React.useEffect(() => {
        if (scrollRef.current) {
            scrollRef.current.scrollTop = scrollRef.current.scrollHeight;
        }
    }, [turns, pending]);

    // Resource string mirrors the presence shape so the agent knows what the
    // user is looking at ("this campaign", "here").
    const resource = React.useMemo(() => resourceFromPath(location.pathname), [location.pathname]);

    function resetChat() {
        abortRef.current?.abort();
        setSessionId(null);
        setTurns([]);
        setPending(null);
        setRunning(false);
    }

    function appendAssistantEvent(ev: AgentStreamEvent) {
        setTurns((prev) => {
            const next = [...prev];
            let cur = next[next.length - 1];
            if (!cur || cur.role !== "assistant") {
                cur = { id: nextId(), role: "assistant", blocks: [] };
                next.push(cur);
            } else {
                cur = { ...cur, blocks: [...cur.blocks] };
                next[next.length - 1] = cur;
            }
            applyEvent(cur, ev);
            return next;
        });
    }

    async function runStream(path: string, body: Record<string, unknown>) {
        setRunning(true);
        setPending(null);
        setIteration(0);
        const ac = new AbortController();
        abortRef.current = ac;
        await streamAgentRun(
            path,
            body,
            (ev) => {
                if (ev.type === "iteration") {
                    if (typeof ev.credits_remaining === "number") setCredits(ev.credits_remaining);
                    if (typeof ev.budget === "number") setBudget(ev.budget);
                    if (typeof ev.iteration === "number") setIteration(ev.iteration);
                    return;
                }
                if (ev.type === "approval_required") {
                    setPending({
                        toolCallId: ev.tool_call_id || "",
                        tool: ev.tool || "",
                        risk: ev.risk || "write",
                        argsSummary: ev.args_summary,
                    });
                    return;
                }
                if (ev.type === "done") {
                    if (typeof ev.credits_remaining === "number") setCredits(ev.credits_remaining);
                    return;
                }
                appendAssistantEvent(ev);
                if (ev.type === "error" && typeof ev.credits_remaining === "number") {
                    setCredits(ev.credits_remaining);
                }
            },
            ac.signal,
        );
        setRunning(false);
        abortRef.current = null;
    }

    async function send() {
        const text = input.trim();
        if (!text || running) return;
        setInput("");
        setTurns((prev) => [...prev, { id: nextId(), role: "user", blocks: [{ kind: "text", text }] }]);

        let sid = sessionId;
        if (!sid) {
            try {
                const sess = await createAgentSession({ page: location.pathname, resource });
                sid = sess.id;
                setSessionId(sid);
            } catch {
                appendAssistantEvent({ type: "error", message: "Could not start a session." });
                return;
            }
        }
        await runStream(`/ai/sessions/${sid}/messages`, {
            message_id: nextId() + ":" + Date.now(),
            text,
            page: location.pathname,
            resource,
        });
    }

    async function decide(decision: "approve" | "deny" | "always_allow") {
        if (!sessionId || !pending) return;
        setPending(null);
        await runStream(`/ai/sessions/${sessionId}/approve`, { decision });
    }

    function stop() {
        abortRef.current?.abort();
        setRunning(false);
    }

    return (
        <AnimatePresence>
            {open && (
                <>
                    <motion.div
                        initial={{ opacity: 0 }}
                        animate={{ opacity: 1 }}
                        exit={{ opacity: 0 }}
                        onClick={() => setOpen(false)}
                        className="fixed inset-0 z-40 bg-slate-900/20 md:hidden"
                    />
                    <motion.aside
                        initial={{ x: "100%" }}
                        animate={{ x: 0 }}
                        exit={{ x: "100%" }}
                        transition={{ type: "spring", stiffness: 380, damping: 40 }}
                        className="fixed right-0 top-0 z-50 h-full w-full sm:w-[420px] bg-white border-l border-slate-200 shadow-[0_0_60px_-12px_rgba(15,23,42,0.3)] flex flex-col"
                    >
                        {/* Header */}
                        <div className="shrink-0 px-4 h-14 flex items-center gap-2 border-b border-slate-200">
                            <div className="size-7 rounded-md bg-sky-50 border border-sky-100 text-sky-600 flex items-center justify-center">
                                <SparklesIcon className="w-4 h-4" />
                            </div>
                            <div className="min-w-0 flex-1">
                                <div className="text-[13px] font-semibold text-slate-900">Assistant</div>
                            </div>
                            <button
                                onClick={resetChat}
                                title="New chat"
                                className="h-7 px-2 rounded-md text-[12px] text-slate-600 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center gap-1 transition-colors"
                            >
                                <PlusIcon className="w-3.5 h-3.5" />
                                New
                            </button>
                            <button
                                onClick={() => setOpen(false)}
                                className="size-7 rounded-md text-slate-500 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center justify-center transition-colors"
                            >
                                <XIcon className="w-4 h-4" />
                            </button>
                        </div>

                        {/* Messages */}
                        <div ref={scrollRef} className="flex-1 min-h-0 overflow-y-auto px-4 py-4 space-y-4">
                            {turns.length === 0 && !running && (
                                <EmptyState />
                            )}
                            {turns.map((t) => (
                                <TurnView key={t.id} turn={t} onOpen={(u) => navigate(stripOrigin(u))} />
                            ))}
                            {pending && (
                                <ApprovalCard pending={pending} onDecide={decide} />
                            )}
                            {running && !pending && (
                                <div className="flex items-center gap-2 text-[12px] text-slate-400">
                                    <Loader2Icon className="w-3.5 h-3.5 animate-spin" />
                                    Working…
                                    {iteration > 0 && (
                                        <span className="font-mono tabular-nums text-slate-300">
                                            step {iteration}/{budget}
                                        </span>
                                    )}
                                </div>
                            )}
                        </div>

                        {/* Composer */}
                        <div className="shrink-0 border-t border-slate-200 p-3">
                            <div className="flex items-end gap-2 rounded-lg border border-slate-200 focus-within:border-sky-400 focus-within:ring-2 focus-within:ring-sky-100 px-2.5 py-2 transition-colors">
                                <textarea
                                    value={input}
                                    onChange={(e) => setInput(e.target.value)}
                                    onKeyDown={(e) => {
                                        if (e.key === "Enter" && !e.shiftKey) {
                                            e.preventDefault();
                                            send();
                                        }
                                    }}
                                    rows={1}
                                    placeholder="Ask about contacts, campaigns, your inbox…"
                                    disabled={!!pending}
                                    className="flex-1 resize-none bg-transparent text-[13px] text-slate-900 placeholder:text-slate-400 outline-none max-h-32 disabled:opacity-60"
                                />
                                {running ? (
                                    <button
                                        onClick={stop}
                                        title="Stop"
                                        className="size-7 rounded-md bg-slate-900 hover:bg-slate-700 text-white inline-flex items-center justify-center transition-colors"
                                    >
                                        <SquareIcon className="w-3 h-3" fill="currentColor" />
                                    </button>
                                ) : (
                                    <button
                                        onClick={send}
                                        disabled={!input.trim() || !!pending}
                                        className="size-7 rounded-md bg-sky-600 hover:bg-sky-700 text-white inline-flex items-center justify-center transition-colors disabled:opacity-40"
                                    >
                                        <ArrowUpIcon className="w-4 h-4" />
                                    </button>
                                )}
                            </div>
                            <div className="mt-2 flex items-center justify-between text-[10.5px] text-slate-400">
                                <span>Read actions run automatically. Writes ask first.</span>
                                {credits !== null && (
                                    <span className="font-mono tabular-nums">
                                        {credits.toLocaleString()} credits
                                    </span>
                                )}
                            </div>
                        </div>
                    </motion.aside>
                </>
            )}
        </AnimatePresence>
    );
}

// applyEvent folds one stream event into the current assistant turn's blocks.
function applyEvent(turn: Turn, ev: AgentStreamEvent) {
    switch (ev.type) {
        case "text": {
            const last = turn.blocks[turn.blocks.length - 1];
            if (last && last.kind === "text") {
                last.text += ev.text || "";
            } else {
                turn.blocks.push({ kind: "text", text: ev.text || "" });
            }
            break;
        }
        case "tool_start": {
            turn.blocks.push({
                kind: "tool",
                step: { id: nextId(), tool: ev.tool || "", argsSummary: ev.args_summary, done: false },
            });
            break;
        }
        case "tool_result": {
            // Complete the most recent unfinished step for this tool.
            for (let i = turn.blocks.length - 1; i >= 0; i--) {
                const b = turn.blocks[i];
                if (b.kind === "tool" && b.step.tool === ev.tool && !b.step.done) {
                    b.step.done = true;
                    b.step.result = ev.result;
                    b.step.entityType = ev.entity_type;
                    b.step.entityId = ev.entity_id;
                    b.step.openURL = ev.open_url;
                    break;
                }
            }
            break;
        }
        case "error": {
            turn.blocks.push({ kind: "error", code: ev.code, message: ev.message || "Something went wrong." });
            break;
        }
    }
}

function TurnView({ turn, onOpen }: { turn: Turn; onOpen: (url: string) => void }) {
    if (turn.role === "user") {
        const text = turn.blocks.map((b) => (b.kind === "text" ? b.text : "")).join("");
        return (
            <div className="flex justify-end">
                <div className="max-w-[85%] rounded-2xl rounded-br-sm bg-sky-600 text-white px-3 py-2 text-[13px] whitespace-pre-wrap break-words">
                    {text}
                </div>
            </div>
        );
    }
    return (
        <div className="space-y-2">
            {turn.blocks.map((b, i) => {
                if (b.kind === "text") {
                    return b.text ? (
                        <div key={i} className="text-[13px] text-slate-800 whitespace-pre-wrap break-words leading-relaxed">
                            {b.text}
                        </div>
                    ) : null;
                }
                if (b.kind === "tool") {
                    return <ToolStepRow key={i} step={b.step} onOpen={onOpen} />;
                }
                return (
                    <div key={i} className="flex items-start gap-2 rounded-md bg-red-50 border border-red-100 px-2.5 py-2 text-[12px] text-red-700">
                        <AlertTriangleIcon className="w-3.5 h-3.5 mt-0.5 shrink-0" />
                        <span>{b.message}</span>
                    </div>
                );
            })}
        </div>
    );
}

function ToolStepRow({ step, onOpen }: { step: ToolStep; onOpen: (url: string) => void }) {
    return (
        <div className="rounded-md border border-slate-200 bg-slate-50/60 px-2.5 py-1.5">
            <div className="flex items-center gap-1.5 text-[11.5px] text-slate-600">
                {step.done ? (
                    <CheckIcon className="w-3 h-3 text-emerald-600 shrink-0" />
                ) : (
                    <Loader2Icon className="w-3 h-3 animate-spin text-slate-400 shrink-0" />
                )}
                <WrenchIcon className="w-3 h-3 text-slate-400 shrink-0" />
                <span className="font-medium text-slate-700">{toolLabel(step.tool)}</span>
                {step.result && <span className="text-slate-400 truncate">— {step.result}</span>}
            </div>
            {step.done && step.openURL && (step.entityType === "campaign" || step.entityType === "automation") && (
                <button
                    onClick={() => onOpen(step.openURL!)}
                    className="mt-1.5 h-7 px-2.5 rounded-md bg-white border border-slate-200 hover:border-sky-400 hover:text-sky-700 text-[12px] text-slate-700 inline-flex items-center gap-1.5 transition-colors"
                >
                    <ExternalLinkIcon className="w-3 h-3" />
                    Open {step.entityType === "campaign" ? "campaign" : "automation"} draft
                </button>
            )}
        </div>
    );
}

function ApprovalCard({
    pending,
    onDecide,
}: {
    pending: Pending;
    onDecide: (d: "approve" | "deny" | "always_allow") => void;
}) {
    const isSend = pending.risk === "send";
    return (
        <div className="rounded-lg border border-amber-200 bg-amber-50/70 p-3">
            <div className="flex items-center gap-1.5 text-[12px] font-medium text-amber-800">
                <ShieldQuestionIcon className="w-3.5 h-3.5" />
                {isSend ? "Send this?" : "Approve this action?"}
            </div>
            <div className="mt-1 text-[12px] text-slate-700">
                <span className="font-medium">{toolLabel(pending.tool)}</span>
                {pending.argsSummary && (
                    <span className="text-slate-500"> — {pending.argsSummary}</span>
                )}
            </div>
            <div className="mt-2.5 flex flex-wrap items-center gap-2">
                <button
                    onClick={() => onDecide("approve")}
                    className="h-7 px-3 rounded-md bg-slate-900 hover:bg-slate-800 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors"
                >
                    <CheckIcon className="w-3 h-3" />
                    {isSend ? "Send" : "Approve"}
                </button>
                <button
                    onClick={() => onDecide("deny")}
                    className="h-7 px-3 rounded-md border border-slate-200 hover:border-slate-300 text-[12px] text-slate-700 transition-colors"
                >
                    Skip
                </button>
                {!isSend && (
                    <button
                        onClick={() => onDecide("always_allow")}
                        className="h-7 px-2.5 rounded-md text-[12px] text-slate-500 hover:text-slate-800 transition-colors"
                    >
                        Always allow
                    </button>
                )}
            </div>
        </div>
    );
}

function EmptyState() {
    return (
        <div className="h-full flex flex-col items-center justify-center text-center px-6 py-10">
            <div className="size-10 rounded-xl bg-sky-50 border border-sky-100 text-sky-600 flex items-center justify-center mb-3">
                <SparklesIcon className="w-5 h-5" />
            </div>
            <div className="text-[13px] font-semibold text-slate-900">How can I help?</div>
            <p className="text-[12px] text-slate-500 mt-1 leading-relaxed max-w-[260px]">
                Ask me to find contacts, check a campaign, draft a reply, or set up a
                draft campaign. I ask before changing anything.
            </p>
        </div>
    );
}

// toolLabel renders a friendly label for a tool name.
function toolLabel(tool: string): string {
    const map: Record<string, string> = {
        search_contacts: "Searched contacts",
        get_contact: "Read contact",
        update_contact_fields: "Update contact",
        add_tag: "Add tag",
        remove_tag: "Remove tag",
        list_campaigns: "Listed campaigns",
        get_campaign_stats: "Campaign stats",
        create_campaign_draft: "Create campaign draft",
        create_automation_draft: "Create automation draft",
        create_task: "Create task",
        create_deal: "Create deal",
        list_threads: "Listed threads",
        get_thread: "Read thread",
        draft_reply: "Drafted reply",
        search_web: "Searched the web",
        fetch_url: "Fetched a page",
    };
    return map[tool] || tool.replace(/_/g, " ");
}

function resourceFromPath(path: string): string {
    const m = path.match(/\/app\/(campaigns|contacts|automations)\/([0-9a-f-]{8,})/i);
    if (!m) return "";
    const kind = m[1] === "campaigns" ? "campaign" : m[1] === "contacts" ? "contact" : "automation";
    return `${kind}:${m[2]}`;
}

// stripOrigin turns an absolute open_url into a router-relative path.
function stripOrigin(url: string): string {
    try {
        const u = new URL(url, window.location.origin);
        return u.pathname + u.search;
    } catch {
        return url;
    }
}
