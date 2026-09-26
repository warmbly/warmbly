// Shared labels and helpers for the seed panel page and its test drawer.

import { APIError } from "@/lib/api/client";

export function pct(rate: number | null | undefined): string {
    if (rate === null || rate === undefined || !Number.isFinite(rate)) return "—";
    return `${Math.round(rate * 100)}%`;
}

export const PANEL_LABEL: Record<string, string> = {
    instance: "Instance",
    workspace: "Workspace",
    cloud: "Warmbly Cloud",
};

export const ORIGIN_LABEL: Record<string, string> = {
    manual: "Workspace member",
    monitor: "Monitor",
    admin: "Admin",
    remote: "Linked instance",
};

// The backend's `error` field is the HTTP status text; the sentence worth
// showing is `message`.
export function describeError(err: unknown): { message: string; code?: string } {
    if (err instanceof APIError) {
        const body = err.body as { message?: string } | undefined;
        return { message: body?.message || err.message, code: err.code };
    }
    return { message: err instanceof Error ? err.message : "Request failed" };
}
