// The one way a dropdown, popover or picker closes itself: a press anywhere
// outside it (an email body's frame included), Escape, or another one opening. Every floating layer in the
// dashboard goes through this so they all behave the same.

import { useEffect, useRef, type RefObject } from "react";

type ElRef = RefObject<HTMLElement | null>;

// Why a layer is being dismissed. A close returning false handled it itself
// (stayed open, or moved focus), so focus is not handed back to the trigger.
export type DismissCause = "outside" | "escape";
type Dismiss = (cause: DismissCause) => void | false;

// Every open layer, oldest first. Opening one closes the others unless it was
// opened from inside them, and Escape closes only the newest.
interface Layer {
    contains: (node: Node) => boolean;
    close: (cause: DismissCause) => void | false;
}

const layers: Layer[] = [];
const frameShields = new Map<HTMLIFrameElement, { count: number; prev: string }>();

const FOCUSABLE = "button:not(:disabled), [href], input:not(:disabled), select, textarea, [tabindex]:not([tabindex=\"-1\"])";

/**
 * Close an open floating layer on a press outside `inside`, on Escape (newest
 * layer first), and when another layer opens outside it. `inside[0]` is where
 * the layer was opened from (its trigger, or a wrapper holding trigger and
 * panel). A portaled panel marked `[data-floating]` counts as inside it.
 */
export default function useClickOutside(open: boolean, onClose: Dismiss, inside: ElRef | ElRef[]) {
    // Through refs: callers pass inline closures and ref arrays, and re-running
    // the effect on every render would reorder the stack.
    const dismiss = useRef(onClose);
    dismiss.current = onClose;
    const refs = useRef<ElRef[]>([]);
    refs.current = Array.isArray(inside) ? inside : [inside];

    useEffect(() => {
        if (!open) return;
        const contains = (node: Node) => refs.current.some((r) => r.current?.contains(node));
        const self: Layer = { contains, close: (cause) => dismiss.current(cause) };

        // The stack is one chain of nested layers: the one holding the origin and
        // everything below it stay, everything above it closes. Opened from inside
        // a floating panel the stack cannot see, it closes nothing.
        const origin = refs.current[0]?.current ?? document.activeElement;
        let parent = layers.length - 1;
        while (parent >= 0 && !(origin && layers[parent].contains(origin))) parent--;
        // A layer's own [data-floating] root is not a panel it was opened from.
        const nested = parent < 0 && !!origin?.parentElement?.closest("[data-floating]");
        if (!nested) for (const other of layers.slice(parent + 1)) other.close("outside");
        layers.push(self);

        // A floating layer this one opened (its own portaled panel, a calendar, a
        // nested menu) is inside it; the panel or dialog holding it is not.
        const isInside = (node: Node) => {
            if (contains(node)) return true;
            const floating = (node as Element).closest?.("[data-floating]");
            return !!floating && !refs.current.some((r) => r.current && floating.contains(r.current));
        };
        const onPointerDown = (e: PointerEvent) => {
            if (!isInside(e.target as Node)) self.close("outside");
        };
        // Escape takes the innermost layer only, and stops there so the dialog or
        // drawer holding it stays open. Focus inside goes back to where it was opened.
        const onKey = (e: KeyboardEvent) => {
            if (e.key !== "Escape" || layers[layers.length - 1] !== self) return;
            // A modal opened above this layer (the confirm, a dialog) owns Escape first.
            const modals = document.querySelectorAll('[role="alertdialog"], [aria-modal="true"]');
            const above = Array.from(modals).some((m) => !refs.current.some((r) => r.current && m.contains(r.current)));
            if (above) return;
            e.stopPropagation();
            const active = document.activeElement;
            const origin = refs.current[0]?.current;
            if (self.close("escape") === false) return;
            if (!origin?.isConnected || !active || !isInside(active)) return;
            const target = origin.matches(FOCUSABLE) ? origin : origin.querySelector<HTMLElement>(FOCUSABLE);
            target?.focus({ preventScroll: true });
        };
        // A press inside an iframe (an email body) never reaches this document; it only blurs the window.
        let blurTimer: ReturnType<typeof setTimeout> | undefined;
        const onBlur = () => {
            blurTimer = setTimeout(() => {
                if (document.activeElement?.tagName === "IFRAME") self.close("outside");
            });
        };
        // A tap on a phone does not move focus into the frame either, so a
        // same-origin frame's own document is listened to as well. A frame
        // (re)loading while open is picked up on its load.
        const frameDocs = new Set<Document>();
        const shieldedFrames = new Set<HTMLIFrameElement>();
        const onFramePress = () => self.close("outside");
        const watchFrame = (frame: HTMLIFrameElement) => {
            if (isInside(frame)) return;
            // iOS/WebKit may deliver neither focus nor events in script-sandboxed frames.
            if (!shieldedFrames.has(frame)) {
                const shield = frameShields.get(frame) ?? { count: 0, prev: frame.style.pointerEvents };
                shield.count++;
                frameShields.set(frame, shield);
                shieldedFrames.add(frame);
                frame.style.pointerEvents = "none";
            }
            const doc = frame.contentDocument;
            if (!doc || frameDocs.has(doc)) return;
            doc.addEventListener("pointerdown", onFramePress, true);
            frameDocs.add(doc);
        };
        const frames = Array.from(document.querySelectorAll("iframe"));
        const onFrameLoad = (e: Event) => watchFrame(e.currentTarget as HTMLIFrameElement);
        for (const frame of frames) {
            watchFrame(frame);
            frame.addEventListener("load", onFrameLoad);
        }
        const onDocumentLoad = (e: Event) => {
            if (e.target instanceof HTMLIFrameElement) watchFrame(e.target);
        };
        document.addEventListener("load", onDocumentLoad, true);
        // Capture phase: dialogs stop mousedown propagation on their card so the
        // backdrop does not close them, which would otherwise swallow this too.
        // Pointer events so a tap closes it on touch screens as well.
        document.addEventListener("pointerdown", onPointerDown, true);
        document.addEventListener("keydown", onKey, true);
        window.addEventListener("blur", onBlur);
        return () => {
            clearTimeout(blurTimer);
            for (const doc of frameDocs) doc.removeEventListener("pointerdown", onFramePress, true);
            for (const frame of frames) frame.removeEventListener("load", onFrameLoad);
            document.removeEventListener("load", onDocumentLoad, true);
            for (const frame of shieldedFrames) {
                const shield = frameShields.get(frame)!;
                if (--shield.count === 0) {
                    frame.style.pointerEvents = shield.prev;
                    frameShields.delete(frame);
                }
            }
            document.removeEventListener("pointerdown", onPointerDown, true);
            document.removeEventListener("keydown", onKey, true);
            window.removeEventListener("blur", onBlur);
            const at = layers.indexOf(self);
            if (at >= 0) layers.splice(at, 1);
        };
    }, [open]);
}
