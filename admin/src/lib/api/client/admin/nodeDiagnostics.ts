import { Request } from "@/lib/api/client";
import type { MonitoringAvailability, MonitoringSource, MonitoringMetric } from "./monitoring";

export interface NodeLogEvent {
    id: string;
    observed_at: string;
    event: string;
    level: "info" | "warn" | "error";
    category: string;
    mailbox_id?: string;
    http_status?: number;
    count?: number;
}

export interface NodeLogHistory {
    node_id: string;
    availability: MonitoringAvailability;
    coverage: "partial" | "unavailable";
    reason?: string;
    observed_at: string;
    capture?: { protocol: number; run_id: string; started_at: string; observed_at: string; received_at: string; dropped: number };
    events: NodeLogEvent[];
    oldest_retained_at?: string;
    newest_retained_at?: string;
    retained_count: number;
    truncated: boolean;
    retention_seconds: number;
    max_events: number;
}

export interface NodeLogFilters { level: "" | "info" | "warn" | "error"; after: string; before: string; mailbox: string }

export interface BrokerPartition { topic: string; partition: number; earliest: number; committed: number; latest: number; committed_lag: number }
export interface BrokerDetail { consumer_group: string; topics: string[]; partitions: BrokerPartition[] }
export type NodeBrokerMetric = MonitoringMetric & { broker?: BrokerDetail };
export type NodeBrokerSource = Omit<MonitoringSource, "metrics"> & { metrics: NodeBrokerMetric[] };
export interface NodeBrokerObservation { node_id: string; observed_at: string; commands: NodeBrokerSource; results: NodeBrokerSource; note: string }

export function getNodeLogs(id: string, filters: NodeLogFilters): Promise<NodeLogHistory> {
    const params = new URLSearchParams({ limit: "200" });
    if (filters.level) params.set("level", filters.level);
    if (filters.after) params.set("after", new Date(filters.after).toISOString());
    if (filters.before) params.set("before", new Date(filters.before).toISOString());
    if (filters.mailbox.trim()) params.set("mailbox_id", filters.mailbox.trim());
    return Request({ method: "GET", url: `/admin/fleet/nodes/${encodeURIComponent(id)}/logs?${params}`, authorization: true });
}

export function getNodeBroker(id: string): Promise<NodeBrokerObservation> {
    return Request({ method: "GET", url: `/admin/fleet/nodes/${encodeURIComponent(id)}/broker`, authorization: true, timeout: 12_000 });
}
