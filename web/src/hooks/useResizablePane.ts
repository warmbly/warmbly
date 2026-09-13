// One drag-resizable pane: the pointer gesture, the keyboard map and the ARIA
// bundle a `role="separator"` needs, in one place.
//
// Written out of the unibox splitter (#479) after the assistant panel turned
// out to be the same gesture with none of the same fixes (#485). The parts that
// are easy to get wrong and cost nothing to share:
//
//   - pointer capture, not window listeners. The pane being dragged into
//     renders message bodies in iframes, and a nested browsing context swallows
//     every pointer event over it: the drag freezes, and the pointerup that
//     would have torn the listeners down never arrives
//   - no preventDefault on pointerdown. A cancelled pointerdown suppresses the
//     compatibility mousedown, and with it the focus this control needs and the
//     mousedown every click-outside listener in the app is registered on
//   - the width goes straight to the DOM for the duration and is committed to
//     the store once, on release. The store is persisted, so a write per
//     pointer frame is a JSON.stringify + localStorage.setItem per frame
//   - only a gesture that actually moved commits, so a click on a pane the
//     viewport is already capping cannot quietly overwrite a width chosen on a
//     bigger monitor

import React from "react";

export type ResizablePaneOptions = {
    /** The stored preference, in px. May exceed what the viewport can render. */
    value: number;
    /** Commit a new preference. Called once per gesture, never per frame. */
    onChange: (width: number) => void;
    min: number;
    /** The cap in force right now (measured by the caller where it varies). */
    max: number;
    /** What Enter and a double-click restore. */
    defaultValue: number;
    /**
     * A fresh cap at gesture time. `max` is a rendered value and can be a frame
     * behind a layout change the gesture itself caused.
     */
    measureMax?: () => number;
    /** The element carrying the width, written to directly during a drag. */
    paneRef: React.RefObject<HTMLElement | null>;
    /** The CSS custom property on that element that holds the width. */
    cssVar: string;
    /** +1 when the pane widens as the pointer moves right, -1 when it narrows. */
    direction?: 1 | -1;
    /** Accessible name for the separator. */
    label: string;
    /** id of the pane, for aria-controls. */
    controls?: string;
    /** Spoken value, e.g. "Conversation list 360 pixels". */
    valueText?: (width: number) => string;
    step?: number;
    coarseStep?: number;
};

export type SeparatorProps = {
    role: "separator";
    "aria-orientation": "vertical";
    "aria-label": string;
    "aria-controls"?: string;
    "aria-valuenow": number;
    "aria-valuemin": number;
    "aria-valuemax": number;
    "aria-valuetext"?: string;
    tabIndex: 0;
    style: React.CSSProperties;
    onPointerDown: (e: React.PointerEvent<HTMLElement>) => void;
    onPointerMove: (e: React.PointerEvent<HTMLElement>) => void;
    onPointerUp: () => void;
    onPointerCancel: () => void;
    onLostPointerCapture: () => void;
    onKeyDown: (e: React.KeyboardEvent<HTMLElement>) => void;
    onDoubleClick: () => void;
};

