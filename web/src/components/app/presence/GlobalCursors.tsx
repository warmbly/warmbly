// GlobalCursors — teammates' live pointers on ordinary (non-canvas) dashboard
// pages. Coordinates are relative to the main scroll container's content box and
// compensated for its scroll, so on pages that scroll at the app level a pointer
// lands on the same logical spot for everyone and follows the content as either
// person scrolls. Many pages scroll an inner container instead (its scrollTop is
// not tracked), so there it degrades gracefully to the pointer's position within
// the content viewport — still a faithful "here is where they are" indicator.
//
// Canvas surfaces (the automation/sequence builders) have their own precise
// flow-space cursor layer; they call useSuppressGlobalCursors() so the page
// layer goes dormant there and a pointer is never broadcast twice.

import React from "react";
import { useLocation } from "react-router-dom";
import { cursorColor, useLiveCursors } from "@/hooks/useLiveCursors";
import { useOnlineMembers } from "@/hooks/PresenceProvider";
import { useUserProfile } from "@/hooks/context/user";
import Cursor from "./Cursor";
import CursorChat from "./CursorChat";

interface SuppressValue {
    claim: () => () => void;
}

const SuppressContext = React.createContext<SuppressValue>({ claim: () => () => {} });

/**
 * Silence the page-level cursor layer while a surface that owns the pointer is
 * active: a canvas with its own cursor layer, or a full-screen modal over the
 * page content. `active` lets a permanently-mounted modal gate it on its open
 * state; it defaults to true for surfaces that only mount when shown. Released
 * on unmount or when `active` goes false.
 */
export function useSuppressGlobalCursors(active: boolean = true) {
    const { claim } = React.useContext(SuppressContext);
    React.useEffect(() => {
        if (!active) return;
        return claim();
    }, [claim, active]);
}

export function GlobalCursorsProvider({
    scrollRef,
    children,
}: {
    scrollRef: React.RefObject<HTMLElement | null>;
    children: React.ReactNode;
}) {
    const [suppressed, setSuppressed] = React.useState(0);
    const claim = React.useCallback(() => {
        setSuppressed((n) => n + 1);
        return () => setSuppressed((n) => n - 1);
    }, []);
    const ctx = React.useMemo(() => ({ claim }), [claim]);

    return (
        <SuppressContext.Provider value={ctx}>
            {children}
            {suppressed === 0 ? <GlobalCursorsOverlay scrollRef={scrollRef} /> : null}
        </SuppressContext.Provider>
    );
}

function GlobalCursorsOverlay({ scrollRef }: { scrollRef: React.RefObject<HTMLElement | null> }) {
    const { pathname } = useLocation();
    const resource = `page:${pathname}`;

    // Only broadcast when a teammate is on this same route (presence carries each
    // member's current page); otherwise stay quiet.
    const members = useOnlineMembers();
    const hasPeers = members.some((m) => m.page === pathname);

    const live = useLiveCursors(resource, { enabled: hasPeers });
    const { pushCursor, clearCursor } = live;

    // Re-render on our own scroll/resize so a stationary teammate's cursor tracks
    // the content as it moves under our viewport.
    const [, tick] = React.useReducer((n: number) => n + 1, 0);
    React.useEffect(() => {
        const el = scrollRef.current;
        if (!el) return;
        let raf = 0;
        const onChange = () => {
            if (raf) return;
            raf = requestAnimationFrame(() => {
                raf = 0;
                tick();
            });
        };
        // Capture so scrolls in inner containers (which don't bubble) also tick.
        el.addEventListener("scroll", onChange, { capture: true, passive: true });
        window.addEventListener("resize", onChange);
        return () => {
            el.removeEventListener("scroll", onChange, { capture: true });
            window.removeEventListener("resize", onChange);
            if (raf) cancelAnimationFrame(raf);
        };
    }, [scrollRef]);

    // Broadcast our pointer in content-space (viewport offset + scroll).
    React.useEffect(() => {
        const el = scrollRef.current;
        if (!el) return;
        const onMove = (e: PointerEvent) => {
            const r = el.getBoundingClientRect();
            pushCursor(e.clientX - r.left + el.scrollLeft, e.clientY - r.top + el.scrollTop);
        };
        const onLeave = () => clearCursor();
        el.addEventListener("pointermove", onMove, { passive: true });
        el.addEventListener("pointerleave", onLeave);
        return () => {
            el.removeEventListener("pointermove", onMove);
            el.removeEventListener("pointerleave", onLeave);
        };
    }, [scrollRef, pushCursor, clearCursor]);

    const { user } = useUserProfile();
    const selfColor = cursorColor(user?.id ?? "");

    const el = scrollRef.current;
    const r = el?.getBoundingClientRect();
    const sl = el?.scrollLeft ?? 0;
    const st = el?.scrollTop ?? 0;
    return (
        <>
            {el && r && live.cursors.length ? (
                <div
                    className="pointer-events-none fixed z-30 overflow-hidden"
                    style={{ left: r.left, top: r.top, width: r.width, height: r.height }}
                >
                    {live.cursors.map((c) => (
                        <Cursor
                            key={c.userId}
                            color={c.color}
                            name={c.name}
                            avatar={c.avatar}
                            chat={c.chat}
                            left={c.x - sl}
                            top={c.y - st}
                        />
                    ))}
                </div>
            ) : null}
            {/* Keyed by resource: navigating remounts the chat, so an open
                input (and its text) never follows you to the next page's
                audience. */}
            <CursorChat key={resource} active={live.active} color={selfColor} setChat={live.setChat} />
        </>
    );
}
