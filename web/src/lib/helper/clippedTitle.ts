// Spread `clippedTitle` onto a `truncate`d element: it attaches the full text as
// a native tooltip only where the text is actually cut off, so a column that
// fits stays quiet. Hover is early enough; the browser reads `title` when it
// decides to show the tip, long after mouseenter fires. The title comes off
// again on the way out, because React does not manage an attribute it was never
// given and would otherwise leave a stale one behind on the next render.
//
// Known bound: text replaced by a realtime update while the cursor is already
// resting on the cell shows the previous value until the pointer leaves and
// returns, since nothing fires in between. Closing that would mean measuring on
// every mousemove — a layout read per row per frame — for a sub-second window on
// a tooltip, so it is left as is deliberately.

import type { MouseEvent } from "react";

// The exact string this module last wrote on an element. Anything else in the
// attribute belongs to the caller and is left alone: React rewrites a `title`
// prop only when its value changes, so clobbering it once would lose it for
// good. Matching on the value rather than the element matters because a caller
// can start supplying a title after we have already titled it.
const ours = new WeakMap<HTMLElement, string>();

function titleWhenClipped(e: MouseEvent<HTMLElement>) {
    const el = e.currentTarget;
    const current = el.getAttribute("title");
    if (current !== null && current !== ours.get(el)) return;
    const text = el.textContent ?? "";
    if (text && el.scrollWidth > el.clientWidth) {
        el.title = text;
        ours.set(el, text);
    } else if (current !== null) {
        el.removeAttribute("title");
        ours.delete(el);
    }
}

function clearTitle(e: MouseEvent<HTMLElement>) {
    const el = e.currentTarget;
    if (el.getAttribute("title") !== ours.get(el)) return;
    el.removeAttribute("title");
    ours.delete(el);
}

// One frozen object so spreading it in a 50-row table allocates nothing.
const clippedTitle = Object.freeze({
    onMouseEnter: titleWhenClipped,
    onMouseLeave: clearTitle,
});

export default clippedTitle;
