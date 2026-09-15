import { useRef, useEffect, useState, useCallback, useMemo } from 'react';
import type SocketProviderProps from "@/lib/socket/models/SocketProviderProps";
import getSocket from '@/lib/api/client/app/socket/getSocket';
import type { AppError } from '@/lib/api/client/normalizeError';
import { useAppStore } from '@/stores';
import {
    SocketContext,
    type ChannelMessage,
    type ChannelState,
    type ChannelEventHandler,
} from './context/socket';

// Constants
const HEARTBEAT_INTERVAL = 25000; // 25s — under typical 60s idle proxy timeouts
const HEARTBEAT_TIMEOUT = 8000; // drop + reconnect if a heartbeat isn't answered in 8s

// Reconnect backoff schedule (ms), modeled on the Phoenix JS client and
// Socket.io: retry almost immediately first, then ramp. A slow 1s→2s→4s ramp is
// what made reconnects "take a long time"; the first retry here is ~120ms so a
// blip is invisible. Indexed by attempt; clamps at the last entry. Each delay
// gets ±25% jitter so many clients don't reconnect in lockstep after an outage.
const RECONNECT_SCHEDULE = [120, 350, 800, 1500, 3000, 5000, 10000];
// Backoff for rejoining ONE channel while the socket stays up: a channel that
// crashed server-side, or a join the server refused for a transient reason.
// Indexed by consecutive attempt; clamps at the last entry. Without this a
// crash-looping channel was rejoined every second forever.
const CHANNEL_REJOIN_SCHEDULE = [1000, 2000, 5000, 10000, 20000, 30000];
const CHANNEL_REJOIN_MAX_ATTEMPTS = 8;
// A topic that has not needed a rejoin for this long starts its backoff over.
// Resetting on a successful join instead would make the backoff useless against
// the case it exists for — a channel that joins, crashes, joins, crashes —
// because every crash is preceded by a success.
const CHANNEL_REJOIN_RESET_MS = 30000;
// Bounds for a server-supplied retry_after_ms, in case it is missing or absurd.
const JOIN_RETRY_MIN_MS = 1000;
const JOIN_RETRY_MAX_MS = 60000;
const PHOENIX_EVENTS = {
    JOIN: 'phx_join',
    LEAVE: 'phx_leave',
    REPLY: 'phx_reply',
    ERROR: 'phx_error',
    CLOSE: 'phx_close',
    HEARTBEAT: 'heartbeat',
};

// Channel internal state
interface ChannelInternal {
    topic: string;
    state: ChannelState;
    joinRef: string;
    params: Record<string, unknown>;
    handlers: Map<string, Set<ChannelEventHandler>>;
}

