import { useQuery } from "@tanstack/react-query";
import { getUpdateState, type UpdateState } from "@/lib/api/client/admin/updates";

export const UPDATE_STATE_KEY = ["admin", "instance", "update"] as const;
export const UPDATE_JOB_KEY = ["admin", "instance", "update", "job"] as const;

// One cache entry feeds the top-bar pill, the Setup and health card and the
// dialog. It polls every minute, and every few seconds while a job runs, so
// the pill follows an update started from another tab or before a reload.
export function useUpdateState(options?: { enabled?: boolean }) {
    return useQuery({
        queryKey: UPDATE_STATE_KEY,
        queryFn: () => getUpdateState(false),
        refetchInterval: (query) => (isUpdating(query.state.data) ? 3_000 : 60_000),
        // While the backend restarts every request fails; keep the last state
        // on screen instead of flashing an error.
        retry: false,
        enabled: options?.enabled ?? true,
    });
}

// The dialog's view of a running job, with the log. Polled only while open.
export function useUpdateJob(enabled: boolean) {
    return useQuery({
        queryKey: UPDATE_JOB_KEY,
        queryFn: () => getUpdateState(true),
        refetchInterval: (query) => (isUpdating(query.state.data) ? 2_000 : 15_000),
        retry: false,
        enabled,
    });
}

export function isUpdating(state: UpdateState | undefined): boolean {
    return state?.updater?.job?.status === "running";
}

// A short, stable label for the running build: the tag when there is one,
// otherwise the commit.
export function buildLabel(state: UpdateState | undefined): string {
    if (!state) return "";
    const v = state.running?.version;
    if (v && v !== "dev") return v;
    const c = state.running?.commit ?? state.updater?.checkout?.commit ?? "";
    return c ? `dev ${c.slice(0, 7)}` : "dev";
}
