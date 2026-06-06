// Mirror of the backend's models/lead_sync.go shapes. Field names match the
// JSON contract exactly. Lead-sync is an on-demand Google Sheet → contacts
// importer: a saved "sync source" the user re-runs via a "Sync now" button.
//
// We deliberately REUSE the contact-import column-mapping + result types from
// the import wizard rather than redeclaring them, so the same TargetPicker /
// MapStep mapper and result rendering work verbatim against /google/preview
// and /sources/:id/sync responses.

import type {
    ImportColumnMapping,
    ImportDedupStrategy,
    ImportPreview,
    ImportResult,
} from "@/lib/api/client/app/contacts/importContacts";

// Re-export the reused contact-import types under lead-sync names so callers
// can import everything lead-sync from one module.
export type {
    ImportColumnMapping,
    ImportDedupStrategy,
    ImportPreview,
    ImportResult,
};

// LeadSyncStatus mirrors models.LeadSyncStatus. The synchronous SyncNow only
// ever lands on idle/error; "syncing" exists for completeness/future async use.
export type LeadSyncStatus = "idle" | "syncing" | "error";

// A saved on-demand Google-Sheet → contacts source.
export interface LeadSyncSource {
    id: string;
    organization_id: string;
    created_by_user_id: string;
    provider: string;
    connection_id: string;
    sheet_id: string;
    sheet_title?: string;
    tab_title?: string;
    a1_range?: string;
    has_header: boolean;
    column_mapping: ImportColumnMapping[];
    dedup: ImportDedupStrategy;
    target_campaign_id?: string;
    category_ids: string[];
    subscribed_default: boolean;
    label?: string;
    status: LeadSyncStatus;
    last_synced_at?: string;
    last_result?: ImportResult;
    last_error?: string;
    created_at: string;
    updated_at: string;
}

// Result of POST /sources/:id/sync.
export interface LeadSyncResult {
    source_id: string;
    result: ImportResult;
}

// Reports whether the org has a connected (hidden) google_sheets OAuth
// connection usable for lead-sync.
export interface LeadSyncConnection {
    connected: boolean;
    connection: {
        id: string;
        external_account_name: string;
        status: string;
    } | null;
}

// One tab of a spreadsheet, from /google/spreadsheet.
export interface SheetTab {
    title: string;
    index: number;
}

// Spreadsheet metadata returned by /google/spreadsheet — drives the tab picker.
export interface SheetMeta {
    sheet_id: string;
    title: string;
    tabs: SheetTab[];
}

// Request body for creating a new source.
export interface CreateLeadSyncSource {
    connection_id: string;
    sheet_id: string;
    sheet_title: string;
    tab_title: string;
    has_header: boolean;
    column_mapping: ImportColumnMapping[];
    dedup: ImportDedupStrategy;
    target_campaign_id?: string;
    category_ids: string[];
    subscribed_default?: boolean;
    label: string;
}

// Request body for PATCH — all fields optional; omitted ⇒ unchanged.
// `clear_campaign: true` detaches the target campaign (a nil pointer alone
// can't express "clear").
export interface UpdateLeadSyncSource {
    sheet_id?: string;
    sheet_title?: string;
    tab_title?: string;
    has_header?: boolean;
    column_mapping?: ImportColumnMapping[];
    dedup?: ImportDedupStrategy;
    target_campaign_id?: string;
    clear_campaign?: boolean;
    category_ids?: string[];
    subscribed_default?: boolean;
    label?: string;
}
