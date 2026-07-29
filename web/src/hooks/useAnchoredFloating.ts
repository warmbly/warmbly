// useAnchoredFloating — robust popover positioning via @floating-ui/dom. Anchors
// a portaled floating element to a reference (a real DOM element OR a virtual
// element for a text caret) and keeps it glued through scroll, resize, and layout
// shifts via autoUpdate. Handles flip (open up when there's no room below) and
// shift (stay on-screen). This replaces the hand-rolled `position: fixed` coords
// that detached on scroll and could land at the top-left before the anchor was
// measured.

import * as React from "react";
import {
    computePosition,
    autoUpdate,
    offset,
    flip,
    shift,
    size,
    type Placement,
    type ReferenceElement,
} from "@floating-ui/dom";

export interface AnchoredFloatingOptions {
    placement?: Placement;
    gap?: number;
    /** Constrain the floating element to the available viewport height. */
    maxHeight?: boolean;
    /** Match the reference element's width (for full-width menus). */
    sameWidth?: boolean;
    padding?: number;
}

export function useAnchoredFloating(open: boolean, opts: AnchoredFloatingOptions = {}) {
    const { placement = "bottom-start", gap = 6, maxHeight = false, sameWidth = false, padding = 8 } = opts;
    const [reference, setReference] = React.useState<ReferenceElement | null>(null);
    const [floating, setFloating] = React.useState<HTMLElement | null>(null);
    // Hidden until the first position is computed, so it never flashes at 0,0.
    // Uses `visibility` (not opacity) so a framer-motion entrance can own opacity
    // without fighting this style.
    const [style, setStyle] = React.useState<React.CSSProperties>({
        position: "fixed",
        top: 0,
        left: 0,
        visibility: "hidden",
    });

    React.useLayoutEffect(() => {
        if (!open || !reference || !floating) return;
        const update = () => {
            computePosition(reference, floating, {
                strategy: "fixed",
                placement,
                middleware: [
                    offset(gap),
                    flip({ padding }),
                    shift({ padding }),
                    size({
                        padding,
                        apply({ availableHeight, rects, elements }) {
                            const s: Record<string, string> = {};
                            if (maxHeight) s.maxHeight = `${Math.max(140, Math.floor(availableHeight))}px`;
                            if (sameWidth) s.width = `${Math.round(rects.reference.width)}px`;
                            Object.assign(elements.floating.style, s);
                        },
                    }),
                ],
            }).then(({ x, y }) => {
                setStyle({
                    position: "fixed",
                    top: 0,
                    left: 0,
                    transform: `translate3d(${Math.round(x)}px, ${Math.round(y)}px, 0)`,
                    visibility: "visible",
                });
            });
        };
        const cleanup = autoUpdate(reference, floating, update);
        return cleanup;
    }, [open, reference, floating, placement, gap, maxHeight, sameWidth, padding]);

    return { setReference, setFloating, floatingStyle: style };
}

// caretReference builds a floating-ui virtual element for a text caret: it
// reports the caret's viewport rect and points autoUpdate at the editor DOM (its
// contextElement) so scrolling that container re-anchors the popover.
export function caretReference(getRect: () => DOMRect | null, contextElement: Element | null): ReferenceElement {
    return {
        contextElement: contextElement ?? undefined,
        getBoundingClientRect: () => {
            const r = getRect();
            if (r) return r;
            // Off-screen fallback keeps computePosition happy until the next tick.
            return new DOMRect(-9999, -9999, 0, 0);
        },
    };
}
