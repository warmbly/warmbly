import { useQuery } from "@tanstack/react-query";
import { getInstanceUpdate } from "../../client/admin/updates";
import type InstanceUpdate from "../../models/auth/InstanceUpdate";

export const INSTANCE_UPDATE_KEY = ["admin", "instance", "update"] as const;
export const INSTANCE_UPDATE_LOG_KEY = ["admin", "instance", "update", "log"] as const;

export function isUpdateRunning(state: InstanceUpdate | undefined): boolean {
    return state?.updater?.job?.status === "running";
}

// The admin's full view: polled every minute, every few seconds while a job
// runs so the pill follows an update started elsewhere or before a reload.
// Requests keep failing while the backend restarts; the last state stays.
export function useInstanceUpdate(enabled: boolean) {
    return useQuery({
        queryKey: INSTANCE_UPDATE_KEY,
        queryFn: () => getInstanceUpdate(false),
        refetchInterval: (q) => (isUpdateRunning(q.state.data) ? 3_000 : 60_000),
        retry: false,
        enabled,
    });
}

// With the log, polled only while the dialog is open.
export function useInstanceUpdateLog(enabled: boolean) {
    return useQuery({
        queryKey: INSTANCE_UPDATE_LOG_KEY,
        queryFn: () => getInstanceUpdate(true),
        refetchInterval: (q) => (isUpdateRunning(q.state.data) ? 1_500 : 10_000),
        retry: false,
        enabled,
    });
}

export function runningLabel(state: InstanceUpdate | undefined): string {
    if (!state) return "";
    const v = state.running?.version;
    if (v && v !== "dev") return v;
    const c = state.running?.commit ?? state.updater?.checkout?.commit ?? "";
    return c ? `dev ${c.slice(0, 7)}` : "dev";
}
