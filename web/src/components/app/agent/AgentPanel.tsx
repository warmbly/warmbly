// Right-side AI assistant workspace. Persistent across routes, opened from the
// header sparkle button or Cmd+I. A real multi-conversation surface: several
// chats run as tabs (each streams its own tool-using agent over SSE), a history
// rail lists past sessions, and the panel expands to a full-width workspace.
//
// Tab state lives in the store (agentSlice) so tabs survive the panel closing
// and a background run keeps streaming into its tab while you read another.
// Reopening a past conversation rehydrates its transcript from the server.
// Sends never happen from AI: draft artifacts open the real editors.

import React from "react";
import { motion } from "framer-motion";
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
    Maximize2Icon,
    Minimize2Icon,
    MessageSquarePlusIcon,
    ClockIcon,
} from "lucide-react";
import { cn } from "@/lib/utils";
import { useAppStore } from "@/stores";
import type {
    AgentTab,
    AgentTurn,
    AgentToolStep,
    AgentPending,
} from "@/stores/slices/agentSlice";
import createAgentSession from "@/lib/api/client/app/agent/createAgentSession";
import getAgentMessages from "@/lib/api/client/app/agent/getAgentMessages";
import streamAgentRun from "@/lib/api/client/app/agent/streamAgentRun";
import useAgentSessions from "@/lib/api/hooks/app/agent/useAgentSessions";
import type {
    AgentStreamEvent,
    AgentSession,
    AgentHydratedTurn,
} from "@/lib/api/models/app/agent/Agent";

// Per-run abort controllers, keyed by tab. Kept module-level (not serializable)
// so the store stays clean; a page reload drops them along with the run.
const aborts = new Map<string, AbortController>();

let mid = 0;
const nextId = () => `m${++mid}`;

function deriveTitle(text: string): string {
    const t = text.trim().replace(/\s+/g, " ");
    return t.length > 40 ? t.slice(0, 40).trimEnd() + "…" : t || "New chat";
}

