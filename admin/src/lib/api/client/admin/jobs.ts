// /admin/jobs/* — every background loop on the instance and "run now".
// Shapes mirror ScheduledJobRun in internal/models/admin_ops.go.

import { Request } from "@/lib/api/client";

export type ScheduledJobStatus = "idle" | "running" | "ok" | "error";

export interface ScheduledJobRun {
    name: string;
    // The process that owns the loop: backend | consumer.
    service: string;
    interval_seconds: number;
    last_started_at?: string;
    last_finished_at?: string;
    last_duration_ms: number;
    last_status: ScheduledJobStatus | string;
    last_error: string;
    run_count: number;
    error_count: number;
    // Set by "run now"; the owning loop clears it when it picks the request up.
    run_requested_at?: string;
    next_run_at?: string;
    updated_at: string;
}

export interface AdminJobsResult {
    data: ScheduledJobRun[];
}

export interface AdminRunJobResult {
    requested: boolean;
}

// The consumer loop that resolves reservations nobody answered
// (StartStuckSendReclaimer); the Sends page runs it on demand.
export const STUCK_SEND_RECLAIMER_JOB = "stuck_send_reclaimer";

export function listJobs(): Promise<AdminJobsResult> {
    return Request({ method: "GET", url: "/admin/jobs", authorization: true });
}

export function runJob(name: string): Promise<AdminRunJobResult> {
    return Request({
        method: "POST",
        url: `/admin/jobs/${encodeURIComponent(name)}/run`,
        authorization: true,
    });
}
