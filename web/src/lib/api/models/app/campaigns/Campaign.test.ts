import { describe, expect, it } from "vitest";
import { isValidCampaignDailyLimit } from "./Campaign";
import { DEFAULT_SENDING_BEHAVIOR } from "../emails/SendingBehavior";

describe("configurable sending limits", () => {
    it("recommends about thirty without imposing that as a maximum", () => {
        expect(DEFAULT_SENDING_BEHAVIOR.daily_limit_min).toBe(28);
        expect(DEFAULT_SENDING_BEHAVIOR.daily_limit_max).toBe(32);
        for (const value of [3, 7, 30, 32, 50, 200, 5000]) expect(isValidCampaignDailyLimit(value)).toBe(true);
        for (const value of [0, -1, 2, 30.5, 5001, Number.NaN]) expect(isValidCampaignDailyLimit(value)).toBe(false);
    });

    it("preserves an unchanged legacy unlimited value without allowing a new one", () => {
        expect(isValidCampaignDailyLimit(0, 0)).toBe(true);
        expect(isValidCampaignDailyLimit(-10, -10)).toBe(true);
        expect(isValidCampaignDailyLimit(0, 50)).toBe(false);
        expect(isValidCampaignDailyLimit(-10, 0)).toBe(false);
        expect(isValidCampaignDailyLimit(30, 0)).toBe(true);
    });
});
