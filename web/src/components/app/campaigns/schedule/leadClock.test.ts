import { describe, expect, it } from "vitest";
import { inWindow, localTime, opensOnTheirClock, windowOnTheirClock, zoneLabel, zoneOffset } from "./leadClock";

// Mon–Fri 09:00–17:00.
const weekdays = Array.from({ length: 7 }, (_, i) => (i < 5 ? [{ start: 540, end: 1020 }] : []));
// Wednesday 11 Nov 2026, 15:00 UTC: 10:00 New York, 16:00 Berlin, 00:00 Thursday Tokyo.
const at = new Date(Date.UTC(2026, 10, 11, 15, 0));

describe("leadClock", () => {
    it("reads the window on each lead's own clock", () => {
        expect(inWindow(weekdays, "America/New_York", at)).toBe(true);
        expect(inWindow(weekdays, "Europe/Berlin", at)).toBe(true);
        expect(inWindow(weekdays, "Asia/Tokyo", at)).toBe(false);
    });

    it("shows where the next campaign-clock window lands for a lead", () => {
        expect(windowOnTheirClock(weekdays, "America/New_York", "Asia/Tokyo", at)).toBe("Wed 23:00–Thu 07:00");
        expect(windowOnTheirClock(weekdays, "America/New_York", "America/New_York", at)).toBe("Wed 09:00–17:00");
        // Sunday 8 Nov 2026: the next window is Monday's.
        const sunday = new Date(Date.UTC(2026, 10, 8, 15, 0));
        expect(windowOnTheirClock(weekdays, "America/New_York", "Europe/Berlin", sunday)).toBe("Mon 15:00–23:00");
        expect(windowOnTheirClock(Array.from({ length: 7 }, () => []), "America/New_York", "Asia/Tokyo", at)).toBeNull();
    });

    it("says when a closed lead's window next opens on their clock", () => {
        // 00:00 Thursday in Tokyo: today's window is ahead.
        expect(opensOnTheirClock(weekdays, "Asia/Tokyo", at)).toBe("Thu 09:00");
        // 20:00 Friday in Berlin: the next window is Monday's, not the one that just ended.
        expect(opensOnTheirClock(weekdays, "Europe/Berlin", new Date(Date.UTC(2026, 10, 13, 19, 0)))).toBe("Mon 09:00");
    });

    it("places a window on a daylight-saving day at its wall time", () => {
        // Sunday 8 March 2026: New York springs forward at 02:00.
        const dstDay = Array.from({ length: 7 }, (_, i) => (i === 6 ? [{ start: 210, end: 600 }] : []));
        const saturday = new Date(Date.UTC(2026, 2, 7, 20, 0));
        expect(opensOnTheirClock(dstDay, "America/New_York", saturday)).toBe("Sun 03:30");
    });

    it("reads offsets and labels", () => {
        expect(zoneOffset("Asia/Kolkata", at)).toBe(330);
        expect(zoneOffset("UTC", at)).toBe(0);
        expect(localTime("Europe/Berlin", at)).toBe("16:00");
        expect(zoneLabel("America/New_York")).toBe("New York");
        expect(zoneLabel("Etc/GMT-2")).toBe("UTC+2");
    });
});
