import { useContext, createContext, useEffect, useCallback, useRef } from "react";

// Phoenix-compatible message format
export interface ChannelMessage {
    topic: string;
    event: string;
    payload: Record<string, unknown>;
    ref?: string;
    join_ref?: string;
}

// Channel state
export type ChannelState = "closed" | "joining" | "joined" | "leaving" | "errored";

// Channel info
export interface ChannelInfo {
    topic: string;
    state: ChannelState;
    joinRef: string;
}

// Handler types
export type ChannelEventHandler = (payload: Record<string, unknown>) => void;

interface SocketContextValue {
    // Connection state
    isConnected: boolean;
    reconnectAttempt: number;

    // Channel management
    joinChannel: (topic: string, params?: Record<string, unknown>) => void;
    leaveChannel: (topic: string) => void;
    getChannelState: (topic: string) => ChannelState;

    // Event handling
    subscribeToChannel: (
        topic: string,
        event: string,
        handler: ChannelEventHandler
    ) => () => void;

    // Push message to channel
    pushToChannel: (topic: string, event: string, payload: Record<string, unknown>) => void;

    // Legacy support - subscribe to any message type
    subscribe: <T extends { type: string }>(
        type: T['type'],
        handler: (msg: T) => void
    ) => () => void;

    // Send raw message
    sendMessage: (msg: unknown) => void;
}

export const SocketContext = createContext<SocketContextValue | undefined>(undefined);

export const useSocket = (): SocketContextValue => {
    const ctx = useContext(SocketContext);
    if (!ctx) {
        throw new Error('useSocket must be used within a <SocketProvider />');
    }
    return ctx;
};

// Hook to join a channel and subscribe to events
export function useChannel(topic: string, params?: Record<string, unknown>) {
    const socket = useSocket();

    useEffect(() => {
        socket.joinChannel(topic, params);
        return () => {
            socket.leaveChannel(topic);
        };
    }, [socket, topic, params]);

    return {
        state: socket.getChannelState(topic),
        push: (event: string, payload: Record<string, unknown>) =>
            socket.pushToChannel(topic, event, payload),
    };
}

// Hook to subscribe to a specific event on a channel
export function useChannelEvent(
    topic: string,
    event: string,
    handler: ChannelEventHandler,
    deps: React.DependencyList = []
) {
    const socket = useSocket();
    const handlerRef = useRef(handler);

    // Update handler ref when it changes
    useEffect(() => {
        handlerRef.current = handler;
    }, [handler]);

    useEffect(() => {
        const wrappedHandler: ChannelEventHandler = (payload) => {
            handlerRef.current(payload);
        };

        const unsubscribe = socket.subscribeToChannel(topic, event, wrappedHandler);
        return unsubscribe;
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [socket, topic, event, ...deps]);
}

// Convenience hook combining channel join and event subscription
export function useChannelSubscription<T extends Record<string, unknown>>(
    topic: string,
    event: string,
    handler: (payload: T) => void,
    params?: Record<string, unknown>
) {
    const channel = useChannel(topic, params);

    useChannelEvent(topic, event, handler as ChannelEventHandler);

    return channel;
}
