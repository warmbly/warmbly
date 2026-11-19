import { describe, expect, it } from "vitest";
import { clampFund, DEFAULT_FUND_USD, FUND_PRESETS_USD, lastsDays, lastsLabel, minFundUsd, nextPresetAbove, ordinal, stepFund } from "./funds";

describe("adding funds", () => {
    it("starts on $100 among six presets", () => {
        expect(FUND_PRESETS_USD).toEqual([100, 250, 500, 1000, 2500, 5000]);
        expect(DEFAULT_FUND_USD).toBe(100);
    });

    it("takes the store's minimum card charge as the floor, $10 when it does not say", () => {
        expect(minFundUsd(undefined)).toBe(10);
        expect(minFundUsd(0)).toBe(10);
        expect(minFundUsd(25_000_000)).toBe(25);
        // Never below what a top-up accepts.
        expect(minFundUsd(500_000)).toBe(5);
    });

    it("steps by $10 on the grid and holds typed amounts inside the bounds", () => {
        expect(stepFund(100, 1, 10)).toBe(110);
        expect(stepFund(105, 1, 10)).toBe(110);
        expect(stepFund(105, -1, 10)).toBe(100);
        expect(stepFund(10, -1, 10)).toBe(10);
        expect(stepFund(5000, 1, 10)).toBe(5000);
        expect(clampFund(3, 10)).toBe(10);
        expect(clampFund(99_999, 10)).toBe(5000);
        expect(clampFund(Number.NaN, 10)).toBe(10);
        expect(clampFund(42.6, 10)).toBe(43);
    });

    it("offers the next preset above a card charge, and none past the largest", () => {
        expect(nextPresetAbove(10)).toBe(100);
        expect(nextPresetAbove(100)).toBe(250);
        expect(nextPresetAbove(6000)).toBeNull();
    });

    it("says how long a balance lasts at the daily spend", () => {
        expect(lastsDays(27, 3)).toBe(9);
        expect(lastsLabel(lastsDays(27, 3))).toBe("Lasts ~9 days");
        expect(lastsLabel(lastsDays(1, 3))).toBe("Runs out today");
        expect(lastsLabel(lastsDays(3, 3))).toBe("Lasts ~1 day");
        expect(lastsLabel(lastsDays(5000, 1))).toBe("Lasts over a year");
        // Nothing spends from it: it lasts, and no day count is made up.
        expect(lastsDays(50, 0)).toBeNull();
        expect(lastsLabel(null)).toBe("Nothing is drawing on it yet");
    });

    it("names the billing day", () => {
        expect([1, 2, 3, 4, 11, 12, 13, 21, 22, 23, 28].map(ordinal)).toEqual(["1st", "2nd", "3rd", "4th", "11th", "12th", "13th", "21st", "22nd", "23rd", "28th"]);
    });
});
