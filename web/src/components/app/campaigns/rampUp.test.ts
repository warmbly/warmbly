import { describe, expect, it } from "vitest";
import { rampUpInvalid, rampUpMode, rampUpPayload, type RampUp } from "./rampUp";

const base: RampUp = { warmup_graduation: true, ramp_enabled: false, ramp_start: 10, ramp_increment: 5, ramp_ceiling: 50 };

describe("ramp-up", () => {
    it("reads the mode off the two server fields", () => {
        expect(rampUpMode(base)).toBe("replies");
        expect(rampUpMode({ ...base, warmup_graduation: false })).toBe("full");
        expect(rampUpMode({ ...base, warmup_graduation: false, ramp_enabled: true })).toBe("schedule");
        // A campaign with both on shows its schedule; the picker notes the reply hold.
        expect(rampUpMode({ ...base, ramp_enabled: true })).toBe("schedule");
    });

    it("sends the schedule numbers only while the schedule is on", () => {
        const stale = { ...base, ramp_start: 80, ramp_ceiling: 20 };
        expect(rampUpPayload(stale)).toEqual({ warmup_graduation: true, ramp_enabled: false });
        expect(rampUpInvalid(stale)).toBeNull();
        const on = { ...stale, ramp_enabled: true, warmup_graduation: false };
        expect(rampUpPayload(on)).toEqual(on);
        expect(rampUpInvalid(on)).not.toBeNull();
    });
});
