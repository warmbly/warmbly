import { describe, expect, it } from "vitest";
import { helpLink, helpLinkProps, isServicePage, portalSupportLink } from "./helpLink";

describe("help links", () => {
    it("tells the service's own pages from a third party's", () => {
        expect(isServicePage("https://docs.warmbly.com/guides/slack/")).toBe(true);
        expect(isServicePage("https://warmbly.com/pricing")).toBe(true);
        expect(isServicePage("https://developer.close.com")).toBe(false);
        expect(isServicePage("https://notwarmbly.com/x")).toBe(false);
        expect(isServicePage("https://warmbly.com.evil.test/x")).toBe(false);
    });
    it("changes nothing off a portal", () => {
        expect(helpLink("https://docs.warmbly.com/api/cli/")).toBe("https://docs.warmbly.com/api/cli/");
        expect(portalSupportLink()).toBe("");
    });
    it("opens a page in a new tab and an address in the mail client", () => {
        expect(helpLinkProps("https://northwind.test/help")).toEqual({ href: "https://northwind.test/help", target: "_blank", rel: "noopener noreferrer" });
        expect(helpLinkProps("mailto:help@northwind.test")).toEqual({ href: "mailto:help@northwind.test" });
    });
});
