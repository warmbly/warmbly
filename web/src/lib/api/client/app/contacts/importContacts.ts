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
    | string; // "custom:<key>"

export type ImportDedupStrategy = "skip" | "update" | "create_duplicate";

export interface ImportColumnMapping {
    index: number;
    target: ImportColumnTarget;
    custom_key?: string;
}

export interface ImportPreview {
    filename: string;
    format: string;
    total_rows: number;
    columns: string[];
    has_header: boolean;
    sample_rows: string[][];
    suggested_mapping: ImportColumnMapping[];
}

export interface ImportCommitOptions {
    mapping: ImportColumnMapping[];
    dedup: ImportDedupStrategy;
    has_header: boolean;
    category_ids?: string[];
    campaign_ids?: string[];
    subscribed_default?: boolean;
}

export interface ImportRowError {
    line: number;
    email?: string;
    values?: string[];
    reason: string;
}

export interface ImportQuality {
    assessment_id?: string;
    invalid: number;
    disposable: number;
    role: number;
    risky_tld: number;
    bad_address_ratio: number;
}

export interface ImportResult {
    total: number;
    imported: number;
    updated: number;
    skipped: number;
    failed: number;
    started_at: string;
    ended_at: string;
    quality?: ImportQuality;
    errors?: ImportRowError[];
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
