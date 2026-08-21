import { describe, expect, it } from "vitest";
import { escapeHtml, plainToHtml, plainToDisplayHtml } from "./body";

describe("plainToHtml", () => {
    it("escapes markup instead of letting it eat the message", () => {
        const out = plainToHtml("a < b & c > d\nsecond line");
        expect(out).toBe("a &lt; b &amp; c &gt; d<br />second line");
    });

    it("keeps paragraph spacing", () => {
        const out = plainToHtml("Hello,\n\nFirst paragraph.\n\nThank you.");
        expect(out).toBe("Hello,<br /><br />First paragraph.<br /><br />Thank you.");
    });

    it("preserves runs of spaces", () => {
        expect(plainToHtml("a    b")).toBe("a&nbsp;&nbsp;&nbsp; b");
    });

    it("leaves unicode and emoji untouched", () => {
        expect(plainToHtml("café ™ 🎉")).toBe("café ™ 🎉");
    });

    it("links bare URLs without swallowing sentence punctuation", () => {
        expect(plainToHtml("See https://warmbly.com/a?x=1&y=2.")).toBe(
            'See <a href="https://warmbly.com/a?x=1&amp;y=2">https://warmbly.com/a?x=1&amp;y=2</a>.',
        );
    });

    it("does not link text that only looks like a tag", () => {
        expect(plainToHtml("<not-a-tag>")).toBe("&lt;not-a-tag&gt;");
    });

    it("returns empty for empty input", () => {
        expect(plainToHtml("")).toBe("");
    });
});

describe("escapeHtml", () => {
    it("escapes quotes too", () => {
        expect(escapeHtml(`"x" 'y'`)).toBe("&quot;x&quot; &#39;y&#39;");
    });
});

describe("plainToDisplayHtml", () => {
    it("opens links in a new tab", () => {
        expect(plainToDisplayHtml("https://warmbly.com")).toContain('target="_blank"');
    });
});
