// Shared, non-component pieces of the contact-import flow. Kept in their own
// module (not exported from ImportWizard.tsx) so both the CSV ImportWizard and
// the Google-Sheet SheetSyncWizard can reuse the exact same column targets,
// dedup options, and error formatter without tripping react-refresh's
// "only export components" rule.

import toast from "react-hot-toast/headless";
import type {
    ImportColumnMapping,
    ImportDedupStrategy,
    ImportPreview,
    ImportResult,
} from "@/lib/api/client/app/contacts/importContacts";

export const STANDARD_TARGETS: { id: string; label: string }[] = [
    { id: "ignore", label: "Ignore" },
    { id: "email", label: "Email" },
    { id: "first_name", label: "First name" },
    { id: "last_name", label: "Last name" },
    { id: "company", label: "Company" },
    { id: "phone", label: "Phone" },
    { id: "subscribed", label: "Subscribed" },
    { id: "categories", label: "Labels" },
    { id: "verification_status", label: "Verification status" },
];

// Vocabularies the verification_status target can read, for the mapping
// row's "recognised as" badge. Mirrors emailverify.KnownVocabulary.
export const VERIFICATION_VOCABULARY_LABELS: Record<string, string> = {
    zerobounce: "ZeroBounce",
    millionverifier: "MillionVerifier",
    cleanmylist: "CleanMyList",
    mailveri: "MailVeri",
    neverbounce: "NeverBounce",
    bouncer: "Bouncer",
    kickbox: "Kickbox",
    emailable: "Emailable",
    debounce: "DeBounce",
    clearout: "Clearout",
    emaillistverify: "EmailListVerify",
    builtin: "Warmbly",
};

export const DEDUP_OPTIONS: { id: ImportDedupStrategy; label: string; hint: string }[] = [
    {
        id: "skip",
        label: "Skip existing",
        hint: "Leave their details alone. They still join the campaigns, labels and segments you pick.",
    },
    {
        id: "update",
        label: "Update existing",
        hint: "Fill in their details from the file. Blank cells never erase what is already there.",
    },
    {
        id: "create_duplicate",
        label: "Create duplicates",
        hint: "Force a new contact. Falls back to update, since one address is one contact per workspace.",
    },
];

// Extract a human-readable message from whatever the API client throws.
// Client.ts rethrows AppError (a plain object), not an Error instance —
// so `err instanceof Error` silently fails and you lose the real reason.
export function describeError(err: unknown, fallback: string): string {
    if (err && typeof err === "object") {
        const e = err as { message?: unknown; error?: unknown; status?: unknown };
        const msg = typeof e.message === "string" ? e.message.trim() : "";
        const title = typeof e.error === "string" ? e.error.trim() : "";
        const status = typeof e.status === "number" ? e.status : undefined;
        if (msg && title && msg !== title) {
            return status ? `${status} ${title}: ${msg}` : `${title}: ${msg}`;
        }
        if (msg) return status ? `${status}: ${msg}` : msg;
        if (title) return status ? `${status} ${title}` : title;
    }
    if (err instanceof Error && err.message) return err.message;
    return fallback;
}

// announceResult toasts a contact-import / sheet-sync result with the standard
// imported/updated/skipped summary (or a warning when rows failed).
export function announceResult(res: ImportResult) {
    if (res.failed === 0) {
        toast.success(
            `Imported ${res.imported} · updated ${res.updated} · skipped ${res.skipped}`,
        );
    } else {
        toast(`Synced with ${res.failed} errors`, { icon: "⚠️" });
    }
}

// ----- Custom-field names -----------------------------------------
//
// Mirrors internal/utils.IsValidJSONKey. A custom field is addressable in
// campaign copy either as {{.Role}} or, for a spaced/dashed name, through the
// server-side rewrite to (index . "Company Mobile"). Anything else would make
// a field the user can store but never merge into an email, so the API rejects
// it — we catch it here so a mistyped name never costs a whole import.
const CUSTOM_KEY_RE = /^[A-Za-z0-9_]+(?:[ -]+[A-Za-z0-9_]+)*$/;

export const CUSTOM_KEY_RULES = "Use letters, numbers, underscores, spaces or dashes.";

export function normalizeCustomKey(key: string): string {
    return key.trim().split(/\s+/).filter(Boolean).join(" ");
}

export function isValidCustomKey(key: string): boolean {
    const k = normalizeCustomKey(key);
    return k.length > 0 && k.length <= 255 && CUSTOM_KEY_RE.test(k);
}

// suggestCustomKey turns a raw spreadsheet header into a name the API accepts,
// so picking "Use as custom field" on a "Company Mobile" column just works.
// Returns "" when nothing usable survives and the user has to type a name.
export function suggestCustomKey(header: string): string {
    const cleaned = normalizeCustomKey(header.replace(/[^A-Za-z0-9_ -]+/g, " "))
        .replace(/^[-\s]+/, "")
        .replace(/[-\s]+$/, "");
    return isValidCustomKey(cleaned) ? cleaned : "";
}

export function isCustomTarget(target: string): boolean {
    return target === "custom" || target.startsWith("custom:");
}

// customKeyOf is the field a custom mapping writes to, reading the legacy
// "custom:<key>" spelling too. "" for a non-custom mapping.
export function customKeyOf(m: ImportColumnMapping): string {
    if (!isCustomTarget(m.target)) return "";
    const explicit = normalizeCustomKey(m.custom_key ?? "");
    return explicit || normalizeCustomKey(m.target.startsWith("custom:") ? m.target.slice(7) : "");
}