export function useResizablePane({
    value,
    onChange,
    min,
    max,
    defaultValue,
    measureMax,
    paneRef,
    cssVar,
    direction = 1,
    label,
    controls,
    valueText,
    step = 16,
    coarseStep = 48,
}: ResizablePaneOptions): { width: number; separatorProps: SeparatorProps } {
    // What is on screen: the preference, capped by what the viewport can give
    // it. This is the number ARIA reports, so the separator never announces a
    // width the pane does not have.
    const width = Math.max(min, Math.min(max, value));

    const dragRef = React.useRef<{
        startX: number;
        startWidth: number;
        max: number;
        moved: boolean;
    } | null>(null);
    const liveWidthRef = React.useRef(width);

    // Handlers are recreated every render and re-attached as props, and the
    // drag deliberately causes no render of its own, so a closure over the
    // current bounds is never stale mid-gesture.
    const freshMax = () => (measureMax ? measureMax() : max);

    const releaseBody = () => {
        document.body.style.removeProperty("cursor");
        document.body.style.removeProperty("user-select");
    };

    const endDrag = () => {
        const drag = dragRef.current;
        if (!drag) return;
        dragRef.current = null;
        releaseBody();
        if (drag.moved) onChange(liveWidthRef.current);
    };

    // A drag interrupted by an unmount would otherwise leave the whole app with
    // `user-select: none` and a resize cursor.
    React.useEffect(
        () => () => {
            if (dragRef.current) releaseBody();
        },
        [],
    );

    const onPointerDown = (e: React.PointerEvent<HTMLElement>) => {
        if (e.button !== 0) return;
        const pane = paneRef.current;
        if (!pane) return;
        const sep = e.currentTarget;
        try {
            sep.setPointerCapture(e.pointerId);
        } catch {
            // jsdom, and any browser that has already lost the pointer.
        }
        sep.focus();
        document.body.style.cursor = "col-resize";
        document.body.style.userSelect = "none";
        const cap = freshMax();
        dragRef.current = {
            // The grab offset inside the handle is part of the start width, so
            // the divider stays under the cursor instead of jumping to meet it.
            startX: e.clientX,
            startWidth: pane.getBoundingClientRect().width || width,
            max: cap,
            moved: false,
        };
        liveWidthRef.current = Math.max(min, Math.min(cap, width));
    };

    const onPointerMove = (e: React.PointerEvent<HTMLElement>) => {
        const drag = dragRef.current;
        if (!drag) return;
        const delta = (e.clientX - drag.startX) * direction;
        const next = Math.round(
            Math.min(drag.max, Math.max(min, drag.startWidth + delta)),
        );
        if (next === liveWidthRef.current) return;
        drag.moved = true;
        liveWidthRef.current = next;
        paneRef.current?.style.setProperty(cssVar, `${next}px`);
    };

    // The ARIA window-splitter keys. Arrows move the separator, so which way
    // widens the pane depends on which edge it sits on; Home and End are about
    // the pane itself (narrowest, widest) and do not flip.
    const onKeyDown = (e: React.KeyboardEvent<HTMLElement>) => {
        const cap = freshMax();
        // From what is on screen, not from the stored preference: on a window
        // that caps the pane, nudging from the stored number would move it
        // through values the viewport cannot render and look like nothing
        // happened.
        const current = Math.max(min, Math.min(cap, value));
        const nudge = e.shiftKey ? coarseStep : step;
        const to = (w: number) => onChange(Math.max(min, Math.min(cap, w)));
        switch (e.key) {
            case "ArrowLeft":
                to(current - nudge * direction);
                break;
            case "ArrowRight":
                to(current + nudge * direction);
                break;
            case "Home":
                to(min);
                break;
            case "End":
                to(cap);
                break;
            case "Enter":
                to(defaultValue);
                break;
            default:
                return;
        }
        e.preventDefault();
    };

    return {
        width,
        separatorProps: {
            role: "separator",
            "aria-orientation": "vertical",
            "aria-label": label,
            "aria-controls": controls,
            "aria-valuenow": width,
            "aria-valuemin": min,
            "aria-valuemax": max,
            "aria-valuetext": valueText?.(width),
            tabIndex: 0,
            // Here rather than in a class the caller has to remember: without it
            // a touch drag scrolls the page instead of reaching pointer capture.
            style: { touchAction: "none" },
            onPointerDown,
            onPointerMove,
            onPointerUp: endDrag,
            onPointerCancel: endDrag,
            onLostPointerCapture: endDrag,
            onKeyDown,
            onDoubleClick: () => onChange(defaultValue),
        },
    };
}

/**
 * A pointer drag that survives whatever it is dragged over: capture retargets
 * every move to the element itself, so an iframe in the path cannot swallow the
 * gesture or the pointerup that ends it. For drags that are not pane resizes
 * (moving a floating window, its corners) and so have no width or ARIA.
 */
export function capturePointerDrag(
    e: React.PointerEvent<Element>,
    handlers: {
        onMove: (e: PointerEvent) => void;
        onEnd?: () => void;
        /** Hold the page still for the gesture instead of preventDefault. */
        cursor?: string;
    },
) {
    const el = e.currentTarget as HTMLElement;
    const pointerId = e.pointerId;
    // Locking the body is what lets the caller skip preventDefault on
    // pointerdown, which would take the compatibility mousedown with it and so
    // leave every open popover on screen (they all close on mousedown).
    document.body.style.userSelect = "none";
    if (handlers.cursor) document.body.style.cursor = handlers.cursor;
    try {
        el.setPointerCapture(pointerId);
    } catch {
        // jsdom, and any browser that has already lost the pointer.
    }
    // A second finger on the same element gets its own pointerId; without the
    // guard it would drive a gesture it did not start.
    const mine = (ev: Event) => (ev as PointerEvent).pointerId === pointerId;
    const move = (ev: Event) => {
        if (mine(ev)) handlers.onMove(ev as PointerEvent);
    };
    // Removing a listener mid-dispatch keeps it off the rest of THIS event too,
    // so the element's handler taking the window pair down is enough: `end`
    // cannot run twice for one gesture.
    const end = (ev: Event) => {
        if (!mine(ev)) return;
        el.removeEventListener("pointermove", move);
        el.removeEventListener("pointerup", end);
        el.removeEventListener("pointercancel", end);
        el.removeEventListener("lostpointercapture", end);
        window.removeEventListener("pointerup", end);
        window.removeEventListener("pointercancel", end);
        document.body.style.removeProperty("user-select");
        document.body.style.removeProperty("cursor");
        handlers.onEnd?.();
    };
    el.addEventListener("pointermove", move);
    el.addEventListener("pointerup", end);
    // A cancelled gesture (a touch the browser took over, a context menu) never
    // sends pointerup; without these the listeners outlive the drag.
    el.addEventListener("pointercancel", end);
    el.addEventListener("lostpointercapture", end);
    // Last resort: the element itself can be unmounted mid-drag, taking its
    // listeners with it and leaving the whole app locked to `user-select: none`.
    window.addEventListener("pointerup", end);
    window.addEventListener("pointercancel", end);
}
