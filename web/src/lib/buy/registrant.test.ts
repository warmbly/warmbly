import { describe, expect, it } from "vitest";
import { blankRegistrant, normalizePhone, registrantComplete, registrantGaps, registrantProblems } from "./registrant";

const full = blankRegistrant({
    first_name: "Ada",
    last_name: "Lovelace",
    organization: "Acme Inc",
    email: "ada@acme.com",
    phone: "+1.4155550100",
    address1: "1 Market St",
    city: "San Francisco",
    state: "CA",
    postal_code: "94105",
    country: "US",
});

describe("registrant", () => {
    it("accepts a complete registrant and asks for nothing", () => {
        expect(registrantComplete(full)).toBe(true);
        expect(registrantGaps(full)).toEqual([]);
    });

    it("asks only for what a billing-details prefill left out, plus what the registry refused, in form order", () => {
        const prefill = { ...full, phone: "", address1: "", postal_code: "" };
        expect(registrantGaps(prefill)).toEqual(["phone", "address1", "postal_code"]);
        expect(registrantGaps(full, ["city"])).toEqual(["city"]);
        // An optional field is never asked for unless the registry refused it.
        expect(registrantGaps({ ...full, organization: "" })).toEqual([]);
    });

    it("names each missing or malformed field", () => {
        const p = registrantProblems({ ...full, city: "", phone: "415 555 0100", country: "usa", email: "ada" });
        expect(Object.keys(p).sort()).toEqual(["city", "country", "email", "phone"]);
        expect(registrantComplete(null)).toBe(false);
    });

    it("rewrites a loosely typed international number into the registry form", () => {
        expect(normalizePhone("+1 (415) 555-0100")).toBe("+1.4155550100");
        expect(normalizePhone("+44 20 7946 0958")).toBe("+44.2079460958");
        expect(normalizePhone("+1.4155550100")).toBe("+1.4155550100");
        expect(normalizePhone("4155550100")).toBe("4155550100");
    });
});
