import { act, type ReactNode } from "react";
import { createRoot, type Root } from "react-dom/client";
import { renderToStaticMarkup } from "react-dom/server";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ConfirmProvider } from "@/components/ConfirmDialog";
import type * as AgenciesAPI from "@/lib/api/client/admin/agencies";
import type { AdminAgency, AdminAgencyDetail } from "@/lib/api/client/admin/agencies";
import type { AdminPlan } from "@/lib/api/models/admin";
import { OrganizationManagedPlanCard } from "../OrganizationManagedPlanCard";
import AgenciesPage from "./AgenciesPage";
import { AgencyClientsPanel } from "./AgencyClientsPanel";
import { AgencyCreditCard } from "./AgencyCreditCard";
import { AgencyOverridesCard, AgencyOverridesDialog } from "./AgencyOverridesCard";
import { AgencyPlanCard, AgencyPlanDialog } from "./AgencyPlanCard";

const access = vi.hoisted(() => ({ canManage: true }));
vi.mock("@/hooks/useAdminPerm", () => ({ useAdminPerm: () => access.canManage }));

const api = vi.hoisted(() => ({ setOverrides: vi.fn() }));
vi.mock("@/lib/api/client/admin/agencies", async (original) => ({
    ...(await original<typeof AgenciesAPI>()),
    setAgencyOverrides: api.setOverrides,
}));

// The Radix select renders no options until it is opened; the list is what these tests read.
vi.mock("@/components/ui/select", () => ({
    Select: ({ children }: { children: ReactNode }) => <div>{children}</div>,
    SelectTrigger: ({ children }: { children: ReactNode }) => <div>{children}</div>,
    SelectValue: () => null,
    SelectContent: ({ children }: { children: ReactNode }) => <ul>{children}</ul>,
    SelectItem: ({ value, children }: { value: string; children: ReactNode }) => <li data-plan={value}>{children}</li>,
}));

const PLANS: AdminPlan[] = [
    { id: "grow", name: "Grow", public: true },
    { id: "partner", name: "Partnership", public: false, monthly_send_pool: null },
    { id: "agency", name: "Agency", public: true, monthly_send_pool: 250_000 },
    // Zero is still an agency plan: a pool with no cap.
    { id: "agency-scale", name: "Agency Scale", public: false, monthly_send_pool: 0 },
];

function agency(over: Partial<AdminAgency> = {}): AdminAgency {
    return {
        id: "agency-1",
        name: "Northwind",
        owner_user_id: "owner-1",
        owner_email: "owner@example.test",
        status: "incomplete",
        cancel_at_period_end: false,
        client_count: 1,
        has_billing: false,
        created_at: "2026-01-01T00:00:00Z",
        pool_used: 10,
        pool_total: 1000,
        unlimited: false,
        entitled: true,
        ...over,
    };
}

function detail(over: Partial<AdminAgencyDetail> = {}): AdminAgencyDetail {
    return {
        agency: agency(),
        members: [],
        clients: [{ organization_id: "org-1", name: "Acme", added_at: "2026-01-02T00:00:00Z" }],
        usage: {
            unlimited: false,
            period_start: "2026-01-01T00:00:00Z",
            period_end: "2026-02-01T00:00:00Z",
            days_left: 10,
            pool_size: 1000,
            topups: 0,
            total: 1000,
            used: 10,
            remaining: 990,
            projected: 30,
            clients: [],
        },
        ai_balance: { unmetered: false, allowance_usd: 50, allowance_left_usd: 50, purchased_usd: 0, spent_usd: 0, moved_usd: 0, balance_usd: 50 },
        ...over,
    };
}

function newClient() {
    const client = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
    client.setQueryData(["admin", "plans"], { plans: PLANS });
    return client;
}

function shell(client: QueryClient, ui: ReactNode) {
    return (
        <QueryClientProvider client={client}>
            <MemoryRouter>
                <ConfirmProvider>{ui}</ConfirmProvider>
            </MemoryRouter>
        </QueryClientProvider>
    );
}

function markup(ui: ReactNode) {
    const client = newClient();
    const html = renderToStaticMarkup(shell(client, ui));
    client.clear();
    return new DOMParser().parseFromString(html, "text/html");
}

function buttonNamed(scope: ParentNode, name: string) {
    return Array.from(scope.querySelectorAll("button")).find((b) => b.textContent?.trim() === name) ?? null;
}

function listedPlans() {
    return Array.from(document.querySelectorAll("[data-plan]")).map((li) => li.getAttribute("data-plan"));
}

