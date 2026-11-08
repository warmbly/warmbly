import { afterEach, describe, expect, it, vi } from "vitest";

vi.mock("@/lib/env", () => ({
    POSTHOG_KEY: "phc_test",
    POSTHOG_HOST: "https://posthog.example.test",
    POSTHOG_UI_HOST: "https://posthog.example.test",
    POSTHOG_ERROR_TRACKING: true,
    POSTHOG_SESSION_REPLAY: false,
    SENTRY_DSN: "",
    SENTRY_ENVIRONMENT: "test",
    SENTRY_RELEASE: "",
}));

// What a content blocker does to the SDK chunk.
vi.mock("posthog-js", () => {
    throw new Error("blocked by client");
});

describe("a configured backend that never loads", () => {
    afterEach(() => vi.restoreAllMocks());

    it("hands what it held to the console and stops claiming to report", async () => {
        const log = vi.spyOn(console, "error").mockImplementation(() => {});
        const { captureException, initErrorReporting, reportsErrors } = await import("./observability");

        initErrorReporting();
        expect(reportsErrors()).toBe(true);

        const failure = new Error("render failed while the SDK loaded");
        captureException(failure);

        await vi.waitFor(() => expect(reportsErrors()).toBe(false));
        expect(log).toHaveBeenCalledWith(failure);
    });
});
