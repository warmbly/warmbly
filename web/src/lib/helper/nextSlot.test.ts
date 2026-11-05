import { describe, expect, it } from "vitest";
import { nextSlotLabel } from "./nextSlot";

describe("nextSlotLabel", () => {
    const now = new Date("2026-11-04T15:58:00Z");

    it("reads the slot on the mailbox's clock and names a zone that is not the browser's", () => {
        const at = "2026-11-04T16:10:00Z";
        const clock = (z?: string) => new Date(at).toLocaleString("en-US", { timeZone: z });
        const zone = clock("Asia/Tokyo") === clock() ? "Europe/London" : "Asia/Tokyo";
        const expected = new Date(at).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit", timeZone: zone, timeZoneName: "short" });
        expect(nextSlotLabel(at, now, zone)).toBe(expected);
        // 20:00 UTC on the 5th is the next local day in London and in Tokyo.
        expect(nextSlotLabel("2026-11-05T20:00:00Z", now, zone)).toMatch(/^tomorrow /);
    });

    it("keeps the plain label when the mailbox runs on the browser's clock", () => {
        const browser = Intl.DateTimeFormat().resolvedOptions().timeZone;
        expect(nextSlotLabel("2026-11-04T16:10:00Z", now, browser)).toBe(nextSlotLabel("2026-11-04T16:10:00Z", now));
    });

    it("reads a zone the browser cannot load on UTC, as the scheduler does", () => {
        expect(nextSlotLabel("2026-11-04T16:10:00Z", now, "Not/AZone")).toBe(nextSlotLabel("2026-11-04T16:10:00Z", now, "UTC"));
    });
});
