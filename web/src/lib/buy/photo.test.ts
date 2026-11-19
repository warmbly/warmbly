import { describe, expect, it } from "vitest";
import { centredCrop, clampCrop, cropSource, photoFileProblem, zoomCrop, PHOTO_MAX_ZOOM } from "./photo";

describe("photoFileProblem", () => {
    it("takes PNG and JPG only", () => {
        expect(photoFileProblem({ type: "image/png", size: 1000 })).toBeNull();
        expect(photoFileProblem({ type: "image/jpeg", size: 1000 })).toBeNull();
        expect(photoFileProblem({ type: "image/svg+xml", size: 1000 })).toMatch(/PNG or JPG/);
        expect(photoFileProblem({ type: "image/webp", size: 1000 })).toMatch(/PNG or JPG/);
    });
    it("refuses an empty or huge file", () => {
        expect(photoFileProblem({ type: "image/png", size: 0 })).toMatch(/empty/);
        expect(photoFileProblem({ type: "image/png", size: 40 * 1024 * 1024 })).toMatch(/too large/);
    });
});

describe("crop", () => {
    const view = 200;
    it("centres a wide photo and crops its middle square", () => {
        const c = centredCrop(400, 200, view);
        expect(c).toEqual({ zoom: 1, x: -100, y: 0 });
        expect(cropSource(c, 400, 200, view)).toEqual({ sx: 100, sy: 0, size: 200 });
    });
    it("never lets the photo leave a gap", () => {
        const c = clampCrop({ zoom: 1, x: 50, y: -500 }, 400, 200, view);
        expect(c.x).toBe(0);
        expect(c.y).toBe(0);
        const far = clampCrop({ zoom: 1, x: -10_000, y: 0 }, 400, 200, view);
        expect(far.x).toBe(-200);
    });
    it("zooms about the centre and stays inside the photo", () => {
        const c = zoomCrop(centredCrop(400, 400, view), 2, 400, 400, view);
        expect(c.zoom).toBe(2);
        expect(cropSource(c, 400, 400, view)).toEqual({ sx: 100, sy: 100, size: 200 });
        expect(zoomCrop(c, 99, 400, 400, view).zoom).toBe(PHOTO_MAX_ZOOM);
        expect(zoomCrop(c, 0.2, 400, 400, view).zoom).toBe(1);
    });
});
