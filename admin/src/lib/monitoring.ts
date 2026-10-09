import { AdminPerm } from "@/lib/auth/permissions";
import type { MonitoringCondition, MonitoringMetric, MonitoringSource } from "@/lib/api/client/admin/monitoring";

export const MONITORING_SOURCES: Record<string, { title: string; permission: number }> = {
    send_safety: { title: "Send safety", permission: AdminPerm.ViewUsers },
    sends: { title: "Send confirmations", permission: AdminPerm.ViewCampaigns },
    campaign_reservations: { title: "Campaign reservations", permission: AdminPerm.ViewCampaigns },
    dispatch: { title: "Scheduled dispatch", permission: AdminPerm.ViewCampaigns },
    sync: { title: "Mailbox sync observations", permission: AdminPerm.ViewUsers },
    mailbox_errors: { title: "Recorded mailbox issues", permission: AdminPerm.ViewUsers },
    worker_samples: { title: "Worker telemetry", permission: AdminPerm.ViewWorkers },
    workers: { title: "Worker registration", permission: AdminPerm.ViewWorkers },
    jobs: { title: "Registered jobs", permission: AdminPerm.ViewAnalytics },
    dead_letters: { title: "Dead letters", permission: AdminPerm.ViewCampaigns },
    webhooks: { title: "Customer webhooks", permission: AdminPerm.ViewOrganizations },
    notifications: { title: "Customer notification rows", permission: AdminPerm.ManageSettings },
    result_effects: { title: "Send result effects", permission: AdminPerm.ViewCampaigns },
    arrivals: { title: "Optional arrival outbox", permission: AdminPerm.ViewUsers },
    domain_auth: { title: "Sending-domain authentication", permission: AdminPerm.ViewUsers },
    warmup_loading: { title: "Warmup mailbox loading", permission: AdminPerm.ViewCampaigns },
    backend_event_broker: { title: "Backend result broker", permission: AdminPerm.ViewAnalytics },
    worker_command_broker: { title: "Worker command broker", permission: AdminPerm.ViewWorkers },
    send_wait_attribution: { title: "Detailed send wait reasons", permission: AdminPerm.ViewCampaigns },
    tracking_broker: { title: "External tracking broker", permission: AdminPerm.ViewAnalytics },
};

export const CONDITION_LABELS: Record<MonitoringCondition, string> = {
    healthy: "Confirmed scoped observation", unknown: "Unknown", no_recent_evidence: "No matching recent evidence",
    recent_failure: "Recent failure evidence", persistent_failure: "Persistent failure evidence",
    recovery_unverified: "Recovery unverified", safety_hold: "Safety hold", backlog: "Measured backlog",
    in_flight: "In flight", waiting_schedule: "Scheduled wait", waiting_retry: "Retry wait",
    budget: "Budget wait", daily_limit: "Daily-limit wait", stale_telemetry: "Stale telemetry",
};

const REASONS: Record<string, string> = {
    permission_denied: "Permission required", unsupported: "Not supported by this source",
    dependency_missing: "Dependency unavailable", schema_absent: "Optional schema not installed",
    timeout: "Collection timed out", query_failed: "Measurement query failed", collection_failed: "Collection failed",
    sample_limit: "Bounded sample", scope_limit: "Bounded scope", no_recent_evidence: "No recent measurement evidence",
    no_registered_scopes: "No registered scopes to measure",
};

export function monitoringReason(reason?: string): string {
    return REASONS[reason ?? ""] ?? "Measurement unavailable or incomplete";
}

export function canMonitor(mask: number | undefined, permission: number): boolean {
    return typeof mask === "number" && (mask & permission) === permission;
}

export function permittedSource(source: MonitoringSource, mask: number | undefined): MonitoringSource {
    const required = MONITORING_SOURCES[source.id]?.permission;
    if (required !== undefined && canMonitor(mask, required)) return source;
    return { ...source, availability: "unavailable", coverage: "unavailable", reason: required === undefined ? "unsupported" : "permission_denied", observed_at: null, measured_scopes: null, expected_scopes: null, metrics: [] };
}

const ATTENTION = new Set<MonitoringCondition>(["recent_failure", "persistent_failure", "recovery_unverified", "safety_hold", "backlog", "stale_telemetry"]);

export function needsMonitoringAttention(metric: MonitoringMetric): boolean {
    return metric.availability !== "unavailable" && typeof metric.count === "number" && metric.count > 0 &&
        (ATTENTION.has(metric.condition) || metric.severity === "warning" || metric.severity === "error" || isBrokerQueue(metric));
}

export function isBrokerQueue(metric: MonitoringMetric): boolean {
    return ["committed_lag", "pending", "ack_pending", "redelivered"].includes(metric.id);
}

export function monitoringAttentionCount(sources: MonitoringSource[]): number {
    return sources.reduce((count, source) => count + source.metrics.filter(needsMonitoringAttention).length, 0);
}

export function investigationPath(path: string | undefined, mask: number | undefined): string | null {
    if (!path) return null;
    let required: number | undefined;
    if (/^\/sends\?tab=(in-flight|failures|dead-letters)$/.test(path)) required = AdminPerm.ViewCampaigns;
    else if (path === "/sends?tab=webhooks") required = AdminPerm.ViewCampaigns | AdminPerm.ViewOrganizations;
    else if (/^\/(sync|mailboxes)(\?state=(throttled|backfilling|stalled|pending|complete))?$/.test(path)) required = AdminPerm.ViewUsers;
    else if (/^\/(workers|fleet)(\/[0-9a-f-]{36})?$/.test(path)) required = AdminPerm.ViewWorkers;
    else if (/^\/organizations\/[0-9a-f-]{36}$/.test(path)) required = AdminPerm.ViewOrganizations;
    else if (/^\/configuration(\?tab=notifications)?$/.test(path)) required = AdminPerm.ManageSettings;
    else if (/^\/(jobs|health)(\?tab=(findings|services|operations))?$/.test(path)) required = AdminPerm.ViewAnalytics;
    return required !== undefined && canMonitor(mask, required) ? path : null;
}

export function timestamp(value: string | null | undefined): number | null {
    const parsed = value ? Date.parse(value) : NaN;
    return Number.isFinite(parsed) && parsed > 0 ? parsed : null;
}

export function observationIsOld(value: string | null | undefined, now: number): boolean {
    const at = timestamp(value);
    return at === null || at > now + 60_000 || now - at > 120_000;
}

export function ageLabel(at: number, now: number): string {
    const seconds = Math.floor((now - at) / 1000);
    if (seconds < 0) return "future timestamp, age unknown";
    if (seconds < 60) return `${seconds}s ago`;
    if (seconds < 3600) return `${Math.floor(seconds / 60)}m ago`;
    if (seconds < 86400) return `${Math.floor(seconds / 3600)}h ago`;
    return `${Math.floor(seconds / 86400)}d ago`;
}
