// The token half of the AI passage round trip (issue #432): what the model
// writes back has to become the chips it came from, and nothing else may be
// treated as markup.

import { describe, it, expect } from "vitest";
import { passageHTML, restoreEdges } from "./richTextPassage";

describe("passageHTML", () => {
    it("starts a paragraph on a blank line and breaks on a single newline", () => {
        expect(passageHTML("one\ntwo\n\nthree")).toBe("<p>one<br>two</p><p>three</p>");
    });

    it("wraps a merge variable as the chip its node parses", () => {
        expect(passageHTML("Hi {{.FirstName}}.")).toBe('<p>Hi <span data-var="">{{.FirstName}}</span>.</p>');
    });

    it("keeps a variable's default fallback inside one chip", () => {
        expect(passageHTML('{{.Company | default "your team"}}')).toBe(
            '<p><span data-var="">{{.Company | default "your team"}}</span></p>',
        );
    });

    it("takes a conditional whole, without chipping the fields inside it", () => {
        expect(passageHTML("{{if .Company}}at {{.Company}}{{end}} today")).toBe(
            '<p><span data-if="">{{if .Company}}at {{.Company}}{{end}}</span> today</p>',
        );
    });

    it("restores a form link by its public id", () => {
        expect(passageHTML("book here: {{form_link:abc123}}")).toBe(
            '<p>book here: <span data-form-link="abc123">{{form_link:abc123}}</span></p>',
        );
    });

    it("restores an AI block only when its configuration came with the passage", () => {
        const configs = new Map([["kept", "Y29uZmln"]]);
        expect(passageHTML("[[ai:kept]]", configs)).toBe(
            '<p><span data-ai-var="kept" data-ai-config="Y29uZmln">[[ai:kept]]</span></p>',
        );
        // An id the passage never carried has no prompt to restore, so it must
        // not become a block that silently generates nothing.
        expect(passageHTML("[[ai:invented]]", configs)).toBe("<p>[[ai:invented]]</p>");
    });

    it("turns a markdown link back into an anchor", () => {
        expect(passageHTML("book [a slot](https://cal.com/x) here")).toBe(
            '<p>book <a href="https://cal.com/x">a slot</a> here</p>',
        );
    });

    it("keeps a merge token as the destination, and chips the tokens in the label", () => {
        expect(passageHTML("[unsubscribe {{.FirstName}}]({{.UnsubscribeLink}})")).toBe(
            '<p><a href="{{.UnsubscribeLink}}">unsubscribe <span data-var="">{{.FirstName}}</span></a></p>',
        );
    });

    it("leaves a destination it cannot read as the words it is", () => {
        expect(passageHTML("[label](ftp://x)")).toBe("<p>[label](ftp://x)</p>");
    });

    it("leaves brackets that are not a link as the words they are", () => {
        expect(passageHTML("[note](see below)")).toBe("<p>[note](see below)</p>");
    });

    it("escapes copy the model wrote, so markup in it is text", () => {
        expect(passageHTML("5 < 6 & <b>bold</b>")).toBe("<p>5 &lt; 6 &amp; &lt;b&gt;bold&lt;/b&gt;</p>");
    });

    it("keeps an empty line as an empty paragraph", () => {
        expect(passageHTML("")).toBe("<p><br></p>");
    });

    it("keeps a blank paragraph the author used as spacing", () => {
        // Two separators in a row is an empty block between two others; a
        // greedy split swallowed it and the spacing disappeared on every edit.
        expect(passageHTML("one\n\n\n\ntwo")).toBe("<p>one</p><p><br></p><p>two</p>");
    });

    it("drops a stray newline at a block edge rather than rendering a break", () => {
        expect(passageHTML("one\n\n\ntwo")).toBe("<p>one</p><p>two</p>");
    });

    it("keeps a destination that carries a paren or a space", () => {
        expect(passageHTML("read [the article](<https://en.wikipedia.org/wiki/Foo_(bar)>) now")).toBe(
            '<p>read <a href="https://en.wikipedia.org/wiki/Foo_(bar)">the article</a> now</p>',
        );
    });

    it("reads the plain form the model may normalise the angle one back to", () => {
        // Stopping at the first ")" would hand back a truncated destination and
        // leave a stray paren in the copy, which is worse than no link at all.
        expect(passageHTML("read [the article](https://en.wikipedia.org/wiki/Foo_(bar)) now")).toBe(
            '<p>read <a href="https://en.wikipedia.org/wiki/Foo_(bar)">the article</a> now</p>',
        );
    });
});

describe("restoreEdges", () => {
    it("gives back the spaces the model trimmed off", () => {
        expect(restoreEdges(" word ", "REWRITE")).toBe(" REWRITE ");
        expect(restoreEdges("word", "REWRITE")).toBe("REWRITE");
    });

    it("makes an unchanged answer compare equal to what was selected", () => {
        expect(restoreEdges("Already fine. ", "Already fine.")).toBe("Already fine. ");
    });

    it("leaves an all-whitespace selection alone", () => {
        expect(restoreEdges("   ", "anything")).toBe("   ");
    });

    it("does not hand back a paragraph break as if it were a space", () => {
        // A selection running to the start of the next paragraph ends on a
        // block separator; giving it back would add a blank paragraph.
        expect(restoreEdges("One two\n\n", "REWRITE")).toBe("REWRITE");
        expect(restoreEdges(" One two\n\n ", "REWRITE")).toBe(" REWRITE ");
    });
});