export default function SocketProvider({
    children,
    onOpen,
    onClose,
    onError,
}: SocketProviderProps) {
    // Connection state
    const [isConnected, setIsConnected] = useState(false);
    const [reconnectAttempt, setReconnectAttempt] = useState(0);
    const reconnectAttemptRef = useRef(0);

    // Refs
    const wsRef = useRef<WebSocket | null>(null);
    const reconnectTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
    const heartbeatTimerRef = useRef<ReturnType<typeof setInterval> | null>(null);
    const refCounterRef = useRef(0);
    const channelsRef = useRef<Map<string, ChannelInternal>>(new Map());
    // Reactive mirror of channelsRef's per-channel state. The map itself is a
    // ref, so a component rendering a channel's state never re-rendered when
    // that channel actually joined and sat on its first-render value forever
    // ("Disconnected" on a live campaign, issue #189). Every transition below
    // goes through markChannelState so the two cannot drift.
    const [channelStates, setChannelStates] = useState<Record<string, ChannelState>>({});
    const markChannelState = useCallback((topic: string, state: ChannelState | null) => {
        setChannelStates((prev) => {
            if (state === null) {
                if (!(topic in prev)) return prev;
                const next = { ...prev };
                delete next[topic];
                return next;
            }
            if (prev[topic] === state) return prev;
            return { ...prev, [topic]: state };
        });
    }, []);
    const pendingJoinsRef = useRef<Map<string, Record<string, unknown>>>(new Map());
    // Pending single-channel rejoin, plus how many consecutive attempts it has
    // taken and when the last one was scheduled. A successful join cancels the
    // timer; the attempt count decays on its own (CHANNEL_REJOIN_RESET_MS).
    const rejoinRef = useRef<
        Map<string, { timer: ReturnType<typeof setTimeout> | null; attempts: number; lastAttemptAt: number }>
    >(new Map());
    // Topics the app currently wants joined (added by joinChannel, removed by
    // leaveChannel). On reconnect we rejoin exactly these, independent of the
    // per-channel live state — the close handler downgrades joined channels to
    // 'closed', so a state-filtered rejoin skipped them all and the socket came
    // back with zero subscriptions (no events, no presence) until a reload.
    const desiredTopicsRef = useRef<Map<string, Record<string, unknown>>>(new Map());
    // Holders per topic, so the first of two surfaces to unmount doesn't take
    // the channel out from under the second. Kept outside the channel entry
    // because that entry is rebuilt on every join and rejoin.
    const holdersRef = useRef<Map<string, number>>(new Map());
    // Distinguishes a close we caused (logout / unmount — don't reconnect) from
    // every other close (server idle-close, channel crash, network drop — do
    // reconnect). The old code only reconnected on `!wasClean`, so a clean
    // server-initiated close stranded the client.
    const intentionalCloseRef = useRef(false);
    // Zombie detection: if a heartbeat goes unanswered this fires and force-
    // closes the socket so onclose schedules a reconnect.
    const heartbeatTimeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null);

    // Legacy handlers for backwards compatibility
    const legacyHandlersRef = useRef<Map<string, Set<(msg: unknown) => void>>>(new Map());

    useEffect(() => {
        reconnectAttemptRef.current = reconnectAttempt;
    }, [reconnectAttempt]);

    // Generate unique ref for messages
    const getRef = useCallback(() => {
        refCounterRef.current += 1;
        return String(refCounterRef.current);
    }, []);

    // Send raw message
    const sendRaw = useCallback((msg: ChannelMessage) => {
        if (wsRef.current?.readyState !== WebSocket.OPEN) {
            console.warn('[WS] Not connected - queuing message');
            return false;
        }
        wsRef.current.send(JSON.stringify(msg));
        return true;
    }, []);

    // Drop any pending rejoin for a topic and reset its backoff.
    const clearRejoin = useCallback((topic: string) => {
        const pending = rejoinRef.current.get(topic);
        if (pending?.timer) clearTimeout(pending.timer);
        rejoinRef.current.delete(topic);
    }, []);

    // Drop a pending rejoin but keep the backoff position, so a join that
    // succeeds and immediately crashes again does not start from zero.
    const cancelRejoinTimer = useCallback((topic: string) => {
        const pending = rejoinRef.current.get(topic);
        if (pending?.timer) {
            clearTimeout(pending.timer);
            rejoinRef.current.set(topic, { ...pending, timer: null });
        }
    }, []);

    const clearAllRejoins = useCallback(() => {
        rejoinRef.current.forEach((pending) => {
            if (pending.timer) clearTimeout(pending.timer);
        });
        rejoinRef.current.clear();
    }, []);

    // Rejoin ONE topic later, on a backoff, while the socket stays open. Used
    // for a channel that crashed server-side and for a join the server refused
    // transiently (a throttled join carries its own retry_after_ms). Gives up
    // after CHANNEL_REJOIN_MAX_ATTEMPTS so a permanently unhappy channel cannot
    // become a slow join loop of its own.
    const scheduleRejoin = useCallback((topic: string, delayMs?: number) => {
        if (!desiredTopicsRef.current.has(topic)) return;

        const pending = rejoinRef.current.get(topic);
        if (pending?.timer) return;

        const now = Date.now();
        const attempts =
            pending && now - pending.lastAttemptAt < CHANNEL_REJOIN_RESET_MS ? pending.attempts : 0;
        if (attempts >= CHANNEL_REJOIN_MAX_ATTEMPTS) return;

        const base =
            delayMs ??
            CHANNEL_REJOIN_SCHEDULE[Math.min(attempts, CHANNEL_REJOIN_SCHEDULE.length - 1)];
        // Jitter upward only: retrying early just spends another refusal.
        const delay = Math.round(base * (1 + Math.random() * 0.25));

        const timer = setTimeout(() => {
            const current = rejoinRef.current.get(topic);
            if (current) rejoinRef.current.set(topic, { ...current, timer: null });

            if (!desiredTopicsRef.current.has(topic)) return;
            if (wsRef.current?.readyState !== WebSocket.OPEN) return;

            const existing = channelsRef.current.get(topic);
            if (existing && (existing.state === 'joined' || existing.state === 'joining')) return;

            const params = desiredTopicsRef.current.get(topic) || {};
            const joinRef = getRef();
            channelsRef.current.set(topic, {
                topic,
                state: 'joining',
                joinRef,
                params,
                handlers: existing?.handlers || new Map(),
            });
            markChannelState(topic, 'joining');
            sendRaw({
                topic,
                event: PHOENIX_EVENTS.JOIN,
                payload: params,
                ref: joinRef,
                join_ref: joinRef,
            });
        }, delay);

        rejoinRef.current.set(topic, { timer, attempts: attempts + 1, lastAttemptAt: now });
    }, [getRef, sendRaw, markChannelState]);

    // Heartbeat roundtrip tracking. We record send time keyed by ref,
    // and when phx_reply with that ref lands we compute the delta and
    // publish it to the store. LivePanel surfaces it.
    const pendingPingRef = useRef<Map<string, number>>(new Map());
    const setWsLatencyMs = useAppStore((s) => s.setWsLatencyMs);

    // Send heartbeat
    const sendHeartbeat = useCallback(() => {
        const ref = getRef();
        pendingPingRef.current.set(ref, performance.now());
        const ok = sendRaw({
            topic: 'phoenix',
            event: PHOENIX_EVENTS.HEARTBEAT,
            payload: {},
            ref,
        });
        if (!ok) return;
        // Arm a watchdog: a healthy connection answers within ~1s. If the
        // socket has silently died (network dropped with no close frame), no
        // reply lands and we close it ourselves to trigger a reconnect.
        if (heartbeatTimeoutRef.current) clearTimeout(heartbeatTimeoutRef.current);
        heartbeatTimeoutRef.current = setTimeout(() => {
            try {
                wsRef.current?.close();
            } catch {
                /* ignore */
            }
        }, HEARTBEAT_TIMEOUT);
    }, [sendRaw, getRef]);

    // Start heartbeat
    const startHeartbeat = useCallback(() => {
        if (heartbeatTimerRef.current) {
            clearInterval(heartbeatTimerRef.current);
        }
        heartbeatTimerRef.current = setInterval(sendHeartbeat, HEARTBEAT_INTERVAL);
    }, [sendHeartbeat]);

    // Stop heartbeat
    const stopHeartbeat = useCallback(() => {
        if (heartbeatTimerRef.current) {
            clearInterval(heartbeatTimerRef.current);
            heartbeatTimerRef.current = null;
        }
    }, []);

    // Handle incoming message
    const handleMessage = useCallback((data: string) => {
        let msg: ChannelMessage;
        try {
            msg = JSON.parse(data) as ChannelMessage;
        } catch {
            console.warn('[WS] Failed to parse message:', data);
            return;
        }

        const { topic, event, payload, ref } = msg;

        // Heartbeat reply → compute roundtrip and publish latency.
        if (event === PHOENIX_EVENTS.REPLY && topic === 'phoenix' && ref) {
            // The connection is alive — disarm the zombie watchdog.
            if (heartbeatTimeoutRef.current) {
                clearTimeout(heartbeatTimeoutRef.current);
                heartbeatTimeoutRef.current = null;
            }
            const sentAt = pendingPingRef.current.get(ref);
            if (sentAt != null) {
                const dt = Math.round(performance.now() - sentAt);
                pendingPingRef.current.delete(ref);
                setWsLatencyMs(dt);
            }
            return;
        }

        // Handle Phoenix system events
        if (event === PHOENIX_EVENTS.REPLY) {
            const channel = channelsRef.current.get(topic);
            if (channel && ref === channel.joinRef) {
                const reply = payload as {
                    status?: string;
                    response?: { reason?: string; retry_after_ms?: number };
                };
                if (reply.status === 'ok') {
                    channel.state = 'joined';
                    cancelRejoinTimer(topic);
                } else {
                    channel.state = 'errored';
                    // A throttled join is transient and the server says when
                    // budget is back; retry then. Every other refusal (not a
                    // member, no such campaign) is final, so stay errored rather
                    // than hammering a door that will not open.
                    const response = reply.response ?? {};
                    if (response.reason === 'rate_limited') {
                        const hint = Number(response.retry_after_ms);
                        const delay = Number.isFinite(hint)
                            ? Math.min(Math.max(hint, JOIN_RETRY_MIN_MS), JOIN_RETRY_MAX_MS)
                            : JOIN_RETRY_MIN_MS;
                        scheduleRejoin(topic, delay);
                    } else {
                        clearRejoin(topic);
                    }
                }
                markChannelState(topic, channel.state);
            }
            return;
        }

        if (event === PHOENIX_EVENTS.ERROR) {
            const channel = channelsRef.current.get(topic);
            const wasJoined = channel?.state === 'joined';
            if (channel) {
                channel.state = 'errored';
                markChannelState(topic, 'errored');
            }
            // A channel that was live crashed server-side while the socket stays
            // open (e.g. an unhandled message in the channel process). Phoenix's
            // own JS client auto-rejoins; ours must too, or this topic stays dead
            // — no events, no presence — until a full socket reconnect. Only
            // retry a channel that HAD joined, so a genuinely refused join
            // (returns errored) doesn't spin in a loop.
            if (wasJoined) {
                scheduleRejoin(topic);
            }
            return;
        }

        if (event === PHOENIX_EVENTS.CLOSE) {
            const channel = channelsRef.current.get(topic);
            if (channel) {
                channel.state = 'closed';
                markChannelState(topic, 'closed');
            }
            return;
        }

        // Dispatch to channel handlers
        const channel = channelsRef.current.get(topic);
        if (channel) {
            const handlers = channel.handlers.get(event);
            if (handlers) {
                handlers.forEach((handler) => {
                    try {
                        handler(payload);
                    } catch (err) {
                        console.error('[WS] Handler error:', err);
                    }
                });
            }

            // Also dispatch to wildcard handlers
            const wildcardHandlers = channel.handlers.get('*');
            if (wildcardHandlers) {
                wildcardHandlers.forEach((handler) => {
                    try {
                        handler({ ...payload, _event: event });
                    } catch (err) {
                        console.error('[WS] Wildcard handler error:', err);
                    }
                });
            }
        }

        // Legacy support: dispatch based on event type in payload
        const eventType = (payload as { type?: string }).type ||
                         (payload as { event_type?: string }).event_type ||
                         event;
        const legacySet = legacyHandlersRef.current.get(eventType);
        if (legacySet) {
            legacySet.forEach((handler) => {
                try {
                    handler(payload);
                } catch (err) {
                    console.error('[WS] Legacy handler error:', err);
                }
            });
        }
    }, [setWsLatencyMs, markChannelState, scheduleRejoin, clearRejoin, cancelRejoinTimer]);

    // Join channel
    const joinChannel = useCallback((topic: string, params: Record<string, unknown> = {}) => {
        // Must count before the already-joined bail-out below, or a second
        // holder asking for a live topic is never counted at all.
        holdersRef.current.set(topic, (holdersRef.current.get(topic) ?? 0) + 1);

        // Remember the intent so a reconnect rejoins this topic even after its
        // live state was reset to 'closed' by a drop.
        desiredTopicsRef.current.set(topic, params);

        // Check if already joined or joining
        const existing = channelsRef.current.get(topic);
        if (existing && (existing.state === 'joined' || existing.state === 'joining')) {
            return;
        }

        const joinRef = getRef();
        const channel: ChannelInternal = {
            topic,
            state: 'joining',
            joinRef,
            params,
            handlers: existing?.handlers || new Map(),
        };
        channelsRef.current.set(topic, channel);
        markChannelState(topic, 'joining');

        // If connected, send join immediately
        if (wsRef.current?.readyState === WebSocket.OPEN) {
            sendRaw({
                topic,
                event: PHOENIX_EVENTS.JOIN,
                payload: params,
                ref: joinRef,
                join_ref: joinRef,
            });
        } else {
            // Queue for when connected
            pendingJoinsRef.current.set(topic, params);
        }
    }, [getRef, sendRaw, markChannelState]);

    // Leave channel
    const leaveChannel = useCallback((topic: string) => {
        // One holder fewer. Anyone still holding the topic keeps it joined.
        const remaining = (holdersRef.current.get(topic) ?? 0) - 1;
        if (remaining > 0) {
            holdersRef.current.set(topic, remaining);
            return;
        }
        holdersRef.current.delete(topic);

        // No longer want this topic — don't let a reconnect rejoin it.
        desiredTopicsRef.current.delete(topic);
        clearRejoin(topic);
        pendingJoinsRef.current.delete(topic);

        const channel = channelsRef.current.get(topic);
        if (!channel) return;

        if (
            wsRef.current?.readyState === WebSocket.OPEN &&
            (channel.state === 'joined' || channel.state === 'joining')
        ) {
            sendRaw({
                topic,
                event: PHOENIX_EVENTS.LEAVE,
                payload: {},
                ref: getRef(),
                join_ref: channel.joinRef,
            });
        }

        // The entry owns the handler map, so dropping it here took every other
        // subscriber's registrations with it. Reset in place while any remain.
        if (channel.handlers.size === 0) {
            channelsRef.current.delete(topic);
        } else {
            channel.state = 'closed';
            channel.joinRef = '';
        }
        markChannelState(topic, null);
    }, [getRef, sendRaw, markChannelState, clearRejoin]);

    // Get channel state
    const getChannelState = useCallback((topic: string): ChannelState => {
        return channelsRef.current.get(topic)?.state || 'closed';
    }, []);

    // Subscribe to channel event
    const subscribeToChannel = useCallback((
        topic: string,
        event: string,
        handler: ChannelEventHandler
    ): (() => void) => {
        let channel = channelsRef.current.get(topic);
        if (!channel) {
            // Create channel entry for handlers (will join when requested)
            channel = {
                topic,
                state: 'closed',
                joinRef: '',
                params: {},
                handlers: new Map(),
            };
            channelsRef.current.set(topic, channel);
        }

        let handlers = channel.handlers.get(event);
        if (!handlers) {
            handlers = new Set();
            channel.handlers.set(event, handlers);
        }
        handlers.add(handler);

        return () => {
            handlers?.delete(handler);
            if (handlers?.size !== 0) return;
            channel?.handlers.delete(event);
            // Nothing listening and nobody holding: drop the entry leaveChannel
            // kept for us. Read the live one; a rejoin rebuilds it around the
            // same handlers map.
            const current = channelsRef.current.get(topic);
            if (
                current &&
                current.handlers.size === 0 &&
                !holdersRef.current.has(topic) &&
                !desiredTopicsRef.current.has(topic)
            ) {
                channelsRef.current.delete(topic);
            }
        };
    }, []);

    // Push to channel
    const pushToChannel = useCallback((
        topic: string,
        event: string,
        payload: Record<string, unknown>
    ) => {
        const channel = channelsRef.current.get(topic);
        if (!channel || channel.state !== 'joined') {
            console.warn('[WS] Cannot push to channel - not joined:', topic);
            return;
        }

        sendRaw({
            topic,
            event,
            payload,
            ref: getRef(),
            join_ref: channel.joinRef,
        });
    }, [sendRaw, getRef]);

    // Legacy subscribe (for backwards compatibility)
    const subscribe = useCallback(<T extends { type: string }>(
        type: T['type'],
        handler: (msg: T) => void
    ): (() => void) => {
        let set = legacyHandlersRef.current.get(type);
        if (!set) {
            set = new Set();
            legacyHandlersRef.current.set(type, set);
        }
        set.add(handler as (msg: unknown) => void);

        return () => {
            set?.delete(handler as (msg: unknown) => void);
            if (set?.size === 0) {
                legacyHandlersRef.current.delete(type);
            }
        };
    }, []);

    // Send message (legacy)
    const sendMessage = useCallback((msg: unknown) => {
        if (wsRef.current?.readyState !== WebSocket.OPEN) {
            console.warn('[WS] Not open - dropping message');
            return;
        }
        const raw = typeof msg === 'string' ? msg : JSON.stringify(msg);
        wsRef.current.send(raw);
    }, []);

    // Rejoin all channels after reconnect. We rejoin every topic the app wants
    // joined (desiredTopicsRef), NOT just channels still flagged 'joined' — the
    // close handler downgrades those to 'closed', so the old state filter
    // skipped them all and the socket reconnected with no subscriptions.
    // Handlers are preserved across the rejoin so existing subscribers keep
    // receiving events without re-subscribing.
    const rejoinChannels = useCallback(() => {
        desiredTopicsRef.current.forEach((params, topic) => {
            const existing = channelsRef.current.get(topic);
            const joinRef = getRef();
            const channel: ChannelInternal = {
                topic,
                state: 'joining',
                joinRef,
                params,
                handlers: existing?.handlers || new Map(),
            };
            channelsRef.current.set(topic, channel);
            markChannelState(topic, 'joining');
            sendRaw({
                topic,
                event: PHOENIX_EVENTS.JOIN,
                payload: params,
                ref: joinRef,
                join_ref: joinRef,
            });
        });
        pendingJoinsRef.current.clear();
    }, [getRef, sendRaw, markChannelState]);

    // Connect to WebSocket
    const connect = useCallback(async () => {
        // Already connected or mid-handshake — don't open a second socket.
        if (
            wsRef.current &&
            (wsRef.current.readyState === WebSocket.OPEN ||
                wsRef.current.readyState === WebSocket.CONNECTING)
        ) {
            return;
        }
        // A manual connect (network back, tab focus) supersedes any pending
        // backoff timer.
        if (reconnectTimerRef.current) {
            clearTimeout(reconnectTimerRef.current);
            reconnectTimerRef.current = null;
        }

        try {
            const urlData = await getSocket();
            // Phoenix vsn=1.0.0 — our sendRaw / joinChannel paths emit the
            // V1 object format ({topic, event, payload, ref}), not the
            // V2 array format. Sending vsn=2.0.0 made the realtime server
            // try to decode each message via Phoenix.Socket.V2.JSONSerializer,
            // which crashes on object payloads (badmatch). The result was
            // the WS would open and immediately die on the first phx_join.
            const url = new URL(urlData.url);
            url.searchParams.set('vsn', '1.0.0');

            wsRef.current = new WebSocket(url.toString());

            wsRef.current.onopen = (ev) => {
                setIsConnected(true);
                setReconnectAttempt(0);
                startHeartbeat();
                rejoinChannels();
                onOpen?.(ev);
            };

            wsRef.current.onmessage = (ev) => {
                handleMessage(ev.data);
            };

            wsRef.current.onclose = (ev) => {
                setIsConnected(false);
                stopHeartbeat();
                if (heartbeatTimeoutRef.current) {
                    clearTimeout(heartbeatTimeoutRef.current);
                    heartbeatTimeoutRef.current = null;
                }
                // Latency only means anything while connected.
                setWsLatencyMs(null);
                pendingPingRef.current.clear();
                onClose?.(ev);

                // A pending per-channel rejoin is now redundant and would
                // double-join: rejoinChannels covers every desired topic when
                // the socket comes back.
                clearAllRejoins();

                // Mark all channels as closed
                channelsRef.current.forEach((channel) => {
                    if (channel.state === 'joined') {
                        channel.state = 'closed';
                        markChannelState(channel.topic, 'closed');
                    }
                });

                // Reconnect with exponential backoff for EVERY close we didn't
                // initiate — clean or not. A graceful server close (idle, channel
                // crash, deploy) is exactly when we most need to come back.
                if (!intentionalCloseRef.current) {
                    const attempt = reconnectAttemptRef.current;
                    const base = RECONNECT_SCHEDULE[Math.min(attempt, RECONNECT_SCHEDULE.length - 1)];
                    // ±25% jitter so clients don't reconnect in lockstep after an outage.
                    const delay = Math.round(base * (0.75 + Math.random() * 0.5));
                    reconnectTimerRef.current = setTimeout(() => {
                        setReconnectAttempt((a) => a + 1);
                        connect();
                    }, delay);
                }
            };

            wsRef.current.onerror = (ev) => {
                // A WebSocket error event carries no detail by spec, and onclose
                // always follows it and drives the reconnect, so this is not an
                // error: as one it reported every deploy and sleep to PostHog.
                console.warn('[WS] Connection error - reconnecting', {
                    readyState: wsRef.current?.readyState,
                    attempt: reconnectAttemptRef.current,
                });
                onError?.(ev);
            };
        } catch (err) {
            const error = err as AppError;
            // AppError is a plain object, not an Error, so console.error rendered
            // it as [object Object]: every report carried no message, no status
            // and no request id, and they all grouped into one bucket.
            const detail = [error.error, error.message, error.status, error.code, error.request_id]
                .filter(Boolean)
                .join(' | ');
            // No status is offline or a timeout, and a 401 here means the session
            // is already gone (Request refreshes and retries once before it
            // throws). Both are expected and handled: the retry below, and the
            // app-wide auth redirect. Only an unexpected answer is an error.
            if (!error.status || error.status === 401) {
                console.warn('[WS] Init failed, retrying -', detail);
            } else {
                console.error('[WS] Init failed -', detail);
            }
            // Token fetch / handshake failed — retry on the same fast backoff
            // rather than a flat 15s wait.
            if (!intentionalCloseRef.current) {
                const attempt = reconnectAttemptRef.current;
                const base = RECONNECT_SCHEDULE[Math.min(attempt, RECONNECT_SCHEDULE.length - 1)];
                const delay = Math.round(base * (0.75 + Math.random() * 0.5));
                reconnectTimerRef.current = setTimeout(() => {
                    setReconnectAttempt((a) => a + 1);
                    connect();
                }, delay);
            }
        }
    }, [
        onOpen,
        onClose,
        onError,
        handleMessage,
        startHeartbeat,
        stopHeartbeat,
        rejoinChannels,
        setWsLatencyMs,
        markChannelState,
        clearAllRejoins,
    ]);

    // Mount effect
    useEffect(() => {
        intentionalCloseRef.current = false;
        connect();

        // Proactively reconnect when the network returns or the tab is
        // refocused — don't wait out a backoff timer if we're already idle.
        const wake = () => {
            if (wsRef.current?.readyState === WebSocket.OPEN) return;
            // Safari (and other browsers) suspend background tabs: the socket can
            // be stuck CONNECTING with no close event ever firing. connect() skips
            // a CONNECTING socket, so clear the zombie first (detach its handlers
            // so its eventual close doesn't trigger our reconnect path) and open
            // a fresh one immediately.
            const stale = wsRef.current;
            if (stale && stale.readyState !== WebSocket.CLOSED) {
                stale.onclose = null;
                stale.onerror = null;
                try {
                    stale.close();
                } catch {
                    /* ignore */
                }
            }
            wsRef.current = null;
            if (reconnectTimerRef.current) {
                clearTimeout(reconnectTimerRef.current);
                reconnectTimerRef.current = null;
            }
            // Force the disconnected→connected transition. A zombie socket (a
            // Safari background tab) dies with no close event, so `isConnected`
            // was never flipped to false. Without this, the new socket's onopen
            // sets it to `true` again — a no-op — and every isConnected-gated
            // effect (channel rejoin, catch-up invalidation, presence
            // re-subscribe and re-push) never re-runs. The passive tab would
            // then silently stop receiving events and stale data until a full
            // reload. Flipping to false guarantees those effects fire on reopen.
            setIsConnected(false);
            reconnectAttemptRef.current = 0;
            setReconnectAttempt(0);
            connect();
        };
        const onVisible = () => {
            if (document.visibilityState === 'visible') wake();
        };
        window.addEventListener('online', wake);
        document.addEventListener('visibilitychange', onVisible);

        return () => {
            // We're tearing down on purpose — suppress the reconnect.
            intentionalCloseRef.current = true;
            window.removeEventListener('online', wake);
            document.removeEventListener('visibilitychange', onVisible);
            if (reconnectTimerRef.current) {
                clearTimeout(reconnectTimerRef.current);
            }
            if (heartbeatTimeoutRef.current) {
                clearTimeout(heartbeatTimeoutRef.current);
            }
            clearAllRejoins();
            stopHeartbeat();
            wsRef.current?.close();
        };
    }, [connect, stopHeartbeat, clearAllRejoins]);

    // Memoised: an inline object is a new identity on every render, and
    // channelStates re-renders this provider on every join. useChannel's effect
    // depends on the context value, so the two together tore the channel down
    // and rejoined it on every render (~400 joins/second on one open campaign
    // page, enough to saturate the realtime service).
    const value = useMemo(
        () => ({
            isConnected,
            reconnectAttempt,
            joinChannel,
            leaveChannel,
            getChannelState,
            channelStates,
            subscribeToChannel,
            pushToChannel,
            subscribe,
            sendMessage,
        }),
        [
            isConnected,
            reconnectAttempt,
            joinChannel,
            leaveChannel,
            getChannelState,
            channelStates,
            subscribeToChannel,
            pushToChannel,
            subscribe,
            sendMessage,
        ]
    );

    return (
        <SocketContext.Provider value={value}>
            {children}
        </SocketContext.Provider>
    );
}
