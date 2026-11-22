import { describe, expect, it } from "vitest";
import { andBits, anyBits, clearBits, hasBits, orBits, toggleBits } from "./bitmask";

const AGENCY_MIGRATE = 2 ** 31;
const READ_STORE = 2 ** 32;
const MANAGE_STORE = 2 ** 33;
const READ_EMAILS = 1;

describe("bitmask", () => {
    it("reads bits past 32 that the native operators drop", () => {
        const mask = READ_EMAILS + READ_STORE + MANAGE_STORE;
        // The bug this replaces: (mask & READ_STORE) is 0 in JavaScript.
        expect(mask & READ_STORE).toBe(0);
        expect(hasBits(mask, READ_STORE)).toBe(true);
        expect(hasBits(mask, MANAGE_STORE)).toBe(true);
        expect(hasBits(mask, AGENCY_MIGRATE)).toBe(false);
        expect(hasBits(AGENCY_MIGRATE, AGENCY_MIGRATE)).toBe(true);
        expect(anyBits(mask, READ_STORE + AGENCY_MIGRATE)).toBe(true);
        expect(anyBits(mask, AGENCY_MIGRATE)).toBe(false);
    });

    it("hasBits needs every bit, anyBits one", () => {
        expect(hasBits(READ_EMAILS + READ_STORE, READ_EMAILS + MANAGE_STORE)).toBe(false);
        expect(anyBits(READ_EMAILS + READ_STORE, READ_EMAILS + MANAGE_STORE)).toBe(true);
        expect(hasBits(0, 0)).toBe(true);
        expect(anyBits(0, 0)).toBe(false);
    });

    it("combines and removes high bits without losing the rest", () => {
        const all = orBits(READ_EMAILS, AGENCY_MIGRATE, READ_STORE, MANAGE_STORE);
        expect(all).toBe(1 + 2 ** 31 + 2 ** 32 + 2 ** 33);
        expect(clearBits(all, READ_STORE)).toBe(1 + 2 ** 31 + 2 ** 33);
        expect(clearBits(all, AGENCY_MIGRATE)).toBe(1 + 2 ** 32 + 2 ** 33);
        expect(andBits(all, READ_STORE + 2)).toBe(READ_STORE);
        expect(orBits()).toBe(0);
    });

    it("toggles a permission on and off", () => {
        const on = toggleBits(READ_EMAILS, MANAGE_STORE);
        expect(on).toBe(READ_EMAILS + MANAGE_STORE);
        expect(toggleBits(on, MANAGE_STORE)).toBe(READ_EMAILS);
    });

    it("treats a missing mask as empty", () => {
        expect(hasBits(undefined as unknown as number, READ_STORE)).toBe(false);
        expect(orBits(NaN, READ_STORE)).toBe(READ_STORE);
    });
});
