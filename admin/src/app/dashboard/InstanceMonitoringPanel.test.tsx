import { renderToStaticMarkup } from "react-dom/server";
import { act } from "react";
import { createRoot } from "react-dom/client";
import { MemoryRouter } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { MonitoringMetric, MonitoringSnapshot, MonitoringSource } from "@/lib/api/client/admin/monitoring";
import { AdminPerm } from "@/lib/auth/permissions";
import { InstanceMonitoringPanel } from "./InstanceMonitoringPanel";

const state = vi.hoisted(() => ({
    mask: 0 as number | undefined,
    query: { data: undefined as MonitoringSnapshot | undefined, isError: false, isLoading: false, isFetching: false, error: new Error("provider-password-private"), refetch: vi.fn() },
}));
vi.mock("@/hooks/useMe", () => ({ useMe: () => ({ data: { id: "admin", admin_permissions: state.mask } }) }));
vi.mock("@/hooks/useInstanceMonitoring", () => ({ useInstanceMonitoring: () => state.query }));

const at = new Date().toISOString();
const evidence = new Date(Date.now() - 3_600_000).toISOString();
function metric(overrides: Partial<MonitoringMetric> = {}): MonitoringMetric {
    return {
        id: "failed_send", title: "Stored failure report", unit: "reports", availability: "fresh",
        condition: "recent_failure", severity: "warning", note: "Only confirmed send evidence closes an incident.",
        count: 7, affected_mailboxes: 3, affected_organizations: 2, unknown_organization_rows: 1,
        observed_at: at, evidence_at: evidence, latest_evidence_at: at, evidence_age_seconds: 3600,
        next_eligible_at: null, window_start: evidence, window_end: at, threshold_seconds: 3600,
        investigate: "/sends?tab=failures", ...overrides,
    };
}
function source(overrides: Partial<MonitoringSource> = {}): MonitoringSource {
    return { id: "sends", availability: "fresh", coverage: "complete", observed_at: at, checked_at: at,
        measured_scopes: null, expected_scopes: null, metrics: [metric()], ...overrides };
}
function snapshot(sources: MonitoringSource[] = [source()]): MonitoringSnapshot {
    return { version: "1", checked_at: at, refresh_after: at, coverage: "complete", sources };
}
function render(compact = false) {
    const html = renderToStaticMarkup(<MemoryRouter><InstanceMonitoringPanel compact={compact} /></MemoryRouter>);
    return new DOMParser().parseFromString(html, "text/html");
}

beforeEach(() => {
    state.mask = Object.values(AdminPerm).reduce((mask, bit) => mask | bit, 0);
    Object.assign(state.query, { data: snapshot(), isError: false, isLoading: false, isFetching: false });
});

