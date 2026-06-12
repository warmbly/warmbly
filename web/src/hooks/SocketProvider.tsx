import { useRef, useEffect, useState, useCallback } from 'react';
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
    const pendingJoinsRef = useRef<Map<string, Record<string, unknown>>>(new Map());
    // Topics the app currently wants joined (added by joinChannel, removed by
    // leaveChannel). On reconnect we rejoin exactly these, independent of the
    // per-channel live state — the close handler downgrades joined channels to
    // 'closed', so a state-filtered rejoin skipped them all and the socket came
    // back with zero subscriptions (no events, no presence) until a reload.
    const desiredTopicsRef = useRef<Map<string, Record<string, unknown>>>(new Map());
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
                const status = (payload as { status?: string }).status;
                if (status === 'ok') {
                    channel.state = 'joined';
                } else {
                    channel.state = 'errored';
                }
            }
            return;
        }

        if (event === PHOENIX_EVENTS.ERROR) {
            const channel = channelsRef.current.get(topic);
            const wasJoined = channel?.state === 'joined';
            if (channel) {
                channel.state = 'errored';
            }
            // A channel that was live crashed server-side while the socket stays
            // open (e.g. an unhandled message in the channel process). Phoenix's
            // own JS client auto-rejoins; ours must too, or this topic stays dead
            // — no events, no presence — until a full socket reconnect. Only
            // retry a channel that HAD joined, so a genuinely refused join
            // (returns errored) doesn't spin in a loop.
            if (wasJoined && desiredTopicsRef.current.has(topic)) {
                setTimeout(() => {
                    if (!desiredTopicsRef.current.has(topic)) return;
                    if (wsRef.current?.readyState !== WebSocket.OPEN) return;
                    const cur = channelsRef.current.get(topic);
                    if (cur && (cur.state === 'joined' || cur.state === 'joining')) return;
                    const params = desiredTopicsRef.current.get(topic) || {};
                    const joinRef = getRef();
                    channelsRef.current.set(topic, {
                        topic,
                        state: 'joining',
                        joinRef,
                        params,
                        handlers: cur?.handlers || new Map(),
                    });
                    sendRaw({
                        topic,
                        event: PHOENIX_EVENTS.JOIN,
                        payload: params,
                        ref: joinRef,
                        join_ref: joinRef,
                    });
                }, 1000);
            }
            return;
        }

        if (event === PHOENIX_EVENTS.CLOSE) {
            const channel = channelsRef.current.get(topic);
            if (channel) {
                channel.state = 'closed';
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
    }, [setWsLatencyMs, getRef, sendRaw]);

    // Join channel
    const joinChannel = useCallback((topic: string, params: Record<string, unknown> = {}) => {
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
    }, [getRef, sendRaw]);

    // Leave channel
    const leaveChannel = useCallback((topic: string) => {
        // No longer want this topic — don't let a reconnect rejoin it.
        desiredTopicsRef.current.delete(topic);

        const channel = channelsRef.current.get(topic);
        if (!channel) return;

        channel.state = 'leaving';

        if (wsRef.current?.readyState === WebSocket.OPEN) {
            sendRaw({
                topic,
                event: PHOENIX_EVENTS.LEAVE,
                payload: {},
                ref: getRef(),
                join_ref: channel.joinRef,
            });
        }

        channelsRef.current.delete(topic);
        pendingJoinsRef.current.delete(topic);
    }, [getRef, sendRaw]);

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
            if (handlers?.size === 0) {
                channel?.handlers.delete(event);
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
            sendRaw({
                topic,
                event: PHOENIX_EVENTS.JOIN,
                payload: params,
                ref: joinRef,
                join_ref: joinRef,
            });
        });
        pendingJoinsRef.current.clear();
    }, [getRef, sendRaw]);

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

                // Mark all channels as closed
                channelsRef.current.forEach((channel) => {
                    if (channel.state === 'joined') {
                        channel.state = 'closed';
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
                console.error('[WS] Error:', ev);
                onError?.(ev);
            };
        } catch (err) {
            const error = err as AppError;
            console.error('[WS] Init failed:', error);
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
            stopHeartbeat();
            wsRef.current?.close();
        };
    }, [connect, stopHeartbeat]);

    return (
        <SocketContext.Provider value={{
            isConnected,
            reconnectAttempt,
            joinChannel,
            leaveChannel,
            getChannelState,
            subscribeToChannel,
            pushToChannel,
            subscribe,
            sendMessage,
        }}>
            {children}
        </SocketContext.Provider>
    );
}
