// Self-contained Phoenix-protocol websocket client for the admin realtime
// layer. Module-level singleton (no React context): RealtimeManager calls
// startRealtime/stopRealtime, everything else consumes onEvent/onStatusChange.
//
// Protocol handling is ported from web/src/hooks/SocketProvider.tsx,
// simplified for a single channel (admin:platform): Phoenix v1 message
// format ({topic, event, payload, ref, join_ref}, vsn=1.0.0), 25s heartbeat
// with an 8s zombie watchdog, and the same fast reconnect backoff schedule.
//
// The socket URL comes from POST /getaway and embeds a short-lived auth
// token (~10 min), so EVERY (re)connect fetches a fresh URL.

import { Request } from "@/lib/api/client";

export type RealtimeStatus = "connecting" | "connected" | "disconnected";

type EventCallback = (name: string, payload: Record<string, unknown>) => void;
type StatusCallback = (status: RealtimeStatus) => void;

interface PhoenixMessage {
    topic: string;
    event: string;
    payload: Record<string, unknown>;
    ref?: string | null;
    join_ref?: string | null;
}

const TOPIC = "admin:platform";
const HEARTBEAT_INTERVAL = 25_000; // under typical 60s idle proxy timeouts
const HEARTBEAT_TIMEOUT = 8_000; // drop + reconnect if a heartbeat isn't answered
const REJOIN_DELAY = 1_000; // channel crashed but socket lives: rejoin after a beat
// Retry almost immediately first, then ramp; each delay gets ±25% jitter.
const RECONNECT_SCHEDULE = [120, 350, 800, 1500, 3000, 5000, 10000];

const PHX = {
    JOIN: "phx_join",
    LEAVE: "phx_leave",
    REPLY: "phx_reply",
    ERROR: "phx_error",
    CLOSE: "phx_close",
    HEARTBEAT: "heartbeat",
} as const;

// Phoenix internals never reach the wildcard dispatch.
const INTERNAL_EVENTS = new Set<string>([
    PHX.REPLY,
    PHX.ERROR,
    PHX.CLOSE,
    "presence_state",
    "presence_diff",
]);

let ws: WebSocket | null = null;
let started = false;
// Bumped on every start/stop so an in-flight async connect from a previous
// lifecycle can detect it is stale and bail instead of opening a zombie.
let epoch = 0;
let status: RealtimeStatus = "disconnected";
let refCounter = 0;
let joinRef = "";
let reconnectAttempt = 0;
let connecting = false; // guards the async URL fetch window before ws exists
let reconnectTimer: ReturnType<typeof setTimeout> | null = null;
let rejoinTimer: ReturnType<typeof setTimeout> | null = null;
let heartbeatTimer: ReturnType<typeof setInterval> | null = null;
let heartbeatWatchdog: ReturnType<typeof setTimeout> | null = null;

const eventCallbacks = new Set<EventCallback>();
const statusCallbacks = new Set<StatusCallback>();

function setStatus(next: RealtimeStatus) {
    if (status === next) return;
    status = next;
    statusCallbacks.forEach((cb) => {
        try {
            cb(next);
        } catch (err) {
            console.error("[realtime] Status callback error:", err);
        }
    });
}

function nextRef(): string {
    refCounter += 1;
    return String(refCounter);
}

async function fetchSocketUrl(): Promise<string> {
    const res = await Request<{ url: string; expires_in: number }>({
        method: "POST",
        url: "/getaway",
        authorization: true,
    });
    // vsn=1.0.0: we speak the V1 object format, not the V2 array format.
    const url = new URL(res.url);
    url.searchParams.set("vsn", "1.0.0");
    return url.toString();
}

function send(msg: PhoenixMessage): boolean {
    if (ws?.readyState !== WebSocket.OPEN) return false;
    ws.send(JSON.stringify(msg));
    return true;
}

function joinChannel() {
    joinRef = nextRef();
    send({
        topic: TOPIC,
        event: PHX.JOIN,
        payload: {},
        ref: joinRef,
        join_ref: joinRef,
    });
}

function clearHeartbeat() {
    if (heartbeatTimer) {
        clearInterval(heartbeatTimer);
        heartbeatTimer = null;
    }
    if (heartbeatWatchdog) {
        clearTimeout(heartbeatWatchdog);
        heartbeatWatchdog = null;
    }
}

function sendHeartbeat() {
    const ok = send({ topic: "phoenix", event: PHX.HEARTBEAT, payload: {}, ref: nextRef() });
    if (!ok) return;
    // Zombie watchdog: a silently dead socket never answers, so close it
    // ourselves and let onclose schedule the reconnect.
    if (heartbeatWatchdog) clearTimeout(heartbeatWatchdog);
    heartbeatWatchdog = setTimeout(() => {
        try {
            ws?.close();
        } catch {
            /* ignore */
        }
    }, HEARTBEAT_TIMEOUT);
}

function startHeartbeat() {
    clearHeartbeat();
    heartbeatTimer = setInterval(sendHeartbeat, HEARTBEAT_INTERVAL);
}

function scheduleReconnect() {
    if (!started || reconnectTimer) return;
    const base = RECONNECT_SCHEDULE[Math.min(reconnectAttempt, RECONNECT_SCHEDULE.length - 1)];
    // ±25% jitter so clients don't reconnect in lockstep after an outage.
    const delay = Math.round(base * (0.75 + Math.random() * 0.5));
    reconnectTimer = setTimeout(() => {
        reconnectTimer = null;
        reconnectAttempt += 1;
        void connect();
    }, delay);
}

