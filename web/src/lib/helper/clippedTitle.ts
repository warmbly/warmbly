// Spread `clippedTitle` onto a `truncate`d element: it attaches the full text as
// a native tooltip only where the text is actually cut off, so a column that
// fits stays quiet. Hover is early enough; the browser reads `title` when it
// decides to show the tip, long after mouseenter fires. The title comes off
// again on the way out, because React does not manage an attribute it was never
// given and would otherwise leave a stale one behind on the next render.

import type { MouseEvent } from "react";

// Only ever remove a title this module put there. An element with its own
// `title` prop keeps it: React rewrites that attribute only when the prop value
// changes, so taking it away once would lose it for good.
const ours = new WeakSet<HTMLElement>();

function titleWhenClipped(e: MouseEvent<HTMLElement>) {
    const el = e.currentTarget;
    if (el.hasAttribute("title") && !ours.has(el)) return;
    const text = el.textContent ?? "";
    if (text && el.scrollWidth > el.clientWidth) {
        el.title = text;
        ours.add(el);
    } else if (ours.has(el)) {
        el.removeAttribute("title");
        ours.delete(el);
    }
}

function clearTitle(e: MouseEvent<HTMLElement>) {
    const el = e.currentTarget;
    if (!ours.has(el)) return;
    el.removeAttribute("title");
    ours.delete(el);
}

// One frozen object so spreading it in a 50-row table allocates nothing.
const clippedTitle = Object.freeze({
    onMouseEnter: titleWhenClipped,
    onMouseLeave: clearTitle,
});

export default clippedTitle;
