import { afterEach, describe, expect, it, vi } from "vitest";
import { acquisitionSearch, isEmpty, readAcquisition } from "./acquisition";

afterEach(() => vi.unstubAllGlobals());

function browser(referrer = "") {
    vi.stubGlobal("window", { location: { hostname: "app.warmbly.com", origin: "https://app.warmbly.com", search: "" } });
    vi.stubGlobal("document", { referrer });
}

describe("cookieless acquisition", () => {
    it("carries only acquisition fields through redirects, not credentials or a nested next", () => {
        const search = "?utm_source=reddit&utm_medium=social&utm_campaign=launch&utm_term=email&utm_content=cta&wb_lp=%2Fpricing&wb_ref=www.reddit.com&next=%2Fapp&code=secret&email=person%40example.com";
        const carried = acquisitionSearch(search);
        expect(carried).toEqual({ utm_source: "reddit", utm_medium: "social", utm_campaign: "launch", utm_term: "email", utm_content: "cta", wb_lp: "/pricing", wb_ref: "www.reddit.com" });
        browser("https://warmbly.com/pricing");
        expect(readAcquisition(new URLSearchParams(carried).toString())).toEqual({ utm_source: "reddit", utm_medium: "social", utm_campaign: "launch", utm_term: "email", utm_content: "cta", landing_path: "/pricing", referrer_host: "www.reddit.com" });
    });

    it("trims and bounds values without fabricating missing tags", () => {
        expect(acquisitionSearch(`?utm_source=%20reddit%20&utm_medium=%20&wb_ref=${"a".repeat(300)}`)).toEqual({ utm_source: "reddit", wb_ref: "a".repeat(255) });
        expect(acquisitionSearch("?next=/app&code=secret")).toEqual({});
    });

    it("does not call our own domains a source or infer direct from an empty referrer", () => {
        for (const referrer of ["", "https://warmbly.com/pricing", "https://www.warmbly.com/", "https://app.warmbly.com/auth/login"]) {
            browser(referrer);
            expect(isEmpty(readAcquisition(""))).toBe(true);
            expect(readAcquisition("?wb_lp=/pricing")).toEqual({ landing_path: "/pricing" });
        }
    });

    it("records an external host without retaining the referrer path or query", () => {
        browser("https://www.Google.com/search?q=private");
        expect(readAcquisition("")).toEqual({ referrer_host: "www.google.com" });
        expect(readAcquisition("?wb_ref=WWW.REDDIT.COM&wb_lp=/pricing%3Femail=private%23section")).toEqual({ referrer_host: "www.reddit.com", landing_path: "/pricing" });
        expect(readAcquisition("?wb_ref=https%3A%2F%2Fwww.reddit.com%2Fr%2Femail").referrer_host).toBe("www.reddit.com");
    });

    it("rejects network-path landing URLs and malformed referrers", () => {
        browser();
        expect(readAcquisition("?wb_lp=//evil.example/path&wb_ref=%25")).toEqual({});
        expect(readAcquisition(`?wb_lp=${encodeURIComponent("/\\evil.example/offer")}`)).toEqual({});
        expect(readAcquisition("?wb_lp=https://evil.example/path&wb_ref=warmbly.com")).toEqual({});
    });
});
