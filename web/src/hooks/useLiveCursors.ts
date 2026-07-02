// useLiveCursors — the reusable live-cursor transport. It rides the org
// channel's ephemeral "live:cursor" path: a teammate's pointer fans out to
// everyone focused on the same `resource` with no database write and no
// react-query churn. Frames are best-effort and never resumed, so we throttle on
// send and prune stale cursors on receive. Identity (name/avatar) is resolved
// from presence, which is already synced; the wire frame stays tiny.
//
// The transport is coordinate-agnostic: x/y mean whatever the caller decides
// (flow-space on a canvas, content-space on a scrolling page). The renderer that
// consumes `cursors` is responsible for mapping them to the screen.

import React from "react";
import { useSocket } from "./context/socket";
import { useUserProfile } from "./context/user";
import { useAppStore } from "@/stores";

export interface RemoteCursor {
    userId: string;
    /** Caller-defined coordinates (flow-space on a canvas, content-space on a page). */
    x: number;
    y: number;
    name: string | null;
    avatar: string | null;
    color: string;
    /** Cursor-chat text riding this teammate's frames; null when not chatting. */
    chat: string | null;
    /** performance.now() of the last frame, for TTL pruning. */
    lastSeen: number;
}

// A teammate cursor disappears this long after its last frame (covers a tab that
// closed without sending a "gone").
export const CURSOR_TTL_MS = 5000;
const PRUNE_INTERVAL_MS = 1000;
// ~22 Hz: smooth enough to read as continuous, well under the channel budget.
const CURSOR_INTERVAL_MS = 45;
// While present but holding still, re-emit the last cursor this often so a
// stationary teammate isn't pruned by the TTL. Must stay under CURSOR_TTL_MS.
const CURSOR_HEARTBEAT_MS = 2500;

const PALETTE = [
    "#0ea5e9", // sky
    "#8b5cf6", // violet
    "#ec4899", // pink
    "#f97316", // orange
    "#10b981", // emerald
    "#eab308", // amber
    "#ef4444", // red
    "#14b8a6", // teal
];

/** Stable per-user color from a hash of the id, so a teammate keeps one color. */
export function cursorColor(userId: string): string {
    let h = 0;
    for (let i = 0; i < userId.length; i++) h = (h * 31 + userId.charCodeAt(i)) >>> 0;
    return PALETTE[h % PALETTE.length];
}

export interface LiveCursors {
    /** Remote cursors on this resource, ready to render. */
    cursors: RemoteCursor[];
    /** True when there is someone else here and we should broadcast. */
    active: boolean;
    /** Broadcast our cursor at the given coordinates (throttled). */
    pushCursor: (x: number, y: number) => void;
    /** Tell teammates our cursor left this surface. */
    clearCursor: () => void;
    /** Attach (or clear, with null) cursor-chat text to our outgoing frames. */
    setChat: (text: string | null) => void;
}

