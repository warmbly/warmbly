// Workspace archives: the shapes behind Settings → Data.
//
// Mirrors internal/models/org_transfer.go. An archive is a portable copy of the
// whole workspace, used to move between a self-hosted instance and the cloud
// (in either direction).

export type OrgTransferStatus =
    | "queued"
    | "running"
    | "completed"
    | "failed"
    | "expired";

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

/** One selectable slice of a workspace, described by the server. */
export interface OrgDataGroupInfo {
    key: OrgDataGroup;
    label: string;
    description: string;
    /** Required groups cannot be switched off. */
    required: boolean;
    /** Heavy groups dominate archive size on a busy workspace. */
    heavy: boolean;
    /**
     * Groups this one cannot travel without, because its rows hold NOT NULL
     * references into them. The server closes over these anyway; the picker
     * applies them too so the toggles match what the archive actually gets.
     */
    requires?: OrgDataGroup[];
}

/**
 * Expands a selection over the catalog's dependencies, mirroring the server's
 * NormalizeGroups. Required groups are always in.
 */
export function expandGroups(
    selected: Iterable<OrgDataGroup>,
    catalog: OrgDataGroupInfo[],
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

/** The selected groups that depend on `key`, so unticking it can explain itself. */
export function dependentsOf(
    key: OrgDataGroup,
    selected: Set<OrgDataGroup>,
    catalog: OrgDataGroupInfo[],
): OrgDataGroupInfo[] {
    return catalog.filter((g) => selected.has(g.key) && (g.requires ?? []).includes(key));
}

export interface OrgTransferGroupsResponse {
    groups: OrgDataGroupInfo[];
    format_version: number;
    min_passphrase: number;
    retention_days: number;
}

/** A member carried by an archive. Never contains password material. */
export interface OrgArchiveUser {
    id: string;
    email: string;
    first_name: string;
    last_name: string;
    role: string;
    is_owner: boolean;
}

/** What an archive says about itself. */
export interface OrgArchiveInfo {
    format_version: number;
    source_instance: string;
    source_app_version: string;
    organization_id: string;
    organization_name: string;
    exported_at: Date;
    groups: OrgDataGroup[];
    has_secrets: boolean;
    row_counts: Record<string, number>;
    blob_count: number;
    members: OrgArchiveUser[];
}

export interface OrgExportJob {
    id: string;
    organization_id: string;
    requested_by?: string;
    status: OrgTransferStatus;
    groups: OrgDataGroup[];
    include_secrets: boolean;
    format_version: number;
    progress_percent: number;
    progress_stage: string;
    archive_bytes?: number;
    archive_sha256?: string;
    row_counts: Record<string, number>;
    error_message?: string;
    started_at?: Date;
    completed_at?: Date;
    expires_at?: Date;
    created_at: Date;
    updated_at: Date;
}

export interface OrgImportJob {
    id: string;
    organization_id: string;
    requested_by?: string;
    status: OrgTransferStatus;
    archive_bytes?: number;
    archive_sha256?: string;
    source_manifest?: OrgArchiveInfo;
    groups: OrgDataGroup[];
    conflict_strategy: OrgImportConflict;
    progress_percent: number;
    progress_stage: string;
    row_counts: Record<string, number>;
    warnings: string[];
    error_message?: string;
    started_at?: Date;
    completed_at?: Date;
    created_at: Date;
    updated_at: Date;
}

/** What applying an archive would do, computed without writing anything. */
export interface OrgImportPreflight {
    archive: OrgArchiveInfo;
    secrets_unsealed: boolean;
    conflicts: Record<string, number>;
    unknown_members: OrgArchiveUser[];
    skipped_tables: string[];
    warnings: string[];
}

export interface CreateOrgExportPayload {
    groups?: OrgDataGroup[];
    include_secrets?: boolean;
    passphrase?: string;
}

export interface CreateOrgImportOptions {
    groups?: OrgDataGroup[];
    conflict_strategy?: OrgImportConflict;
}

/** Sums a job's per-table counts for the "12,481 rows" summary line. */
export function totalRows(counts: Record<string, number> | undefined): number {
    if (!counts) return 0;
    return Object.values(counts).reduce((sum, n) => sum + n, 0);
}