export default function AgentPanel() {
    const open = useAppStore((s) => s.aiAssistantOpen);
    const setOpen = useAppStore((s) => s.setAIAssistantOpen);
    const expanded = useAppStore((s) => s.agentExpanded);
    const setExpanded = useAppStore((s) => s.setAgentExpanded);
    const tabs = useAppStore((s) => s.agentTabs);
    const activeKey = useAppStore((s) => s.agentActiveKey);

    const navigate = useNavigate();
    const location = useLocation();
    const [input, setInput] = React.useState("");
    const scrollRef = React.useRef<HTMLDivElement>(null);
    const hydrating = React.useRef<Set<string>>(new Set());

    const activeTab = tabs.find((t) => t.key === activeKey) ?? null;

    // Resource string mirrors the presence shape so the agent knows what the
    // user is looking at ("this campaign", "here").
    const resource = React.useMemo(
        () => resourceFromPath(location.pathname),
        [location.pathname],
    );

    // Opening the panel with no tabs starts a fresh conversation.
    React.useEffect(() => {
        if (open) useAppStore.getState().agentEnsureTab();
    }, [open]);

    // Lazily rehydrate a tab opened from history (sessionId set, transcript not
    // yet loaded). Guarded so we fetch each session once.
    React.useEffect(() => {
        if (!activeTab || !activeTab.sessionId || activeTab.hydrated) return;
        if (hydrating.current.has(activeTab.key)) return;
        const key = activeTab.key;
        const sid = activeTab.sessionId;
        hydrating.current.add(key);
        getAgentMessages(sid)
            .then((tr) => {
                useAppStore.getState().agentUpdateTab(key, (t) => ({
                    ...t,
                    turns: fromHydrated(tr.turns),
                    pending: tr.pending
                        ? {
                              toolCallId: tr.pending.tool_call_id,
                              tool: tr.pending.tool_name,
                              risk: tr.pending.risk,
                              argsSummary: tr.pending.args_summary,
                          }
                        : null,
                    title: tr.title || t.title,
                    hydrated: true,
                }));
            })
            .catch(() => {
                useAppStore.getState().agentUpdateTab(key, (t) => ({
                    ...t,
                    hydrated: true,
                    turns: foldEvent(t.turns, {
                        type: "error",
                        message: "Could not load this conversation.",
                    }),
                }));
            })
            .finally(() => hydrating.current.delete(key));
    }, [activeTab]);

    // Keep the transcript pinned to the newest content.
    React.useEffect(() => {
        if (scrollRef.current) {
            scrollRef.current.scrollTop = scrollRef.current.scrollHeight;
        }
    }, [activeTab?.turns, activeTab?.pending, activeKey]);

    async function runStream(
        tabKey: string,
        path: string,
        body: Record<string, unknown>,
    ) {
        const store = useAppStore.getState();
        store.agentPatchTab(tabKey, { running: true, pending: null, iteration: 0 });
        const ac = new AbortController();
        aborts.set(tabKey, ac);
        await streamAgentRun(
            path,
            body,
            (ev) => {
                const s = useAppStore.getState();
                if (ev.type === "iteration") {
                    s.agentPatchTab(tabKey, {
                        ...(typeof ev.credits_remaining === "number"
                            ? { credits: ev.credits_remaining }
                            : {}),
                        ...(typeof ev.budget === "number" ? { budget: ev.budget } : {}),
                        ...(typeof ev.iteration === "number"
                            ? { iteration: ev.iteration }
                            : {}),
                    });
                    return;
                }
                if (ev.type === "approval_required") {
                    s.agentPatchTab(tabKey, {
                        pending: {
                            toolCallId: ev.tool_call_id || "",
                            tool: ev.tool || "",
                            risk: ev.risk || "write",
                            argsSummary: ev.args_summary,
                        },
                    });
                    return;
                }
                if (ev.type === "done") {
                    if (typeof ev.credits_remaining === "number") {
                        s.agentPatchTab(tabKey, { credits: ev.credits_remaining });
                    }
                    return;
                }
                s.agentUpdateTab(tabKey, (t) => ({ ...t, turns: foldEvent(t.turns, ev) }));
                if (ev.type === "error" && typeof ev.credits_remaining === "number") {
                    s.agentPatchTab(tabKey, { credits: ev.credits_remaining });
                }
            },
            ac.signal,
        );
        useAppStore.getState().agentPatchTab(tabKey, { running: false });
        aborts.delete(tabKey);
    }

    async function send() {
        const tab = activeTab;
        if (!tab) return;
        const text = input.trim();
        if (!text || tab.running) return;
        setInput("");
        const store = useAppStore.getState();
        store.agentUpdateTab(tab.key, (t) => ({
            ...t,
            title:
                t.sessionId || t.title !== "New chat" ? t.title : deriveTitle(text),
            turns: [
                ...t.turns,
                { id: nextId(), role: "user", blocks: [{ kind: "text", text }] },
            ],
        }));

        let sid = tab.sessionId;
        if (!sid) {
            try {
                const sess = await createAgentSession({
                    page: location.pathname,
                    resource,
                });
                sid = sess.id;
                store.agentPatchTab(tab.key, { sessionId: sid });
            } catch {
                store.agentUpdateTab(tab.key, (t) => ({
                    ...t,
                    turns: foldEvent(t.turns, {
                        type: "error",
                        message: "Could not start a session.",
                    }),
                }));
                return;
            }
        }
        await runStream(tab.key, `/ai/sessions/${sid}/messages`, {
            message_id: nextId() + ":" + Date.now(),
            text,
            page: location.pathname,
            resource,
        });
    }

    async function decide(
        tabKey: string,
        decision: "approve" | "deny" | "always_allow",
    ) {
        const tab = useAppStore.getState().agentTabs.find((t) => t.key === tabKey);
        if (!tab || !tab.sessionId || !tab.pending) return;
        useAppStore.getState().agentPatchTab(tabKey, { pending: null });
        await runStream(tabKey, `/ai/sessions/${tab.sessionId}/approve`, { decision });
    }

    function stop(tabKey: string) {
        aborts.get(tabKey)?.abort();
        useAppStore.getState().agentPatchTab(tabKey, { running: false });
    }

    function closeTab(key: string) {
        aborts.get(key)?.abort();
        aborts.delete(key);
        useAppStore.getState().agentCloseTab(key);
    }

    function openSession(s: AgentSession) {
        useAppStore.getState().agentOpenSession(s.id, s.title || "Conversation");
    }

    return (
        <>
            {/* Mobile backdrop. */}
            {open && (
                <div
                    onClick={() => setOpen(false)}
                    className="fixed inset-0 z-40 bg-slate-900/20 md:hidden"
                />
            )}
            <motion.aside
                initial={false}
                animate={{ x: open ? 0 : "101%" }}
                transition={{ type: "spring", stiffness: 380, damping: 40 }}
                className={cn(
                    "fixed right-0 top-0 z-50 h-full bg-white border-l border-slate-200 shadow-[0_0_60px_-12px_rgba(15,23,42,0.3)] flex",
                    expanded ? "w-full sm:w-[min(1080px,94vw)]" : "w-full sm:w-[440px]",
                )}
                aria-hidden={!open}
            >
                {/* History rail (expanded only). */}
                {expanded && (
                    <SessionSidebar
                        activeSessionId={activeTab?.sessionId ?? null}
                        onOpen={openSession}
                        onNew={() => useAppStore.getState().agentNewTab()}
                    />
                )}

                {/* Main column. */}
                <div className="flex-1 min-w-0 flex flex-col">
                    {/* Header */}
                    <div className="shrink-0 px-3 h-12 flex items-center gap-2 border-b border-slate-200">
                        <button
                            onClick={() => setExpanded(!expanded)}
                            title={expanded ? "Collapse" : "Expand to workspace"}
                            className="size-7 rounded-md text-slate-500 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center justify-center transition-colors"
                        >
                            {expanded ? (
                                <Minimize2Icon className="w-4 h-4" />
                            ) : (
                                <Maximize2Icon className="w-4 h-4" />
                            )}
                        </button>
                        <div className="size-7 rounded-md bg-sky-50 border border-sky-100 text-sky-600 flex items-center justify-center">
                            <SparklesIcon className="w-4 h-4" />
                        </div>
                        <div className="text-[13px] font-semibold text-slate-900">
                            Assistant
                        </div>
                        <div className="ml-auto flex items-center gap-1">
                            <button
                                onClick={() => useAppStore.getState().agentNewTab()}
                                title="New chat"
                                className="h-7 px-2 rounded-md text-[12px] text-slate-600 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center gap-1 transition-colors"
                            >
                                <MessageSquarePlusIcon className="w-3.5 h-3.5" />
                                <span className="hidden sm:inline">New</span>
                            </button>
                            <button
                                onClick={() => setOpen(false)}
                                title="Close"
                                className="size-7 rounded-md text-slate-500 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center justify-center transition-colors"
                            >
                                <XIcon className="w-4 h-4" />
                            </button>
                        </div>
                    </div>

                    {/* Tab bar */}
                    <TabBar
                        tabs={tabs}
                        activeKey={activeKey}
                        onSelect={(k) => useAppStore.getState().agentSetActive(k)}
                        onClose={closeTab}
                        onNew={() => useAppStore.getState().agentNewTab()}
                    />

                    {/* Transcript */}
                    <div
                        ref={scrollRef}
                        className="flex-1 min-h-0 overflow-y-auto px-4 py-4 space-y-4"
                    >
                        {activeTab && !activeTab.hydrated && (
                            <div className="flex items-center gap-2 text-[12px] text-slate-400">
                                <Loader2Icon className="w-3.5 h-3.5 animate-spin" />
                                Loading conversation…
                            </div>
                        )}
                        {activeTab &&
                            activeTab.hydrated &&
                            activeTab.turns.length === 0 &&
                            !activeTab.running && <EmptyState onPick={(q) => setInput(q)} />}
                        {activeTab?.turns.map((t) => (
                            <TurnView
                                key={t.id}
                                turn={t}
                                onOpen={(u) => navigate(stripOrigin(u))}
                            />
                        ))}
                        {activeTab?.pending && (
                            <ApprovalCard
                                pending={activeTab.pending}
                                onDecide={(d) => decide(activeTab.key, d)}
                            />
                        )}
                        {activeTab?.running && !activeTab.pending && (
                            <div className="flex items-center gap-2 text-[12px] text-slate-400">
                                <Loader2Icon className="w-3.5 h-3.5 animate-spin" />
                                Working…
                                {activeTab.iteration > 0 && (
                                    <span className="font-mono tabular-nums text-slate-300">
                                        step {activeTab.iteration}/{activeTab.budget}
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
                                disabled={!!activeTab?.pending}
                                className="flex-1 resize-none bg-transparent text-[13px] text-slate-900 placeholder:text-slate-400 outline-none max-h-32 disabled:opacity-60"
                            />
                            {activeTab?.running ? (
                                <button
                                    onClick={() => stop(activeTab.key)}
                                    title="Stop"
                                    className="size-7 rounded-md bg-slate-900 hover:bg-slate-700 text-white inline-flex items-center justify-center transition-colors"
                                >
                                    <SquareIcon className="w-3 h-3" fill="currentColor" />
                                </button>
                            ) : (
                                <button
                                    onClick={send}
                                    disabled={!input.trim() || !!activeTab?.pending}
                                    className="size-7 rounded-md bg-sky-600 hover:bg-sky-700 text-white inline-flex items-center justify-center transition-colors disabled:opacity-40"
                                >
                                    <ArrowUpIcon className="w-4 h-4" />
                                </button>
                            )}
                        </div>
                        <div className="mt-2 flex items-center justify-between text-[10.5px] text-slate-400">
                            <span>Read actions run automatically. Writes ask first.</span>
                            {activeTab?.credits != null && (
                                <span className="font-mono tabular-nums">
                                    {activeTab.credits.toLocaleString()} credits
                                </span>
                            )}
                        </div>
                    </div>
                </div>
            </motion.aside>
        </>
    );
}

// ── Tab bar ─────────────────────────────────────────────────────────
function TabBar({
    tabs,
    activeKey,
    onSelect,
    onClose,
    onNew,
}: {
    tabs: AgentTab[];
    activeKey: string | null;
    onSelect: (k: string) => void;
    onClose: (k: string) => void;
    onNew: () => void;
}) {
    if (tabs.length === 0) return null;
    return (
        <div className="shrink-0 flex items-stretch gap-1 px-2 h-9 border-b border-slate-200 overflow-x-auto">
            {tabs.map((t) => {
                const active = t.key === activeKey;
                return (
                    <div
                        key={t.key}
                        onClick={() => onSelect(t.key)}
                        className={cn(
                            "group shrink-0 max-w-[160px] h-7 my-1 pl-2.5 pr-1.5 rounded-md inline-flex items-center gap-1.5 cursor-pointer text-[12px] transition-colors",
                            active
                                ? "bg-slate-100 text-slate-900"
                                : "text-slate-500 hover:bg-slate-50 hover:text-slate-700",
                        )}
                    >
                        {t.running ? (
                            <Loader2Icon className="w-3 h-3 shrink-0 animate-spin text-sky-500" />
                        ) : (
                            <span
                                className={cn(
                                    "size-1.5 shrink-0 rounded-full",
                                    t.pending ? "bg-amber-500" : "bg-slate-300",
                                )}
                            />
                        )}
                        <span className="truncate">{t.title}</span>
                        <button
                            onClick={(e) => {
                                e.stopPropagation();
                                onClose(t.key);
                            }}
                            className="size-4 shrink-0 rounded inline-flex items-center justify-center text-slate-400 hover:text-slate-900 hover:bg-slate-200 opacity-100 md:opacity-0 md:group-hover:opacity-100 transition-opacity"
                            aria-label="Close tab"
                        >
                            <XIcon className="w-3 h-3" />
                        </button>
                    </div>
                );
            })}
            <button
                onClick={onNew}
                title="New chat"
                className="shrink-0 size-7 my-1 rounded-md text-slate-400 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center justify-center transition-colors"
            >
                <PlusIcon className="w-4 h-4" />
            </button>
        </div>
    );
}

// ── History rail ────────────────────────────────────────────────────
function SessionSidebar({
    activeSessionId,
    onOpen,
    onNew,
}: {
    activeSessionId: string | null;
    onOpen: (s: AgentSession) => void;
    onNew: () => void;
}) {
    const q = useAgentSessions(20);
    const sessions = React.useMemo(
        () => (q.data?.pages ?? []).flatMap((p) => p.data),
        [q.data],
    );
    return (
        <div className="hidden sm:flex w-60 shrink-0 flex-col border-r border-slate-200 bg-slate-50/50">
            <div className="shrink-0 h-12 px-3 flex items-center border-b border-slate-200">
                <button
                    onClick={onNew}
                    className="w-full h-8 rounded-md bg-sky-600 hover:bg-sky-700 text-white text-[12.5px] font-medium inline-flex items-center justify-center gap-1.5 transition-colors"
                >
                    <PlusIcon className="w-3.5 h-3.5" />
                    New chat
                </button>
            </div>
            <div className="flex-1 min-h-0 overflow-y-auto px-2 py-2">
                <div className="px-1.5 pb-1 text-[10px] uppercase tracking-[0.14em] text-slate-400 font-semibold">
                    History
                </div>
                {sessions.length === 0 && !q.isLoading && (
                    <p className="px-1.5 py-3 text-[11.5px] text-slate-400 leading-relaxed">
                        Your past conversations show up here.
                    </p>
                )}
                <div className="space-y-0.5">
                    {sessions.map((s) => (
                        <button
                            key={s.id}
                            onClick={() => onOpen(s)}
                            className={cn(
                                "w-full text-left px-2 py-1.5 rounded-md flex items-start gap-2 transition-colors",
                                s.id === activeSessionId
                                    ? "bg-white ring-1 ring-slate-200"
                                    : "hover:bg-white",
                            )}
                        >
                            <ClockIcon className="w-3 h-3 mt-0.5 shrink-0 text-slate-400" />
                            <span className="min-w-0 flex-1">
                                <span className="block truncate text-[12px] text-slate-700">
                                    {s.title || "Conversation"}
                                </span>
                                <span className="block text-[10.5px] text-slate-400">
                                    {relativeTime(s.updated_at || s.created_at)}
                                </span>
                            </span>
                        </button>
                    ))}
                </div>
                {q.hasNextPage && (
                    <button
                        onClick={() => q.fetchNextPage()}
                        disabled={q.isFetchingNextPage}
                        className="mt-1 w-full h-7 rounded-md text-[11.5px] text-slate-500 hover:text-slate-800 hover:bg-white transition-colors"
                    >
                        {q.isFetchingNextPage ? "Loading…" : "Load more"}
                    </button>
                )}
            </div>
        </div>
    );
}

// foldEvent folds one stream event into a copy of the turns array, appending to
// (or creating) the trailing assistant turn — mirrors the server-side hydration.
function foldEvent(turns: AgentTurn[], ev: AgentStreamEvent): AgentTurn[] {
    const next = turns.slice();
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
}

function applyEvent(turn: AgentTurn, ev: AgentStreamEvent) {
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
                step: {
                    id: nextId(),
                    tool: ev.tool || "",
                    argsSummary: ev.args_summary,
                    done: false,
                },
            });
            break;
        }
        case "tool_result": {
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
            turn.blocks.push({
                kind: "error",
                code: ev.code,
                message: ev.message || "Something went wrong.",
            });
            break;
        }
    }
}

