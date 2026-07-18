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

export interface LineRect {
    top: number;
    left: number;
    width: number;
    height: number;
}

// textareaRangeRects returns one viewport rect per rendered line segment of
// the [start, end] range, clipped to the visible textarea, for painting a
// selection-highlight overlay (an unfocused textarea doesn't paint its own
// selection, and the AI popover steals focus).
export function textareaRangeRects(
    ta: HTMLTextAreaElement,
    start: number,
    end: number,
): LineRect[] {
    const measured = measureRange(ta, start, end);
    if (!measured) return [];
    const { spanRects, mirrorRect, taRect } = measured;
    const out: LineRect[] = [];
    for (const r of spanRects) {
        const top = taRect.top + (r.top - mirrorRect.top) - ta.scrollTop;
        const left = taRect.left + (r.left - mirrorRect.left) - ta.scrollLeft;
        const bottom = top + r.height;
        if (bottom < taRect.top + 2 || top > taRect.bottom - 2) continue;
        out.push({
            top,
            left,
            width: Math.min(r.width, taRect.right - left),
            height: r.height,
        });
    }
    return out;
}

function measureRange(
    ta: HTMLTextAreaElement,
    start: number,
    end: number,
): { spanRects: DOMRect[]; mirrorRect: DOMRect; taRect: DOMRect } | null {
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
    span.textContent = ta.value.slice(start, end) || "​";
    mirror.appendChild(span);
    mirror.appendChild(doc.createTextNode(ta.value.slice(end)));
    doc.body.appendChild(mirror);

    const spanRects = Array.from(span.getClientRects());
    const mirrorRect = mirror.getBoundingClientRect();
    const taRect = ta.getBoundingClientRect();
    doc.body.removeChild(mirror);
    if (spanRects.length === 0) return null;
    return { spanRects, mirrorRect, taRect };
}

export default function textareaRangeRect(
    ta: HTMLTextAreaElement,
    start: number,
    end: number,
): RangeRect | null {
    const measured = measureRange(ta, start, end);
    if (!measured) return null;
    const { spanRects, mirrorRect, taRect } = measured;
    const first = spanRects[0];
    const last = spanRects[spanRects.length - 1];

    const rect: RangeRect = {
        top: taRect.top + (first.top - mirrorRect.top) - ta.scrollTop,
        left: taRect.left + (first.left - mirrorRect.left) - ta.scrollLeft,
        bottom: taRect.top + (last.bottom - mirrorRect.top) - ta.scrollTop,
        centerX:
            taRect.left +
            (first.left + Math.min(first.width, taRect.width) / 2 - mirrorRect.left) -
            ta.scrollLeft,
    };

    // Selection scrolled out of the visible textarea: report nothing so the
    // toolbar hides instead of floating over unrelated UI.
    if (rect.bottom < taRect.top + 4 || rect.top > taRect.bottom - 4) return null;
    return rect;
}
