import { describe, expect, it } from "vitest";
import { EXTERNAL_WARMUP_MAX_TAGS, warmupTagProblem } from "./OutreachSettings";

describe("warmupTagProblem", () => {
    it("accepts a specific new tag", () => {
        expect(warmupTagProblem("  WRM-7Q2X ", ["lemwarm-abc"])).toBeNull();
        expect(warmupTagProblem("", [])).toBeNull();
    });

    it("refuses what the server refuses", () => {
        expect(warmupTagProblem("re", [])).toMatch(/at least 4 characters/);
        expect(warmupTagProblem("wrm-7q2x", ["WRM-7Q2X"])).toMatch(/already on the list/);
        const full = Array.from({ length: EXTERNAL_WARMUP_MAX_TAGS }, (_, i) => `tag-${i}-xx`);
        expect(warmupTagProblem("one-more", full)).toMatch(/At most/);
    });
});
