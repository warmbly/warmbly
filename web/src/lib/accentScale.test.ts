import { describe, expect, it } from "vitest";
import { STEPS, accentScale, contrastRatio, isHexColour, oklchCSS, oklchHex, relativeLuminance, stockScale } from "./accentScale";

// A spread of hues and lightnesses, with the awkward ends: near white, near black, greys and neons.
const ACCENTS = [
    "#0284c7", "#dc2626", "#f97316", "#facc15", "#fef08a", "#fffbeb", "#84cc16", "#16a34a", "#00ffff", "#14b8a6",
    "#1e3a8a", "#7c3aed", "#ff00ff", "#ec4899", "#0f172a", "#000000", "#ffffff", "#777777", "#f5f5f4", "#3f1d0b",
];

const WHITE = "#ffffff";
const stock = stockScale();
const STOCK_600 = contrastRatio(stock.light[600], WHITE);
// Rounding a colour to CSS digits moves its contrast by a hair.
const SLACK = 0.03;

describe("accentScale", () => {
    it("refuses anything that is not #rrggbb", () => {
        for (const bad of ["", "red", "#fff", "#12345g", "0284c7", "#0284c7ff", "url(x)", undefined, null]) {
            expect(accentScale(bad as string)).toBeNull();
        }
        expect(isHexColour("#0284C7")).toBe(true);
    });

    it("keeps white text on the 600 fill at least as readable as stock sky, whatever the accent", () => {
        expect(STOCK_600).toBeGreaterThan(3.9);
        for (const hex of ACCENTS) {
            const scale = accentScale(hex)!;
            expect(contrastRatio(scale.light[600], WHITE), `light 600 for ${hex}`).toBeGreaterThanOrEqual(STOCK_600 - SLACK);
            expect(contrastRatio(scale.solid[600], WHITE), `solid 600 for ${hex}`).toBeGreaterThanOrEqual(
                contrastRatio(stock.solid[600], WHITE) - SLACK,
            );
        }
    });

    it("never turns a pale accent into a pale button", () => {
        for (const hex of ["#fef08a", "#fffbeb", "#ffffff", "#f5f5f4"]) {
            const fill = accentScale(hex)!.light[600];
            expect(relativeLuminance(fill)).toBeLessThan(0.23);
            expect(contrastRatio(fill, WHITE)).toBeGreaterThan(3.9);
        }
    });

    it("holds every step to the stock step's luminance, so each pairing reads as it does in sky", () => {
        for (const hex of ACCENTS) {
            const scale = accentScale(hex)!;
            for (const step of STEPS) {
                const mine = relativeLuminance(scale.light[step]);
                const sky = relativeLuminance(stock.light[step]);
                // A dark accent may deepen 500 and up, never lighten them.
                expect(mine, `${hex} ${step}`).toBeLessThanOrEqual(sky + 0.004);
                if (step < 500) expect(mine, `${hex} ${step}`).toBeGreaterThanOrEqual(sky - 0.004);
            }
            // The tint text sits on keeps its contrast with the text shade.
            expect(contrastRatio(scale.light[700], scale.light[50]), hex).toBeGreaterThanOrEqual(
                contrastRatio(stock.light[700], stock.light[50]) - SLACK,
            );
        }
    });

    it("runs from light to dark in the light theme", () => {
        for (const hex of ACCENTS) {
            const scale = accentScale(hex)!.light;
            for (let i = 1; i < STEPS.length; i++) {
                expect(scale[STEPS[i]].l, `${hex} ${STEPS[i]}`).toBeLessThan(scale[STEPS[i - 1]].l);
            }
        }
    });

    it("carries the accent's hue, and no hue at all for a grey", () => {
        const red = accentScale("#dc2626")!;
        for (const step of STEPS) {
            expect(red.light[step].h).toBeCloseTo(red.light[600].h, 5);
            expect(red.dark[step].h).toBeCloseTo(red.light[600].h, 5);
        }
        expect(red.light[600].h).toBeGreaterThan(20);
        expect(red.light[600].h).toBeLessThan(35);
        for (const grey of ["#777777", "#ffffff", "#000000"]) {
            const scale = accentScale(grey)!;
            for (const step of STEPS) {
                expect(scale.light[step].c).toBe(0);
                expect(scale.dark[step].c).toBe(0);
            }
        }
    });

    it("reproduces sky closely when given sky", () => {
        const scale = accentScale("#0284c7")!;
        expect(oklchHex(scale.light[600])).toBe("#0284c7");
        for (const step of STEPS) {
            expect(Math.abs(scale.light[step].l - stock.light[step].l)).toBeLessThan(0.02);
        }
    });

    it("mirrors the dark theme's formula: translucent washes for the tints and light shades for the text", () => {
        for (const hex of ACCENTS) {
            const { dark } = accentScale(hex)!;
            for (const step of STEPS) {
                expect(dark[step].l).toBe(stock.dark[step].l);
                expect(dark[step].alpha).toBe(stock.dark[step].alpha);
            }
            expect(dark[50].alpha).toBeLessThan(0.2);
            expect(dark[600].alpha).toBe(1);
            // Accent text on the dark page.
            expect(contrastRatio(dark[600], "#111214"), hex).toBeGreaterThan(6);
        }
    });

    it("lets a dark accent deepen the fills, within a limit", () => {
        const navy = accentScale("#0f172a")!;
        expect(contrastRatio(navy.light[600], WHITE)).toBeGreaterThan(6);
        // Still a colour a button can be told apart by, not black.
        expect(relativeLuminance(navy.light[600])).toBeGreaterThan(0.07);
        expect(relativeLuminance(navy.light[50])).toBeCloseTo(relativeLuminance(stock.light[50]), 2);
    });

    it("stays inside sRGB", () => {
        for (const hex of ACCENTS) {
            const scale = accentScale(hex)!;
            for (const set of [scale.light, scale.dark, scale.solid]) {
                for (const step of STEPS) expect(oklchHex(set[step])).toMatch(/^#[0-9a-f]{6}$/);
            }
        }
    });
});

describe("oklchCSS", () => {
    it("writes numbers and nothing else", () => {
        for (const hex of ACCENTS) {
            const scale = accentScale(hex)!;
            for (const set of [scale.light, scale.dark, scale.solid]) {
                for (const step of STEPS) {
                    expect(oklchCSS(set[step])).toMatch(/^oklch\(\d+(\.\d+)?% \d+(\.\d+)? \d+(\.\d+)?( \/ 0\.\d+)?\)$/);
                }
            }
        }
    });
    it("adds the opacity only when there is one", () => {
        expect(oklchCSS({ l: 0.5, c: 0.1, h: 240, alpha: 1 })).toBe("oklch(50% 0.1 240)");
        expect(oklchCSS({ l: 0.68, c: 0.14, h: 240, alpha: 0.12 })).toBe("oklch(68% 0.14 240 / 0.12)");
    });
});
