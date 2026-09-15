// Where a drag lands in the form builder's field list. The canvas wraps into
// rows and two half-width fields share one, so no dnd-kit sorting strategy can
// predict where a neighbour ends up: nothing reflows while a drag is in flight
// and this module is what the insertion caret draws and what the drop applies
// (#497).

import type { FormField } from "@/lib/api/models/app/forms/Form";

/** Palette draggables carry their block type; a field carries its own id. */
export const PALETTE_PREFIX = "palette:";
/** The whole build canvas; a release outside it is a cancel, not a drop. */
export const CANVAS_DROPPABLE_ID = "form-canvas";
/** The slot after the last field. */
export const CANVAS_END_DROPPABLE_ID = "form-canvas-end";

export function isPaletteDrag(activeId: string): boolean {
    return activeId.startsWith(PALETTE_PREFIX);
}

/** Two half-width fields share a row; everything else takes the whole row. */
export function isHalfWidth(f: FormField): boolean {
    return f.type !== "page_break" && f.width === "half";
}

/**
 * Which fields begin a flex-wrap row: a half-width field joins the one before
 * it only when that one opened its row. A caret for a slot inside a row is
 * vertical, everywhere else it is a rule above the row.
 */
export function rowStarts(fields: FormField[]): boolean[] {
    const opens: boolean[] = [];
    let roomOnRow = false;
    for (const f of fields) {
        const half = isHalfWidth(f);
        opens.push(!(half && roomOnRow));
        roomOnRow = half && !roomOnRow;
    }
    return opens;
}

/**
 * The slot a release would land in, as an index into `fields` before the move,
 * or null when it resolves to nothing. Hovering a field below the dragged one
 * lands after it and above it lands before it, which is what makes dragging the
 * first field onto the last one put it last.
 */
export function insertionIndex(fields: FormField[], activeId: string, overId: string | null): number | null {
    if (!overId) return null;
    if (overId === CANVAS_END_DROPPABLE_ID) return fields.length;
    const to = fields.findIndex((f) => f.id === overId);
    if (to < 0) return null;
    if (isPaletteDrag(activeId)) return to;
    const from = fields.findIndex((f) => f.id === activeId);
    if (from < 0) return null;
    return to > from ? to + 1 : to;
}

/** True when the slot is the one the dragged field already occupies. */
export function isNoopMove(fields: FormField[], activeId: string, at: number | null): boolean {
    if (at === null) return true;
    if (isPaletteDrag(activeId)) return false;
    const from = fields.findIndex((f) => f.id === activeId);
    return from >= 0 && (at === from || at === from + 1);
}

/** Moves a field into `at`, counted against the list before the move. */
export function moveField(fields: FormField[], activeId: string, at: number): FormField[] {
    const from = fields.findIndex((f) => f.id === activeId);
    if (from < 0 || at === from || at === from + 1) return fields;
    const next = [...fields];
    const [moved] = next.splice(from, 1);
    next.splice(at > from ? at - 1 : at, 0, moved);
    return next;
}
