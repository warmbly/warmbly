// /admin/transfers and /admin/organizations/:id/{exports,imports} — the
// operator's side of workspace archives. Same jobs owners build from
// Settings > Data, keyed by organization id instead of the caller's own org.
// Shapes mirror internal/models/org_transfer.go and the transfer section of
// internal/models/admin_ops.go.

import { Request } from "@/lib/api/client";

export type OrgTransferStatus = "queued" | "running" | "completed" | "failed" | "expired";

export type OrgDataGroup =
    | "core"
    | "contacts"
    | "campaigns"
    | "crm"
    | "automations"
    | "ai"
    | "warmup"
    | "inbox"
    | "sending"
    | "events"
    | "logs"
    | "billing";

export type OrgImportConflict = "skip" | "overwrite";

export interface OrgDataGroupInfo {
    key: OrgDataGroup;
    label: string;
    description: string;
    required: boolean;
    heavy: boolean;
    requires?: OrgDataGroup[];
}

// The admin API has no catalog endpoint (the org-scoped one needs an org
// context), so this is models.OrgDataGroupCatalog copied verbatim. Keep the
// two in step when a group is added.
export const ORG_DATA_GROUP_CATALOG: OrgDataGroupInfo[] = [
    { key: "core", label: "Workspace", description: "Organization, members, roles, teams, mailboxes, API keys, webhooks, and settings.", required: true, heavy: false },
    { key: "contacts", label: "Contacts", description: "Contacts, categories, notes, activities, and the suppression list.", required: false, heavy: false },
    { key: "campaigns", label: "Campaigns", description: "Campaigns, sequences, senders, attachments, and per-campaign settings.", required: false, heavy: false, requires: ["contacts"] },
    { key: "crm", label: "CRM", description: "Pipelines, deals, tasks, and meeting bookings.", required: false, heavy: false },
    { key: "automations", label: "Automations", description: "Automations, connected integrations, and lead sync sources.", required: false, heavy: false },
    { key: "ai", label: "Assistant", description: "Assistant sessions and messages, skills, MCP servers, and AI settings.", required: false, heavy: false },
    { key: "warmup", label: "Warmup", description: "Warmup participation, routing rules, statistics, and appeals.", required: false, heavy: false },
    { key: "inbox", label: "Inbox", description: "Unified inbox threads, message bodies, and mailbox sync state.", required: false, heavy: true, requires: ["contacts"] },
    { key: "sending", label: "Send history", description: "Queued and completed send tasks with their payloads.", required: false, heavy: true },
    { key: "events", label: "Delivery events", description: "Bounces, complaints, opens, clicks, and placement tests.", required: false, heavy: true },
    { key: "logs", label: "Logs", description: "Audit log, campaign logs, and notifications.", required: false, heavy: true, requires: ["campaigns"] },
    { key: "billing", label: "Billing history", description: "Subscription, credit ledger, and referral records. Read-only on import: the destination instance owns billing.", required: false, heavy: false },
];

// MinOrgExportPassphraseLength in org_transfer.go.
export const MIN_EXPORT_PASSPHRASE = 12;

// Expands a selection over the catalog's dependencies, mirroring the server's
// NormalizeGroups. Required groups are always in.
export function expandGroups(
    selected: Iterable<OrgDataGroup>,
    catalog: OrgDataGroupInfo[] = ORG_DATA_GROUP_CATALOG,
): Set<OrgDataGroup> {
    const byKey = new Map(catalog.map((g) => [g.key, g]));
    const out = new Set<OrgDataGroup>(catalog.filter((g) => g.required).map((g) => g.key));
    const queue = [...selected];
    while (queue.length > 0) {
        const key = queue.pop() as OrgDataGroup;
        if (out.has(key) || !byKey.has(key)) continue;
        out.add(key);
        queue.push(...(byKey.get(key)?.requires ?? []));
    }
    return out;
}

// The selected groups that depend on `key`, so unticking it can explain itself.
export function dependentsOf(
    key: OrgDataGroup,
    selected: Set<OrgDataGroup>,
    catalog: OrgDataGroupInfo[] = ORG_DATA_GROUP_CATALOG,
): OrgDataGroupInfo[] {
    return catalog.filter((g) => selected.has(g.key) && (g.requires ?? []).includes(key));
}

export interface OrgArchiveUser {
    id: string;
    email: string;
    first_name: string;
    last_name: string;
    role: string;
    is_owner: boolean;
}

export interface OrgArchiveInfo {
    format_version: number;
    source_instance: string;
    source_app_version: string;
    organization_id: string;
    organization_name: string;
    exported_at: string;
    groups: OrgDataGroup[];
    has_secrets: boolean;
    row_counts: Record<string, number> | null;
    blob_count: number;
    members: OrgArchiveUser[] | null;
}

export interface OrgExportJob {
    id: string;
    organization_id: string;
    requested_by?: string | null;
    status: OrgTransferStatus;
    groups: OrgDataGroup[];
    include_secrets: boolean;
    format_version: number;
    progress_percent: number;
    progress_stage: string;
    archive_bytes?: number | null;
    archive_sha256?: string | null;
    row_counts: Record<string, number> | null;
    error_message?: string | null;
    started_at?: string | null;
    completed_at?: string | null;
    expires_at?: string | null;
    created_at: string;
    updated_at: string;
}

export interface OrgImportJob {
    id: string;
    organization_id: string;
    requested_by?: string | null;
    status: OrgTransferStatus;
    archive_bytes?: number | null;
    archive_sha256?: string | null;
    source_manifest?: OrgArchiveInfo | null;
    groups: OrgDataGroup[];
    conflict_strategy: OrgImportConflict;
    progress_percent: number;
    progress_stage: string;
    row_counts: Record<string, number> | null;
    warnings: string[] | null;
    error_message?: string | null;
    started_at?: string | null;
    completed_at?: string | null;
    created_at: string;
    updated_at: string;
}

