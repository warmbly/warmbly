// What a screen can offer the keyboard, and the only way a shortcut reaches it.
//
// The global dispatcher knows the keys; it does not know what "move down in
// list" means on any given page. A component that can answer one of these
// registers it while it is mounted, and a shortcut that names an action nobody
// provides is both inert AND hidden from the `?` modal. That is the point: the
// modal used to advertise nine shortcuts with no implementation at all (#484),
// because the shown list and the dispatcher were two unrelated literals.
//
// The registry is module-level rather than store state on purpose: it changes
// on every mount of a list, and routing that through zustand would re-render
// every subscriber for something only a keypress ever reads.

import React from "react";

export type ShortcutActions = Partial<{
    /** Move the selection by n rows (negative moves up). */
    listMove: (delta: number) => void;
    /** Jump the selection to the first or last row. */
    listEdge: (edge: "first" | "last") => void;
    /** Open whatever is selected. */
    listOpen: () => void;
    /** Drop the selection. */
    listDeselect: () => void;
    /** Put the caret in this screen's own search box. */
    focusSearch: () => void;
    /** Open the label menu for the open conversation. */
    labelThread: () => void;
}>;

type Entry = { actions: ShortcutActions; suspended: boolean };

// Innermost mounted provider wins, so a dialog's list takes the keys from the
// page's list behind it.
const stack: React.RefObject<Entry>[] = [];

/**
 * Offer these actions to the keyboard for as long as this component is mounted.
 * Pass `suspended` while something else on screen owns the keys (an open filter
 * sheet, a dialog), which is cheaper and more honest than unmounting.
 */
export function useShortcutActions(
    actions: ShortcutActions,
    options?: { suspended?: boolean },
) {
    const entry = React.useRef<Entry>({ actions, suspended: false });
    entry.current = { actions, suspended: !!options?.suspended };
    React.useEffect(() => {
        stack.push(entry);
        return () => {
            const i = stack.indexOf(entry);
            if (i >= 0) stack.splice(i, 1);
        };
    }, []);
}

/** The handler for one action, or null when nothing on screen provides it. */
export function shortcutAction<K extends keyof ShortcutActions>(
    name: K,
): NonNullable<ShortcutActions[K]> | null {
    for (let i = stack.length - 1; i >= 0; i--) {
        const e = stack[i].current;
        if (e.suspended) continue;
        const fn = e.actions[name];
        if (fn) return fn as NonNullable<ShortcutActions[K]>;
    }
    return null;
}
