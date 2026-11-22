import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type * as StoreAPI from "@/lib/api/client/admin/store";
import type { AdminStoreOrder } from "@/lib/api/client/admin/store";
import { RefundDialog } from "./RefundDialog";
import { dollarsToMicros, usd } from "./format";

const request = vi.hoisted(() => vi.fn());
vi.mock("@/lib/api/client", async (original) => ({ ...(await original<object>()), Request: request }));

const api = vi.hoisted(() => ({ refund: vi.fn() }));
vi.mock("@/lib/api/client/admin/store", async (original) => {
    const real = await original<typeof StoreAPI>();
    return { ...real, refundStoreOrder: api.refund };
});

vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

function order(over: Partial<AdminStoreOrder> = {}): AdminStoreOrder {
    return {
        id: "ord-1",
        organization_id: "org-1",
        organization_name: "Northwind",
        status: "completed",
        failure_code: "",
        due_now_micros: 20_000_000,
        charged_micros: 20_000_000,
        refunded_micros: 5_000_000,
        refunded_lines: [],
        placed: true,
        upstream_id: "ord_x",
        stuck: false,
        steps: [],
        lines: [{ type: "domain", domain: "northwind.com", years: 1, amount_micros: 20_000_000 }],
        created_by: null,
        created_at: "2026-11-01T00:00:00Z",
        updated_at: "2026-11-01T00:00:00Z",
        completed_at: "2026-11-01T00:00:00Z",
        ...over,
    };
}

function type(el: Element | null, value: string) {
    const proto = el instanceof HTMLTextAreaElement ? HTMLTextAreaElement.prototype : HTMLInputElement.prototype;
    Object.getOwnPropertyDescriptor(proto, "value")?.set?.call(el, value);
    el?.dispatchEvent(new Event("input", { bubbles: true }));
}

const button = (text: RegExp) => Array.from(document.querySelectorAll("button")).find((b) => text.test(b.textContent ?? "")) as HTMLButtonElement;

describe("store API client", () => {
    beforeEach(() => request.mockReset().mockResolvedValue({}));

    it("sends false auto-renew as a filter and drops unset ones", async () => {
        const { listStoreDomains } = await vi.importActual<typeof StoreAPI>("@/lib/api/client/admin/store");
        await listStoreDomains({ auto_renew: "false", renewal_failed: true, expiring_days: 30, q: "" });
        expect(request).toHaveBeenLastCalledWith({
            method: "GET",
            url: "/admin/store/domains?auto_renew=false&renewal_failed=true&expiring_days=30",
            authorization: true,
        });
    });

    it("sends the idempotency key on a refund", async () => {
        const { refundStoreOrder } = await vi.importActual<typeof StoreAPI>("@/lib/api/client/admin/store");
        await refundStoreOrder("ord-1", { amount_micros: 1_500_000, reason: "goodwill" }, "key-1");
        expect(request).toHaveBeenLastCalledWith({
            method: "POST",
            url: "/admin/store/orders/ord-1/refund",
            authorization: true,
            data: { amount_micros: 1_500_000, reason: "goodwill" },
            headers: { "Idempotency-Key": "key-1" },
        });
    });

    it("reads money as micros", () => {
        expect(dollarsToMicros("12.50")).toBe(12_500_000);
        expect(dollarsToMicros("$1,000")).toBe(1_000_000_000);
        expect(dollarsToMicros("1.234")).toBeNaN();
        expect(dollarsToMicros("")).toBeNull();
        expect(usd(1_500_000)).toBe("$1.50");
        expect(usd(1_234_500)).toBe("$1.2345");
    });
});

describe("refund dialog", () => {
    let root: Root;
    let host: HTMLDivElement;
    beforeEach(() => {
        vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
        api.refund.mockReset();
        host = document.createElement("div");
        document.body.append(host);
        root = createRoot(host);
    });
    afterEach(async () => {
        await act(async () => root.unmount());
        host.remove();
        vi.unstubAllGlobals();
    });

    async function render(o: AdminStoreOrder) {
        const client = new QueryClient({ defaultOptions: { mutations: { retry: 0 } } });
        await act(async () =>
            root.render(
                <QueryClientProvider client={client}>
                    <RefundDialog order={o} open onOpenChange={() => {}} onRefunded={() => {}} />
                </QueryClientProvider>,
            ),
        );
    }

    it("reuses the key when a failed refund is retried and takes a new one when the amount changes", async () => {
        api.refund.mockRejectedValueOnce(new Error("timeout")).mockResolvedValue({ order: order(), refunded_micros: 1_000_000 });
        await render(order());
        await act(async () => button(/An amount/).click());
        await act(async () => type(document.querySelector("#refund-amount"), "10"));
        await act(async () => type(document.querySelector("#refund-reason"), "goodwill"));
        await act(async () => button(/Refund \$10\.00/).click());
        await act(async () => button(/Refund \$10\.00/).click());
        expect(api.refund).toHaveBeenCalledTimes(2);
        const [first, second] = api.refund.mock.calls;
        expect(first[1]).toEqual({ amount_micros: 10_000_000, reason: "goodwill" });
        expect(second[2]).toBe(first[2]);

        await act(async () => type(document.querySelector("#refund-amount"), "11"));
        await act(async () => button(/Refund \$11\.00/).click());
        expect(api.refund.mock.calls[2][2]).not.toBe(first[2]);
    });

    it("refuses an amount above what is left to refund", async () => {
        await render(order());
        await act(async () => button(/An amount/).click());
        await act(async () => type(document.querySelector("#refund-amount"), "15.01"));
        await act(async () => type(document.querySelector("#refund-reason"), "goodwill"));
        expect(document.body.textContent).toContain("At most $15.00 is left to refund");
        expect(button(/^Refund/).disabled).toBe(true);
    });
});
