// Remie's speech bubble under the header blob: one problem worth fixing,
// offered at a moment that does not interrupt, with one way to act on it.
// remieTips.ts decides whether and which; this decides when (idle, nothing else
// open, the tab visible). It never shows while Remie's panel is open, where the
// same suggestions are listed instead.

import React from "react";
import { AnimatePresence, motion } from "framer-motion";
import { useLocation } from "@tanstack/react-router";
import { BellOffIcon, XIcon } from "lucide-react";
import { useAppStore } from "@/stores";
import useClickOutside from "@/hooks/useClickOutside";
import { usePermission } from "@/hooks/usePermission";
import { SEVERITY_RANK } from "@/lib/api/models/app/advisor/Advisor";
import { useAdvisorFindings } from "@/lib/api/hooks/app/advisor/useAdvisor";
import AgentMark from "./AgentMark";
import { askRemie } from "./askRemie";
import {
    TIP_RULES,
    fixPromptFor,
    loadMemory,
    pauseForWeek,
    pickTip,
    pruneSeen,
    recordEngaged,
    recordIgnored,
    recordShown,
    saveMemory,
    suggestionsFrom,
    type RemieSuggestion,
    type TipMemory,
} from "./remieTips";

// Layers that mean the member is in the middle of something.
const BUSY_SELECTOR = '[role="dialog"], [role="alertdialog"], [data-floating]';

