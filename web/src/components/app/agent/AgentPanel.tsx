// Right-side AI assistant workspace. Persistent across routes, opened from the
// header spark button or Cmd+I. A real multi-conversation surface: several
// chats run as tabs (each streams its own tool-using agent over SSE), a history
// rail lists past sessions, the panel expands to a full-width workspace, and
// minimizing docks it into a bottom-right status bar while runs keep going.
//
// Tab state (including the unsent composer draft) lives in the store
// (agentSlice) so tabs survive the panel closing and a background run keeps
// streaming into its tab while you read another. Reopening a past conversation
// rehydrates its transcript from the server. Sends never happen from AI: draft
// artifacts open the real editors.

import React from "react";
import { motion } from "framer-motion";
import { useLocation, useNavigate } from "react-router-dom";
import {
    XIcon,
    ArrowUpIcon,
    ArrowDownIcon,
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
    MinusIcon,
    ChevronUpIcon,
    ClockIcon,
    PanelLeftIcon,
    PanelRightIcon,
    PictureInPicture2Icon,
} from "lucide-react";
import { cn } from "@/lib/utils";
import { useAppStore } from "@/stores";
import {
    AGENT_FLOAT_MIN_W,
    AGENT_FLOAT_MAX_W,
    AGENT_FLOAT_MIN_H,
    type AgentFloatRect,
    type AgentTab,
    type AgentTurn,
    type AgentToolStep,
    type AgentPending,
} from "@/stores/slices/agentSlice";
import createAgentSession from "@/lib/api/client/app/agent/createAgentSession";
import getAgentMessages from "@/lib/api/client/app/agent/getAgentMessages";
import streamAgentRun from "@/lib/api/client/app/agent/streamAgentRun";
import useAgentSessions from "@/lib/api/hooks/app/agent/useAgentSessions";
import Markdown from "./Markdown";
import AgentMark from "./AgentMark";
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

// Keep the floating window fully on screen and inside its size bounds.
function clampFloatRect(r: AgentFloatRect): AgentFloatRect {
    const vw = document.documentElement.clientWidth;
    const vh = window.innerHeight;
    const w = Math.min(Math.max(r.w, AGENT_FLOAT_MIN_W), Math.min(AGENT_FLOAT_MAX_W, vw - 16));
    const h = Math.min(Math.max(r.h, AGENT_FLOAT_MIN_H), vh - 16);
    return {
        w,
        h,
        x: Math.min(Math.max(r.x, 8), vw - w - 8),
        y: Math.min(Math.max(r.y, 8), vh - h - 8),
    };
}

function defaultFloatRect(): AgentFloatRect {
    const vw = document.documentElement.clientWidth;
    const vh = window.innerHeight;
    const w = 460;
    const h = Math.min(640, vh - 48);
    return clampFloatRect({ x: vw - w - 24, y: vh - h - 24, w, h });
}

type ResizeDir = "n" | "s" | "e" | "w" | "ne" | "nw" | "se" | "sw";

