import { Request } from "@/lib/api/client";

export type MonitoringAvailability = "fresh" | "stale" | "unavailable";
export type MonitoringCondition = "healthy" | "unknown" | "no_recent_evidence" | "recent_failure" | "persistent_failure" | "recovery_unverified" | "safety_hold" | "backlog" | "in_flight" | "waiting_schedule" | "waiting_retry" | "budget" | "daily_limit" | "stale_telemetry";

export interface MonitoringMetric {
    id: string;
    title: string;
    unit: string;
    scope_id?: string;
    availability: MonitoringAvailability;
    condition: MonitoringCondition;
    severity: string;
    reason?: string;
    note: string;
    count: number | null;
    affected_mailboxes: number | null;
    affected_organizations: number | null;
    unknown_organization_rows: number | null;
    observed_at: string | null;
    evidence_at: string | null;
    latest_evidence_at: string | null;
    evidence_age_seconds: number | null;
    next_eligible_at: string | null;
    window_start: string | null;
    window_end: string | null;
    threshold_seconds: number | null;
    investigate?: string;
}

export interface MonitoringSource {
    id: string;
    availability: MonitoringAvailability;
    coverage: string;
    reason?: string;
    observed_at: string | null;
    checked_at: string;
    measured_scopes: number | null;
    expected_scopes: number | null;
    metrics: MonitoringMetric[];
}

export interface MonitoringSnapshot {
    version: string;
    checked_at: string;
    refresh_after: string;
    coverage: string;
    sources: MonitoringSource[];
}

export function getInstanceMonitoring(): Promise<MonitoringSnapshot> {
    return Request({ method: "GET", url: "/admin/instance/monitoring", authorization: true, timeout: 12_000 });
}
