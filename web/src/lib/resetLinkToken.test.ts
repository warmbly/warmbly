import { describe, expect, it } from "vitest";
import { resetLinkToken } from "./resetLinkToken";

describe("resetLinkToken", () => {
    it("reads the token from the fragment", () => {
        expect(resetLinkToken("#session=a.b.c")).toBe("a.b.c");
    });

    it("is empty without a token in the fragment", () => {
        expect(resetLinkToken("#other=1")).toBe("");
        expect(resetLinkToken("")).toBe("");
    });
});
