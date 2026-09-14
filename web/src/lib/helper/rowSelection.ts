// Row selection for a server-paged table.
//
// A selection is one of two things and the difference matters at every call
// site: a list of rows the user ticked, or "everything the current search
// matches" minus the rows unticked afterwards. Only the second reaches past
// the pages the table has loaded, and only the server can resolve it, so it
// travels as the search itself.
//
// The wire shape differs per resource (contacts send `contacts`, tasks send
// `tasks`), so each table keeps its own `toRequest`; everything else here is
// shared.

export interface RowSelection {
    /** Every row the current search matches, rather than `ids`. */
    all: boolean;
    /** Ticked rows. Empty while `all` is set. */
    ids: string[];
    /** Rows unticked after a select-all. Empty while `all` is not set. */
    excluded: string[];
}

export const emptySelection: RowSelection = { all: false, ids: [], excluded: [] };

export function isRowSelected(s: RowSelection, id: string): boolean {
    return s.all ? !s.excluded.includes(id) : s.ids.includes(id);
}

/** How many rows the selection covers. `total` is the search's own total. */
export function selectionCount(s: RowSelection, total: number): number {
    return s.all ? Math.max(total - s.excluded.length, 0) : s.ids.length;
}

export function isEmpty(s: RowSelection, total: number): boolean {
    return selectionCount(s, total) === 0;
}

export function toggleRow(s: RowSelection, id: string, on: boolean): RowSelection {
    if (s.all) {
        return { ...s, excluded: on ? s.excluded.filter((x) => x !== id) : [...s.excluded, id] };
    }
    return { ...s, ids: on ? [...s.ids, id] : s.ids.filter((x) => x !== id) };
}

/** Whether every row on screen is selected: the header checkbox's state. */
export function allLoadedSelected(s: RowSelection, loaded: string[]): boolean {
    return loaded.length > 0 && loaded.every((id) => isRowSelected(s, id));
}

/**
 * The header checkbox, which reads the rows on screen. In select-all mode an
 * unchecked box means some of them were unticked, so it puts those back rather
 * than dropping a selection the user never asked to lose; a checked one clears.
 */
export function toggleLoaded(s: RowSelection, loaded: string[]): RowSelection {
    if (s.all) {
        if (allLoadedSelected(s, loaded)) return emptySelection;
        return { ...s, excluded: s.excluded.filter((id) => !loaded.includes(id)) };
    }
    if (allLoadedSelected(s, loaded)) {
        return { ...s, ids: s.ids.filter((id) => !loaded.includes(id)) };
    }
    return { ...s, ids: Array.from(new Set([...s.ids, ...loaded])) };
}

/**
 * A checkbox over a SUBSET of the loaded rows (a group header, say). Unlike
 * toggleLoaded it never clears the whole selection: ticking adds this group,
 * unticking takes only this group back out, which in select-all mode means
 * excluding it rather than dropping every other row with it.
 */
export function toggleGroup(s: RowSelection, ids: string[]): RowSelection {
    const on = allLoadedSelected(s, ids);
    if (s.all) {
        return {
            ...s,
            excluded: on
                ? Array.from(new Set([...s.excluded, ...ids]))
                : s.excluded.filter((id) => !ids.includes(id)),
        };
    }
    return {
        ...s,
        ids: on ? s.ids.filter((id) => !ids.includes(id)) : Array.from(new Set([...s.ids, ...ids])),
    };
}

/**
 * Drops exclusions for rows the search no longer returns, so a teammate
 * deleting an unticked row does not keep counting it against the selection.
 *
 * Only sound with the COMPLETE result set: an id missing from a partial page
 * is a row on a page nobody has loaded, not one that stopped matching.
 * Returns the same object when nothing changed, so callers comparing a
 * submitted selection by reference still recognise it.
 */
export function pruneExcluded(s: RowSelection, present: string[]): RowSelection {
    if (!s.all || s.excluded.length === 0) return s;
    const keep = s.excluded.filter((id) => present.includes(id));
    return keep.length === s.excluded.length ? s : { ...s, excluded: keep };
}

export function selectAllMatching(): RowSelection {
    return { all: true, ids: [], excluded: [] };
}

/**
 * Whether the "select all N matching" bar has anything to offer: every loaded
 * row is ticked and more rows match than are ticked.
 */
export function canSelectAllMatching(s: RowSelection, loaded: string[], total: number): boolean {
    return !s.all && allLoadedSelected(s, loaded) && total > s.ids.length;
}
