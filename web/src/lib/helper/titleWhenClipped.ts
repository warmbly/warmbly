// Spread `clippedTitle` onto a `truncate`d element: it attaches the full text as
// a native tooltip only where the text is actually cut off, so a column that
// fits stays quiet. Hover is early enough; the browser reads `title` when it
// decides to show the tip, long after mouseenter fires. The title is dropped
// again on the way out, because React does not manage an attribute it was never
// given and would otherwise leave a stale one behind on the next render.

import type { MouseEvent } from "react";

function titleWhenClipped(e: MouseEvent<HTMLElement>) {
    const el = e.currentTarget;
    const text = el.textContent ?? "";
    if (text && el.scrollWidth > el.clientWidth) el.title = text;
    else el.removeAttribute("title");
}

function clearTitle(e: MouseEvent<HTMLElement>) {
    e.currentTarget.removeAttribute("title");
}

// One frozen object so spreading it in a 50-row table allocates nothing.
const clippedTitle = Object.freeze({
    onMouseEnter: titleWhenClipped,
    onMouseLeave: clearTitle,
});

export default clippedTitle;
