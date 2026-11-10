import { describe, expect, it } from "vitest";
import { parseLink, snippets } from "./embedCode";

const token = "abcdefghijklmnopqrstuvwxyzABCDEF0123456789_-";

describe("parseLink", () => {
    it("reads the kind and token from a full link", () => {
        expect(parseLink(`https://portal.agency.com/connect/${token}`)).toEqual({ kind: "connect", token });
        expect(parseLink(`https://forms.warmbly.com/r/${token}?x=1`)).toEqual({ kind: "report", token });
    });
    it("refuses anything that is not a connect or report link", () => {
        expect(parseLink("https://forms.warmbly.com/f/abc")).toBeNull();
        expect(parseLink(`https://x.com/connect/${token}"><script>`)).toBeNull();
        expect(parseLink("")).toBeNull();
    });
});

describe("snippets", () => {
    it("points the loader and the iframe at the portal origin", () => {
        const out = snippets("https://portal.agency.com", "report", token);
        expect(out.script).toBe(`<script src="https://portal.agency.com/embed.js" data-warmbly-report="${token}" async></script>`);
        expect(out.iframe).toContain(`src="https://portal.agency.com/r/${token}?embed=1"`);
    });
});
