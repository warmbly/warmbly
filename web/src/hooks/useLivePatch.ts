// useLivePatch — a generic ephemeral "collaborative state hint" transport over
// the org channel's live:patch path. A client broadcasts a small scalar map
// scoped by `resource`; teammates on the same resource receive it and can update
// optimistically (e.g. move a deal to a stage the instant a teammate drops it,
// ahead of the durable audit refetch). Best-effort and never resumed, so the
// audit/invalidation path remains the source of truth.

import React from "react";
import { useSocket } from "./context/socket";
import { useUserProfile } from "./context/user";
import { useAppStore } from "@/stores";

export type LivePatchData = Record<string, string | number | boolean>;

export interface LivePatch {
    /** Broadcast a patch to teammates on this resource. */
    pushPatch: (data: LivePatchData) => void;
}

export function useLivePatch(
    resource: string | null,
    opts: { onPatch?: (data: LivePatchData, by: string) => void } = {},
): LivePatch {
    const { isConnected, subscribeToChannel, pushToChannel } = useSocket();
    const orgId = useAppStore((s) => s.currentOrganization?.id ?? null);
    const { user } = useUserProfile();

    const resourceRef = React.useRef(resource);
    resourceRef.current = resource;
    const orgRef = React.useRef(orgId);
    orgRef.current = orgId;
    const selfRef = React.useRef<string | null>(user?.id ?? null);
    selfRef.current = user?.id ?? null;
    const onPatchRef = React.useRef(opts.onPatch);
    onPatchRef.current = opts.onPatch;

    // Send-only callers (mutation hooks that just broadcast) pass no onPatch and
    // never open a channel subscription.
    const listening = !!opts.onPatch;

    React.useEffect(() => {
        if (!listening || !isConnected || !orgId) return;
        return subscribeToChannel(`org:${orgId}`, "LIVE_PATCH", (p) => {
            const by = typeof p.user_id === "string" ? p.user_id : "";
            if (!by || by === selfRef.current) return;
            // With no resource we match nothing (p.resource is always a string).
            if (!resourceRef.current || p.resource !== resourceRef.current) return;
            const data = (p.data ?? {}) as LivePatchData;
            onPatchRef.current?.(data, by);
        });
    }, [listening, isConnected, orgId, subscribeToChannel]);

    const pushPatch = React.useCallback(
        (data: LivePatchData) => {
            const o = orgRef.current;
            const r = resourceRef.current;
            if (!o || !r) return;
            pushToChannel(`org:${o}`, "live:patch", { resource: r, data });
        },
        [pushToChannel],
    );

    return { pushPatch };
}
