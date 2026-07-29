// /admin/system/status — live health probes against the platform's
// backing services (postgres, redis, kafka, schema-registry, realtime,
// tracking). The backend runs the probes on each request, so a fetch
// here is a fresh check, not a cached snapshot.

import { Request } from "@/lib/api/client";

export interface SystemComponentStatus {
    name: string;
    ok: boolean;
    latency_ms: number;
    error?: string;
}

export interface SystemStatusResult {
    data: SystemComponentStatus[];
    checked_at: string;
}

export function getSystemStatus(): Promise<SystemStatusResult> {
    return Request({
        method: "GET",
        url: "/admin/system/status",
        authorization: true,
    });
}
