import { describe, expect, it } from "vitest";
import type { NodeBrokerSource, NodeLogEvent } from "@/lib/api/client/admin/nodeDiagnostics";
import { emptyLogFilters, logFilterError, parseLogFilters, safeLogEvent, validatedBroker } from "./nodeDiagnostics";

const id = "11111111-1111-4111-8111-111111111111";
const group = `worker-${id}`;
const topic = `w.${id}`;
const at = "2026-10-10T08:00:00Z";
function source(): NodeBrokerSource {
    return {
        id, availability: "fresh", coverage: "complete", observed_at: at, checked_at: at, measured_scopes: 1, expected_scopes: 1,
        metrics: [{ id: "committed_lag", title: "Broker committed_lag", unit: "messages", scope_id: id, availability: "fresh", condition: "unknown", severity: "info", note: "Observation only", count: 6,
            observed_at: at, evidence_at: at, latest_evidence_at: at, affected_mailboxes: null, affected_organizations: null, unknown_organization_rows: null, evidence_age_seconds: null, next_eligible_at: null, window_start: null, window_end: null, threshold_seconds: null,
            broker: { consumer_group: group, topics: [topic], partitions: [{ topic, partition: 0, earliest: 10, committed: 16, latest: 22, committed_lag: 6 }] },
        }],
    };
}

describe("read-only node broker evidence", () => {
    it("accepts exact retained committed offsets, not send rates or invented member counts", () => {
        const result = validatedBroker(source(), group, topic);
        expect(result?.lag).toBe(6);
        expect(result?.members).toBeNull();
    });
    it("keeps validated zero lag distinct from an unsupported observation", () => {
        const zero = source();
        zero.metrics[0].count = 0;
        zero.metrics[0].broker!.partitions[0].committed = 22;
        zero.metrics[0].broker!.partitions[0].committed_lag = 0;
        expect(validatedBroker(zero, group, topic)?.lag).toBe(0);
        expect(validatedBroker({ ...zero, availability: "unavailable", metrics: [], reason: "unsupported" }, group, topic)).toBeNull();
    });
    it.each(["stale", "unavailable"] as const)("does not render %s observations as fresh values", (availability) => {
        expect(validatedBroker({ ...source(), availability }, group, topic)).toBeNull();
    });
    it("does not attribute the shared result group to worker commands", () => {
        expect(validatedBroker(source(), "consumer-group", "jobs.worker-events")).toBeNull();
    });
    it.each(["missing", "duplicate", "negative", "out-of-retention", "future-commit", "mismatch", "wrong-topic", "wrong-group", "overflow", "unsafe-offset", "healthy-condition", "wrong-scope", "partial"])("withholds %s data", (invalid) => {
        const value = source();
        const metric = value.metrics[0];
        const detail = metric.broker!;
        const partition = detail.partitions[0];
        switch (invalid) {
            case "missing": metric.count = null; break;
            case "duplicate": detail.partitions.push({ ...partition }); break;
            case "negative": partition.partition = -1; break;
            case "out-of-retention": partition.committed = 9; break;
            case "future-commit": partition.committed = 23; break;
            case "mismatch": metric.count = 7; break;
            case "wrong-topic": partition.topic = "unexpected"; break;
            case "wrong-group": detail.consumer_group = "consumer-group"; break;
            case "overflow": detail.partitions = Array.from({ length: 129 }, (_, i) => ({ ...partition, partition: i })); break;
            case "unsafe-offset": partition.latest = Number.MAX_SAFE_INTEGER + 1; break;
            case "healthy-condition": metric.condition = "healthy"; break;
            case "wrong-scope": metric.scope_id = "other"; break;
            case "partial": value.coverage = "partial"; break;
        }
        expect(validatedBroker(value, group, topic)).toBeNull();
    });
});

describe("redacted node evidence and filters", () => {
    it("only accepts canonical allowlisted event records", () => {
        const event: NodeLogEvent = { id, event: "sync_control_plane_held", observed_at: at, level: "warn", category: "control_plane", http_status: 503 };
        expect(safeLogEvent(event)).toBe(true);
        expect(safeLogEvent({ ...event, event: "raw_provider_message" })).toBe(false);
        expect(safeLogEvent({ ...event, category: "provider" })).toBe(false);
        expect(safeLogEvent({ ...event, observed_at: "invalid" })).toBe(false);
    });
    it("strips unrelated stored fields and rejects invalid UUID/time ranges", () => {
        expect(parseLogFilters({ ...emptyLogFilters, level: "warn", secret: "not-stored" })).toEqual({ ...emptyLogFilters, level: "warn" });
        expect(parseLogFilters({ ...emptyLogFilters, mailbox: "not-a-uuid" })).toBeNull();
        expect(parseLogFilters({ ...emptyLogFilters, after: "invalid" })).toBeNull();
        expect(logFilterError({ ...emptyLogFilters, after: "2026-10-10T10:00", before: "2026-10-10T09:00" })).toMatch(/Start time/);
        expect(logFilterError({ ...emptyLogFilters, mailbox: id, after: "2026-10-10T08:00" })).toBeNull();
    });
});
