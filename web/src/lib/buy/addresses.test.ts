import { describe, expect, it } from "vitest";
import { addressCandidates, checkLocalPart, checkLocalParts, forwardingProblem, normalizeForwarding, normalizeTyped, slug } from "./addresses";

describe("addressCandidates", () => {
    it("offers the natural forms of a name first", () => {
        expect(addressCandidates({ first: "Ada", last: "Lovelace" }).slice(0, 3)).toEqual(["ada", "ada.lovelace", "alovelace"]);
    });

    it("folds accents, ligatures and punctuation into plain ASCII", () => {
        expect(slug("Zoë O'Brien")).toBe("zoeobrien");
        expect(slug("Jürgen Groß")).toBe("jurgengross");
        expect(addressCandidates({ first: "José", last: "Núñez-Ortiz" }).slice(0, 2)).toEqual(["jose", "jose.nunezortiz"]);
    });
});

describe("checkLocalPart", () => {
    it.each([
        ["ada", null],
        ["ada.lovelace", null],
        ["a_b-c9", null],
        ["", "empty"],
        ["Ada", "invalid_chars"],
        ["ada lovelace", "invalid_chars"],
        [".ada", "edge"],
        ["ada-", "edge"],
        ["ada..l", "dots"],
        ["postmaster", "reserved"],
        ["a".repeat(65), "too_long"],
    ])("%s -> %s", (input, want) => {
        expect(checkLocalPart(input)).toBe(want);
    });

    it("marks the second of two equal addresses as the duplicate", () => {
        expect(checkLocalParts(["ada", "bob", "ada"])).toEqual([null, null, "duplicate"]);
    });
});

describe("typing and forwarding", () => {
    it("lowercases, turns spaces into dots and drops anything after an @", () => {
        expect(normalizeTyped("Ada Lovelace@acme.com")).toBe("ada.lovelace");
    });

    it("accepts an empty or bare-domain forward and refuses junk", () => {
        expect(forwardingProblem("")).toBeNull();
        expect(forwardingProblem("acme.com")).toBeNull();
        expect(forwardingProblem("https://acme.com/about")).toBeNull();
        expect(forwardingProblem("acme")).not.toBeNull();
        expect(forwardingProblem("ftp://acme.com")).not.toBeNull();
        expect(normalizeForwarding(" acme.com ")).toBe("https://acme.com");
        expect(normalizeForwarding("")).toBe("");
    });
});
