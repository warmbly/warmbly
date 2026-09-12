// Issue #432: the AI card was centred on the selection and clamped only to the
// viewport, so a selection at the left edge of a campaign body drew it outside
// the step drawer, over the flow canvas behind it.

import { describe, it, expect, beforeEach } from "vitest";
import { AI_CARD_WIDTH, clampCardLeft } from "./floatingBounds";

// A host box at [400, 1000] on a 1440px viewport: the campaign step drawer.
const host = (left: number, right: number): Element =>
    ({ getBoundingClientRect: () => ({ left, right, width: right - left }) }) as unknown as Element;

describe("clampCardLeft", () => {
    beforeEach(() => {
        Object.defineProperty(window, "innerWidth", { value: 1440, configurable: true });
    });

    it("centres the card on the selection when it fits", () => {
        expect(clampCardLeft(700, AI_CARD_WIDTH, host(400, 1000))).toBe(550);
    });

    it("keeps the card inside the panel when the selection sits at its left edge", () => {
        // Centred it would start at 410 - 150 = 260, which is 140px outside.
        expect(clampCardLeft(410, AI_CARD_WIDTH, host(400, 1000))).toBe(400);
    });

    it("keeps the card inside the panel at its right edge too", () => {
        expect(clampCardLeft(990, AI_CARD_WIDTH, host(400, 1000))).toBe(700);
    });

    it("falls back to the viewport for a host too narrow to hold the card", () => {
        expect(clampCardLeft(120, AI_CARD_WIDTH, host(100, 280))).toBe(8);
    });

    it("stays on screen when the panel runs past the viewport", () => {
        expect(clampCardLeft(1430, AI_CARD_WIDTH, host(1000, 1600))).toBe(1440 - AI_CARD_WIDTH - 8);
    });

    it("clamps to the viewport with no host at all", () => {
        expect(clampCardLeft(10, AI_CARD_WIDTH, null)).toBe(8);
    });
});
