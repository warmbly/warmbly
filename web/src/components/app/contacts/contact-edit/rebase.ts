// Rebasing the 360 panel's draft onto a record that changed under it.
//
// The panel is a long-lived form over one contact, and the contact keeps
// moving while it is open: lifting a suppression on the Overview tab
// re-subscribes them, and a teammate's edit arrives through the audit spine.
// Adopting the new value wholesale would throw away what the user is typing;
// ignoring it leaves the draft disagreeing with a record nobody edited, which
// is what made closing the panel ask to discard changes that were never made
// (issue #415).
//
// So: field by field, keep what the user changed and take the server's value
// for everything they did not touch.

import type MiniCampaign from "@/lib/api/models/app/campaigns/MiniCampaign";
import type { CustomField } from "../customFields";
import { normalizeCustomKey } from "../importShared";

export function rebase<T>(
    local: T,
    prevServer: T,
    nextServer: T,
    same: (a: T, b: T) => boolean = Object.is,
): T {
    return same(local, prevServer) ? nextServer : local;
}

// Set equality on ids: order is not meaningful for campaigns or categories.
// Sorted rather than through a Set, so two lists of the same length cannot
// compare equal just because one repeats an id the other does not.
export function sameIDs(a: string[], b: string[]): boolean {
    if (a.length !== b.length) return false;
    const x = [...a].sort();
    const y = [...b].sort();
    return x.every((id, i) => id === y[i]);
}

export function idsOf(items: { id: string }[]): string[] {
    return items.map((i) => i.id);
}

export function sameCampaigns(a: MiniCampaign[], b: MiniCampaign[]): boolean {
    return sameIDs(idsOf(a), idsOf(b));
}

// The rows as they save (unnamed and empty dropped); built from entries so a field named `__proto__` stays an own property.
export function recordFromCF(fields: CustomField[]): Record<string, string> {
    const entries: [string, string][] = [];
    for (const f of fields) {
        const name = normalizeCustomKey(f.name);
        if (!name || f.value === "") continue;
        entries.push([name, f.value]);
    }
    return Object.fromEntries(entries);
}

export function fieldsOf(record: Record<string, string> | undefined): CustomField[] {
    return Object.entries(record ?? {}).map(([name, value]) => ({ name, value }));
}

// Key by key, not through JSON: two records with the same entries in a
// different order are the same record, and stringifying them says otherwise.
// That is what left the panel permanently unsaved after a custom-field row was
// removed and typed back in.
export function sameRecords(a: Record<string, string>, b: Record<string, string>): boolean {
    const keys = Object.keys(a);
    if (keys.length !== Object.keys(b).length) return false;
    return keys.every((k) => Object.hasOwn(b, k) && a[k] === b[k]);
}

// Whether two sets of rows save as the same thing. This is the comparison for
// `dirty` and the save payload, where a row with no name yet is nothing.
export function sameFields(a: CustomField[], b: CustomField[]): boolean {
    return sameRecords(recordFromCF(a), recordFromCF(b));
}

// Whether two sets of rows ARE the same, half-typed rows included. This is the
// comparison for the rebase: a row whose name is still blank holds work the
// user is in the middle of, and dropping it because it saves as nothing would
// delete what they were typing.
export function sameRows(a: CustomField[], b: CustomField[]): boolean {
    if (a.length !== b.length) return false;
    return a.every((f, i) => f.name === b[i].name && f.value === b[i].value && !f.draft === !b[i].draft);
}

// Key by key, so a teammate's new field reaches the draft instead of being saved as a removal.
export function rebaseFields(
    local: CustomField[],
    prevServer: CustomField[],
    nextServer: CustomField[],
): CustomField[] {
    if (sameRows(local, prevServer)) return nextServer;
    const prev = new Map(prevServer.map((f) => [f.name, f.value]));
    const next = new Map(nextServer.map((f) => [f.name, f.value]));
    const out: CustomField[] = [];
    const drafts: CustomField[] = [];
    const named = new Set<string>();
    for (const f of local) {
        if (f.draft) {
            drafts.push(f);
            continue;
        }
        named.add(f.name);
        const untouched = prev.has(f.name) && prev.get(f.name) === f.value;
        if (!untouched) out.push(f);
        else if (next.has(f.name)) out.push({ ...f, value: next.get(f.name)! });
    }
    // A field the server gained that the user neither has nor removed.
    for (const [name, value] of next) {
        if (!named.has(name) && !prev.has(name)) out.push({ name, value });
    }
    return [...out, ...drafts];
}

// A row with a value but no name yet saves nowhere, so no comparison of what
// would be sent can see it. It is still work in progress, and closing the
// panel on it should ask before throwing it away.
export function hasUnnamedValue(fields: CustomField[]): boolean {
    return fields.some((f) => !f.name.trim() && f.value.trim() !== "");
}
