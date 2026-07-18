// textareaRangeRect — measures where a [start, end] character range of a
// textarea sits on screen. Textareas expose no Range API, so the text is
// mirrored into a hidden div with identical typography and the selection span
// is measured there. Used to anchor the floating AI edit toolbar at the
// user's selection.

const MIRROR_STYLES = [
    "boxSizing",
    "width",
    "fontFamily",
    "fontSize",
    "fontWeight",
    "fontStyle",
    "letterSpacing",
    "lineHeight",
    "textTransform",
    "wordSpacing",
    "textIndent",
    "paddingTop",
    "paddingRight",
    "paddingBottom",
    "paddingLeft",
    "borderTopWidth",
    "borderRightWidth",
    "borderBottomWidth",
    "borderLeftWidth",
] as const;

export interface RangeRect {
    // Viewport coordinates of the selection's first line.
    top: number;
    left: number;
    // Viewport coordinates of the selection's last line end.
    bottom: number;
    // Horizontal center of the first selected line segment.
    centerX: number;
}

export default function textareaRangeRect(
    ta: HTMLTextAreaElement,
    start: number,
    end: number,
): RangeRect | null {
    if (typeof window === "undefined") return null;
    const doc = ta.ownerDocument;
    const mirror = doc.createElement("div");
    const style = window.getComputedStyle(ta);
    for (const prop of MIRROR_STYLES) {
        mirror.style[prop] = style[prop];
    }
    mirror.style.position = "absolute";
    mirror.style.visibility = "hidden";
    mirror.style.whiteSpace = "pre-wrap";
    mirror.style.wordWrap = "break-word";
    mirror.style.top = "0";
    mirror.style.left = "-9999px";

    mirror.appendChild(doc.createTextNode(ta.value.slice(0, start)));
    const span = doc.createElement("span");
    // A zero-width space keeps the span measurable when the range is empty.
    span.textContent = ta.value.slice(start, end) || "​";
    mirror.appendChild(span);
    mirror.appendChild(doc.createTextNode(ta.value.slice(end)));
    doc.body.appendChild(mirror);

    const spanRects = span.getClientRects();
    const mirrorRect = mirror.getBoundingClientRect();
    if (spanRects.length === 0) {
        doc.body.removeChild(mirror);
        return null;
    }
    const first = spanRects[0];
    const last = spanRects[spanRects.length - 1];
    const taRect = ta.getBoundingClientRect();

    const rect: RangeRect = {
        top: taRect.top + (first.top - mirrorRect.top) - ta.scrollTop,
        left: taRect.left + (first.left - mirrorRect.left) - ta.scrollLeft,
        bottom: taRect.top + (last.bottom - mirrorRect.top) - ta.scrollTop,
        centerX:
            taRect.left +
            (first.left + Math.min(first.width, taRect.width) / 2 - mirrorRect.left) -
            ta.scrollLeft,
    };
    doc.body.removeChild(mirror);

    // Selection scrolled out of the visible textarea: report nothing so the
    // toolbar hides instead of floating over unrelated UI.
    if (rect.bottom < taRect.top + 4 || rect.top > taRect.bottom - 4) return null;
    return rect;
}
