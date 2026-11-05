import { describe, expect, it } from "vitest";
import { nextSlotLabel } from "./nextSlot";

describe("nextSlotLabel", () => {
    const now = new Date("2026-11-04T15:58:00Z");

    it("reads the slot on the mailbox's clock and names a zone that is not the browser's", () => {
        const browser = Intl.DateTimeFormat().resolvedOptions().timeZone;
        const zone = browser === "Asia/Tokyo" ? "Europe/London" : "Asia/Tokyo";
        const at = "2026-11-04T16:10:00Z";
        const clock = new Date(at).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit", timeZone: zone, timeZoneName: "short" });
        expect(nextSlotLabel(at, now, zone)).toBe(clock);
        // 20:00 UTC on the 5th is the next local day in London and in Tokyo.
        expect(nextSlotLabel("2026-11-05T20:00:00Z", now, zone)).toMatch(/^tomorrow /);
    });

    it("keeps the plain label when the mailbox runs on the browser's clock", () => {
        const browser = Intl.DateTimeFormat().resolvedOptions().timeZone;
        expect(nextSlotLabel("2026-11-04T16:10:00Z", now, browser)).toBe(nextSlotLabel("2026-11-04T16:10:00Z", now));
    });

    it("falls back to the browser's clock for an unknown zone", () => {
        expect(nextSlotLabel("2026-11-04T16:10:00Z", now, "Not/AZone")).toBe(nextSlotLabel("2026-11-04T16:10:00Z", now));
    });
});
