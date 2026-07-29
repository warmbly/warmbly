// Ring-buffer store for the live platform event feed, consumable via
// useSyncExternalStore. RealtimeManager pushes every socket event in here;
// EventsPage renders the buffer. Kept outside React so the buffer survives
// route changes and the page can mount/unmount freely.

import { useSyncExternalStore } from "react";
import { getStatus, onStatusChange, type RealtimeStatus } from "./socket";

export const MAX_EVENTS = 500;

export interface LiveEvent {
    /** Monotonic counter, unique per event for the session. */
    id: number;
    /** Date.now() at the moment the event landed. */
    receivedAt: number;
    name: string;
    payload: Record<string, unknown>;
}

export interface EventsSnapshot {
    /** Newest first, capped at MAX_EVENTS. */
    events: LiveEvent[];
    paused: boolean;
    /** Events dropped while paused; reset on resume and on clear. */
    missedCount: number;
}

let counter = 0;
let events: LiveEvent[] = [];
let paused = false;
let missedCount = 0;

const listeners = new Set<() => void>();

// Stable snapshot reference until something actually changes, as
// useSyncExternalStore requires.
let snapshot: EventsSnapshot = { events, paused, missedCount };

function emit() {
    snapshot = { events, paused, missedCount };
    listeners.forEach((l) => l());
}

export function pushEvent(name: string, payload: Record<string, unknown>) {
    if (paused) {
        missedCount += 1;
        emit();
        return;
    }
    counter += 1;
    const ev: LiveEvent = { id: counter, receivedAt: Date.now(), name, payload };
    events = [ev, ...events];
    if (events.length > MAX_EVENTS) events = events.slice(0, MAX_EVENTS);
    emit();
}

export function clearEvents() {
    events = [];
    missedCount = 0;
    emit();
}

/** While paused, incoming events are dropped and only counted; resuming
 * clears the missed counter. */
export function setPaused(next: boolean) {
    if (paused === next) return;
    paused = next;
    if (!next) missedCount = 0;
    emit();
}

export function subscribe(listener: () => void): () => void {
    listeners.add(listener);
    return () => {
        listeners.delete(listener);
    };
}

export function getSnapshot(): EventsSnapshot {
    return snapshot;
}

export function useLiveEvents(): EventsSnapshot {
    return useSyncExternalStore(subscribe, getSnapshot);
}

/** Connection status through the same external-store pattern, fed directly
 * by the socket singleton's status callbacks. */
export function useRealtimeStatus(): RealtimeStatus {
    return useSyncExternalStore(
        (listener) => onStatusChange(() => listener()),
        getStatus,
    );
}
