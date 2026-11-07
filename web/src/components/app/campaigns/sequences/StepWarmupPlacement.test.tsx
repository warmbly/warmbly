import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import StepWarmupPlacement from "./StepWarmupPlacement";

const api = vi.hoisted(() => ({ get: vi.fn() }));
vi.mock("@/lib/api/client/app/campaigns/getCampaignWarmupPlacement", () => ({ default: api.get }));

const group = (g: string, reported: number, inbox: number, spam: number) => ({ group: g, sent: reported, reported, inbox, tabs: 0, spam, rescued: 0 });

describe("StepWarmupPlacement", () => {
    beforeEach(() => api.get.mockReset());
    afterEach(cleanup);

    const draw = (armName: (k: string) => string | undefined = (k) => (k === "original" ? "Original" : "Variant B")) =>
        render(
            <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
                <StepWarmupPlacement campaignId="c1" stepId="s1" armName={armName} />
            </QueryClientProvider>,
        );

    it("stays hidden until an arm has enough receipts to read", async () => {
        api.get.mockResolvedValue({ days: 30, data: [{ sequence_id: "s1", variant_id: null, placement: { sent: 30, reported: 19, inbox: 10, tabs: 0, spam: 9, rescued: 0 }, by_group: [group("google", 19, 10, 9)] }] });
        const { container } = draw();
        await vi.waitFor(() => expect(api.get).toHaveBeenCalled());
        await new Promise((r) => setTimeout(r, 0));
        expect(container.textContent).toBe("");
    });

    it("shows each arm's headline and business inbox split for this step only", async () => {
        api.get.mockResolvedValue({
            days: 30,
            data: [
                { sequence_id: "s1", variant_id: null, placement: { sent: 20, reported: 20, inbox: 18, tabs: 0, spam: 2, rescued: 2 }, by_group: [group("google", 10, 10, 0), group("microsoft", 10, 8, 2)] },
                { sequence_id: "s1", variant_id: "v1", placement: { sent: 25, reported: 25, inbox: 25, tabs: 0, spam: 0, rescued: 0 }, by_group: [group("google", 21, 21, 0), group("microsoft", 4, 4, 0)] },
                { sequence_id: "s2", variant_id: null, placement: { sent: 29, reported: 29, inbox: 0, tabs: 0, spam: 29, rescued: 0 }, by_group: [] },
            ],
        });
        draw();
        expect(await screen.findByText("Warmup placement")).toBeTruthy();
        expect(screen.getByText("Original")).toBeTruthy();
        expect(screen.getByText("Variant B")).toBeTruthy();
        expect(screen.getByText("90%")).toBeTruthy();
        expect(screen.getByText("80%")).toBeTruthy();
        expect(screen.getByText("20 checked")).toBeTruthy();
        expect(screen.getByText("25 checked")).toBeTruthy();
        expect(screen.queryByText("29 checked")).toBeNull();
        // Microsoft 365 shows for the Original's ten receipts and not for Variant B's four.
        expect(screen.getAllByText(/Microsoft 365/)).toHaveLength(2);
    });

    it("drops a row for an arm no longer on the step", async () => {
        api.get.mockResolvedValue({ days: 30, data: [{ sequence_id: "s1", variant_id: "gone", placement: { sent: 25, reported: 25, inbox: 25, tabs: 0, spam: 0, rescued: 0 }, by_group: [] }] });
        const { container } = draw((k) => (k === "original" ? "Original" : undefined));
        await vi.waitFor(() => expect(api.get).toHaveBeenCalled());
        await new Promise((r) => setTimeout(r, 0));
        expect(container.textContent).toBe("");
    });
});
