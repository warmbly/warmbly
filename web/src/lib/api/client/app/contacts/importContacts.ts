// Two-step contact import: preview (parse + suggest mapping) then
// commit (apply with chosen options). Both endpoints accept multipart
// uploads so we keep the file off the JSON path entirely — large CSVs
// would otherwise need base64 encoding or weird workarounds.

import Client from "../../Client";
import getToken from "@/lib/helper/getToken";

export type ImportColumnTarget =
    | "ignore"
    | "email"
    | "first_name"
    | "last_name"
    | "company"
    | "phone"
    | "subscribed"
    | "categories"
    | "verification_status"
    | string; // "custom:<key>"

export type ImportDedupStrategy = "skip" | "update" | "create_duplicate";

export interface ImportColumnMapping {
    index: number;
    target: ImportColumnTarget;
    custom_key?: string;
    // For a verification_status column: the vocabulary the header or its
    // values were recognised as (e.g. "zerobounce"). Empty means each value
    // is recognised by itself.
    verification_provider?: string;
}

export interface ImportPreview {
    filename: string;
    format: string;
    total_rows: number;
    columns: string[];
    has_header: boolean;
    sample_rows: string[][];
    suggested_mapping: ImportColumnMapping[];
    // Columns whose suggestion came from the TypeSafe judgment rather than
    // the header or the values. Absent when none did.
    inferred_columns?: number[];
    // Every column's fill over the whole file (the background import sends it).
    column_stats?: ImportColumnStats[];
    // "saved" when this workspace confirmed a mapping for these exact headers before.
    mapping_source?: "suggested" | "saved";
}

export interface ImportColumnStats {
    filled: number;
    distinct: number;
    samples: string[];
}

export interface ImportCommitOptions {
    mapping: ImportColumnMapping[];
    dedup: ImportDedupStrategy;
    has_header: boolean;
    category_ids?: string[];
    campaign_ids?: string[];
    // Leave an existing contact out of campaign_ids while their sequence runs in another campaign.
    skip_active_in_other_campaigns?: boolean;
    segment_ids?: string[];
    subscribed_default?: boolean;
}

export interface ImportRowError {
    line: number;
    email?: string;
    values?: string[];
    reason: string;
}

export interface ImportResult {
    total: number;
    imported: number;
    updated: number;
    skipped: number;
    failed: number;
    // A Date when revived through Request (a lead-sync run); a string from the direct import upload.
    started_at: Date | string;
    ended_at: Date | string;
    errors?: ImportRowError[];
    // Set when more rows failed than the API reports back; `errors` then holds
    // the first slice of them and `failed` is the true count.
    errors_truncated?: boolean;
    // Absent when the import had no segment targets. With targets, true when
    // every membership write landed and false when one did not, with the
    // reason among the notes in `errors`.
    segments_pinned?: boolean;
    // What the uploaded addresses look like. Advisory: a bad list is reported
    // here and stopped at launch, never refused here.
    quality?: ImportQuality;
}

export interface ImportQuality {
    malformed: number;
    disposable: number;
    /** Shared inboxes. Reported, not counted against the list. */
    role: number;
    bad_share_pct: number;
    flagged: boolean;
    summary?: string;
}

async function authHeader(): Promise<Record<string, string>> {
    const token = getToken();
    if (token?.access_token) {
        return { Authorization: `Bearer ${token.access_token}` };
    }
    return {};
}

export async function importPreviewContacts(file: File): Promise<ImportPreview> {
    const form = new FormData();
    form.append("file", file);
    const res = await Client.request<ImportPreview>({
        method: "POST",
        url: "/contacts/import/preview",
        data: form,
        headers: { ...(await authHeader()) },
    });
    return res.data;
}

export async function importCommitContacts(
    file: File,
    opts: ImportCommitOptions,
): Promise<ImportResult> {
    const form = new FormData();
    form.append("file", file);
    form.append("options", JSON.stringify(opts));
    const res = await Client.request<ImportResult>({
        method: "POST",
        url: "/contacts/import/commit",
        data: form,
        headers: { ...(await authHeader()) },
    });
    return res.data;
}