export interface OrgImportPreflight {
    archive: OrgArchiveInfo;
    secrets_unsealed: boolean;
    conflicts: Record<string, number> | null;
    unknown_members: OrgArchiveUser[] | null;
    skipped_tables: string[] | null;
    warnings: string[] | null;
}

export interface AdminTransferJob {
    kind: "export" | "import";
    id: string;
    organization_id: string;
    organization_name: string;
    requested_by?: string | null;
    requested_by_email: string;
    status: OrgTransferStatus;
    groups: OrgDataGroup[] | null;
    include_secrets: boolean;
    progress_percent: number;
    progress_stage: string;
    archive_bytes?: number | null;
    error_message?: string | null;
    started_at?: string | null;
    completed_at?: string | null;
    expires_at?: string | null;
    created_at: string;
}

export interface CreateOrgExportRequest {
    groups?: OrgDataGroup[];
    include_secrets?: boolean;
    passphrase?: string;
}

// The import endpoint's "options" multipart field; passphrase travels as its
// own field and the archive as "file".
export interface CreateOrgImportOptions {
    groups?: OrgDataGroup[];
    conflict_strategy?: OrgImportConflict;
}

export function isTransferActive(status: OrgTransferStatus): boolean {
    return status === "queued" || status === "running";
}

export function totalRows(counts: Record<string, number> | null | undefined): number {
    if (!counts) return 0;
    return Object.values(counts).reduce((sum, n) => sum + n, 0);
}

export function formatBytes(n: number | null | undefined): string {
    if (n == null) return "—";
    if (n < 1024) return `${n} B`;
    const units = ["KB", "MB", "GB", "TB"];
    let v = n / 1024;
    let i = 0;
    while (v >= 1024 && i < units.length - 1) {
        v /= 1024;
        i++;
    }
    return `${v.toFixed(v >= 100 ? 0 : 1)} ${units[i]}`;
}

export function listTransfers(limit = 100): Promise<{ data: AdminTransferJob[] | null }> {
    return Request({
        method: "GET",
        url: `/admin/transfers?limit=${limit}`,
        authorization: true,
    });
}

export function listOrgExports(orgId: string): Promise<{ data: OrgExportJob[] | null }> {
    return Request({
        method: "GET",
        url: `/admin/organizations/${orgId}/exports`,
        authorization: true,
    });
}

export function createOrgExport(orgId: string, body: CreateOrgExportRequest): Promise<OrgExportJob> {
    return Request({
        method: "POST",
        url: `/admin/organizations/${orgId}/exports`,
        authorization: true,
        data: body,
    });
}

export function getOrgExport(orgId: string, exportId: string): Promise<OrgExportJob> {
    return Request({
        method: "GET",
        url: `/admin/organizations/${orgId}/exports/${exportId}`,
        authorization: true,
    });
}

export function deleteOrgExport(orgId: string, exportId: string): Promise<void> {
    return Request({
        method: "DELETE",
        url: `/admin/organizations/${orgId}/exports/${exportId}`,
        authorization: true,
    });
}

// Mirrors orgtransfer.ArchiveFilename so the saved file matches what the
// backend names it; Request() drops response headers, so Content-Disposition
// is not available here.
export function archiveFilename(orgName: string, exportId: string): string {
    let slug = orgName
        .toLowerCase()
        .replace(/[ _.]/g, "-")
        .replace(/[^a-z0-9-]/g, "")
        .replace(/^-+|-+$/g, "");
    if (!slug) slug = "workspace";
    if (slug.length > 48) slug = slug.slice(0, 48).replace(/^-+|-+$/g, "");
    return `${slug}-${exportId.slice(0, 8)}.warmbly.zip`;
}

// The download endpoint is bearer-authenticated, so a plain anchor cannot
// fetch it: pull the bytes through the client and hand them to the browser.
export async function downloadOrgExport(
    orgId: string,
    exportId: string,
    orgName: string,
): Promise<{ blob: Blob; filename: string }> {
    const blob = await Request<Blob>({
        method: "GET",
        url: `/admin/organizations/${orgId}/exports/${exportId}/download`,
        authorization: true,
        responseType: "blob",
    });
    return { blob, filename: archiveFilename(orgName, exportId) };
}

export function saveBlob(blob: Blob, filename: string) {
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = filename;
    document.body.appendChild(a);
    a.click();
    a.remove();
    URL.revokeObjectURL(url);
}

export function listOrgImports(orgId: string): Promise<{ data: OrgImportJob[] | null }> {
    return Request({
        method: "GET",
        url: `/admin/organizations/${orgId}/imports`,
        authorization: true,
    });
}

export function preflightOrgImport(
    orgId: string,
    file: File,
    passphrase: string,
): Promise<OrgImportPreflight> {
    const form = new FormData();
    form.append("file", file);
    if (passphrase) form.append("passphrase", passphrase);
    return Request({
        method: "POST",
        url: `/admin/organizations/${orgId}/imports/preflight`,
        authorization: true,
        data: form,
    });
}

export function createOrgImport(
    orgId: string,
    file: File,
    options: CreateOrgImportOptions,
    passphrase: string,
): Promise<OrgImportJob> {
    const form = new FormData();
    form.append("file", file);
    form.append("options", JSON.stringify(options));
    if (passphrase) form.append("passphrase", passphrase);
    return Request({
        method: "POST",
        url: `/admin/organizations/${orgId}/imports`,
        authorization: true,
        data: form,
    });
}
