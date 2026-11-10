import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { THEME_DEFAULT_STORAGE_KEY, THEME_STORAGE_KEY, onDefaultThemeChange, readStoredTheme, setDefaultTheme, storedChoice } from "./theme";

// setupTests replaces localStorage with spies; these tests give it a memory.
const store = new Map<string, string>();
const storage = window.localStorage as unknown as Record<"getItem" | "setItem" | "removeItem", ReturnType<typeof vi.fn>>;

beforeEach(() => {
    store.clear();
    storage.getItem.mockImplementation((k: string) => store.get(k) ?? null);
    storage.setItem.mockImplementation((k: string, v: string) => void store.set(k, v));
    storage.removeItem.mockImplementation((k: string) => void store.delete(k));
});

afterEach(() => {
    store.clear();
    setDefaultTheme(null);
});

const isDark = () => document.documentElement.classList.contains("dark");

describe("a brand's default theme", () => {
    it("applies to a viewer who has not chosen", () => {
        expect(readStoredTheme()).toBe("light");
        setDefaultTheme("dark");
        expect(readStoredTheme()).toBe("dark");
        expect(storedChoice()).toBeNull();
        expect(isDark()).toBe(true);
        setDefaultTheme(null);
        expect(readStoredTheme()).toBe("light");
        expect(isDark()).toBe(false);
    });

    it("never overrides the viewer's own choice", () => {
        store.set(THEME_STORAGE_KEY, "light");
        setDefaultTheme("dark");
        expect(readStoredTheme()).toBe("light");
        expect(isDark()).toBe(false);
    });

    it("is remembered for the next first paint only when asked", () => {
        setDefaultTheme("dark");
        expect(store.has(THEME_DEFAULT_STORAGE_KEY)).toBe(false);
        setDefaultTheme("dark", true);
        expect(store.get(THEME_DEFAULT_STORAGE_KEY)).toBe("dark");
        setDefaultTheme(null, true);
        expect(store.has(THEME_DEFAULT_STORAGE_KEY)).toBe(false);
    });

    it("tells followers when it changes, and nobody when it does not", () => {
        const heard = vi.fn();
        const stop = onDefaultThemeChange(heard);
        setDefaultTheme("dark");
        setDefaultTheme("dark");
        expect(heard).toHaveBeenCalledTimes(1);
        store.set(THEME_STORAGE_KEY, "dark");
        setDefaultTheme(null);
        expect(heard).toHaveBeenCalledTimes(1);
        stop();
    });
});