// fromHydrated maps a server transcript into the client turn/block model.
function fromHydrated(turns: AgentHydratedTurn[]): AgentTurn[] {
    return turns.map((t) => ({
        id: nextId(),
        role: t.role,
        blocks: t.blocks.map((b) =>
            b.kind === "tool"
                ? {
                      kind: "tool" as const,
                      step: {
                          id: nextId(),
                          tool: b.tool ?? "",
                          argsSummary: b.args_summary,
                          result: b.result,
                          done: b.done,
                          entityType: b.entity_type,
                          entityId: b.entity_id,
                          openURL: b.open_url,
                      },
                  }
                : { kind: "text" as const, text: b.text ?? "" },
        ),
    }));
}

function TurnView({
    turn,
    onOpen,
}: {
    turn: AgentTurn;
    onOpen: (url: string) => void;
}) {
    if (turn.role === "user") {
        const text = turn.blocks
            .map((b) => (b.kind === "text" ? b.text : ""))
            .join("");
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
                        <div
                            key={i}
                            className="text-[13px] text-slate-800 whitespace-pre-wrap break-words leading-relaxed"
                        >
                            {b.text}
                        </div>
                    ) : null;
                }
                if (b.kind === "tool") {
                    return <ToolStepRow key={i} step={b.step} onOpen={onOpen} />;
                }
                return (
                    <div
                        key={i}
                        className="flex items-start gap-2 rounded-md bg-red-50 border border-red-100 px-2.5 py-2 text-[12px] text-red-700"
                    >
                        <AlertTriangleIcon className="w-3.5 h-3.5 mt-0.5 shrink-0" />
                        <span>{b.message}</span>
                    </div>
                );
            })}
        </div>
    );
}

