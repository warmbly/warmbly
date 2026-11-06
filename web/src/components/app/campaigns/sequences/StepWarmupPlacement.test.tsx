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

    it("stays hidden until a recipient files a receipt", async () => {
        api.get.mockResolvedValue({ days: 30, data: [{ sequence_id: "s1", variant_id: null, placement: { sent: 4, reported: 0, inbox: 0, tabs: 0, spam: 0, rescued: 0 }, by_group: [] }] });
        const { container } = draw();
        await vi.waitFor(() => expect(api.get).toHaveBeenCalled());
        await new Promise((r) => setTimeout(r, 0));
        expect(container.textContent).toBe("");
    });

    it("shows each arm's headline and provider split for this step only", async () => {
        api.get.mockResolvedValue({
            days: 30,
            data: [
                { sequence_id: "s1", variant_id: null, placement: { sent: 20, reported: 20, inbox: 18, tabs: 0, spam: 2, rescued: 2 }, by_group: [group("google", 10, 10, 0), group("microsoft", 10, 8, 2), group("other", 3, 1, 2)] },
                { sequence_id: "s1", variant_id: "v1", placement: { sent: 10, reported: 10, inbox: 10, tabs: 0, spam: 0, rescued: 0 }, by_group: [group("google", 10, 10, 0)] },
                { sequence_id: "s2", variant_id: null, placement: { sent: 9, reported: 9, inbox: 0, tabs: 0, spam: 9, rescued: 0 }, by_group: [] },
            ],
        });
        draw();
        expect(await screen.findByText("Warmup placement")).toBeTruthy();
        expect(screen.getByText("Original")).toBeTruthy();
        expect(screen.getByText("Variant B")).toBeTruthy();
        expect(screen.getByText("90%")).toBeTruthy();
        expect(screen.getByText("80%")).toBeTruthy();
        expect(screen.getByText("20 checked")).toBeTruthy();
        expect(screen.queryByText("9 checked")).toBeNull();
        expect(screen.queryByText(/Other/)).toBeNull();
    });

    it("drops a row for an arm no longer on the step", async () => {
        api.get.mockResolvedValue({ days: 30, data: [{ sequence_id: "s1", variant_id: "gone", placement: { sent: 5, reported: 5, inbox: 5, tabs: 0, spam: 0, rescued: 0 }, by_group: [] }] });
        const { container } = draw((k) => (k === "original" ? "Original" : undefined));
        await vi.waitFor(() => expect(api.get).toHaveBeenCalled());
        await new Promise((r) => setTimeout(r, 0));
        expect(container.textContent).toBe("");
    });
});
