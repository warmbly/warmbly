import { fireEvent, render, screen } from "@testing-library/react";
import type { ComponentProps, ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { UserContext } from "@/hooks/context/user";
import { useAppStore } from "@/stores/useAppStore";
import WebhooksPage from "./page";

const api = vi.hoisted(() => ({
    catalog: [{ type: "contact.created", category: "contacts", description: "Contact created", firehose: false }],
    endpoints: ["one", "two"].map((id) => ({ id, url: `https://${id}.example/webhook`, description: "", event_types: [], enabled: true, consecutive_failures: 0, ownership_confirmed: true, created_at: new Date(), updated_at: new Date() })),
    deliveries: { data: { data: [], pagination: { next_cursor: null, has_more: false } } },
    query: vi.fn(),
}));
vi.mock("@/hooks/usePermission", () => ({ usePermission: () => true }));
vi.mock("@/hooks/context/confirm", () => ({ useConfirm: () => ({ show: vi.fn() }) }));
vi.mock("@/lib/api/hooks/app/webhooks/useWebhooks", () => ({
    useWebhooks: () => ({ data: { endpoints: api.endpoints, event_types: api.catalog } }),
    useWebhookDrops: () => ({ data: { drops: [] } }),
    useWebhookDeliveries: (query: unknown) => { api.query(query); return api.deliveries; },
    useCreateWebhook: () => ({}), useUpdateWebhook: () => ({}), useDeleteWebhook: () => ({}),
    useRotateWebhookSecret: () => ({}), useVerifyWebhook: () => ({}), useRedeliverDelivery: () => ({}),
}));

function Owner({ children }: { children: ReactNode }) {
    const value = { user: { id: "user" } } as ComponentProps<typeof UserContext.Provider>["value"];
    return <UserContext.Provider value={value}>{children}</UserContext.Provider>;
}
const open = (id: string) => fireEvent.click(screen.getByRole("button", { name: new RegExp(`https://${id}.example/webhook`) }));
const select = (index: number, value: string) => fireEvent.change(screen.getAllByRole("combobox")[index], { target: { value } });

describe("webhook delivery browsing", () => {
    beforeEach(() => {
        sessionStorage.clear();
        vi.clearAllMocks();
        api.catalog = [{ type: "contact.created", category: "contacts", description: "Contact created", firehose: false }];
        useAppStore.setState({ currentOrganization: { id: "workspace", name: "Workspace", role: "owner" } });
    });

    it("restores the tab and delivery filters only after reopening the drawer, and remembers clearing", () => {
        const page = render(<WebhooksPage />, { wrapper: Owner });
        open("one");
        fireEvent.click(screen.getByRole("button", { name: "Deliveries" }));
        select(0, "failed");
        select(1, "contact.created");
        page.unmount();
        api.query.mockClear();
        const refreshed = render(<WebhooksPage />, { wrapper: Owner });
        expect(screen.queryByRole("button", { name: "Close" })).not.toBeInTheDocument();
        expect(api.query).not.toHaveBeenCalled();
        open("one");
        expect(screen.getAllByRole("combobox")[0]).toHaveValue("failed");
        expect(screen.getAllByRole("combobox")[1]).toHaveValue("contact.created");
        expect(api.query).toHaveBeenCalledWith({ endpointId: "one", status: "failed", eventType: "contact.created", limit: 25 });
        select(0, "");
        select(1, "");
        refreshed.unmount();
        render(<WebhooksPage />, { wrapper: Owner });
        open("one");
        expect(screen.getAllByRole("combobox")[0]).toHaveValue("");
        expect(screen.getAllByRole("combobox")[1]).toHaveValue("");
    });

    it("isolates endpoint histories and keeps a removable event filter while the catalog is empty", () => {
        const page = render(<WebhooksPage />, { wrapper: Owner });
        open("one");
        fireEvent.click(screen.getByRole("button", { name: "Deliveries" }));
        select(0, "failed");
        select(1, "contact.created");
        fireEvent.click(screen.getByRole("button", { name: "Close" }));
        open("two");
        expect(screen.queryByRole("combobox")).not.toBeInTheDocument();
        fireEvent.click(screen.getByRole("button", { name: "Deliveries" }));
        expect(screen.getAllByRole("combobox")[0]).toHaveValue("");
        expect(screen.getAllByRole("combobox")[1]).toHaveValue("");
        fireEvent.click(screen.getByRole("button", { name: "Close" }));
        api.catalog = [];
        page.rerender(<WebhooksPage />);
        open("one");
        expect(screen.getAllByRole("combobox")[1]).toHaveValue("contact.created");
        expect(screen.getByRole("option", { name: "contact.created (unavailable)" })).toBeInTheDocument();
        select(1, "");
        expect(screen.getAllByRole("combobox")[1]).toHaveValue("");
    });
});