function ToolStepRow({
    step,
    onOpen,
}: {
    step: AgentToolStep;
    onOpen: (url: string) => void;
}) {
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
                {step.result && (
                    <span className="text-slate-400 truncate">— {step.result}</span>
                )}
            </div>
            {step.done &&
                step.openURL &&
                (step.entityType === "campaign" ||
                    step.entityType === "automation") && (
                    <button
                        onClick={() => onOpen(step.openURL!)}
                        className="mt-1.5 h-7 px-2.5 rounded-md bg-white border border-slate-200 hover:border-sky-400 hover:text-sky-700 text-[12px] text-slate-700 inline-flex items-center gap-1.5 transition-colors"
                    >
                        <ExternalLinkIcon className="w-3 h-3" />
                        Open {step.entityType === "campaign" ? "campaign" : "automation"}{" "}
                        draft
                    </button>
                )}
        </div>
    );
}

function ApprovalCard({
    pending,
    onDecide,
}: {
    pending: AgentPending;
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

const STARTERS = [
    "Which leads went cold and need a follow-up?",
    "Summarize replies in my inbox from this week",
    "Draft a reply to my latest positive reply",
    "How are my campaigns performing?",
];

function EmptyState({ onPick }: { onPick: (q: string) => void }) {
    return (
        <div className="h-full flex flex-col items-center justify-center text-center px-6 py-10">
            <div className="size-10 rounded-xl bg-sky-50 border border-sky-100 text-sky-600 flex items-center justify-center mb-3">
                <SparklesIcon className="w-5 h-5" />
            </div>
            <div className="text-[13px] font-semibold text-slate-900">How can I help?</div>
            <p className="text-[12px] text-slate-500 mt-1 leading-relaxed max-w-[280px]">
                Ask me to find contacts, check a campaign, draft a reply, or set up a
                draft campaign. I ask before changing anything.
            </p>
            <div className="mt-4 w-full max-w-[320px] space-y-1.5">
                {STARTERS.map((q) => (
                    <button
                        key={q}
                        onClick={() => onPick(q)}
                        className="w-full text-left px-3 py-2 rounded-md border border-slate-200 hover:border-sky-300 hover:bg-sky-50/50 text-[12px] text-slate-600 hover:text-slate-900 transition-colors"
                    >
                        {q}
                    </button>
                ))}
            </div>
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
    const m = path.match(
        /\/app\/(campaigns|contacts|automations)\/([0-9a-f-]{8,})/i,
    );
    if (!m) return "";
    const kind =
        m[1] === "campaigns"
            ? "campaign"
            : m[1] === "contacts"
              ? "contact"
              : "automation";
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

function relativeTime(value: string | Date): string {
    const date = typeof value === "string" ? new Date(value) : value;
    const diff = Date.now() - date.getTime();
    const sec = Math.round(diff / 1000);
    if (sec < 60) return "just now";
    const min = Math.floor(sec / 60);
    if (min < 60) return `${min}m ago`;
    const hr = Math.floor(min / 60);
    if (hr < 24) return `${hr}h ago`;
    const day = Math.floor(hr / 24);
    if (day < 7) return `${day}d ago`;
    return date.toLocaleDateString("en-US", { month: "short", day: "numeric" });
}
