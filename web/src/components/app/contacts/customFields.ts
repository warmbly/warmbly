// Custom-field rows as the contact forms edit them, and what they save as.

import {
    CUSTOM_KEY_RULES,
    foldCustomKey,
    isValidCustomKey,
    matchExistingKey,
    normalizeCustomKey,
} from "./importShared";
import { fieldsOf, recordFromCF } from "./contact-edit/rebase";

// A `draft` row is one the user is naming; every other row fills a labelled field.
export interface CustomField {
    name: string;
    value: string;
    draft?: boolean;
}

// The labelled fields a form shows: the workspace's, most used first, then any
// the form has held that the cached list does not have yet.
export function labelledKeys(workspace: string[], held: Iterable<string>): string[] {
    const out = [...workspace];
    const known = new Set(out);
    for (const k of held) {
        if (k === "" || known.has(k)) continue;
        known.add(k);
        out.push(k);
    }
    return out;
}

// The labelled field a draft's name refers to, spelled exactly or differing
// only in case and separators.
export function draftMatch(name: string, keys: string[]): { key: string; exact: boolean } | null {
    const n = normalizeCustomKey(name);
    const key = matchExistingKey(n, keys);
    return key === undefined ? null : { key, exact: key === n };
}

// Keys whose folded form contains the query, most used first.
export function suggestKeys(query: string, keys: string[], limit = 8): string[] {
    const q = foldCustomKey(normalizeCustomKey(query));
    return keys.filter((k) => q === "" || foldCustomKey(k).includes(q)).slice(0, limit);
}

// Why the rows cannot be saved as they stand, or null.
export function customFieldsProblem(rows: CustomField[]): string | null {
    const seen = new Set<string>();
    for (const r of rows) {
        const name = normalizeCustomKey(r.name);
        const filled = r.value.trim() !== "";
        if (name === "") {
            if (filled) return "Name every custom field you filled in, or remove it.";
            continue;
        }
        if (!isValidCustomKey(name)) return `“${name}” cannot be used as a field name. ${CUSTOM_KEY_RULES}`;
        if (!filled) continue;
        if (seen.has(name)) return `“${name}” is filled in twice.`;
        seen.add(name);
    }
    return null;
}

// The PATCH body: the server merges it into the stored fields and an empty
// value removes a key, so a cleared or removed field is sent as "".
export function customFieldsPatch(
    server: Record<string, string> | undefined,
    rows: CustomField[],
): Record<string, string> {
    const before = recordFromCF(fieldsOf(server));
    const after = recordFromCF(rows);
    const entries: [string, string][] = [];
    for (const [k, v] of Object.entries(after)) {
        if (!Object.hasOwn(before, k) || before[k] !== v) entries.push([k, v]);
    }
    for (const k of Object.keys(before)) {
        if (!Object.hasOwn(after, k)) entries.push([k, ""]);
    }
    return Object.fromEntries(entries);
}
