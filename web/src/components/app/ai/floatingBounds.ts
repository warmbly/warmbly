// Where the AI cards are allowed to sit.
//
// They were centred on the selection and then clamped to the viewport, which
// says nothing about the surface being edited. A selection starting at the left
// edge of a campaign body put the 300px card 150px further left again, and in
// the step drawer that is outside the panel entirely, floating over the flow
// canvas behind it (issue #432). A card belongs to the thing it is editing, so
// it is clamped to that element first and to the viewport second.

// One declaration of the card's width, shared by the card and by the hosts that
// position it. Two copies drifted apart and the second one decided placement.
export const AI_CARD_WIDTH = 300;

const EDGE_GAP = 8;

// clampCardLeft centres a card of `width` on `centerX`, then pulls it back
// inside `host` (when the host is wide enough to hold it) and inside the
// viewport. Returns a viewport-relative left, for a position: fixed element.
export function clampCardLeft(centerX: number, width: number, host: Element | null | undefined): number {
    const viewportWidth = typeof window === "undefined" ? 1024 : window.innerWidth;
    let min = EDGE_GAP;
    let max = viewportWidth - width - EDGE_GAP;

    const box = host?.getBoundingClientRect();
    // A host narrower than the card cannot contain it; the viewport clamp is
    // then the only useful bound.
    if (box && box.width >= width) {
        min = Math.max(min, box.left);
        max = Math.min(max, box.right - width);
    }
    if (max < min) max = min;
    return Math.min(Math.max(centerX - width / 2, min), max);
}