// Mirrors contact.FoldCustomFieldKey: the form two spellings of one field
// ("Company URL", "company_url") share.
export function foldCustomKey(key: string): string {
    return key.toLowerCase().replace(/[^\p{L}\p{N}]+/gu, "");
}

// matchExistingKey finds the workspace field a header or typed name refers to:
// the exact name first, then one that differs only in case or separators.
// `existing` is most-used first, so of two such spellings the common one wins.
export function matchExistingKey(name: string, existing: string[]): string | undefined {
    const n = normalizeCustomKey(name);
    if (n === "") return undefined;
    if (existing.includes(n)) return n;
    const f = foldCustomKey(n);
    if (f === "") return undefined;
    return existing.find((k) => foldCustomKey(k) === f);
}

export type CustomKeyStatus =
    | { kind: "existing" }
    | { kind: "similar"; existing: string }
    | { kind: "new" };

// customKeyStatus says whether a custom mapping writes into a field the
// workspace already has, a near-duplicate of one, or a brand new field.
export function customKeyStatus(key: string, existing: string[]): CustomKeyStatus {
    const k = normalizeCustomKey(key);
    if (existing.includes(k)) return { kind: "existing" };
    const match = matchExistingKey(k, existing);
    return match ? { kind: "similar", existing: match } : { kind: "new" };
}

// targetIdentity names where a mapping writes, so two columns writing to the
// same place can be spotted. null for targets that take any number of columns.
export function targetIdentity(m: ImportColumnMapping): string | null {
    if (m.target === "ignore" || m.target === "categories") return null;
    if (isCustomTarget(m.target)) {
        const key = customKeyOf(m);
        return key ? `custom:${key}` : null;
    }
    return m.target;
}

// mappingProblem returns the first reason the mapping cannot be committed, or
// null when it is good to go. Same order of checks as the server so the two
// never disagree about which column is at fault.
export function mappingProblem(mapping: ImportColumnMapping[]): string | null {
    for (const m of mapping) {
        if (!isCustomTarget(m.target)) continue;
        const key = m.custom_key ?? (m.target.startsWith("custom:") ? m.target.slice(7) : "");
        if (normalizeCustomKey(key) === "") {
            return `Column ${m.index + 1} needs a custom field name.`;
        }
        if (!isValidCustomKey(key)) {
            return `"${key.trim()}" is not a valid field name. ${CUSTOM_KEY_RULES}`;
        }
    }
    if (!mapping.some((m) => m.target === "email")) {
        return "Map a column to Email.";
    }
    return null;
}

// derivePreview re-reads a preview under the other header choice, so toggling
// "First row is header" really moves the first row between header and data.
// Column stats are measured under the detected choice and stay approximate.
export function derivePreview(preview: ImportPreview, hasHeader: boolean): ImportPreview {
    if (hasHeader === preview.has_header) return preview;
    const synth = (i: number) => `Column ${i + 1}`;
    if (!hasHeader) {
        // Blank header cells were named "Column N"; as data they are blank again.
        const first = preview.columns.map((c, i) => (c === synth(i) ? "" : c));
        return {
            ...preview,
            has_header: false,
            columns: preview.columns.map((_, i) => synth(i)),
            sample_rows: [first, ...preview.sample_rows],
            total_rows: preview.total_rows + 1,
        };
    }
    const [first = [], ...rest] = preview.sample_rows;
    return {
        ...preview,
        has_header: true,
        columns: preview.columns.map((_, i) => (first[i] ?? "").trim() || synth(i)),
        sample_rows: rest,
        total_rows: Math.max(0, preview.total_rows - 1),
    };
}

// columnSamples is a few distinct values of a column, from the whole-file
// stats when the server sent them, else from the sample rows.
export function columnSamples(preview: ImportPreview, idx: number, n = 3): string[] {
    const stats = preview.column_stats?.[idx];
    if (stats && stats.samples.length > 0) return stats.samples.slice(0, n);
    const out: string[] = [];
    for (const row of preview.sample_rows) {
        const v = (row[idx] ?? "").trim();
        if (v && !out.includes(v)) out.push(v);
        if (out.length >= n) break;
    }
    return out;
}

// fillRate is how full a column is over the whole file, 0..1, or null
// when the server sent no stats.
export function fillRate(preview: ImportPreview, idx: number): number | null {
    const stats = preview.column_stats?.[idx];
    if (!stats || preview.total_rows <= 0) return null;
    return Math.min(1, stats.filled / preview.total_rows);
}

// sampleCSV is a small file with every standard column, for people starting
// from scratch.
export function sampleCSV(): string {
    return [
        "Email,First name,Last name,Company,Phone,Labels,Job title",
        "dana@acme.com,Dana,Reyes,Acme,+1 555 0100,Prospects;Q3,Head of Growth",
        "sam@northwind.io,Sam,Okafor,Northwind,,Prospects,Founder",
    ].join("\n") + "\n";
}

// MAX_IMPORT_UPLOAD_BYTES mirrors the API's upload cap, so an oversized file
// is refused before it spends a minute uploading.
export const MAX_IMPORT_UPLOAD_BYTES = 50 * 1024 * 1024;
