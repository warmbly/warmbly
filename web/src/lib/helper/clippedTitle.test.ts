// Issue #461: long cell text is truncated, and the full value has to be
// reachable on hover — but only where it was actually cut off.

import { describe, it, expect } from "vitest";
import clippedTitle from "./clippedTitle";

function span(text: string, scrollWidth: number, clientWidth: number) {
    const el = document.createElement("span");
    el.textContent = text;
    Object.defineProperty(el, "scrollWidth", { value: scrollWidth });
    Object.defineProperty(el, "clientWidth", { value: clientWidth });
    return el;
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

    it("drops a stale title once the column is wide enough", () => {
        const el = span("Acme", 40, 160);
        el.title = "Acme Corporation Holdings International";
        fire(clippedTitle.onMouseEnter, el);
        expect(el.hasAttribute("title")).toBe(false);
    });

    it("says nothing for an empty cell", () => {
        const el = span("", 0, 0);
        fire(clippedTitle.onMouseEnter, el);
        expect(el.hasAttribute("title")).toBe(false);
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
