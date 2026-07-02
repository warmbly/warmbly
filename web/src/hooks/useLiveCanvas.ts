// useLiveCanvas — live collaboration for a shared @xyflow canvas. It composes the
// generic live-cursor transport (useLiveCursors) with two more ephemeral streams:
// card drags ("live:node") and selections ("live:select"), so teammates see each
// other's pointers, moving nodes, and colored selection outlines in real time.
// Coordinates are flow-space.

import React from "react";
import { useSocket } from "./context/socket";
import { useUserProfile } from "./context/user";
import { useAppStore } from "@/stores";
import { cursorColor, useLiveCursors, type LiveCursors } from "./useLiveCursors";

export { cursorColor } from "./useLiveCursors";
export type { RemoteCursor } from "./useLiveCursors";

// ~22 Hz, matching the cursor stream: smooth, well under the channel budget.
const NODE_INTERVAL_MS = 45;
// Re-emit a non-empty selection this often so a teammate who opens the canvas
// mid-session sees existing outlines, and a dropped frame self-heals.
const SELECT_HEARTBEAT_MS = 5000;

export interface RemoteSelection {
    userId: string;
    /** Node ids this teammate currently has selected. Never empty. */
    ids: string[];
    name: string | null;
    color: string;
}

export interface LiveCanvas extends LiveCursors {
    /** Broadcast a node position; dragging=false sends immediately (final spot). */
    pushNode: (id: string, x: number, y: number, dragging: boolean) => void;
    /** Broadcast our current selection (deduped; empty clears it for teammates). */
    pushSelect: (ids: string[]) => void;
    /** Teammates' selections on this canvas, ready to outline. */
    selections: RemoteSelection[];
}