describe("operational monitoring evidence", () => {
    it("uses the current clock when a new snapshot arrives between clock ticks", async () => {
        vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
        vi.useFakeTimers();
        vi.setSystemTime(new Date(at));
        const container = document.createElement("div");
        const root = createRoot(container);
        try {
            await act(async () => root.render(<MemoryRouter><InstanceMonitoringPanel /></MemoryRouter>));
            vi.setSystemTime(new Date(Date.parse(at) + 5_000));
            const fresh = new Date(Date.now()).toISOString();
            state.query.data = { ...snapshot([source({ observed_at: fresh, checked_at: fresh, metrics: [metric({ observed_at: fresh, latest_evidence_at: fresh })] })]), checked_at: fresh };
            await act(async () => root.render(<MemoryRouter><InstanceMonitoringPanel /></MemoryRouter>));
            expect(container.textContent).not.toContain("future timestamp, age unknown");
            expect(container.textContent).toContain("0s ago");
            await act(async () => vi.advanceTimersByTime(30_000));
            expect(container.textContent).toContain("30s ago");
            const future = new Date(Date.now() + 90_000).toISOString();
            state.query.data = snapshot([source({ observed_at: future, metrics: [metric({ observed_at: future })] })]);
            await act(async () => root.render(<MemoryRouter><InstanceMonitoringPanel /></MemoryRouter>));
            expect(container.textContent).toContain("future timestamp, age unknown");
            expect(container.textContent).toContain("Stale observation");
        } finally {
            await act(async () => root.unmount());
            vi.useRealTimers();
            vi.unstubAllGlobals();
        }
    });

    it("keeps metric counts distinct from affected mailboxes, organizations and attention signals", () => {
        const document = render();
        const text = document.body.textContent;
        expect(text).toContain("1 recorded attention signals");
        expect(text).toContain("7 reports");
        expect(text).toContain("3 affected mailboxes");
        expect(text).toContain("2 affected organizations");
        expect(text).toContain("1 rows with unknown organization");
        expect(text).toContain("Observed:");
        expect(text).toContain("Evidence since:");
        expect(text).toContain("Latest evidence:");
        expect(document.querySelector(`time[datetime="${evidence}"]`)).not.toBeNull();
        expect(document.querySelector('a[href="/sends?tab=failures"]')).not.toBeNull();
    });

    it("renders loading, failed and successful empty requests without a green verdict", () => {
        state.query.data = undefined;
        state.query.isLoading = true;
        expect(render().querySelector('[role="status"]')?.textContent).toContain("Loading operational monitoring");
        state.query.isLoading = false;
        state.query.isError = true;
        expect(render().body.textContent).toContain("Monitoring refresh failed");
        expect(render().body.textContent).not.toContain("provider-password-private");
        state.query.isError = false;
        state.query.data = snapshot([]);
        expect(render().body.textContent).toContain("No monitoring sources returned");
        expect(render().body.textContent).not.toContain("recorded attention signals");
    });

    it.each(["unsupported", "dependency_missing", "schema_absent", "timeout", "query_failed", "permission_denied"])("exposes %s coverage without inventing zero counts", (reason) => {
        state.query.data = snapshot([source({ availability: "unavailable", coverage: "unavailable", reason, metrics: [] })]);
        const text = render().body.textContent;
        expect(text).toContain("1 sources with coverage gaps");
        expect(text).toContain("No current measurement available");
        expect(text).not.toContain("0 reports");
    });

    it("keeps a missing measurement distinct from a measured zero", () => {
        state.query.data = snapshot([source({ metrics: [metric({ count: null, affected_mailboxes: null, affected_organizations: null }), metric({ id: "sent", title: "Send confirmations", count: 0, condition: "no_recent_evidence" })] })]);
        const text = render().body.textContent;
        expect(text).toContain("Not measured");
        expect(text).toContain("0 reports");
        expect(text).toContain("No matching recent evidence");
        expect(text).not.toContain("Confirmed scoped observation");
    });

    it("retains unsupported metric explanations without presenting their counts as current measurements", () => {
        state.query.data = snapshot([source({ id: "send_wait_attribution", availability: "unavailable", coverage: "unavailable", reason: "unsupported", metrics: [metric({ title: "Detailed live send waits", condition: "budget", count: 123, note: "Scheduler does not persist per-task budget decisions." })] })]);
        const text = render().body.textContent;
        expect(text).toContain("Detailed live send waits");
        expect(text).toContain("Scheduler does not persist per-task budget decisions");
        expect(text).toContain("Not measured");
        expect(text).not.toContain("123 reports");
        expect(text).not.toContain("3 affected mailboxes");
    });

    it("shows retained stale observations and partial samples with their original evidence clocks", () => {
        state.query.data = snapshot([source({ availability: "stale", coverage: "partial", measured_scopes: 20, expected_scopes: 35, metrics: [metric({ availability: "stale" })] })]);
        const document = render();
        expect(document.body.textContent).toContain("20 / 35 scopes measured");
        expect(document.body.textContent).toContain("lower bounds");
        expect(document.body.textContent).toContain("Stale observation");
        expect(document.body.textContent).toContain("historical, not proof of a current failure");
        expect(document.querySelector(`time[datetime="${evidence}"]`)).not.toBeNull();
    });

    it("a failed refresh cannot make cached healthy data appear fresh", () => {
        state.query.isError = true;
        state.query.data = snapshot([source({ metrics: [metric({ condition: "healthy", severity: "info" })] })]);
        const document = render();
        expect(document.body.textContent).toContain("previous snapshot with its original timestamps");
        expect(document.body.textContent).toContain("Stale observation");
        expect(document.body.textContent).not.toContain("Fresh / complete");
        expect(document.querySelector(`time[datetime="${at}"]`)).not.toBeNull();
    });

    it("ages snapshots by observation time, not successful client fetch time", () => {
        const old = new Date(Date.now() - 300_000).toISOString();
        state.query.data = snapshot([source({ observed_at: old, metrics: [metric({ observed_at: old })] })]);
        expect(render().body.textContent).toContain("Stale observation");
    });

    it("denies metrics and drill-downs when permission bits are absent or unknown", () => {
        for (const mask of [undefined, 0]) {
            state.mask = mask;
            const document = render(true);
            expect(document.body.textContent).toContain("Monitoring permission required");
            expect(document.body.textContent).not.toContain("Stored failure report");
            expect(document.querySelector("a")).toBeNull();
        }
        state.mask = AdminPerm.ViewAnalytics;
        expect(render().body.textContent).toContain("Permission required");
        expect(render().body.textContent).not.toContain("Stored failure report");
        expect(render().querySelector('a[href="/sends?tab=failures"]')).toBeNull();
    });

    it("checks the link destination permission separately from the visible source", () => {
        state.mask = AdminPerm.ViewAnalytics | AdminPerm.ViewOrganizations;
        state.query.data = snapshot([source({ id: "webhooks", metrics: [metric({ investigate: "/sends?tab=webhooks" })] })]);
        expect(render().body.textContent).toContain("7 reports");
        expect(render().querySelector('a[href="/sends?tab=webhooks"]')).toBeNull();
        state.mask |= AdminPerm.ViewCampaigns;
        expect(render().querySelector('a[href="/sends?tab=webhooks"]')).not.toBeNull();
    });

    it("rejects unrecognized source data, arbitrary links, raw reason and scope payloads", () => {
        state.query.data = snapshot([source({ reason: "provider-password-private", metrics: [metric({ scope_id: "secret@example.com", reason: "provider-password-private", investigate: "https://evil.invalid/?token=secret" })] }), source({ id: "secret-source", metrics: [metric({ title: "secret-title" })] })]);
        const text = render().body.innerHTML;
        for (const secret of ["provider-password-private", "secret@example.com", "evil.invalid", "secret-title", "secret-source"]) expect(text).not.toContain(secret);
    });

    it.each(["committed_lag", "pending", "ack_pending", "redelivered"])("keeps positive broker %s prominent while preserving unknown/info", (id) => {
        state.query.data = snapshot([source({ metrics: Array.from({ length: 8 }, (_, i) => metric({ id: `failure-${i}` })) }), source({ id: "backend_event_broker", metrics: [metric({ id, title: `Broker ${id}`, condition: "unknown", severity: "info", count: 45 })] })]);
        const document = render(true);
        expect(document.body.textContent).toContain("1 positive broker queue measurements");
        expect(document.body.textContent).toContain("45 reports");
        expect(document.body.textContent).toContain("Queue measurement, not outage");
        expect(document.body.textContent).toContain("Unknown");
        expect(document.body.textContent).not.toContain("Confirmed scoped observation");
    });

    it("does not classify waits or a fresh zero queue as a failure", () => {
        state.query.data = snapshot([source({ metrics: ["waiting_schedule", "waiting_retry", "budget", "daily_limit", "in_flight"].map((condition) => metric({ condition: condition as MonitoringMetric["condition"], severity: "info", next_eligible_at: at })) }), source({ id: "backend_event_broker", metrics: [metric({ id: "pending", condition: "unknown", severity: "info", count: 0 })] })]);
        const text = render().body.textContent;
        expect(text).toContain("0 recorded attention signals");
        expect(text).toContain("Scheduled wait");
        expect(text).toContain("Retry wait");
        expect(text).toContain("Budget wait");
        expect(text).toContain("Daily-limit wait");
        expect(text).toContain("Next eligible:");
        expect(text).toContain("not an instance-wide all-clear");
    });

    it("shows safety holds as restrictions and counts unknown warning observations without outage claims", () => {
        state.query.data = snapshot([source({ metrics: [metric({ condition: "safety_hold" }), metric({ id: "missing", condition: "unknown", severity: "warning" })] })]);
        const text = render(true).body.textContent;
        expect(text).toContain("2 recorded attention signals");
        expect(text).toContain("Safety hold");
        expect(text).toContain("Unknown");
        expect(text).toContain("restrictions, not provider outages");
    });

    it("does not interpret an unsupported contract version", () => {
        state.query.data = { ...snapshot(), version: "2" };
        expect(render().body.textContent).toContain("Unsupported monitoring version");
        expect(render().body.textContent).not.toContain("7 reports");
    });
});
