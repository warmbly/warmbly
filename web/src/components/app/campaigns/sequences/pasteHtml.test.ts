import { describe, expect, it } from "vitest";
import { normalizePastedHTML } from "./pasteHtml";
import { htmlToPlain } from "./emailPreview";

describe("normalizePastedHTML", () => {
    it("drops the empty block Gmail writes a blank line as", () => {
        // Our paragraphs carry their own bottom margin, so keeping this one
        // renders the gap twice — the double spacing in issue #380.
        const out = normalizePastedHTML("<div>One</div><div><br></div><div>Two</div>");
        expect(out).toBe("<div>One</div><div>Two</div>");
    });

    it("drops Word's spacer paragraph and its namespaced tags", () => {
        const out = normalizePastedHTML(
            '<p class="MsoNormal">One<o:p></o:p></p>' +
                '<p class="MsoNormal"><o:p> </o:p></p>' +
                '<p class="MsoNormal">Two</p>',
        );
        expect(out).toBe('<p class="MsoNormal">One</p><p class="MsoNormal">Two</p>');
    });

    it("removes a stylesheet instead of letting its CSS land as copy", () => {
        const out = normalizePastedHTML("<style>p{color:red}</style><p>Hi</p>");
        expect(out).toBe("<p>Hi</p>");
    });

    it("collapses a run of breaks and strips the one a block ends with", () => {
        expect(normalizePastedHTML("<p>One<br><br>Two<br></p>")).toBe("<p>One<br>Two</p>");
    });

    it("keeps an image a mail client can load and removes one it cannot", () => {
        expect(normalizePastedHTML('<p><img src="https://x.test/a.png"></p>')).toContain("https://x.test/a.png");
        // A `cid:` part belongs to the message it was copied from, so the
        // paragraph holding it is empty once it goes and drops with it.
        expect(normalizePastedHTML('<p><img src="cid:part1"></p>')).toBe("");
    });

    it("keeps a block whose only content is an image", () => {
        const out = normalizePastedHTML('<div><img src="https://x.test/a.png"></div>');
        expect(out).toContain("<img");
    });

    it("keeps our merge-field chips through the span unwrap", () => {
        const chip = '<p><span data-var="">{{.FirstName}}</span></p>';
        expect(normalizePastedHTML(chip)).toBe(chip);
    });

    // A colour someone chose is design and the schema holds it now (issue
    // #393). The source editor's own font stack is not, and importing it
    // renders a cold email in Arial in every inbox.
    it("keeps a chosen colour off a pasted span and drops the source's fonts", () => {
        expect(normalizePastedHTML('<p><font color="red">Hi</font></p>')).toBe(
            '<p><span style="color: red">Hi</span></p>',
        );
        expect(normalizePastedHTML('<p><span style="background-color:#ff0">Hi</span></p>')).toBe(
            '<p><span style="background-color: #ff0">Hi</span></p>',
        );
        expect(
            normalizePastedHTML('<p><span style="font-family:Arial;font-size:13px">Hi</span></p>'),
        ).toBe("<p>Hi</p>");
    });

    it("drops a near-black colour, which is a default rather than a choice", () => {
        expect(normalizePastedHTML('<p><span style="color:#000000">Hi</span></p>')).toBe("<p>Hi</p>");
        expect(normalizePastedHTML('<p><span style="color: rgb(34, 34, 34)">Hi</span></p>')).toBe("<p>Hi</p>");
    });

    it("leaves a copy from another TipTap editor untouched", () => {
        // ProseMirror marks its own clipboard HTML and re-parses it exactly.
        const html = '<div data-pm-slice="1 1 []"><p>One</p><p><br></p></div>';
        expect(normalizePastedHTML(html)).toBe(html);
    });
});

describe("htmlToPlain with images", () => {
    it("stands an image in for its alt text", () => {
        expect(htmlToPlain('<p>Look:</p><img src="https://x.test/a.png" alt="Our dashboard">')).toBe(
            "Look:\n[Our dashboard]",
        );
    });

    it("drops an image with no alt text rather than leaving brackets", () => {
        expect(htmlToPlain('<p>Hi</p><img src="https://x.test/a.png">')).toBe("Hi");
    });
});

describe("htmlToPlain with links", () => {
    it("keeps a link's destination after its text", () => {
        expect(htmlToPlain('<p>See our <a href="https://x.test/pricing">pricing</a></p>')).toBe(
            "See our pricing (https://x.test/pricing)",
        );
    });

    it("gives a linked image its alt text and its destination", () => {
        expect(
            htmlToPlain('<a href="https://x.test/demo"><img src="https://x.test/a.png" alt="Watch the demo"></a>'),
        ).toBe("[Watch the demo] (https://x.test/demo)");
    });

    it("renders a button as its label and where it goes", () => {
        expect(
            htmlToPlain(
                '<table data-warmbly-button=""><tbody><tr><td><a href="https://cal.test/me">Book a call</a></td></tr></tbody></table>',
            ),
        ).toBe("Book a call (https://cal.test/me)");
    });

    it("says an address once when the text already is the address", () => {
        expect(htmlToPlain('<p><a href="https://x.test">https://x.test</a></p>')).toBe("https://x.test");
        expect(htmlToPlain('<p><a href="mailto:sam@x.test">sam@x.test</a></p>')).toBe("sam@x.test");
    });

    it("leaves a merge token alone, because it is not an address yet", () => {
        expect(htmlToPlain('<p><a href="{{.UnsubscribeLink}}">Unsubscribe</a></p>')).toBe("Unsubscribe");
    });

    it("leaves them alone for text that goes back into an editor", () => {
        // An autolinked address would gain a copy of itself on every round
        // trip, which is how the AI-block prompt is stored.
        expect(htmlToPlain('<p>See <a href="https://x.test/a">our page</a></p>', { links: false })).toBe(
            "See our page",
        );
    });

    it("falls back to the address when the link has no text", () => {
        expect(htmlToPlain('<p><a href="https://x.test/a"></a></p>')).toBe("https://x.test/a");
    });
});