// `anchor` wraps the blob button, so clicking the blob is not a dismissal.
export default function RemieTip({ anchor }: { anchor: React.RefObject<HTMLElement | null> }) {
    const userId = useAppStore((s) => s.user?.id ?? "");
    const orgId = useAppStore((s) => s.currentOrganization?.id ?? "");
    const panelOpen = useAppStore((s) => s.aiAssistantOpen && !s.agentMinimized);
    const pathname = useLocation({ select: (l) => l.pathname });

    const [wide, setWide] = React.useState(() => window.matchMedia("(min-width: 640px)").matches);
    React.useEffect(() => {
        const m = window.matchMedia("(min-width: 640px)");
        const fn = () => setWide(m.matches);
        m.addEventListener("change", fn);
        return () => m.removeEventListener("change", fn);
    }, []);

    // Findings are read under View analytics, the Advisor's own permission.
    const canSeeAdvisor = usePermission("VIEW_ANALYTICS");
    const touring = useAppStore((s) => s.productTourOpen);
    const enabled = !!userId && !!orgId && wide && canSeeAdvisor && !touring;
    const { data: findings } = useAdvisorFindings({ limit: 50 }, enabled);
    const suggestions = React.useMemo(
        () => suggestionsFrom(findings ?? [], SEVERITY_RANK.low),
        [findings],
    );

    const [tip, setTip] = React.useState<RemieSuggestion | null>(null);
    const hovered = React.useRef(false);
    const bubble = React.useRef<HTMLDivElement>(null);
    // A bubble that unmounts under the cursor never sees pointerleave.
    React.useEffect(() => {
        hovered.current = false;
    }, [tip]);

    const update = React.useCallback(
        (fn: (m: TipMemory) => TipMemory) => {
            if (!userId || !orgId) return;
            saveMemory(userId, orgId, fn(loadMemory(userId, orgId)));
        },
        [userId, orgId],
    );

    // Last time the member typed, clicked or scrolled. A tip waits for a pause.
    const mountedAt = React.useRef(Date.now());
    const lastInput = React.useRef(Date.now());
    React.useEffect(() => {
        const mark = () => {
            lastInput.current = Date.now();
        };
        const opts = { capture: true, passive: true };
        window.addEventListener("keydown", mark, opts);
        window.addEventListener("pointerdown", mark, opts);
        window.addEventListener("wheel", mark, opts);
        return () => {
            window.removeEventListener("keydown", mark, opts);
            window.removeEventListener("pointerdown", mark, opts);
            window.removeEventListener("wheel", mark, opts);
        };
    }, []);

    // Forget kinds of problem that have closed.
    React.useEffect(() => {
        if (findings) update((m) => pruneSeen(m, new Set(suggestions.map((s) => s.key))));
    }, [findings, suggestions, update]);

    // Check now and then whether this is a good moment. Local only: findings
    // arrive through the realtime advisor invalidation, not by polling.
    React.useEffect(() => {
        if (!enabled || suggestions.length === 0 || tip || panelOpen) return;
        const t = window.setInterval(() => {
            const now = Date.now();
            if (document.hidden) return;
            if (now - mountedAt.current < TIP_RULES.warmupMs) return;
            if (now - lastInput.current < TIP_RULES.idleMs) return;
            if (document.querySelector(BUSY_SELECTOR)) return;
            const m = loadMemory(userId, orgId);
            const next = pickTip(suggestions, m, now, pathname);
            if (!next) return;
            saveMemory(userId, orgId, recordShown(m, next, now));
            setTip(next);
        }, 2000);
        return () => window.clearInterval(t);
    }, [enabled, suggestions, tip, panelOpen, pathname, userId, orgId]);

    // Something else claiming attention (a dialog, a popover) puts the tip away
    // without counting it against tips.
    React.useEffect(() => {
        if (!tip) return;
        const t = window.setInterval(() => {
            if (document.querySelector(BUSY_SELECTOR)) setTip(null);
        }, 500);
        return () => window.clearInterval(t);
    }, [tip]);

    // A tip nobody touches leaves on its own and counts as not wanted.
    React.useEffect(() => {
        if (!tip) return;
        let t = 0;
        const hide = () => {
            if (hovered.current) {
                t = window.setTimeout(hide, 2000);
                return;
            }
            update((m) => recordIgnored(m, Date.now()));
            setTip(null);
        };
        t = window.setTimeout(hide, TIP_RULES.autoHideMs);
        return () => window.clearTimeout(t);
    }, [tip, update]);

    // Opening Remie takes over: the panel lists suggestions itself.
    React.useEffect(() => {
        if (panelOpen && tip) setTip(null);
    }, [panelOpen, tip]);

    const close = React.useCallback(
        (how: "ignored" | "engaged") => {
            update((m) => (how === "ignored" ? recordIgnored(m, Date.now()) : recordEngaged(m)));
            setTip(null);
        },
        [update],
    );

    // Click-away and Escape, through the shared layer stack like every floating layer.
    useClickOutside(!!tip, () => close("ignored"), [bubble, anchor]);

    return (
        <AnimatePresence>
            {tip && (
                <motion.div
                    ref={bubble}
                    key={tip.key}
                    role="status"
                    aria-live="polite"
                    initial={{ opacity: 0, y: -8, scale: 0.92 }}
                    animate={{ opacity: 1, y: 0, scale: 1 }}
                    exit={{ opacity: 0, y: -6, scale: 0.96 }}
                    transition={{ type: "spring", stiffness: 460, damping: 30 }}
                    style={{ transformOrigin: "calc(100% - 14px) -6px" }}
                    onPointerEnter={() => {
                        hovered.current = true;
                    }}
                    onPointerLeave={() => {
                        hovered.current = false;
                    }}
                    className="absolute right-0 top-full z-40 mt-2.5 w-[300px] rounded-2xl border border-slate-200/80 bg-white p-3.5 shadow-[0_18px_44px_-14px_rgba(15,23,42,0.3)]"
                >
                    <span
                        aria-hidden
                        className="absolute -top-[5px] right-[10px] size-2.5 rotate-45 rounded-[2px] border-l border-t border-slate-200/80 bg-white"
                    />
                    <div className="flex items-center gap-2">
                        <AgentMark size={18} />
                        <span className="text-[12px] font-semibold text-slate-900">Remie</span>
                        <div className="ml-auto -mr-1.5 flex items-center">
                            <button
                                type="button"
                                onClick={() => {
                                    update((m) => pauseForWeek(m, Date.now()));
                                    setTip(null);
                                }}
                                title="Pause tips for a week"
                                aria-label="Pause tips for a week"
                                className="size-6 inline-flex items-center justify-center rounded-md text-slate-300 hover:text-slate-600 hover:bg-slate-100 transition-colors"
                            >
                                <BellOffIcon className="size-3.5" />
                            </button>
                            <button
                                type="button"
                                onClick={() => close("ignored")}
                                aria-label="Dismiss"
                                className="size-6 inline-flex items-center justify-center rounded-md text-slate-300 hover:text-slate-600 hover:bg-slate-100 transition-colors"
                            >
                                <XIcon className="size-3.5" />
                            </button>
                        </div>
                    </div>
                    <p className="mt-2 text-[13px] leading-snug text-slate-800">{tip.text}</p>
                    <div className="mt-3 flex items-center gap-1">
                        <button
                            type="button"
                            onClick={() => {
                                close("engaged");
                                askRemie(fixPromptFor(tip));
                            }}
                            className="h-7 px-3 rounded-lg bg-sky-600 hover:bg-sky-700 text-white text-[12px] font-medium transition-colors"
                        >
                            Fix with Remie
                        </button>
                        <button
                            type="button"
                            onClick={() => close("engaged")}
                            className="h-7 px-2.5 rounded-lg text-[12px] text-slate-500 hover:text-slate-800 hover:bg-slate-100 transition-colors"
                        >
                            Not now
                        </button>
                    </div>
                </motion.div>
            )}
        </AnimatePresence>
    );
}
