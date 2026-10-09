import { renderToStaticMarkup } from "react-dom/server";
import { MemoryRouter } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { InstanceHealthResult } from "@/lib/api/client/admin/instance";
import type { SystemStatusResult } from "@/lib/api/client/admin/system";
import { FindingsTab } from "./FindingsTab";
import { ServicesTab } from "./ServicesTab";

const state = vi.hoisted(() => ({
    health: { data: undefined as InstanceHealthResult | undefined, isError: false, isLoading: false, isFetching: false, dataUpdatedAt: 0, error: new Error("secret-provider-detail"), refetch: vi.fn() },
    services: { data: undefined as SystemStatusResult | undefined, isError: false, isLoading: false, isFetching: false, error: new Error("secret-provider-detail"), refetch: vi.fn() },
    canConfigure: true,
}));
vi.mock("@/hooks/useInstanceHealth", () => ({ useInstanceHealth: () => state.health, findingCount: () => 0 }));
vi.mock("@/hooks/useUpdateState", () => ({ useUpdateState: () => ({ data: undefined }) }));
vi.mock("@/components/layout/UpdateDialog", () => ({ UpdateDialog: () => null }));
vi.mock("../InstanceHealthPanel", () => ({ InstanceFindings: () => <div>Retained findings</div> }));
vi.mock("@tanstack/react-query", () => ({ useQuery: () => state.services }));
vi.mock("@/hooks/useAdminPerm", () => ({ useAdminPerm: () => state.canConfigure }));
vi.mock("../MailStatusCard", () => ({ MailStatusCard: () => <span>Protected mail probe</span> }));

function render(page: "findings" | "services") {
    const html = renderToStaticMarkup(<MemoryRouter>{page === "findings" ? <FindingsTab /> : <ServicesTab />}</MemoryRouter>);
    return new DOMParser().parseFromString(html, "text/html").body.textContent;
}

beforeEach(() => {
    state.canConfigure = true;
    Object.assign(state.health, { data: { checks: [], summary: { error: 0, warning: 0, info: 0 }, execution_coverage: "unreported" }, isError: false, dataUpdatedAt: Date.now() });
    Object.assign(state.services, { data: { data: [], checked_at: new Date().toISOString() }, isError: false });
});

describe("configuration and service coverage", () => {
    it("does not mount the protected platform mail probe without manage-settings access", () => {
        state.canConfigure = false;
        expect(render("services")).toContain("Platform mail permission required");
        expect(render("services")).not.toContain("Protected mail probe");
    });
    it("does not equate absent findings or an older coverage-free response to healthy", () => {
        for (const coverage of [undefined, "unreported"]) {
            state.health.data!.execution_coverage = coverage;
            expect(render("findings")).toContain("Check execution coverage is not established");
            expect(render("findings")).not.toContain("No problems found");
            expect(render("findings")).toContain("Response received");
        }
    });

    it("keeps failed refresh explicit and redacts raw query errors", () => {
        state.health.isError = true;
        state.services.isError = true;
        for (const page of ["findings", "services"] as const) {
            expect(render(page)).toContain("refresh");
            expect(render(page)).not.toContain("secret-provider-detail");
            expect(render(page)).not.toContain("All systems operational");
        }
    });

    it("does not call an empty service list operational", () => {
        expect(render("services")).toContain("Current service coverage unknown");
        expect(render("services")).toContain("No components were returned");
    });

    it("scopes passed probes to reachability and hides connection errors", () => {
        state.services.data!.data = [{ name: "postgres", ok: true, latency_ms: 5 }, { name: "redis", ok: false, latency_ms: 20, error: "password=private secret-provider-detail" }];
        expect(render("services")).toContain("1 service probe failed");
        expect(render("services")).toContain("Probe passed");
        expect(render("services")).not.toContain("secret-provider-detail");
        state.services.data!.data = [{ name: "postgres", ok: true, latency_ms: 5 }];
        expect(render("services")).toContain("Reported service probes passed");
        expect(render("services")).toContain("does not prove send throughput");
    });

    it("does not turn stale probes or cached successful probes into a fresh verdict", () => {
        state.services.data!.data = [{ name: "postgres", ok: true, latency_ms: 5 }];
        state.services.data!.checked_at = new Date(Date.now() - 300_000).toISOString();
        expect(render("services")).toContain("Current service coverage unknown");
        expect(render("services")).toContain("Cached: passed");
        expect(render("services")).not.toContain("Probe passed");
    });

    it("retains a cached failure even when the earlier probe supplied no error details", () => {
        state.services.data!.data = [{ name: "postgres", ok: true, latency_ms: 5 }, { name: "redis", ok: false, latency_ms: 20 }];
        state.services.isError = true;
        expect(render("services")).toContain("Cached: passed");
        expect(render("services")).toContain("Cached: failed");
        expect(render("services")).not.toContain("Probe passed");
    });
});
