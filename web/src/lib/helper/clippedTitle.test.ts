// Issue #461: long cell text is truncated, and the full value has to be
// reachable on hover — but only where it was actually cut off.

import { describe, it, expect } from "vitest";
import clippedTitle from "./clippedTitle";

// jsdom does no layout, so the two widths the helper reads are backed by a
// box the test can resize between hovers.
function span(text: string, scrollWidth: number, clientWidth: number) {
    const box = { scrollWidth, clientWidth };
    const el = document.createElement("span");
    el.textContent = text;
    Object.defineProperty(el, "scrollWidth", { get: () => box.scrollWidth });
    Object.defineProperty(el, "clientWidth", { get: () => box.clientWidth });
    return Object.assign(el, { resize: (s: number, c: number) => Object.assign(box, { scrollWidth: s, clientWidth: c }) });
}

type Handler = (typeof clippedTitle)["onMouseEnter"];
const fire = (h: Handler, el: HTMLElement) =>
    h({ currentTarget: el } as unknown as Parameters<Handler>[0]);

describe("clippedTitle", () => {
    it("titles text that overflows its column", () => {
        const el = span("Some Extremely Long Company Name Ltd", 420, 160);
        fire(clippedTitle.onMouseEnter, el);
        expect(el.getAttribute("title")).toBe("Some Extremely Long Company Name Ltd");
    });

    it("leaves text that fits untitled", () => {
        const el = span("Acme", 40, 160);
        fire(clippedTitle.onMouseEnter, el);
        expect(el.hasAttribute("title")).toBe(false);
    });

    it("drops its own title once the column is wide enough", () => {
        const el = span("Acme Corporation Holdings International", 420, 160);
        fire(clippedTitle.onMouseEnter, el);
        expect(el.hasAttribute("title")).toBe(true);
        el.resize(160, 420);
        fire(clippedTitle.onMouseEnter, el);
        expect(el.hasAttribute("title")).toBe(false);
    });

    it("says nothing for an empty cell", () => {
        const el = span("", 0, 0);
        fire(clippedTitle.onMouseEnter, el);
        expect(el.hasAttribute("title")).toBe(false);
    });

    // A caller's own `title` prop is React-managed and is only rewritten when its
    // value changes, so taking it away once would lose it for good.
    it("leaves a title it did not set alone", () => {
        const el = span("Failed", 420, 160);
        el.title = "Could not send: mailbox rejected the recipient";
        fire(clippedTitle.onMouseEnter, el);
        expect(el.getAttribute("title")).toBe("Could not send: mailbox rejected the recipient");
        fire(clippedTitle.onMouseLeave, el);
        expect(el.getAttribute("title")).toBe("Could not send: mailbox rejected the recipient");
    });

    // A caller can start supplying a title after we have already titled the
    // element, so ownership has to be judged on the value, not the element.
    it("stands down when a caller titles an element it had titled", () => {
        const el = span("Queued", 420, 160);
        fire(clippedTitle.onMouseEnter, el);
        expect(el.getAttribute("title")).toBe("Queued");
        el.title = "Could not send: mailbox rejected the recipient";
        fire(clippedTitle.onMouseEnter, el);
        expect(el.getAttribute("title")).toBe("Could not send: mailbox rejected the recipient");
        fire(clippedTitle.onMouseLeave, el);
        expect(el.getAttribute("title")).toBe("Could not send: mailbox rejected the recipient");
    });

    // Nothing removes the attribute on re-render, so it must not outlive the
    // hover that set it: a row whose text changes underneath a resting cursor
    // would otherwise show the previous value.
    it("takes the title back off on the way out", () => {
        const el = span("Some Extremely Long Company Name Ltd", 420, 160);
        fire(clippedTitle.onMouseEnter, el);
        expect(el.hasAttribute("title")).toBe(true);
        fire(clippedTitle.onMouseLeave, el);
        expect(el.hasAttribute("title")).toBe(false);
    });
});
