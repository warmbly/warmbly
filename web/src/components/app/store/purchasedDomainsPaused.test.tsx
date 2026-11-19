import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ConfirmContext } from "@/hooks/context/confirm";
import type * as StoreHooks from "@/lib/api/hooks/app/store/useStore";
import type { StoreState } from "@/lib/api/hooks/app/store/useStore";
import type { StoreDomain, StoreStatus } from "@/lib/api/models/app/store/Store";
import { useAppStore } from "@/stores";
import PurchasedDomains from "./PurchasedDomains";

const bought = {
    id: "d1",
    name: "getacme.com",
    source: "purchased",
    status: "active",
    auto_renew: true,
    renewable: true,
    renew_micros: 11_170_000,
    expires_at: "2027-11-16T00:00:00Z",
    mailbox_count: 2,
    forwarding_url: "https://acme.test",
    forwarding_status: "active",
    mail_auth: { mx: "ok", spf: "ok", dkim: "ok", dmarc: "ok" },
    dns: [{ id: "r1", type: "TXT", name: "@", content: "v=spf1", ttl: 3600, locked: false }],
} as unknown as StoreDomain;

const status = {
    enabled: false,
    configured: false,
    reason: "not_configured",
    locked: true,
    can_configure: false,
    metered: true,
    currency: "usd",
    mailbox_monthly_micros: 3_000_000,
    domain_markup: "at_cost",
    limits: { mailboxes_per_domain_max: 5, mailboxes_per_domain_recommended: 3 },
    providers: ["google"],
    balance: null,
    registrant: null,
    plan_active: true,
} as StoreStatus;

const fixture = vi.hoisted(() => ({ store: { kind: "hidden" } as StoreState, detailIds: [] as (string | null)[] }));

vi.mock("@tanstack/react-router", () => ({ Link: ({ children }: { children: ReactNode }) => <a>{children}</a>, useNavigate: () => () => {} }));
vi.mock("@/lib/api/hooks/auth/useAuthConfig", () => ({ default: () => ({ config: { billing_enabled: true } }) }));
vi.mock("@/lib/api/hooks/app/store/useStore", async (original) => {
    const idle = () => ({ mutate: vi.fn(), mutateAsync: vi.fn(), isPending: false, isError: false });
    return {
        ...(await original<typeof StoreHooks>()),
        useStoreState: () => fixture.store,
        useStoreStatus: () => ({ data: fixture.store.kind === "hidden" ? undefined : fixture.store.status }),
        useStoreDomains: () => ({ data: { data: [bought] }, isPending: false, isError: false }),
        useStoreDomain: (id: string | null) => {
            fixture.detailIds.push(id);
            return { data: id ? bought : undefined, isPending: !id };
        },
        useStoreMailboxes: () => ({ data: { data: [] } }),
        usePatchStoreDomain: idle,
        useRenewStoreDomain: idle,
        useTransferOutStoreDomain: idle,
        useRemoveStoreDomain: idle,
        useCheckDnsRecords: idle,
        useDeleteDnsRecord: idle,
        useCheckStoreDomainConnection: idle,
        useRegenerateStoreDomainConnection: idle,
    };
});

const confirm = { show: vi.fn(), setLoading: vi.fn(), setShow: vi.fn() };

function mount() {
    render(
        <QueryClientProvider client={new QueryClient()}>
            <ConfirmContext.Provider value={confirm}>
                <PurchasedDomains query="" />
            </ConfirmContext.Provider>
        </QueryClientProvider>,
    );
}

const openDrawer = () => {
    fireEvent.click(screen.getByText("getacme.com"));
    return screen.getByRole("dialog", { name: "getacme.com" });
};

beforeEach(() => {
    fixture.detailIds = [];
    useAppStore.setState({ currentOrganization: { id: "workspace", name: "Workspace", role: "owner" } });
});
afterEach(cleanup);

describe("purchased domains while GraphKit is disconnected", () => {
    it("offers no renew, auto-renew, forward, DNS or transfer action, only the paused notice", () => {
        fixture.store = { kind: "locked", status: { ...status, disconnected: { mailboxes: 2, domains: 1 } } };
        mount();
        expect(screen.queryByRole("switch", { name: "Auto-renew getacme.com" })).not.toBeInTheDocument();
        fireEvent.click(screen.getByRole("button", { name: "Actions for getacme.com" }));
        expect(screen.getByText("Details")).toBeInTheDocument();
        expect(screen.queryByText("Renew now")).not.toBeInTheDocument();
        expect(screen.queryByText("Transfer out…")).not.toBeInTheDocument();
        fireEvent.keyDown(document, { key: "Escape" });

        const drawer = openDrawer();
        expect(within(drawer).getByRole("status")).toHaveTextContent("Renewals, monthly billing and Google mailbox connections are paused");
        for (const name of [/Renew now/, /Transfer out/, /Add record/, /Check records/, /^Save$/]) {
            expect(within(drawer).queryByRole("button", { name })).not.toBeInTheDocument();
        }
        expect(within(drawer).queryByLabelText("Renew automatically")).not.toBeInTheDocument();
        expect(within(drawer).queryByPlaceholderText("https://yourcompany.com")).not.toBeInTheDocument();
        // The record list needs GraphKit, so the drawer does not ask for it.
        expect(fixture.detailIds.every((id) => id === null)).toBe(true);
        expect(within(drawer).getByText("Mail authentication")).toBeInTheDocument();
    });

    it("offers every action while the store is on", () => {
        fixture.store = { kind: "on", status: { ...status, enabled: true, configured: true, reason: undefined } };
        mount();
        expect(screen.getByRole("switch", { name: "Auto-renew getacme.com" })).toBeInTheDocument();
        fireEvent.click(screen.getByRole("button", { name: "Actions for getacme.com" }));
        expect(screen.getByText("Renew now")).toBeInTheDocument();
        expect(screen.getByText("Transfer out…")).toBeInTheDocument();
        fireEvent.keyDown(document, { key: "Escape" });

        const drawer = openDrawer();
        expect(within(drawer).queryByRole("status")).not.toBeInTheDocument();
        for (const name of [/Renew now/, /Transfer out/, /Add record/, /Check records/, /^Save$/]) {
            expect(within(drawer).getByRole("button", { name })).toBeInTheDocument();
        }
        expect(within(drawer).getByLabelText("Renew automatically")).toBeInTheDocument();
        expect(fixture.detailIds).toContain("d1");
    });
});
