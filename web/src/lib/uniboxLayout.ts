import {
  UNIBOX_LIST_MAX_WIDTH,
  UNIBOX_LIST_MIN_WIDTH,
} from "@/stores/slices/uiSlice";

/** Minimum readable width for the message column of the thread pane. */
export const UNIBOX_THREAD_MIN_WIDTH = 360;

/**
 * ContactContextPanel's static width from `lg` up (`lg:w-80`). It is a flex
 * sibling INSIDE the thread pane, so the thread's reserve has to cover it or
 * the message column gets whatever is left after the contact form.
 */
export const UNIBOX_CONTACT_RAIL_WIDTH = 320;

/** The drag handle between the list and the thread (`w-1.5`). */
export const UNIBOX_HANDLE_WIDTH = 6;

/**
 * How wide the conversation list is allowed to get right now.
 *
 * The preference bounds live in the store; this is the viewport's say. It is a
 * floor, not a ceiling, when the window is too small for both panes: below
 * `UNIBOX_LIST_MIN_WIDTH` the thread pane gives way instead, which is what the
 * fixed 360px column did before it was resizable. Capping the list there
 * instead would shrink it to ~150px on a tablet and leave the drag handle
 * unable to move anything.
 *
 * Geometry is passed in rather than measured so the arithmetic is testable
 * without a layout engine.
 */
export function uniboxListMaxWidth({
  rowRight,
  listLeft,
  reservedForThread,
}: {
  rowRight: number;
  listLeft: number;
  reservedForThread: number;
}): number {
  // A zero-height/width box means the element is not laid out (jsdom, or a
  // display:none ancestor). Refusing to cap on a guess is better than capping
  // to the floor.
  if (!Number.isFinite(rowRight) || !Number.isFinite(listLeft) || rowRight <= listLeft) {
    return UNIBOX_LIST_MAX_WIDTH;
  }
  const available = rowRight - listLeft - UNIBOX_HANDLE_WIDTH - reservedForThread;
  return Math.max(
    UNIBOX_LIST_MIN_WIDTH,
    Math.min(UNIBOX_LIST_MAX_WIDTH, Math.round(available)),
  );
}

/** What the thread pane needs, including the contact rail when it is showing. */
export function uniboxThreadReserve(contactRailShowing: boolean): number {
  return (
    UNIBOX_THREAD_MIN_WIDTH + (contactRailShowing ? UNIBOX_CONTACT_RAIL_WIDTH : 0)
  );
}
