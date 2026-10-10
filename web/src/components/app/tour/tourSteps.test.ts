import { describe, expect, it } from "vitest";
import { TOUR_CHAPTERS, TOUR_SLIDES } from "./tourSteps";

describe("tour slides", () => {
    it("opens on the welcome and ends on the plans", () => {
        expect(TOUR_SLIDES[0].scene).toBe("welcome");
        expect(TOUR_SLIDES[TOUR_SLIDES.length - 1].scene).toBe("plans");
    });

    it("points sidebar slides at dashboard routes", () => {
        for (const slide of TOUR_SLIDES) {
            if (slide.anchor && slide.anchor !== "remie") expect(slide.anchor).toMatch(/^\/app\//);
        }
    });

    it("has no duplicate chapters or slides", () => {
        expect(new Set(TOUR_CHAPTERS.map((c) => c.id)).size).toBe(TOUR_CHAPTERS.length);
        const keys = TOUR_SLIDES.map((s) => `${s.chapter.id}.${s.id}`);
        expect(new Set(keys).size).toBe(keys.length);
    });

    it("flattens chapters in order with their positions", () => {
        expect(TOUR_SLIDES).toHaveLength(TOUR_CHAPTERS.reduce((n, c) => n + c.slides.length, 0));
        for (const slide of TOUR_SLIDES) expect(TOUR_CHAPTERS[slide.ci].slides[slide.si].id).toBe(slide.id);
    });

    it("ends on the three plan slides", () => {
        expect(TOUR_SLIDES.filter((s) => s.chapter.id === "plans").map((s) => s.scene)).toEqual(["planIncluded", "planCompare", "plans"]);
    });
});