function type(input: Element | null, value: string) {
    if (!(input instanceof HTMLInputElement)) throw new Error("Missing input");
    const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value")?.set;
    if (!setter) throw new Error("Missing input setter");
    setter.call(input, value);
    input.dispatchEvent(new Event("input", { bubbles: true }));
}

let root: Root;
let host: HTMLDivElement;
let client: QueryClient;
beforeEach(() => {
    vi.clearAllMocks();
    vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
    access.canManage = true;
    client = newClient();
    host = document.createElement("div");
    document.body.append(host);
    root = createRoot(host);
});
afterEach(async () => {
    await act(async () => root.unmount());
    client.clear();
    host.remove();
    vi.unstubAllGlobals();
});

describe("plan pickers", () => {
    it("lists only agency plans when giving an agency a plan", async () => {
        await act(async () => root.render(shell(client, <AgencyPlanDialog agency={agency()} open onOpenChange={() => {}} />)));
        expect(listedPlans()).toEqual(["agency", "agency-scale"]);
        expect(document.body.textContent).toContain("Agency (250,000 sends a month)");
        expect(document.body.textContent).toContain("Agency Scale (no pool cap, private)");
    });

    it("leaves agency plans out of the workspace grant and says where they are given", async () => {
        client.setQueryData(["admin", "organizations", "org-1", "managed-plan"], { managed: false, expired: false, plan_id: "" });
        await act(async () => root.render(shell(client, <OrganizationManagedPlanCard orgId="org-1" />)));
        await act(async () => buttonNamed(host, "Grant")?.click());
        expect(listedPlans()).toEqual(["grow", "partner"]);
        expect(host.textContent).toContain("Agency plans are given from Agencies.");
        expect(host.querySelector('a[href="/agencies"]')).not.toBeNull();
    });
});

describe("custom pool and AI allowance", () => {
    const stored = { monthly_send_pool: 500_000, ai_allowance_usd: 50, reason: "Scale deal" };

    async function open() {
        const onOpenChange = vi.fn();
        api.setOverrides.mockResolvedValue(detail());
        await act(async () =>
            root.render(shell(client, <AgencyOverridesDialog agency={agency({ overrides: stored })} open onOpenChange={onOpenChange} />)),
        );
        const form = document.querySelector("#agency-override-pool")?.closest("form");
        if (!form) throw new Error("Missing overrides form");
        const submit = () => act(async () => form.dispatchEvent(new Event("submit", { bubbles: true, cancelable: true })));
        return { onOpenChange, submit };
    }

    it("starts from the stored values and sends null for an emptied field", async () => {
        const { onOpenChange, submit } = await open();
        expect(document.querySelector<HTMLInputElement>("#agency-override-pool")?.value).toBe("500000");
        expect(document.querySelector<HTMLInputElement>("#agency-override-ai")?.value).toBe("50");
        await act(async () => type(document.querySelector("#agency-override-ai"), ""));
        await submit();
        expect(api.setOverrides).toHaveBeenCalledWith("agency-1", { monthly_send_pool: 500_000, ai_allowance_usd: null, reason: "Scale deal" });
        expect(onOpenChange).toHaveBeenCalledWith(false);
        // The answer is the agency as it now stands, so the page needs no refetch.
        expect(client.getQueryData(["admin", "agencies", "agency-1"])).toEqual(detail());
    });

    it("goes back to the plan's own values without a reason, and asks for one while a value is set", async () => {
        const { submit } = await open();
        await act(async () => type(document.querySelector("#agency-override-reason"), ""));
        await submit();
        expect(api.setOverrides).not.toHaveBeenCalled();
        await act(async () => {
            type(document.querySelector("#agency-override-pool"), "");
            type(document.querySelector("#agency-override-ai"), "");
        });
        await submit();
        expect(api.setOverrides).toHaveBeenCalledWith("agency-1", { monthly_send_pool: null, ai_allowance_usd: null, reason: "" });
    });

    it("refuses a pool outside the range before it reaches the server", async () => {
        const { submit } = await open();
        await act(async () => type(document.querySelector("#agency-override-pool"), "0"));
        await submit();
        expect(api.setOverrides).not.toHaveBeenCalled();
        expect(document.querySelector('[role="alert"]')?.textContent).toBe("A whole number from 1 to 100,000,000.");
    });

    it("shows the plan default for a value that is not set", () => {
        const plan = { id: "agency", name: "Agency", monthly_send_pool: 250_000, agency_ai_allowance_usd: 25 };
        const text = markup(<AgencyOverridesCard agency={agency({ plan, overrides: { monthly_send_pool: 900_000, reason: "Scale deal" } })} />).body
            .textContent;
        expect(text).toContain("900,000 sends");
        expect(text).toContain("Plan default ($25.00)");
    });
});