function scheduleRejoin() {
    if (!started || rejoinTimer) return;
    rejoinTimer = setTimeout(() => {
        rejoinTimer = null;
        if (ws?.readyState === WebSocket.OPEN) joinChannel();
        else scheduleReconnect();
    }, REJOIN_DELAY);
}

function handleMessage(raw: string) {
    let msg: PhoenixMessage;
    try {
        msg = JSON.parse(raw) as PhoenixMessage;
    } catch {
        console.warn("[realtime] Failed to parse message:", raw);
        return;
    }
    const { topic, event, payload } = msg;

    if (event === PHX.REPLY) {
        if (topic === "phoenix") {
            // Heartbeat answered: the connection is alive, disarm the watchdog.
            if (heartbeatWatchdog) {
                clearTimeout(heartbeatWatchdog);
                heartbeatWatchdog = null;
            }
        } else if (topic === TOPIC && msg.ref === joinRef) {
            const st = (payload as { status?: string }).status;
            if (st !== "ok") {
                // Refused join (likely an expired token URL): reconnect with a
                // freshly minted URL instead of spinning on the dead socket.
                try {
                    ws?.close();
                } catch {
                    /* ignore */
                }
            }
        }
        return;
    }

    // A channel-level crash or close while the socket stays open: rejoin.
    if ((event === PHX.ERROR || event === PHX.CLOSE) && topic === TOPIC) {
        scheduleRejoin();
        return;
    }

    if (INTERNAL_EVENTS.has(event)) return;
    if (topic !== TOPIC) return;

    eventCallbacks.forEach((cb) => {
        try {
            cb(event, payload ?? {});
        } catch (err) {
            console.error("[realtime] Event callback error:", err);
        }
    });
}

function teardownSocket() {
    clearHeartbeat();
    if (rejoinTimer) {
        clearTimeout(rejoinTimer);
        rejoinTimer = null;
    }
    const stale = ws;
    ws = null;
    if (stale) {
        // Detach handlers so the eventual close doesn't trigger a reconnect.
        stale.onopen = null;
        stale.onmessage = null;
        stale.onclose = null;
        stale.onerror = null;
        try {
            stale.close();
        } catch {
            /* ignore */
        }
    }
}

async function connect(): Promise<void> {
    if (!started || connecting) return;
    if (
        ws &&
        (ws.readyState === WebSocket.OPEN || ws.readyState === WebSocket.CONNECTING)
    ) {
        return;
    }
    if (reconnectTimer) {
        clearTimeout(reconnectTimer);
        reconnectTimer = null;
    }

    const myEpoch = epoch;
    connecting = true;
    setStatus("connecting");

    let url: string;
    try {
        // The /getaway URL expires in ~10 min, so every attempt fetches fresh.
        url = await fetchSocketUrl();
    } catch (err) {
        connecting = false;
        if (!started || myEpoch !== epoch) return;
        console.error("[realtime] Socket URL fetch failed:", err);
        setStatus("disconnected");
        scheduleReconnect();
        return;
    }
    connecting = false;
    // Stopped (or restarted) while the URL fetch was in flight: bail.
    if (!started || myEpoch !== epoch) return;

    const socket = new WebSocket(url);
    ws = socket;

    socket.onopen = () => {
        if (socket !== ws) return;
        reconnectAttempt = 0;
        setStatus("connected");
        startHeartbeat();
        joinChannel();
    };

    socket.onmessage = (ev) => {
        if (socket !== ws) return;
        handleMessage(String(ev.data));
    };

    socket.onclose = () => {
        if (socket !== ws) return;
        ws = null;
        clearHeartbeat();
        if (rejoinTimer) {
            clearTimeout(rejoinTimer);
            rejoinTimer = null;
        }
        setStatus("disconnected");
        // Reconnect on EVERY close we didn't initiate, clean or not: a
        // graceful server close is exactly when we most need to come back.
        scheduleReconnect();
    };

    socket.onerror = () => {
        // The close event always follows an error; reconnect happens there.
        console.warn("[realtime] Socket error");
    };
}

// Reconnect promptly when the network returns or the tab is refocused
// instead of waiting out a backoff timer. Also clears a socket stuck
// CONNECTING (suspended background tab) so a fresh one can open.
function wake() {
    if (!started) return;
    if (ws?.readyState === WebSocket.OPEN) return;
    teardownSocket();
    if (reconnectTimer) {
        clearTimeout(reconnectTimer);
        reconnectTimer = null;
    }
    reconnectAttempt = 0;
    void connect();
}

export function startRealtime() {
    if (started) return;
    started = true;
    epoch += 1;
    reconnectAttempt = 0;
    window.addEventListener("online", wake);
    window.addEventListener("focus", wake);
    void connect();
}

export function stopRealtime() {
    if (!started) return;
    started = false;
    epoch += 1;
    window.removeEventListener("online", wake);
    window.removeEventListener("focus", wake);
    if (reconnectTimer) {
        clearTimeout(reconnectTimer);
        reconnectTimer = null;
    }
    teardownSocket();
    setStatus("disconnected");
}

/** Wildcard subscription to every platform event on the channel (Phoenix
 * internals and join replies excluded). Returns an unsubscribe function. */
export function onEvent(cb: EventCallback): () => void {
    eventCallbacks.add(cb);
    return () => {
        eventCallbacks.delete(cb);
    };
}

export function onStatusChange(cb: StatusCallback): () => void {
    statusCallbacks.add(cb);
    return () => {
        statusCallbacks.delete(cb);
    };
}

export function getStatus(): RealtimeStatus {
    return status;
}