export function useLiveCanvas(
    resource: string | null,
    opts: {
        enabled: boolean;
        onRemoteNode?: (id: string, x: number, y: number, dragging: boolean, by: string) => void;
    },
): LiveCanvas {
    const cursors = useLiveCursors(resource, { enabled: opts.enabled });

    const { isConnected, subscribeToChannel, pushToChannel } = useSocket();
    const orgId = useAppStore((s) => s.currentOrganization?.id ?? null);
    const presence = useAppStore((s) => s.presence);
    const { user } = useUserProfile();

    const resourceRef = React.useRef(resource);
    resourceRef.current = resource;
    const orgRef = React.useRef(orgId);
    orgRef.current = orgId;
    const activeRef = React.useRef(cursors.active);
    activeRef.current = cursors.active;
    const selfRef = React.useRef<string | null>(user?.id ?? null);
    selfRef.current = user?.id ?? null;
    const onRemoteNodeRef = React.useRef(opts.onRemoteNode);
    onRemoteNodeRef.current = opts.onRemoteNode;

    // Subscribe to teammates' node drags. (Cursor frames are handled by the
    // cursor hook; this only adds the node stream.) The server excludes the
    // sender, but a second tab of the same user would echo, so filter self.
    React.useEffect(() => {
        if (!isConnected || !orgId) return;
        const topic = `org:${orgId}`;
        return subscribeToChannel(topic, "LIVE_NODE", (p) => {
            const by = typeof p.user_id === "string" ? p.user_id : "";
            if (!by || by === selfRef.current) return;
            if (resourceRef.current && p.resource !== resourceRef.current) return;
            if (typeof p.id !== "string") return;
            onRemoteNodeRef.current?.(p.id, Number(p.x) || 0, Number(p.y) || 0, p.dragging === true, by);
        });
    }, [isConnected, orgId, subscribeToChannel]);

    const rawPush = React.useCallback(
        (event: string, payload: Record<string, unknown>) => {
            const o = orgRef.current;
            if (!o) return;
            pushToChannel(`org:${o}`, event, payload);
        },
        [pushToChannel],
    );
    // Stable handle for [] effects (heartbeat), mirroring useLiveCursors.
    const rawPushRef = React.useRef(rawPush);
    rawPushRef.current = rawPush;

    // ── Selections ────────────────────────────────────────────────────────────
    // Raw per-teammate selection map from the wire. Display additionally gates
    // on presence (the teammate must still be on this resource), so a closed
    // tab's outline disappears with their presence rather than lingering.
    const [selMap, setSelMap] = React.useState<Map<string, string[]>>(new Map());

    React.useEffect(() => {
        if (!isConnected || !orgId) return;
        return subscribeToChannel(`org:${orgId}`, "LIVE_SELECT", (p) => {
            const by = typeof p.user_id === "string" ? p.user_id : "";
            if (!by || by === selfRef.current) return;
            if (resourceRef.current && p.resource !== resourceRef.current) return;
            const ids = Array.isArray(p.ids) ? p.ids.filter((v): v is string => typeof v === "string") : [];
            setSelMap((m) => {
                if (!ids.length && !m.has(by)) return m;
                const next = new Map(m);
                if (ids.length) next.set(by, ids);
                else next.delete(by);
                return next;
            });
        });
    }, [isConnected, orgId, subscribeToChannel]);

    // Selections are per-surface: wipe on resource change and when idle.
    React.useEffect(() => {
        setSelMap((m) => (m.size ? new Map() : m));
    }, [resource]);
    React.useEffect(() => {
        if (cursors.active) return;
        setSelMap((m) => (m.size ? new Map() : m));
    }, [cursors.active]);

    const selections = React.useMemo<RemoteSelection[]>(() => {
        const out: RemoteSelection[] = [];
        for (const [uid, ids] of selMap) {
            const metas = presence[uid];
            if (!metas?.some((m) => m.resource === resource)) continue;
            const meta = metas[metas.length - 1];
            out.push({ userId: uid, ids, name: meta?.name ?? null, color: cursorColor(uid) });
        }
        return out;
    }, [selMap, presence, resource]);

    // Track our latest selection even while inactive, so the heartbeat can
    // surface it the moment a teammate arrives.
    const lastSelIds = React.useRef<string[]>([]);
    const lastSelKey = React.useRef("");

    const pushSelect = React.useCallback(
        (ids: string[]) => {
            const key = ids.join("\n");
            if (key === lastSelKey.current) return;
            lastSelKey.current = key;
            lastSelIds.current = ids;
            if (!activeRef.current || !resourceRef.current) return;
            rawPush("live:select", { resource: resourceRef.current, ids });
        },
        [rawPush],
    );

    React.useEffect(() => {
        const t = setInterval(() => {
            if (!activeRef.current || !resourceRef.current || !lastSelIds.current.length) return;
            rawPushRef.current("live:select", { resource: resourceRef.current, ids: lastSelIds.current });
        }, SELECT_HEARTBEAT_MS);
        return () => clearInterval(t);
    }, []);

    // Clear our selection for teammates when the canvas unmounts. Presence
    // alone is not enough here: on surfaces that claim presence above the
    // canvas (the campaign layout), leaving the canvas tab keeps the member on
    // the resource, so without this the outline would linger.
    React.useEffect(
        () => () => {
            if (lastSelIds.current.length && activeRef.current && resourceRef.current) {
                rawPushRef.current("live:select", { resource: resourceRef.current, ids: [] });
            }
        },
        [],
    );

    // Node-drag throttle, keyed by node id so two nodes dragged in quick
    // succession don't drop each other's trailing frame.
    const nodeLastSent = React.useRef<Map<string, number>>(new Map());
    const nodeTimers = React.useRef<Map<string, number>>(new Map());

    const pushNode = React.useCallback(
        (id: string, x: number, y: number, dragging: boolean) => {
            if (!activeRef.current || !resourceRef.current) return;
            const send = () => {
                nodeLastSent.current.set(id, performance.now());
                rawPush("live:node", { resource: resourceRef.current, id, x, y, dragging });
            };
            // The drag's final frame must land exactly, so skip the throttle.
            if (!dragging) {
                const t = nodeTimers.current.get(id);
                if (t != null) {
                    clearTimeout(t);
                    nodeTimers.current.delete(id);
                }
                send();
                return;
            }
            const elapsed = performance.now() - (nodeLastSent.current.get(id) ?? 0);
            if (elapsed >= NODE_INTERVAL_MS) {
                const t = nodeTimers.current.get(id);
                if (t != null) {
                    clearTimeout(t);
                    nodeTimers.current.delete(id);
                }
                send();
            } else if (!nodeTimers.current.has(id)) {
                const handle = window.setTimeout(() => {
                    nodeTimers.current.delete(id);
                    send();
                }, NODE_INTERVAL_MS - elapsed);
                nodeTimers.current.set(id, handle);
            }
        },
        [rawPush],
    );

    React.useEffect(() => {
        const nodeTimerMap = nodeTimers.current;
        return () => {
            for (const t of nodeTimerMap.values()) clearTimeout(t);
            nodeTimerMap.clear();
        };
    }, []);

    return { ...cursors, pushNode, pushSelect, selections };
}
