import type { NodeBrokerSource, NodeLogFilters, NodeLogEvent, BrokerDetail } from "@/lib/api/client/admin/nodeDiagnostics";

export const emptyLogFilters: NodeLogFilters = { level: "", after: "", before: "", mailbox: "" };
export const isUUID = (value: string) => /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(value) && value !== "00000000-0000-0000-0000-000000000000";
export const safeCount = (value: unknown): value is number => typeof value === "number" && Number.isSafeInteger(value) && value >= 0;
export const validTime = (value: unknown): value is string => typeof value === "string" && Number.isFinite(Date.parse(value));

export function parseLogFilters(value: unknown): NodeLogFilters | null {
    if (!value || typeof value !== "object") return null;
    const v = value as NodeLogFilters;
    if (!["", "info", "warn", "error"].includes(v.level) || typeof v.after !== "string" || typeof v.before !== "string" || v.after.length > 24 || v.before.length > 24 || typeof v.mailbox !== "string" || v.mailbox.length > 36) return null;
    if (logFilterError(v)) return null;
    return { level: v.level, after: v.after, before: v.before, mailbox: v.mailbox };
}

export function logFilterError(value: NodeLogFilters): string | null {
    if (value.mailbox.trim() && !isUUID(value.mailbox.trim())) return "Enter a valid mailbox UUID.";
    if ((value.after && !validTime(value.after)) || (value.before && !validTime(value.before))) return "Enter valid start and end times.";
    if (value.after && value.before && Date.parse(value.after) > Date.parse(value.before)) return "Start time must not be after end time.";
    return null;
}

export const eventLabels: Record<string, { title: string; level: string; category: string }> = {
    sync_control_plane_held: { title: "Control-plane sync held", level: "warn", category: "control_plane" },
    mailbox_provider_unreachable: { title: "Mailbox provider unreachable", level: "warn", category: "provider" },
    mailbox_provider_auth_failed: { title: "Mailbox provider authentication failed", level: "warn", category: "provider" },
    mailbox_provider_throttled: { title: "Mailbox provider throttled", level: "warn", category: "provider" },
    mailbox_provider_sync_completed: { title: "Mailbox provider sync completed", level: "info", category: "provider" },
    sync_folder_skipped: { title: "Sync folder skipped", level: "info", category: "sync_policy" },
    sync_skipped_folder_search_failed: { title: "Folder search failed; sync skipped", level: "warn", category: "provider" },
    sync_message_deferred: { title: "Sync message deferred", level: "warn", category: "control_plane" },
    sync_message_deferred_by_policy: { title: "Sync message deferred by policy", level: "info", category: "sync_policy" },
    sync_legacy_arrival: { title: "Legacy sync arrival", level: "warn", category: "compatibility" },
    worker_error: { title: "Coarse worker runtime error", level: "error", category: "worker_runtime" },
};

export function safeLogEvent(event: NodeLogEvent): boolean {
    const canonical = eventLabels[event.event];
    return !!canonical && isUUID(event.id) && validTime(event.observed_at) && canonical.level === event.level && canonical.category === event.category;
}

export const diagnosticReason = (reason?: string) => ({
    log_credential_unavailable: "Log credential unavailable. Existing or manually enrolled nodes need upgrade and re-enrollment.",
    capture_not_observed: "No capture has been observed.",
    cache_unavailable: "Evidence cache could not be read.",
    no_recent_capture: "No recent capture. Older retained evidence is not current health.",
    no_recent_evidence: "Recent capture contained no retained events. This does not establish health.",
    allowlisted_evidence_only: "Only allowlisted, redacted events are captured, not the full journal.",
    unsupported: "This broker does not support committed-offset observations.",
    dependency_missing: "Broker diagnostics are not configured.",
    resource_absent: "The requested group or topic could not be observed.",
    permission_denied: "The broker denied read access.",
    timeout: "The broker observation timed out.",
    collection_failed: "The observation could not be validated or collected. Missing commits, mismatched scopes or offsets outside retention remain unknown.",
}[reason ?? ""] ?? "Observation unavailable or incomplete.");

export function validatedBroker(source: NodeBrokerSource, group: string, topic: string): { lag: number; members: number | null; detail: BrokerDetail } | null {
    if (source.availability !== "fresh" || source.coverage !== "complete" || source.measured_scopes !== 1 || source.expected_scopes !== 1) return null;
    const scope = group === "consumer-group" ? "worker_events" : group.replace(/^worker-/, "");
    if (source.id !== scope || !validTime(source.observed_at) || !validTime(source.checked_at)) return null;
    const metrics = source.metrics ?? [];
    const lagMetrics = metrics.filter((m) => m.id === "committed_lag");
    const metric = lagMetrics[0];
    if (lagMetrics.length !== 1 || !metric || metric.scope_id !== scope || metric.availability !== "fresh" || metric.condition !== "unknown" || !safeCount(metric.count) || !validTime(metric.observed_at)) return null;
    const detail = metric.broker;
    if (!detail || detail.consumer_group !== group || detail.topics?.length !== 1 || detail.topics[0] !== topic || !Array.isArray(detail.partitions) || detail.partitions.length < 1 || detail.partitions.length > 128) return null;
    let total = 0;
    const seen = new Set<number>();
    for (const partition of detail.partitions) {
        if (partition.topic !== topic || !safeCount(partition.partition) || partition.partition > 0x7fffffff || seen.has(partition.partition) ||
            !safeCount(partition.earliest) || !safeCount(partition.committed) || !safeCount(partition.latest) || !safeCount(partition.committed_lag) ||
            partition.earliest > partition.committed || partition.committed > partition.latest || partition.latest - partition.committed !== partition.committed_lag) return null;
        seen.add(partition.partition);
        total += partition.committed_lag;
        if (!safeCount(total)) return null;
    }
    if (total !== metric.count) return null;
    const memberMetrics = metrics.filter((m) => m.id === "members");
    const member = memberMetrics[0];
    const members = memberMetrics.length === 1 && member?.scope_id === scope && member.availability === "fresh" && member.condition === "unknown" && validTime(member.observed_at) && safeCount(member.count) ? member.count : null;
    return { lag: total, members, detail };
}
