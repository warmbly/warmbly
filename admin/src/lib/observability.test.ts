import { describe, expect, it } from "vitest";
import type { CaptureResult } from "posthog-js";
import { dropBrowserNoise } from "./observability";

const thrown = (type: string, value: string, filenames: string[] = []) => ({
    event: "$exception",
    properties: {
        $exception_list: [{
            type,
            value,
            mechanism: { handled: true, synthetic: false },
            stacktrace: { type: "raw", frames: filenames.map((filename) => ({ platform: "web:javascript", filename })) },
        }],
    },
}) as unknown as CaptureResult;

describe("dropBrowserNoise", () => {
    it.each([
        ["SessionExpiredError", "Session expired", ["https://admin.warmbly.com/assets/index-abc.js"]],
        ["ReauthCancelledError", "Confirmation cancelled", ["https://admin.warmbly.com/assets/index-abc.js"]],
        ["TypeError", "undefined is not an object (evaluating 'window.ethereum.selectedAddress = undefined')", ["https://admin.warmbly.com/"]],
        ["Error", "Could not establish connection. Receiving end does not exist.", []],
        ["TypeError", "x is undefined", ["chrome-extension://abcdef/inject.js"]],
    ])("drops %s: %s", (type, value, filenames) => {
        expect(dropBrowserNoise(thrown(type, value, filenames))).toBeNull();
    });

    it("keeps a real error from the bundle", () => {
        const event = thrown("TypeError", "Cannot read properties of undefined (reading 'id')", ["https://admin.warmbly.com/assets/index-abc.js"]);
        expect(dropBrowserNoise(event)).toBe(event);
    });

    it("keeps an injected-looking message that passed through the bundle", () => {
        const event = thrown("ReferenceError", "Can't find variable: CONFIG", ["https://admin.warmbly.com/assets/index-abc.js"]);
        expect(dropBrowserNoise(event)).toBe(event);
    });
});
