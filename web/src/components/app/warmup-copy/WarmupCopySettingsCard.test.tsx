import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import WarmupCopySettingsCard from "./WarmupCopySettingsCard";

const api = vi.hoisted(() => ({ get: vi.fn(), put: vi.fn() }));
vi.mock("@/lib/api/client/app/warmup/warmupCopy", () => ({ getWarmupCopySettings: api.get, putWarmupCopySettings: api.put }));

describe("WarmupCopySettingsCard", () => {
    beforeEach(() => {
        api.get.mockReset().mockResolvedValue({ template_share: 25, templates_only: false, campaign_copy: true });
        api.put.mockReset().mockImplementation(async (s) => s);
    });
    afterEach(cleanup);

    const draw = () =>
        render(
            <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
                <WarmupCopySettingsCard />
            </QueryClientProvider>,
        );

    it("sends every field when one toggle changes", async () => {
        draw();
        fireEvent.click(await screen.findByRole("switch", { name: "Warm up running campaigns" }));
        await waitFor(() => expect(api.put).toHaveBeenCalledWith({ template_share: 25, templates_only: false, campaign_copy: false }));
    });

    it("shows the change before the server answers and restores it on refusal", async () => {
        let refuse: (e: unknown) => void = () => {};
        api.put.mockReturnValue(new Promise((_, reject) => (refuse = reject)));
        draw();
        const only = await screen.findByRole("switch", { name: "Only send my own copy" });
        fireEvent.click(only);
        await waitFor(() => expect(only).toHaveAttribute("aria-checked", "true"));
        refuse({ status: 500, error: "Internal", message: "nope" });
        await waitFor(() => expect(only).toHaveAttribute("aria-checked", "false"));
    });
});
