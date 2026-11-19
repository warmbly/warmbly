import { render, screen } from "@testing-library/react";
import { useState, type ComponentProps, type ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { UserContext } from "@/hooks/context/user";
import { storeDisconnected, storeListable, type StoreState } from "@/lib/api/hooks/app/store/useStore";
import type * as StoreHooks from "@/lib/api/hooks/app/store/useStore";
import type { StoreStatus } from "@/lib/api/models/app/store/Store";
import { useAppStore } from "@/stores/useAppStore";
import DomainsPage from "./page";

const fixture = vi.hoisted(() => ({ store: { kind: "hidden" } as StoreState }));

vi.mock("@/hooks/useSearchParams", () => ({
    useSearchParams: () => {
        const [params, setParams] = useState(() => new URLSearchParams("tab=purchased"));
        return [params, (next: URLSearchParams | ((prev: URLSearchParams) => URLSearchParams)) => setParams((prev) => (typeof next === "function" ? next(prev) : next))];
    },
}));
vi.mock("@/lib/api/hooks/app/emails/useSendingDomains", () => ({ useSendingDomains: () => ({ data: { data: [] }, isLoading: false }) }));
vi.mock("@tanstack/react-router", () => ({ Link: ({ children }: { children: ReactNode }) => <a>{children}</a> }));
vi.mock("@/lib/api/hooks/app/store/useStore", async (original) => ({
    ...(await original<typeof StoreHooks>()),
    useStoreState: () => fixture.store,
    useStoreDomains: (enabled: boolean) => ({ data: enabled ? { data: [{ id: "d1", name: "getacme.com" }] } : undefined }),
}));
vi.mock("@/hooks/usePartnerManaged", () => ({ default: () => null }));
vi.mock("@/hooks/useWorkspaceWritable", () => ({ default: () => ({ writable: true, reason: undefined }), showWorkspaceReadOnly: () => {} }));
vi.mock("@/components/app/store/PurchasedDomains", () => ({ default: () => <section aria-label="Purchased domains" /> }));
vi.mock("@/components/app/emails/domains/RedirectRescue", () => ({ default: () => null }));
vi.mock("@/components/app/emails/domains/BulkSetupDialog", () => ({ default: () => null }));
vi.mock("@/components/app/emails/domains/DomainSelectionBar", () => ({ default: () => null }));
vi.mock("@/components/app/emails/domains/SendingDomainDrawer", () => ({ default: () => null }));

const locked = { enabled: false, reason: "not_configured", locked: true, can_configure: false } as StoreStatus;

function Page() {
    const profile = { user: { id: "account-user" } } as ComponentProps<typeof UserContext.Provider>["value"];
    return (
        <UserContext.Provider value={profile}>
            <DomainsPage />
        </UserContext.Provider>
    );
}

describe("the Purchased tab without GraphKit", () => {
    beforeEach(() => {
        sessionStorage.clear();
        useAppStore.setState({ currentOrganization: { id: "workspace", name: "Workspace", role: "owner" } });
    });

    it("stays for live purchases, under a paused notice", () => {
        fixture.store = { kind: "locked", status: { ...locked, disconnected: { mailboxes: 2, domains: 1 } } };
        expect(storeListable(fixture.store)).toBe(true);
        render(<Page />);
        expect(screen.getByRole("region", { name: "Purchased domains" })).toBeInTheDocument();
        expect(screen.getByRole("status")).toHaveTextContent("Renewals, monthly billing and Google mailbox connections are paused until your instance operator adds a GraphKit API key.");
    });

    it("is hidden when nothing was bought", () => {
        fixture.store = { kind: "locked", status: locked };
        expect(storeDisconnected(fixture.store)).toBeNull();
        render(<Page />);
        expect(screen.queryByRole("region", { name: "Purchased domains" })).not.toBeInTheDocument();
        expect(screen.queryByRole("status")).not.toBeInTheDocument();
    });
});
