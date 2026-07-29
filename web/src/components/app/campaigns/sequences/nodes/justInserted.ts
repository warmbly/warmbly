// Tracks the nodes created THIS session by a toolbar/insert command, so a chip's
// config popover opens exactly once — right after it's inserted — and never again
// on the remounts caused by toggling the editor's Edit/Preview tabs (which fully
// unmount and rebuild the TipTap view, re-running each node view's initial state).
//
// The insert command marks the fresh node's id; the node view consumes it on mount.
// Loaded/pasted content never marks anything, so existing blocks stay closed.

const pending = new Set<string>();

// markJustInserted flags a node id as freshly inserted (call it from the command).
export function markJustInserted(id: string): void {
    if (id) pending.add(id);
}

// consumeJustInserted reports whether this id was just inserted, clearing the flag
// so a later remount won't reopen the popover.
export function consumeJustInserted(id: string): boolean {
    if (id && pending.has(id)) {
        pending.delete(id);
        return true;
    }
    return false;
}

// freshId mints a transient id for nodes (like the conditional) that don't already
// carry a stable serialized id.
export function freshId(): string {
    if (typeof crypto !== "undefined" && crypto.randomUUID) return crypto.randomUUID();
    return `id-${Math.random().toString(36).slice(2)}${Date.now().toString(36)}`;
}