describe("operator controls", () => {
    const granted = agency({
        plan: { id: "agency", name: "Agency" },
        plan_id: "agency",
        status: "active",
        current_period_end: "2026-02-01T00:00:00Z",
        granted: { plan_id: "agency" },
        managed_plan: { plan_id: "agency", plan_name: "Agency", reason: "Design partner", granted_at: "2026-01-05T00:00:00Z", active: true },
    });

    it("says an operator's plan applies over a live subscription", () => {
        const text = markup(<AgencyPlanCard agency={granted} />).body.textContent;
        expect(text).toContain("Given by an operator");
        expect(text).toContain("Design partner");
        expect(text).toContain("No end date");
        expect(text).toContain("The operator's plan is the one that applies.");
    });

    it("shows a lapsed operator plan as lapsed and still lets it be ended", () => {
        const lapsed = agency({
            entitled: false,
            managed_plan: { plan_id: "agency", plan_name: "Agency", reason: "Trial", until: "2026-01-10T00:00:00Z", granted_at: "2026-01-05T00:00:00Z", active: false },
        });
        const document = markup(<AgencyPlanCard agency={lapsed} />);
        expect(document.body.textContent).toContain("lapsed");
        expect(buttonNamed(document, "End plan")).not.toBeNull();
        expect(buttonNamed(document, "Give a plan")).not.toBeNull();
    });

    it("hides every write from an admin who cannot manage organizations", () => {
        access.canManage = false;
        const document = markup(
            <>
                <AgencyPlanCard agency={granted} />
                <AgencyOverridesCard agency={granted} />
                <AgencyCreditCard detail={detail()} />
                <AgencyClientsPanel detail={detail()} />
            </>,
        );
        expect(document.querySelectorAll("button")).toHaveLength(0);
        expect(document.querySelector('a[href="/organizations/org-1"]')).not.toBeNull();
    });

    it("offers a release on each client and an attach action to managers", () => {
        const document = markup(<AgencyClientsPanel detail={detail()} />);
        expect(document.querySelector('button[aria-label="Release Acme"]')).not.toBeNull();
        expect(buttonNamed(document, "Attach workspace")).not.toBeNull();
    });

    it("offers AI balance only where the instance charges for AI", () => {
        expect(buttonNamed(markup(<AgencyCreditCard detail={detail()} />), "Add AI balance")).not.toBeNull();
        const unmetered = detail({ ai_balance: { ...detail().ai_balance!, unmetered: true } });
        const document = markup(<AgencyCreditCard detail={unmetered} />);
        expect(buttonNamed(document, "Add AI balance")).toBeNull();
        expect(buttonNamed(document, "Add sends")?.disabled).toBe(false);
    });

    it("does not offer sends to a pool with no cap", () => {
        const document = markup(<AgencyCreditCard detail={detail({ usage: { ...detail().usage, unlimited: true } })} />);
        expect(buttonNamed(document, "Add sends")?.disabled).toBe(true);
        expect(document.body.textContent).toContain("The pool has no cap");
    });
});

describe("agencies list", () => {
    function list(rows: AdminAgency[]) {
        const client = newClient();
        client.setQueryData(["admin", "agencies", "", 0], { data: rows, total: rows.length });
        const html = renderToStaticMarkup(shell(client, <AgenciesPage />));
        client.clear();
        return new DOMParser().parseFromString(html, "text/html");
    }

    it("marks a plan an operator gave and offers a new agency to managers", () => {
        const rows = [
            agency({ id: "a", name: "Alpha Co", plan: { id: "agency", name: "Agency" }, granted: { plan_id: "agency" } }),
            agency({ id: "b", name: "Beta Co", plan: { id: "agency", name: "Agency" }, plan_id: "agency", status: "active" }),
        ];
        const document = list(rows);
        const cells = Array.from(document.querySelectorAll("tbody tr")).map((tr) => tr.textContent ?? "");
        expect(cells.find((c) => c.includes("Alpha Co"))).toContain("Given");
        expect(cells.find((c) => c.includes("Beta Co"))).not.toContain("Given");
        expect(buttonNamed(document, "New agency")).not.toBeNull();
        access.canManage = false;
        expect(buttonNamed(list(rows), "New agency")).toBeNull();
    });
});
