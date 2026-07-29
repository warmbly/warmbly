// Headless realtime wiring for the admin app. Mount once inside the
// QueryClientProvider: connects the singleton socket, feeds every platform
// event into the events store, and runs a throttled query-invalidation spine
// so admin lists stay live without per-page refetch intervals.

import { useEffect } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { onEvent, startRealtime, stopRealtime } from "./socket";
import { pushEvent } from "./eventsStore";

// At most one invalidation per group per window; the feed itself is
// unthrottled. High-frequency events (EMAIL_SENT/OPENED/CLICKED/REPLIED/
// RECEIVED, TASK_PROGRESS, ...) match no group on purpose: feed only.
const THROTTLE_MS = 5_000;

interface SpineGroup {
    id: string;
    match: (name: string) => boolean;
    keys: string[][];
}

const SPINE: SpineGroup[] = [
    {
        id: "warmup",
        match: (n) => n.includes("ACCOUNT") || n.includes("WARMUP"),
        keys: [
            ["admin", "warmup"],
            ["admin", "mailboxes"],
        ],
    },
    {
        id: "campaigns",
        match: (n) => n.includes("CAMPAIGN"),
        keys: [["admin", "campaigns"]],
    },
    {
        id: "users",
        match: (n) =>
            n.includes("MEMBER") ||
            n.includes("USER") ||
            n.includes("SUBSCRIPTION") ||
            n.includes("BILLING"),
        keys: [
            ["admin", "users"],
            ["admin", "organizations"],
        ],
    },
    {
        id: "workers",
        match: (n) => n.includes("WORKER"),
        keys: [["admin", "workers"]],
    },
];

export default function RealtimeManager() {
    const queryClient = useQueryClient();

    useEffect(() => {
        const lastFired = new Map<string, number>();
        startRealtime();
        const unsubscribe = onEvent((name, payload) => {
            pushEvent(name, payload);

            const upper = name.toUpperCase();
            for (const group of SPINE) {
                if (!group.match(upper)) continue;
                const now = Date.now();
                const last = lastFired.get(group.id) ?? 0;
                if (now - last < THROTTLE_MS) continue;
                lastFired.set(group.id, now);
                for (const queryKey of group.keys) {
                    queryClient.invalidateQueries({ queryKey });
                }
            }
        });
        return () => {
            unsubscribe();
            stopRealtime();
        };
    }, [queryClient]);

    return null;
}