export default function AgentPanel() {
    const open = useAppStore((s) => s.aiAssistantOpen);
    const setOpen = useAppStore((s) => s.setAIAssistantOpen);
    const expanded = useAppStore((s) => s.agentExpanded);
    const setExpanded = useAppStore((s) => s.setAgentExpanded);
    const minimized = useAppStore((s) => s.agentMinimized);
    const setMinimized = useAppStore((s) => s.setAgentMinimized);
    const side = useAppStore((s) => s.agentSide);
    const setSide = useAppStore((s) => s.setAgentSide);
    const width = useAppStore((s) => s.agentWidth);
    const floating = useAppStore((s) => s.agentFloating);
    const setFloating = useAppStore((s) => s.setAgentFloating);
    const floatRect = useAppStore((s) => s.agentFloatRect);
    const tabs = useAppStore((s) => s.agentTabs);
    const activeKey = useAppStore((s) => s.agentActiveKey);
    const visible = open && !minimized;

    // Floating is desktop-only; below sm the panel stays a full-width sheet.
    const [smUp, setSmUp] = React.useState(
        () => typeof window !== "undefined" && window.matchMedia("(min-width: 640px)").matches,
    );
    React.useEffect(() => {
        const m = window.matchMedia("(min-width: 640px)");
        const fn = () => setSmUp(m.matches);
        m.addEventListener("change", fn);
        return () => m.removeEventListener("change", fn);
    }, []);
    const isFloat = floating && !expanded && smUp;
    const rect = React.useMemo(
        () => (isFloat ? clampFloatRect(floatRect ?? defaultFloatRect()) : null),
        [isFloat, floatRect],
    );

    const navigate = useNavigate();
    const location = useLocation();
    const scrollRef = React.useRef<HTMLDivElement>(null);
    const inputRef = React.useRef<HTMLTextAreaElement>(null);
    const hydrating = React.useRef<Set<string>>(new Set());
    // Stick-to-bottom: autoscroll only while the user is pinned near the end,
    // so streaming never yanks them away from scrollback they are reading.
    const [pinned, setPinned] = React.useState(true);

    const activeTab = tabs.find((t) => t.key === activeKey) ?? null;
    const draft = activeTab?.draft ?? "";
    const composerLocked = !activeTab?.hydrated || !!activeTab?.pending;

    // Resource string mirrors the presence shape so the agent knows what the
    // user is looking at ("this campaign", "here").
    const resource = React.useMemo(
        () => resourceFromPath(location.pathname),
        [location.pathname],
    );

    const openUrl = React.useCallback(
        (u: string) => navigate(stripOrigin(u)),
        [navigate],
    );

    function scrollToBottom(behavior: ScrollBehavior = "auto") {
        const el = scrollRef.current;
        if (el) el.scrollTo({ top: el.scrollHeight, behavior });
    }

    const resizeInput = React.useCallback(() => {
        const el = inputRef.current;
        if (!el) return;
        el.style.height = "0px";
        el.style.height = Math.min(el.scrollHeight, 128) + "px";
    }, []);

    // Opening (or restoring from the dock) with no tabs starts a fresh
    // conversation; focus lands in the composer once the slide-in starts.
    React.useEffect(() => {
        if (!visible) return;
        useAppStore.getState().agentEnsureTab();
        const t = window.setTimeout(() => inputRef.current?.focus(), 80);
        return () => window.clearTimeout(t);
    }, [visible]);

    // Viewing a tab (panel open, not minimized) marks its finished runs seen.
    React.useEffect(() => {
        if (visible && activeTab?.unseen) {
            useAppStore.getState().agentPatchTab(activeTab.key, { unseen: false });
        }
    }, [visible, activeTab?.key, activeTab?.unseen]);

    // Switching tabs jumps to that conversation's latest message and refocuses
    // the composer.
    React.useEffect(() => {
        setPinned(true);
        requestAnimationFrame(() => scrollToBottom());
        if (visible) inputRef.current?.focus();
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [activeKey]);

    React.useEffect(() => {
        resizeInput();
    }, [draft, activeKey, resizeInput]);

    // Lazily rehydrate a tab opened from history (sessionId set, transcript not
    // yet loaded). Guarded so we fetch each session once; the composer stays
    // locked until the transcript is in, so a send can never race the fetch.
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
                    freeModel: tr.free_model ?? t.freeModel,
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

    // Follow new content only while pinned to the bottom.
    React.useEffect(() => {
        if (pinned) scrollToBottom();
    }, [activeTab?.turns, activeTab?.pending, pinned]);

    function onScroll() {
        const el = scrollRef.current;
        if (!el) return;
        setPinned(el.scrollHeight - el.scrollTop - el.clientHeight < 48);
    }

    async function runStream(
        tabKey: string,
        path: string,
        body: Record<string, unknown>,
    ) {
        const store = useAppStore.getState();
        store.agentPatchTab(tabKey, { running: true, pending: null, iteration: 0 });
        const ac = new AbortController();
        aborts.set(tabKey, ac);
        try {
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
                            ...(typeof ev.budget === "number"
                                ? { budget: ev.budget }
                                : {}),
                            ...(typeof ev.iteration === "number"
                                ? { iteration: ev.iteration }
                                : {}),
                            ...(ev.free_model ? { freeModel: true } : {}),
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
                            s.agentPatchTab(tabKey, {
                                credits: ev.credits_remaining,
                            });
                        }
                        return;
                    }
                    s.agentUpdateTab(tabKey, (t) => ({
                        ...t,
                        turns: foldEvent(t.turns, ev),
                    }));
                    if (
                        ev.type === "error" &&
                        typeof ev.credits_remaining === "number"
                    ) {
                        s.agentPatchTab(tabKey, { credits: ev.credits_remaining });
                    }
                },
                ac.signal,
            );
        } finally {
            aborts.delete(tabKey);
            const st = useAppStore.getState();
            st.agentPatchTab(tabKey, { running: false });
            // Finished while the user wasn't looking at this tab: flag it so the
            // tab dot, dock, and header icon can say a response is ready.
            if (!st.aiAssistantOpen || st.agentMinimized || st.agentActiveKey !== tabKey) {
                st.agentPatchTab(tabKey, { unseen: true });
            }
        }
    }

    async function send() {
        const tab = activeTab;
        if (!tab || tab.running || tab.pending || !tab.hydrated) return;
        const text = draft.trim();
        if (!text) return;
        const store = useAppStore.getState();
        // running flips on synchronously so a double Enter can't double-send.
        store.agentUpdateTab(tab.key, (t) => ({
            ...t,
            running: true,
            draft: "",
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
                    running: false,
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
        await runStream(tabKey, `/ai/sessions/${tab.sessionId}/approve`, {
            decision,
        });
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

    function setDraft(value: string) {
        if (activeTab) {
            useAppStore.getState().agentPatchTab(activeTab.key, { draft: value });
        }
    }

    function closePanel() {
        setOpen(false);
        setMinimized(false);
    }

    function cycleTab(dir: number) {
        const idx = tabs.findIndex((t) => t.key === activeKey);
        if (idx === -1 || tabs.length < 2) return;
        const next = tabs[(idx + dir + tabs.length) % tabs.length];
        useAppStore.getState().agentSetActive(next.key);
    }

    // Panel-scoped shortcuts (fire only while focus is inside the panel).
    // Alt combos match on e.code because macOS Option remaps e.key to symbols.
    function onPanelKeyDown(e: React.KeyboardEvent) {
        if (e.key === "Escape") {
            e.stopPropagation();
            closePanel();
            return;
        }
        const mod = e.metaKey || e.ctrlKey;
        if (mod && !e.altKey && (e.key === "]" || e.key === "[")) {
            e.preventDefault();
            e.stopPropagation();
            cycleTab(e.key === "]" ? 1 : -1);
            return;
        }
        if (e.altKey && !mod) {
            switch (e.code) {
                case "KeyN":
                    e.preventDefault();
                    useAppStore.getState().agentNewTab();
                    return;
                case "KeyW":
                    e.preventDefault();
                    if (activeTab) closeTab(activeTab.key);
                    return;
                case "KeyM":
                    e.preventDefault();
                    setMinimized(true);
                    return;
                case "KeyP":
                    e.preventDefault();
                    if (smUp && !expanded) setFloating(!floating);
                    return;
            }
        }
    }

    // Drag the panel's inner edge to resize (persisted via the store clamp).
    function startResize(e: React.PointerEvent) {
        if (expanded || isFloat) return;
        e.preventDefault();
        const onMove = (ev: PointerEvent) => {
            const w =
                side === "right" ? window.innerWidth - ev.clientX : ev.clientX;
            useAppStore.getState().setAgentWidth(w);
        };
        const onUp = () => {
            window.removeEventListener("pointermove", onMove);
            window.removeEventListener("pointerup", onUp);
        };
        window.addEventListener("pointermove", onMove);
        window.addEventListener("pointerup", onUp);
    }

    // Keep the floating window inside the viewport when the browser resizes.
    React.useEffect(() => {
        if (!isFloat) return;
        const fn = () => {
            const r = useAppStore.getState().agentFloatRect;
            if (r) useAppStore.getState().setAgentFloatRect(clampFloatRect(r));
        };
        window.addEventListener("resize", fn);
        return () => window.removeEventListener("resize", fn);
    }, [isFloat]);

    // Grab the header to move the window. From docked mode the same gesture
    // tears the panel off into a floating window once it travels far enough.
    function startHeaderDrag(e: React.PointerEvent) {
        if (expanded || !smUp || e.button !== 0) return;
        if ((e.target as HTMLElement).closest("button, input, textarea, a")) return;
        const startX = e.clientX;
        const startY = e.clientY;

        let r = isFloat && rect ? rect : null;
        // Pointer offset into the window; for a tear-off the pointer lands
        // near the top center of the new window.
        let offX = r ? startX - r.x : 0;
        let offY = r ? startY - r.y : 0;
        let torn = isFloat;

        const onMove = (ev: PointerEvent) => {
            if (!torn) {
                if (Math.abs(ev.clientX - startX) + Math.abs(ev.clientY - startY) < 16) return;
                const base = defaultFloatRect();
                r = { ...base, w: Math.min(base.w, useAppStore.getState().agentWidth) };
                offX = Math.min(r.w / 2, 200);
                offY = 24;
                torn = true;
                useAppStore.getState().setAgentFloating(true);
            }
            if (!r) return;
            ev.preventDefault();
            useAppStore
                .getState()
                .setAgentFloatRect(clampFloatRect({ ...r, x: ev.clientX - offX, y: ev.clientY - offY }));
        };
        const onUp = () => {
            window.removeEventListener("pointermove", onMove);
            window.removeEventListener("pointerup", onUp);
        };
        window.addEventListener("pointermove", onMove);
        window.addEventListener("pointerup", onUp);
    }

    // Resize the floating window from any edge or corner.
    function startFloatResize(e: React.PointerEvent, dir: ResizeDir) {
        if (!isFloat || !rect) return;
        e.preventDefault();
        e.stopPropagation();
        const start = { ...rect };
        const sx = e.clientX;
        const sy = e.clientY;
        const onMove = (ev: PointerEvent) => {
            ev.preventDefault();
            const dx = ev.clientX - sx;
            const dy = ev.clientY - sy;
            let { x, y, w, h } = start;
            if (dir.includes("e")) w = start.w + dx;
            if (dir.includes("s")) h = start.h + dy;
            if (dir.includes("w")) {
                w = start.w - dx;
                x = start.x + Math.min(dx, start.w - AGENT_FLOAT_MIN_W);
            }
            if (dir.includes("n")) {
                h = start.h - dy;
                y = start.y + Math.min(dy, start.h - AGENT_FLOAT_MIN_H);
            }
            useAppStore.getState().setAgentFloatRect(clampFloatRect({ x, y, w, h }));
        };
        const onUp = () => {
            window.removeEventListener("pointermove", onMove);
            window.removeEventListener("pointerup", onUp);
        };
        window.addEventListener("pointermove", onMove);
        window.addEventListener("pointerup", onUp);
    }

    return (
        <>
            {/* Mobile backdrop. */}
            {visible && (
                <div
                    onClick={closePanel}
                    className="fixed inset-0 z-40 bg-slate-900/20 md:hidden"
                />
            )}
            {/* Minimized dock: compact status bar; runs keep streaming into it. */}
            {open && minimized && (
                <DockBar
                    tabs={tabs}
                    activeKey={activeKey}
                    side={side}
                    onRestore={(key) => {
                        if (key) useAppStore.getState().agentSetActive(key);
                        setMinimized(false);
                    }}
                    onClose={closePanel}
                />
            )}
            <motion.aside
                initial={false}
                animate={
                    isFloat
                        ? { x: 0, opacity: visible ? 1 : 0, scale: visible ? 1 : 0.97 }
                        : {
                              x: visible ? 0 : side === "right" ? "101%" : "-101%",
                              opacity: 1,
                              scale: 1,
                          }
                }
                transition={{ type: "spring", stiffness: 380, damping: 40 }}
                // inert keeps the off-screen panel out of the tab order.
                inert={!visible}
                onKeyDown={onPanelKeyDown}
                style={
                    isFloat && rect
                        ? ({
                              "--agent-w": `${width}px`,
                              left: rect.x,
                              top: rect.y,
                              width: rect.w,
                              height: rect.h,
                          } as React.CSSProperties)
                        : ({ "--agent-w": `${width}px` } as React.CSSProperties)
                }
                className={cn(
                    "fixed z-50 bg-white flex",
                    isFloat
                        ? "rounded-xl border border-slate-200 shadow-2xl overflow-hidden"
                        : cn(
                              "top-0 h-full shadow-[0_0_60px_-12px_rgba(15,23,42,0.3)]",
                              side === "right"
                                  ? "right-0 border-l border-slate-200"
                                  : "left-0 border-r border-slate-200",
                              expanded
                                  ? "w-full sm:w-[min(1080px,94vw)]"
                                  : "w-full sm:w-[min(var(--agent-w),94vw)]",
                          ),
                )}
                aria-hidden={!visible}
            >
                {/* Floating: resize from any edge or corner. */}
                {isFloat && (
                    <>
                        {(
                            [
                                ["n", "top-0 left-3 right-3 h-1.5 cursor-ns-resize"],
                                ["s", "bottom-0 left-3 right-3 h-1.5 cursor-ns-resize"],
                                ["e", "right-0 top-3 bottom-3 w-1.5 cursor-ew-resize"],
                                ["w", "left-0 top-3 bottom-3 w-1.5 cursor-ew-resize"],
                                ["nw", "top-0 left-0 size-3 cursor-nwse-resize"],
                                ["se", "bottom-0 right-0 size-3 cursor-nwse-resize"],
                                ["ne", "top-0 right-0 size-3 cursor-nesw-resize"],
                                ["sw", "bottom-0 left-0 size-3 cursor-nesw-resize"],
                            ] as [ResizeDir, string][]
                        ).map(([dir, pos]) => (
                            <div
                                key={dir}
                                onPointerDown={(e) => startFloatResize(e, dir)}
                                className={cn("absolute z-20 touch-none", pos)}
                            />
                        ))}
                    </>
                )}
                {/* Drag handle on the inner edge (desktop, docked mode). */}
                {!expanded && !isFloat && (
                    <div
                        onPointerDown={startResize}
                        title="Drag to resize"
                        className={cn(
                            "hidden sm:block absolute top-0 h-full w-1.5 cursor-col-resize touch-none z-10 hover:bg-sky-400/40 active:bg-sky-500/50 transition-colors",
                            side === "right" ? "left-0" : "right-0",
                        )}
                    />
                )}
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
                    {/* Header. Grabbing it moves the floating window; from a
                        docked panel the same drag tears it off into one. */}
                    <div
                        onPointerDown={startHeaderDrag}
                        className={cn(
                            "shrink-0 px-3 h-12 flex items-center gap-2 border-b border-slate-200",
                            !expanded && smUp && "touch-none select-none",
                            isFloat
                                ? "cursor-grab active:cursor-grabbing"
                                : !expanded && smUp && "sm:cursor-grab",
                        )}
                    >
                        <AgentMark className="w-4 h-4 text-sky-600" />
                        <div className="text-[13px] font-semibold text-slate-900">
                            Assistant
                        </div>
                        <div className="ml-auto flex items-center gap-1">
                            {!isFloat && (
                                <button
                                    onClick={() =>
                                        setSide(side === "right" ? "left" : "right")
                                    }
                                    title={
                                        side === "right"
                                            ? "Move to the left edge"
                                            : "Move to the right edge"
                                    }
                                    aria-label="Switch panel side"
                                    className="size-7 rounded-md text-slate-500 hover:text-slate-900 hover:bg-slate-100 hidden sm:inline-flex items-center justify-center transition-colors"
                                >
                                    {side === "right" ? (
                                        <PanelLeftIcon className="w-4 h-4" />
                                    ) : (
                                        <PanelRightIcon className="w-4 h-4" />
                                    )}
                                </button>
                            )}
                            {!expanded && (
                                <button
                                    onClick={() => setFloating(!floating)}
                                    title={
                                        isFloat
                                            ? `Dock to the ${side} edge (⌥P)`
                                            : "Pop out into a window (⌥P)"
                                    }
                                    aria-label={
                                        isFloat ? "Dock panel" : "Pop out into a floating window"
                                    }
                                    className="size-7 rounded-md text-slate-500 hover:text-slate-900 hover:bg-slate-100 hidden sm:inline-flex items-center justify-center transition-colors"
                                >
                                    {isFloat ? (
                                        side === "right" ? (
                                            <PanelRightIcon className="w-4 h-4" />
                                        ) : (
                                            <PanelLeftIcon className="w-4 h-4" />
                                        )
                                    ) : (
                                        <PictureInPicture2Icon className="w-4 h-4" />
                                    )}
                                </button>
                            )}
                            <button
                                onClick={() => setMinimized(true)}
                                title="Minimize to dock (⌥M)"
                                aria-label="Minimize to dock"
                                className="size-7 rounded-md text-slate-500 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center justify-center transition-colors"
                            >
                                <MinusIcon className="w-4 h-4" />
                            </button>
                            <button
                                onClick={() => setExpanded(!expanded)}
                                title={expanded ? "Collapse" : "Expand to workspace"}
                                aria-label={
                                    expanded ? "Collapse" : "Expand to workspace"
                                }
                                className="size-7 rounded-md text-slate-500 hover:text-slate-900 hover:bg-slate-100 hidden sm:inline-flex items-center justify-center transition-colors"
                            >
                                {expanded ? (
                                    <Minimize2Icon className="w-4 h-4" />
                                ) : (
                                    <Maximize2Icon className="w-4 h-4" />
                                )}
                            </button>
                            <button
                                onClick={closePanel}
                                title="Close"
                                aria-label="Close assistant"
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
                    <div className="relative flex-1 min-h-0">
                        <div
                            ref={scrollRef}
                            onScroll={onScroll}
                            className="h-full overflow-y-auto"
                        >
                            <div
                                className={cn(
                                    "min-h-full flex flex-col px-4 py-4 space-y-4",
                                    expanded && "mx-auto w-full max-w-[760px]",
                                )}
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
                                    !activeTab.running && (
                                        <EmptyState
                                            onPick={(q) => {
                                                setDraft(q);
                                                inputRef.current?.focus();
                                            }}
                                        />
                                    )}
                                {activeTab?.turns.map((t, i) => (
                                    <TurnView
                                        key={t.id}
                                        turn={t}
                                        onOpen={openUrl}
                                        streaming={
                                            !!activeTab.running &&
                                            !activeTab.pending &&
                                            i === activeTab.turns.length - 1
                                        }
                                    />
                                ))}
                                {activeTab?.pending && (
                                    <ApprovalCard
                                        pending={activeTab.pending}
                                        onDecide={(d) => decide(activeTab.key, d)}
                                    />
                                )}
                                {activeTab?.running && !activeTab.pending && (
                                    <motion.div
                                        initial={{ opacity: 0, y: 4 }}
                                        animate={{ opacity: 1, y: 0 }}
                                        transition={{ duration: 0.16 }}
                                        className="flex items-center gap-2 text-[12px]"
                                    >
                                        <AgentMark className="w-3.5 h-3.5 text-sky-500 animate-pulse" />
                                        <span className="ai-shimmer-text font-medium">
                                            Working…
                                        </span>
                                        {activeTab.iteration > 0 && (
                                            <span className="font-mono tabular-nums text-slate-300">
                                                step {activeTab.iteration}/
                                                {activeTab.budget}
                                            </span>
                                        )}
                                    </motion.div>
                                )}
                            </div>
                        </div>
                        {!pinned && activeTab?.running && (
                            <button
                                onClick={() => {
                                    setPinned(true);
                                    scrollToBottom("smooth");
                                }}
                                className="absolute bottom-3 left-1/2 -translate-x-1/2 h-7 px-3 rounded-full bg-white border border-slate-200 shadow-sm text-[11.5px] text-slate-600 hover:text-slate-900 inline-flex items-center gap-1.5 transition-colors"
                            >
                                <ArrowDownIcon className="w-3 h-3" />
                                Latest
                            </button>
                        )}
                    </div>

                    {/* Composer */}
                    <div className="shrink-0 border-t border-slate-200 p-3">
                        <div className={cn(expanded && "mx-auto w-full max-w-[760px]")}>
                            {/* py-1 + leading-5 make a single line exactly the
                                size-7 button height, so text centers against it;
                                items-end keeps the button pinned when it grows. */}
                            <div className="flex items-end gap-2 rounded-lg border border-slate-200 focus-within:border-sky-400 focus-within:ring-2 focus-within:ring-sky-100 px-2.5 py-1.5 transition-colors">
                                <textarea
                                    ref={inputRef}
                                    value={draft}
                                    onChange={(e) => setDraft(e.target.value)}
                                    onKeyDown={(e) => {
                                        if (
                                            e.key === "Enter" &&
                                            !e.shiftKey &&
                                            !e.nativeEvent.isComposing
                                        ) {
                                            e.preventDefault();
                                            send();
                                        }
                                    }}
                                    rows={1}
                                    placeholder={
                                        activeTab?.pending
                                            ? "Respond to the approval above first"
                                            : "Ask about contacts, campaigns, your inbox…"
                                    }
                                    disabled={composerLocked}
                                    className="flex-1 resize-none bg-transparent py-1 text-[13px] leading-5 text-slate-900 placeholder:text-slate-400 outline-none max-h-32 disabled:opacity-60"
                                />
                                {activeTab?.running ? (
                                    <button
                                        onClick={() => stop(activeTab.key)}
                                        title="Stop"
                                        aria-label="Stop run"
                                        className="size-7 rounded-md bg-slate-900 hover:bg-slate-700 text-white inline-flex items-center justify-center transition-colors"
                                    >
                                        <SquareIcon
                                            className="w-3 h-3"
                                            fill="currentColor"
                                        />
                                    </button>
                                ) : (
                                    <button
                                        onClick={send}
                                        disabled={!draft.trim() || composerLocked}
                                        title="Send"
                                        aria-label="Send message"
                                        className="size-7 rounded-md bg-sky-600 hover:bg-sky-700 text-white inline-flex items-center justify-center transition-colors disabled:opacity-40"
                                    >
                                        <ArrowUpIcon className="w-4 h-4" />
                                    </button>
                                )}
                            </div>
                            {activeTab?.freeModel && (
                                <div className="mt-2 flex items-center gap-1.5 text-[10.5px] text-amber-600">
                                    <AlertTriangleIcon className="w-3 h-3 shrink-0" />
                                    <span>
                                        Free local model. Responses may be lower quality,
                                        and nothing is charged.
                                    </span>
                                </div>
                            )}
                            <div className="mt-2 flex items-center justify-between text-[10.5px] text-slate-400">
                                <span>
                                    Read actions run automatically. Writes ask first.
                                </span>
                                {!activeTab?.freeModel &&
                                    activeTab?.credits != null && (
                                        <span className="font-mono tabular-nums">
                                            {activeTab.credits.toLocaleString()} credits
                                        </span>
                                    )}
                            </div>
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
    const ref = React.useRef<HTMLDivElement>(null);

    // Keep the active tab visible when the bar overflows.
    React.useEffect(() => {
        ref.current
            ?.querySelector('[data-active="true"]')
            ?.scrollIntoView({ block: "nearest", inline: "nearest" });
    }, [activeKey, tabs.length]);

    if (tabs.length === 0) return null;
    return (
        <div
            ref={ref}
            role="tablist"
            aria-label="Conversations"
            className="shrink-0 flex items-stretch gap-1 px-2 h-9 border-b border-slate-200 overflow-x-auto no-scrollbar"
        >
            {tabs.map((t) => {
                const active = t.key === activeKey;
                return (
                    <div
                        key={t.key}
                        role="tab"
                        aria-selected={active}
                        tabIndex={0}
                        data-active={active || undefined}
                        title={t.title}
                        onClick={() => onSelect(t.key)}
                        onKeyDown={(e) => {
                            if (e.key === "Enter" || e.key === " ") {
                                e.preventDefault();
                                onSelect(t.key);
                            }
                        }}
                        // Middle-click closes, like editor tabs.
                        onMouseDown={(e) => {
                            if (e.button === 1) e.preventDefault();
                        }}
                        onAuxClick={(e) => {
                            if (e.button === 1) onClose(t.key);
                        }}
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
                                    t.pending
                                        ? "bg-amber-500"
                                        : t.unseen
                                          ? "bg-sky-500"
                                          : "bg-slate-300",
                                )}
                            />
                        )}
                        <span className="truncate">{t.title}</span>
                        <button
                            onClick={(e) => {
                                e.stopPropagation();
                                onClose(t.key);
                            }}
                            tabIndex={-1}
                            className="size-4 shrink-0 rounded inline-flex items-center justify-center text-slate-400 hover:text-slate-900 hover:bg-slate-200 opacity-100 md:opacity-0 md:group-hover:opacity-100 md:group-focus-within:opacity-100 transition-opacity"
                            aria-label={`Close ${t.title}`}
                        >
                            <XIcon className="w-3 h-3" />
                        </button>
                    </div>
                );
            })}
            <button
                onClick={onNew}
                title="New chat"
                aria-label="New chat"
                className="shrink-0 size-7 my-1 rounded-md text-slate-400 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center justify-center transition-colors"
            >
                <PlusIcon className="w-4 h-4" />
            </button>
        </div>
    );
}

