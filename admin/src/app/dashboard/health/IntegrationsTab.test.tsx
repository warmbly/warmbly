import { renderToStaticMarkup } from "react-dom/server";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { StoreConnection } from "@/lib/api/client/admin/store";
import { StoreConnectionCard } from "./IntegrationsTab";

const state = vi.hoisted(() => ({ conn: undefined as StoreConnection | undefined }));
vi.mock("@tanstack/react-query", () => ({
    useQuery: () => ({ data: state.conn, isLoading: false, isError: false, refetch: vi.fn() }),
    useMutation: () => ({ mutate: vi.fn(), isPending: false }),
    useQueryClient: () => ({}),
}));
vi.mock("@/components/ConfirmDialog", () => ({ useConfirm: () => vi.fn() }));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

function conn(over: Partial<StoreConnection> = {}): StoreConnection {
    return {
        source: "settings",
        configured: true,
        mode: "live",
        api_url: "https://gk.example/v1/infrastructure",
        default_api_url: "https://api.graphkit.com/v1/infrastructure",
        api_key_set: true,
        webhook_secret_set: false,
        webhook_url: "https://api.example/v1/webhooks/graphkit",
        sealing_available: true,
        updated_at: "2026-11-01T00:00:00Z",
        updated_by: "user-1",
        purchases_without_connection: null,
        ...over,
    };
}

function render() {
    const html = renderToStaticMarkup(<StoreConnectionCard />);
    return new DOMParser().parseFromString(html, "text/html");
}

const buttons = (doc: Document) => [...doc.querySelectorAll("button")].map((b) => b.textContent?.trim());

beforeEach(() => {
    state.conn = conn();
});

describe("GraphKit connection card", () => {
    it("renders an environment connection read-only", () => {
        state.conn = conn({ source: "environment" });
        const doc = render();
        expect(doc.body.textContent).toContain("Configured by the environment");
        expect(doc.body.textContent).toContain("https://gk.example/v1/infrastructure");
        expect(doc.querySelector("input[type=password]")).toBeNull();
        expect(doc.querySelector("#graphkit-url")).toBeNull();
        expect(buttons(doc)).not.toContain("Save");
        expect(buttons(doc)).not.toContain("Disconnect");
    });

    it("offers Disconnect for stored settings and starts secrets empty", () => {
        const doc = render();
        expect(buttons(doc)).toContain("Disconnect");
        const secrets = [...doc.querySelectorAll<HTMLInputElement>("input[type=password]")];
        expect(secrets).toHaveLength(2);
        for (const s of secrets) expect(s.value).toBe("");
        expect(doc.querySelector<HTMLInputElement>("#graphkit-url")?.value).toBe("https://gk.example/v1/infrastructure");
        expect(doc.body.textContent).toContain("Not set");
    });

    it("blocks saving without the credentials key and reports unconnected purchases", () => {
        state.conn = conn({ source: "none", configured: false, api_key_set: false, sealing_available: false, purchases_without_connection: { mailboxes: 3, domains: 1 } });
        const doc = render();
        expect(doc.body.textContent).toContain("CREDENTIALS_ENCRYPTION_KEY must be set");
        expect(doc.body.textContent).toContain("3 mailboxes and 1 domain were bought here");
        const save = [...doc.querySelectorAll("button")].find((b) => b.textContent?.includes("Save"));
        expect(save?.hasAttribute("disabled")).toBe(true);
        expect(buttons(doc)).not.toContain("Disconnect");
    });
});
