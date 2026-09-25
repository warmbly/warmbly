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
    id?: string;
}

// Workspace fields, most used first, then held ones the cached list lacks.
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

// The labelled field a draft names, exactly or up to case and separators.
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

// Why the rows cannot be saved, or null; `payload` is what the save would send.
export function customFieldsProblem(rows: CustomField[], payload: Record<string, string>): string | null {
    const seen = new Set<string>();
    for (const r of rows) {
        const name = normalizeCustomKey(r.name);
        if (name === "") {
            if (r.value.trim() !== "") return "Name every custom field you filled in, or remove it.";
            continue;
        }
        if (r.draft && !isValidCustomKey(name)) return `“${name}” cannot be used as a field name. ${CUSTOM_KEY_RULES}`;
        if (r.value === "") continue;
        if (seen.has(name)) return `“${name}” is filled in twice.`;
        seen.add(name);
    }
    // The server refuses the whole write over one old name, even to remove it.
    const stale = Object.keys(payload).find((k) => !isValidCustomKey(k));
    return stale === undefined ? null : `“${stale}” is an old field name that can no longer be changed.`;
}

// The PATCH body: the server merges it and "" removes a key, so a removed field is sent as "".
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

let draftSeq = 0;

export function newDraft(): CustomField {
    draftSeq += 1;
    return { name: "", value: "", draft: true, id: `new-${draftSeq}` };
}