// ── Minimized dock ──────────────────────────────────────────────────
// Compact bottom-right status bar shown while the panel is minimized. It
// mirrors the most urgent tab (running > approval > unread > active) live;
// clicking it restores the panel on that tab.
function DockBar({
    tabs,
    activeKey,
    side,
    onRestore,
    onClose,
}: {
    tabs: AgentTab[];
    activeKey: string | null;
    side: "left" | "right";
    onRestore: (key: string | null) => void;
    onClose: () => void;
}) {
    const focus =
        tabs.find((t) => t.running) ??
        tabs.find((t) => t.pending) ??
        tabs.find((t) => t.unseen) ??
        tabs.find((t) => t.key === activeKey) ??
        tabs[0] ??
        null;

    let status: React.ReactNode;
    if (focus?.running) {
        status = (
            <span className="inline-flex items-center gap-1.5 text-slate-500">
                <Loader2Icon className="w-3 h-3 animate-spin text-sky-500" />
                Working
                {focus.iteration > 0 && (
                    <span className="font-mono tabular-nums text-slate-400">
                        {focus.iteration}/{focus.budget}
                    </span>
                )}
            </span>
        );
    } else if (focus?.pending) {
        status = (
            <span className="inline-flex items-center gap-1.5 text-amber-700">
                <span className="size-1.5 rounded-full bg-amber-500" />
                Needs approval
            </span>
        );
    } else if (focus?.unseen) {
        status = (
            <span className="inline-flex items-center gap-1.5 text-sky-700">
                <span className="relative flex size-1.5">
                    <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-sky-400 opacity-60" />
                    <span className="relative inline-flex size-1.5 rounded-full bg-sky-500" />
                </span>
                Response ready
            </span>
        );
    } else {
        status = (
            <span className="inline-flex items-center gap-1.5 text-slate-400">
                <span className="size-1.5 rounded-full bg-slate-300" />
                Idle
            </span>
        );
    }

    return (
        <motion.div
            initial={{ y: 16, opacity: 0 }}
            animate={{ y: 0, opacity: 1 }}
            transition={{ type: "spring", stiffness: 420, damping: 34 }}
            className={cn(
                "fixed bottom-4 z-50",
                side === "right" ? "right-4" : "left-4",
            )}
        >
            <div
                role="button"
                tabIndex={0}
                onClick={() => onRestore(focus?.key ?? null)}
                onKeyDown={(e) => {
                    if (e.key === "Enter" || e.key === " ") {
                        e.preventDefault();
                        onRestore(focus?.key ?? null);
                    }
                }}
                className="h-10 pl-2.5 pr-1 rounded-lg border border-slate-200 bg-white shadow-lg shadow-slate-900/10 flex items-center gap-2 cursor-pointer hover:border-slate-300 transition-colors"
            >
                <AgentMark className="w-4 h-4 text-sky-600 shrink-0" />
                <span className="max-w-[160px] truncate text-[12.5px] font-medium text-slate-800">
                    {focus?.title ?? "Assistant"}
                </span>
                <span className="text-[11.5px]">{status}</span>
                <span className="flex items-center gap-0.5 pl-1">
                    <button
                        onClick={(e) => {
                            e.stopPropagation();
                            onRestore(focus?.key ?? null);
                        }}
                        title="Restore"
                        aria-label="Restore assistant"
                        className="size-6 rounded inline-flex items-center justify-center text-slate-400 hover:text-slate-900 hover:bg-slate-100 transition-colors"
                    >
                        <ChevronUpIcon className="w-3.5 h-3.5" />
                    </button>
                    <button
                        onClick={(e) => {
                            e.stopPropagation();
                            onClose();
                        }}
                        title="Close"
                        aria-label="Close assistant"
                        className="size-6 rounded inline-flex items-center justify-center text-slate-400 hover:text-slate-900 hover:bg-slate-100 transition-colors"
                    >
                        <XIcon className="w-3.5 h-3.5" />
                    </button>
                </span>
            </div>
        </motion.div>
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
                turn.blocks[turn.blocks.length - 1] = {
                    kind: "text",
                    text: last.text + (ev.text || ""),
                };
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
                    turn.blocks[i] = {
                        kind: "tool",
                        step: {
                            ...b.step,
                            done: true,
                            result: ev.result,
                            entityType: ev.entity_type,
                            entityId: ev.entity_id,
                            openURL: ev.open_url,
                        },
                    };
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

// Memoized so streaming into the trailing turn doesn't re-render the whole
// transcript on every token (fold copies only the turn it touches).
const TurnView = React.memo(function TurnView({
    turn,
    onOpen,
    streaming = false,
}: {
    turn: AgentTurn;
    onOpen: (url: string) => void;
    // True for the trailing assistant turn while its run is live; puts a
    // blinking caret at the end of the text still being generated.
    streaming?: boolean;
}) {
    if (turn.role === "user") {
        const text = turn.blocks
            .map((b) => (b.kind === "text" ? b.text : ""))
            .join("");
        return (
            <motion.div
                initial={{ opacity: 0, y: 6, scale: 0.98 }}
                animate={{ opacity: 1, y: 0, scale: 1 }}
                transition={{ type: "spring", stiffness: 500, damping: 36 }}
                className="flex justify-end"
            >
                <div className="max-w-[85%] rounded-2xl rounded-br-sm bg-sky-600 text-white px-3 py-2 text-[13px] whitespace-pre-wrap break-words">
                    {text}
                </div>
            </motion.div>
        );
    }
    const lastIdx = turn.blocks.length - 1;
    return (
        <div className="space-y-2">
            {turn.blocks.map((b, i) => {
                if (b.kind === "text") {
                    return b.text ? (
                        <Markdown
                            key={i}
                            text={b.text}
                            onOpen={onOpen}
                            caret={streaming && i === lastIdx}
                        />
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
});

function ToolStepRow({
    step,
    onOpen,
}: {
    step: AgentToolStep;
    onOpen: (url: string) => void;
}) {
    return (
        <motion.div
            initial={{ opacity: 0, y: 4 }}
            animate={{ opacity: 1, y: 0 }}
            transition={{ duration: 0.16, ease: [0.16, 1, 0.3, 1] }}
            className="rounded-md border border-slate-200 bg-slate-50/60 px-2.5 py-1.5"
        >
            <div className="flex items-center gap-1.5 text-[11.5px] text-slate-600">
                {step.done ? (
                    <motion.span
                        initial={{ scale: 0.5, opacity: 0 }}
                        animate={{ scale: 1, opacity: 1 }}
                        transition={{ type: "spring", stiffness: 600, damping: 24 }}
                        className="shrink-0 inline-flex"
                    >
                        <CheckIcon className="w-3 h-3 text-emerald-600" />
                    </motion.span>
                ) : (
                    <Loader2Icon className="w-3 h-3 animate-spin text-slate-400 shrink-0" />
                )}
                <WrenchIcon className="w-3 h-3 text-slate-400 shrink-0" />
                <span className="font-medium text-slate-700">
                    {toolLabel(step.tool)}
                </span>
                {step.result && (
                    <span className="text-slate-400 truncate" title={step.result}>
                        — {step.result}
                    </span>
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
        </motion.div>
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
        <div className="flex-1 flex flex-col items-center justify-center text-center px-6 py-10">
            <AgentMark className="w-6 h-6 text-sky-600 mb-3" />
            <div className="text-[13px] font-semibold text-slate-900">
                How can I help?
            </div>
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