export function useLiveCursors(resource: string | null, opts: { enabled: boolean }): LiveCursors {
    const { isConnected, subscribeToChannel, pushToChannel } = useSocket();
    const orgId = useAppStore((s) => s.currentOrganization?.id ?? null);
    const presence = useAppStore((s) => s.presence);
    const { user } = useUserProfile();
    const selfId = user?.id ?? null;

    const active = opts.enabled && !!resource && isConnected;

    const [cursors, setCursors] = React.useState<RemoteCursor[]>([]);
    const cursorMapRef = React.useRef<Map<string, RemoteCursor>>(new Map());
    const rafRef = React.useRef<number | null>(null);

    // Latest values read inside the long-lived subscription callbacks.
    const resourceRef = React.useRef(resource);
    resourceRef.current = resource;
    const selfRef = React.useRef(selfId);
    selfRef.current = selfId;
    const presenceRef = React.useRef(presence);
    presenceRef.current = presence;
    const orgRef = React.useRef(orgId);
    orgRef.current = orgId;
    const activeRef = React.useRef(active);
    activeRef.current = active;

    // Coalesce cursor-state updates to one paint per frame, even with several
    // teammates streaming at once.
    const scheduleFlush = React.useCallback(() => {
        if (rafRef.current != null) return;
        rafRef.current = requestAnimationFrame(() => {
            rafRef.current = null;
            const now = performance.now();
            const map = cursorMapRef.current;
            for (const [id, c] of map) if (now - c.lastSeen > CURSOR_TTL_MS) map.delete(id);
            setCursors([...map.values()]);
        });
    }, []);

    // Subscribe to the org channel's cursor frames. The subscription outlives any
    // single resource: it filters by the current resource so navigating between
    // surfaces never leaks cursors.
    React.useEffect(() => {
        if (!isConnected || !orgId) return;
        const topic = `org:${orgId}`;
        return subscribeToChannel(topic, "LIVE_CURSOR", (p) => {
            const uid = typeof p.user_id === "string" ? p.user_id : "";
            if (!uid || uid === selfRef.current) return;
            if (resourceRef.current && p.resource !== resourceRef.current) return;
            const map = cursorMapRef.current;
            if (p.gone === true) {
                if (map.delete(uid)) scheduleFlush();
                return;
            }
            const metas = presenceRef.current[uid];
            const meta = metas && metas.length ? metas[metas.length - 1] : undefined;
            map.set(uid, {
                userId: uid,
                x: typeof p.x === "number" ? p.x : 0,
                y: typeof p.y === "number" ? p.y : 0,
                name: meta?.name ?? null,
                avatar: meta?.avatar ?? null,
                color: cursorColor(uid),
                chat: typeof p.chat === "string" && p.chat.trim() ? p.chat : null,
                lastSeen: performance.now(),
            });
            scheduleFlush();
        });
    }, [isConnected, orgId, subscribeToChannel, scheduleFlush]);

    // Drop teammates who went quiet without a "gone" (closed tab, lost socket).
    React.useEffect(() => {
        const t = setInterval(() => {
            const now = performance.now();
            const map = cursorMapRef.current;
            let changed = false;
            for (const [id, c] of map)
                if (now - c.lastSeen > CURSOR_TTL_MS) {
                    map.delete(id);
                    changed = true;
                }
            if (changed) setCursors([...map.values()]);
        }, PRUNE_INTERVAL_MS);
        return () => clearInterval(t);
    }, []);

    // Wipe local cursors when the resource changes, so the previous surface's
    // ghosts never bleed onto the next one (runs even while collaboration stays
    // active across an in-place navigation).
    React.useEffect(() => {
        if (cursorMapRef.current.size) {
            cursorMapRef.current.clear();
            setCursors([]);
        }
    }, [resource]);

    // And when collaboration goes idle (no peers / disconnected).
    React.useEffect(() => {
        if (active) return;
        if (cursorMapRef.current.size) {
            cursorMapRef.current.clear();
            setCursors([]);
        }
    }, [active]);

    const rawPush = React.useCallback(
        (event: string, payload: Record<string, unknown>) => {
            const o = orgRef.current;
            if (!o) return;
            pushToChannel(`org:${o}`, event, payload);
        },
        [pushToChannel],
    );
    // Stable handle for effects whose cleanup pushes (so a reconnect-driven
    // rawPush identity change never re-fires their teardown).
    const rawPushRef = React.useRef(rawPush);
    rawPushRef.current = rawPush;

    // Leading + trailing throttle so the final resting position is never lost.
    const cursorLastSent = React.useRef(0);
    const cursorPending = React.useRef<{ x: number; y: number } | null>(null);
    const cursorTimer = React.useRef<number | null>(null);
    // Last position + presence, for the keep-alive heartbeat below.
    const lastCursorPos = React.useRef<{ x: number; y: number } | null>(null);
    const cursorPresent = React.useRef(false);
    // Cursor-chat text attached to every outgoing frame while set.
    const chatRef = React.useRef<string | null>(null);

    const sendCursor = React.useCallback(
        (x: number, y: number) => {
            cursorLastSent.current = performance.now();
            lastCursorPos.current = { x, y };
            cursorPresent.current = true;
            const payload: Record<string, unknown> = { resource: resourceRef.current, x, y };
            if (chatRef.current) payload.chat = chatRef.current;
            rawPush("live:cursor", payload);
        },
        [rawPush],
    );

    const pushCursor = React.useCallback(
        (x: number, y: number) => {
            if (!activeRef.current || !resourceRef.current) return;
            const elapsed = performance.now() - cursorLastSent.current;
            if (elapsed >= CURSOR_INTERVAL_MS) {
                if (cursorTimer.current != null) {
                    clearTimeout(cursorTimer.current);
                    cursorTimer.current = null;
                }
                cursorPending.current = null;
                sendCursor(x, y);
            } else {
                cursorPending.current = { x, y };
                if (cursorTimer.current == null) {
                    cursorTimer.current = window.setTimeout(() => {
                        cursorTimer.current = null;
                        const p = cursorPending.current;
                        cursorPending.current = null;
                        if (p) sendCursor(p.x, p.y);
                    }, CURSOR_INTERVAL_MS - elapsed);
                }
            }
        },
        [sendCursor],
    );

    const clearCursor = React.useCallback(() => {
        if (cursorTimer.current != null) {
            clearTimeout(cursorTimer.current);
            cursorTimer.current = null;
        }
        cursorPending.current = null;
        cursorPresent.current = false;
        if (!resourceRef.current) return;
        rawPush("live:cursor", { resource: resourceRef.current, x: 0, y: 0, gone: true });
    }, [rawPush]);

    // Chat travels on the cursor frames themselves, so the bubble is glued to
    // the pointer with zero extra transport. Setting/clearing emits a frame
    // immediately (at the last known position) so it never waits for movement.
    // Deliberately not gated on cursorPresent: typing IS presence, so chat
    // re-plants the cursor at its last spot even if the pointer wandered off
    // the tracked surface (e.g. over the chat input's own portal).
    const setChat = React.useCallback(
        (text: string | null) => {
            chatRef.current = text && text.trim() ? text : null;
            const p = lastCursorPos.current;
            if (activeRef.current && resourceRef.current && p) sendCursor(p.x, p.y);
        },
        [sendCursor],
    );

    // Keep-alive: while present but holding still, re-emit the last cursor so
    // teammates' TTL never prunes a genuinely-present idle user.
    React.useEffect(() => {
        const t = setInterval(() => {
            if (!activeRef.current || !cursorPresent.current || !resourceRef.current) return;
            const p = lastCursorPos.current;
            if (!p) return;
            const payload: Record<string, unknown> = { resource: resourceRef.current, x: p.x, y: p.y };
            if (chatRef.current) payload.chat = chatRef.current;
            rawPushRef.current("live:cursor", payload);
        }, CURSOR_HEARTBEAT_MS);
        return () => clearInterval(t);
    }, []);

    // In-place resource change (the page cursor layer survives navigation rather
    // than unmounting): tell teammates on the resource we just left that our
    // cursor is gone, and drop our presence so the heartbeat can't re-emit the
    // old page's coordinates against the new resource. Canvas hooks unmount on
    // navigation instead, which the teardown below already covers.
    const prevResourceRef = React.useRef(resource);
    React.useEffect(() => {
        const prev = prevResourceRef.current;
        prevResourceRef.current = resource;
        if (prev && prev !== resource && cursorPresent.current) {
            if (orgRef.current) rawPushRef.current("live:cursor", { resource: prev, x: 0, y: 0, gone: true });
            cursorPresent.current = false;
            lastCursorPos.current = null;
            // Chat is a conversation on a surface; it never follows across one.
            chatRef.current = null;
        }
    }, [resource]);

    // Tear down on unmount, and tell teammates our cursor is gone right away
    // rather than making them wait out the TTL.
    React.useEffect(() => {
        return () => {
            if (cursorPresent.current && resourceRef.current && orgRef.current) {
                rawPushRef.current("live:cursor", { resource: resourceRef.current, x: 0, y: 0, gone: true });
            }
            if (cursorTimer.current != null) clearTimeout(cursorTimer.current);
            if (rafRef.current != null) cancelAnimationFrame(rafRef.current);
        };
    }, []);

    return { cursors, active, pushCursor, clearCursor, setChat };
}
