import { describe, expect, it } from "vitest";
import { checkLocalPart } from "./addresses";
import { ADDRESS_PATTERNS, defaultPatterns, nameParts, patternAddress, patternsFromStyle, pickAddress, type AddressPattern } from "./patterns";

const matt = { first: "Matt", last: "Meszaros" };

describe("address patterns", () => {
    it.each<[AddressPattern, string]>([
        ["first", "matt"],
        ["first.l", "matt.m"],
        ["first.last", "matt.meszaros"],
        ["firstlast", "mattmeszaros"],
        ["first_last", "matt_meszaros"],
        ["first-last", "matt-meszaros"],
        ["f.last", "m.meszaros"],
        ["flast", "mmeszaros"],
        ["firstl", "mattm"],
        ["last", "meszaros"],
        ["last.first", "meszaros.matt"],
        ["lastf", "meszarosm"],
    ])("%s makes %s@", (p, want) => {
        expect(patternAddress(matt, p)).toBe(want);
    });

    it("covers every pattern in the catalog", () => {
        expect(new Set(ADDRESS_PATTERNS.map((p) => patternAddress(matt, p))).size).toBe(ADDRESS_PATTERNS.length);
    });

    it("makes nothing a pattern needs a missing name for", () => {
        expect(patternAddress({ first: "Matt", last: "" }, "first.last")).toBe("");
        expect(patternAddress({ first: "Matt", last: "" }, "first")).toBe("matt");
        expect(patternAddress({ first: "", last: "Meszaros" }, "first")).toBe("");
        expect(patternAddress({ first: "", last: "Meszaros" }, "last")).toBe("meszaros");
    });

    it("defaults to three addresses of one person, and first.last@ for a team", () => {
        expect(defaultPatterns(1).map((p) => patternAddress(matt, p))).toEqual(["matt", "matt.m", "matt.meszaros"]);
        expect(defaultPatterns(2)).toEqual(["first.last"]);
        expect(defaultPatterns(5)).toEqual(["first.last"]);
    });
});

describe("names", () => {
    it("transliterates accents and the letters NFKD keeps whole", () => {
        expect(patternAddress({ first: "José", last: "Müller" }, "first.last")).toBe("jose.muller");
        expect(patternAddress({ first: "Søren", last: "Ærø" }, "first.last")).toBe("soren.aero");
        expect(patternAddress({ first: "Łukasz", last: "Đorđević" }, "first.last")).toBe("lukasz.dordevic");
        expect(patternAddress({ first: "Þóra", last: "Groß" }, "firstlast")).toBe("thoragross");
        expect(patternAddress({ first: "Zoë", last: "O'Brien" }, "first.last")).toBe("zoe.obrien");
    });

    it("takes the first word of a first name, and joins or hyphenates a last name by pattern", () => {
        expect(nameParts({ first: "Mary Ann", last: "van der Berg" })).toEqual({ first: "mary", last: "vanderberg", lastHyphen: "vanderberg" });
        const jose = { first: "José Luis", last: "Núñez-Ortiz" };
        expect(patternAddress(jose, "first.last")).toBe("jose.nunez-ortiz");
        expect(patternAddress(jose, "first_last")).toBe("jose_nunez-ortiz");
        expect(patternAddress(jose, "last")).toBe("nunez-ortiz");
        expect(patternAddress(jose, "firstlast")).toBe("josenunezortiz");
        expect(patternAddress(jose, "first-last")).toBe("jose-nunezortiz");
        expect(patternAddress(jose, "flast")).toBe("jnunezortiz");
        expect(patternAddress({ first: "Jean-Luc", last: "Picard" }, "first")).toBe("jeanluc");
    });

    it("drops what cannot go in an address and keeps within 64 characters", () => {
        expect(patternAddress({ first: "李", last: "Wei" }, "first.last")).toBe("");
        expect(patternAddress({ first: "李", last: "Wei" }, "last")).toBe("wei");
        const long = { first: "A".repeat(40), last: "B".repeat(40) };
        const a = patternAddress(long, "first.last");
        expect(a.length).toBeLessThanOrEqual(64);
        expect(checkLocalPart(a)).toBeNull();
    });
});

describe("pickAddress", () => {
    it("goes through the chosen patterns, then the rest, never repeating a taken address", () => {
        expect(pickAddress(matt, ["flast"], new Set())).toEqual({ local_part: "mmeszaros", numbered: false });
        expect(pickAddress(matt, ["first"], new Set(["matt"])).local_part).toBe("matt.m");
    });

    it("falls back to role addresses when there is no name", () => {
        expect(pickAddress({ first: "", last: "" }, ["first"], new Set()).local_part).toBe("hello");
    });

    it("numbers addresses only once every pattern is taken, still valid", () => {
        const used = new Set<string>();
        for (let i = 0; i < 14; i++) used.add(pickAddress({ first: "Bo", last: "" }, ["first"], used).local_part);
        expect(used.size).toBe(14);
        expect([...used].every((a) => checkLocalPart(a) === null)).toBe(true);
        const all = new Set(ADDRESS_PATTERNS.map((p) => patternAddress(matt, p)));
        expect(pickAddress(matt, ["first"], all)).toEqual({ local_part: "matt2", numbered: true });
    });

    it("reads a saved single style as patterns, the old default as automatic", () => {
        expect(patternsFromStyle("first")).toBeNull();
        expect(patternsFromStyle("flast")).toEqual(["flast"]);
        expect(patternsFromStyle("nonsense")).toBeNull();
    });
});
