import { describe, expect, it } from "vitest";
import { connectErrorMessage, isPlanRequired, refusedRegistrantFields, repricedQuote, storeErrorMessage } from "./errors";

describe("storeErrorMessage", () => {
    it("speaks Warmbly's words for a known code, whatever the server said", () => {
        expect(storeErrorMessage({ status: 409, code: "price_changed", message: "upstream said something", error: "Conflict" })).toBe(
            "Prices updated since you started. Review the new total.",
        );
        expect(storeErrorMessage({ status: 503, code: "store_unavailable", message: "raw", error: "x", request_id: "req_1" })).toBe(
            "The store is taking a moment. Try again shortly.",
        );
    });

    it("never shows an unknown code's raw message, and quotes the request id", () => {
        const msg = storeErrorMessage({ status: 500, code: "internal", message: "dial tcp 10.0.0.1", error: "x", request_id: "req_1" });
        expect(msg).toBe("That did not go through. Try again in a moment. (req_1)");
    });

    it("tells a dropped connection apart", () => {
        expect(storeErrorMessage({ error: "Network Error", message: "Please check your connection." })).toMatch(/offline/);
    });

    it("reads a closed sign-in prompt as nothing changed", () => {
        expect(storeErrorMessage(new Error("cancelled"))).toMatch(/^Nothing changed/);
    });
});

describe("repricedQuote", () => {
    const quote = { id: "q2", due_now_micros: 12_000_000, lines: [] };

    it("reads the fresh quote from details.quote", () => {
        expect(repricedQuote({ status: 409, code: "price_changed", error: "x", message: "x", details: { quote } })?.id).toBe("q2");
    });

    it("refuses anything else", () => {
        expect(repricedQuote({ status: 409, code: "price_changed", error: "x", message: "x", details: quote })).toBeNull();
        expect(repricedQuote({ status: 409, code: "price_changed", error: "x", message: "x" })).toBeNull();
    });
});

describe("refusedRegistrantFields", () => {
    it("maps details.fields onto the form, dropping names it does not have", () => {
        const err = { status: 400, code: "contact_invalid", error: "x", message: "x", details: { fields: ["phone", "country", "fax"] } };
        expect(refusedRegistrantFields(err)).toEqual(["phone", "country"]);
    });

    it("falls back to the comma list in reason", () => {
        expect(refusedRegistrantFields({ status: 400, code: "contact_invalid", error: "x", message: "x", reason: "email,postal_code" })).toEqual(["email", "postal_code"]);
    });

    it("is empty for another code", () => {
        expect(refusedRegistrantFields({ status: 409, code: "price_changed", error: "x", message: "x", details: { fields: ["phone"] } })).toEqual([]);
    });
});

describe("plan and connect errors", () => {
    it("recognises the plan refusal", () => {
        expect(isPlanRequired({ status: 402, code: "plan_required", error: "x", message: "x" })).toBe(true);
        expect(isPlanRequired({ status: 402, code: "insufficient_balance", error: "x", message: "x" })).toBe(false);
        expect(storeErrorMessage({ status: 402, code: "plan_required", error: "x", message: "raw" })).toMatch(/needs a Warmbly plan/);
    });

    it("explains why a domain cannot be connected from details.reason", () => {
        const err = (reason?: string) => ({ status: 409, code: "domain_unavailable", error: "x", message: "x", reason });
        expect(connectErrorMessage(err("already_added"))).toMatch(/already in this workspace/);
        expect(connectErrorMessage(err("connected_elsewhere"))).toMatch(/Another workspace/);
        expect(connectErrorMessage(err("something_new"))).toMatch(/cannot be added/);
        expect(connectErrorMessage({ status: 400, code: "store_items_invalid", error: "x", message: "x" })).toMatch(/Enter a domain name/);
        expect(connectErrorMessage({ status: 402, code: "plan_required", error: "x", message: "x" })).toMatch(/plan/);
    });
});

describe("partner and state refusals", () => {
    it("names the partner path and a conflicting state in Warmbly's words", () => {
        expect(storeErrorMessage({ status: 403, code: "store_managed_by_partner", error: "x", message: "raw" })).toMatch(/through its partner/);
        expect(storeErrorMessage({ status: 409, code: "store_state_conflict", error: "x", message: "raw" })).toMatch(/Refresh/);
    });

    it("still reads a reason sent in details", () => {
        expect(connectErrorMessage({ status: 409, code: "domain_unavailable", error: "x", message: "x", details: { reason: "registered" } })).toMatch(/bought through Warmbly/);
    });
});
