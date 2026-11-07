import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { isModuleLoadError } from "./moduleLoadError";

describe("module-load recovery", () => {
    it.each([
        "Failed to fetch dynamically imported module: https://app.warmbly.com/assets/layout.js",
        "error loading dynamically imported module: https://app.warmbly.com/assets/layout.js",
        "Importing a module script failed.",
        `'text/html' is not a valid JavaScript MIME type for module script '${window.location.origin}/assets/layout-aTnJVElG.js'.`,
    ])("recognizes module load failures: %s", (message) => {
        expect(isModuleLoadError(new TypeError(message))).toBe(true);
    });

    it.each([
        new SyntaxError("Unterminated regular expression literal '/'"),
        new SyntaxError("Unexpected end of script"),
        new Error("Failed to fetch dynamically imported module"),
        new TypeError("Failed to fetch"),
        new TypeError("Cannot read properties of undefined"),
        new TypeError("'text/html' is not a valid JavaScript MIME type for module script 'https://other.example/assets/layout.js'."),
        new TypeError(`'text/html' is not a valid JavaScript MIME type for module script '${window.location.origin}/api/script.js'.`),
    ])("does not reload unrelated errors: %s", (error) => {
        expect(isModuleLoadError(error)).toBe(false);
    });
});

describe("stale chunk reload", () => {
    const KEY = "warmbly:stale-chunk-reload-at";
    const stale = () => new TypeError("Failed to fetch dynamically imported module: https://app.warmbly.com/assets/layout-abc.js");
    // A fresh module per test: the in-flight reload flag is module state.
    const load = () => import("./moduleLoadError");

    beforeEach(() => {
        vi.resetModules();
        window.sessionStorage.clear();
        vi.useFakeTimers();
        vi.setSystemTime(10_000_000);
        // jsdom cannot navigate and says so on the console; the stamp is what this asserts.
        vi.spyOn(console, "error").mockImplementation(() => {});
    });

    afterEach(() => {
        vi.useRealTimers();
        vi.restoreAllMocks();
    });

    it("reloads once and keeps answering true while the reload is under way", async () => {
        const mod = await load();
        expect(mod.reloadForStaleChunk(stale())).toBe(true);
        expect(window.sessionStorage.getItem(KEY)).toBe("10000000");
        vi.setSystemTime(10_001_000);
        expect(mod.reloadForStaleChunk(stale())).toBe(true);
        expect(window.sessionStorage.getItem(KEY)).toBe("10000000");
    });

    it("does not reload again when the chunk still fails right after a reload", async () => {
        window.sessionStorage.setItem(KEY, String(10_000_000 - 5_000));
        const mod = await load();
        expect(mod.canReloadForStaleChunk()).toBe(false);
        expect(mod.reloadForStaleChunk(stale())).toBe(false);
    });

    it("recovers again from a later deploy", async () => {
        window.sessionStorage.setItem(KEY, String(10_000_000 - 10 * 60_000));
        const mod = await load();
        expect(mod.reloadForStaleChunk(stale())).toBe(true);
        expect(window.sessionStorage.getItem(KEY)).toBe("10000000");
    });

    it("ignores errors that are not a missing chunk", async () => {
        const mod = await load();
        expect(mod.reloadForStaleChunk(new TypeError("Cannot read properties of undefined"))).toBe(false);
        expect(window.sessionStorage.getItem(KEY)).toBeNull();
    });
});
