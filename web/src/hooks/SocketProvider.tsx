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
const HEARTBEAT_INTERVAL = 30000; // 30 seconds
const RECONNECT_MAX_DELAY = 30000; // 30 seconds max
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
        sendRaw({
            topic: 'phoenix',
            event: PHOENIX_EVENTS.HEARTBEAT,
            payload: {},
            ref,
        });
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
            if (channel) {
                channel.state = 'errored';
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
    }, [setWsLatencyMs]);

    // Join channel
    const joinChannel = useCallback((topic: string, params: Record<string, unknown> = {}) => {
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

    // Rejoin all channels after reconnect
    const rejoinChannels = useCallback(() => {
        channelsRef.current.forEach((channel, topic) => {
            if (channel.state === 'joined' || channel.state === 'joining') {
                const joinRef = getRef();
                channel.joinRef = joinRef;
                channel.state = 'joining';
                sendRaw({
                    topic,
                    event: PHOENIX_EVENTS.JOIN,
                    payload: channel.params,
                    ref: joinRef,
                    join_ref: joinRef,
                });
            }
        });

        // Also join any pending
        pendingJoinsRef.current.forEach((params, topic) => {
            const channel = channelsRef.current.get(topic);
            if (channel) {
                const joinRef = getRef();
                channel.joinRef = joinRef;
                channel.state = 'joining';
                sendRaw({
                    topic,
                    event: PHOENIX_EVENTS.JOIN,
                    payload: params,
                    ref: joinRef,
                    join_ref: joinRef,
                });
            }
        });
        pendingJoinsRef.current.clear();
    }, [getRef, sendRaw]);

    // Connect to WebSocket
    const connect = useCallback(async () => {
        if (wsRef.current?.readyState === WebSocket.OPEN) return;

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

                // Reconnect with exponential backoff
                if (!ev.wasClean) {
                    const delay = Math.min(1000 * Math.pow(2, reconnectAttemptRef.current), RECONNECT_MAX_DELAY);
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
            // Retry after 15 seconds on init failure
            setTimeout(connect, 15000);
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
        connect();
        return () => {
            if (reconnectTimerRef.current) {
                clearTimeout(reconnectTimerRef.current);
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
