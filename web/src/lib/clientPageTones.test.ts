import { describe, expect, it } from "vitest";
import { contrastRatio } from "./accentScale";
import { CLIENT_SURFACES, clientPageTones, mixHex } from "./clientPageTones";

const ACCENTS = ["#0284c7", "#dc2626", "#fef08a", "#ffffff", "#000000", "#16a34a", "#7c3aed", "#facc15", "#0f172a"];

describe("clientPageTones", () => {
    it("keeps the text on a button readable for any accent", () => {
        for (const accent of ACCENTS) {
            for (const mode of ["light", "dark"] as const) {
                const tones = clientPageTones(accent, mode);
                expect(contrastRatio(tones.accent, tones.onAccent), `${accent} ${mode}`).toBeGreaterThanOrEqual(4.5);
            }
        }
    });
    it("keeps accent text readable on the page and the card", () => {
        for (const accent of ACCENTS) {
            for (const mode of ["light", "dark"] as const) {
                const tones = clientPageTones(accent, mode);
                const surface = CLIENT_SURFACES[mode];
                expect(contrastRatio(tones.link, surface.page), `${accent} ${mode}`).toBeGreaterThanOrEqual(4.5);
                expect(contrastRatio(tones.link, surface.card), `${accent} ${mode}`).toBeGreaterThanOrEqual(4.5);
            }
        }
    });
    it("falls back to the default accent for anything that is not a colour", () => {
        expect(clientPageTones("blue", "light")).toEqual(clientPageTones("#0284c7", "light"));
        expect(clientPageTones(undefined, "dark")).toEqual(clientPageTones("#0284c7", "dark"));
    });
    it("mixes two colours by share", () => {
        expect(mixHex("#000000", "#ffffff", 0.5)).toBe("#808080");
        expect(mixHex("#102030", "#ffffff", 0)).toBe("#102030");
    });
});
