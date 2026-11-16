// A migration from another outreach tool (Smartlead, Instantly, lemlist, ...).
// Mirrors internal/models/tool_migration.go and the outreachsource types it embeds.

import type Pagination from "@/lib/api/models/app/Pagination";

/** Names a tool, such as "smartlead" or "replyio". */
export type ToolProviderId = string;

/** One category of data a migration can bring over. */
export type ToolMigrationKind = "mailboxes" | "campaigns" | "leads" | "blocklist";

/** Every kind in the order a migration applies them. */
export const TOOL_MIGRATION_KINDS: ToolMigrationKind[] = ["blocklist", "mailboxes", "campaigns", "leads"];

/** One credential input the provider needs. */
export interface ToolProviderField {
    key: string;
    label: string;
    help?: string;
    secret: boolean;
    optional?: boolean;
    placeholder?: string;
    /** Limits the value to a fixed set, such as a data region. */
    options?: string[];
}

export interface ToolProviderInfo {
    id: ToolProviderId;
    label: string;
    fields: ToolProviderField[];
    key_help: string;
    key_url?: string;
    /** Kinds the provider's API can read. Anything else comes from a CSV. */
    kinds: ToolMigrationKind[];
    /** One key sees several client accounts or workspaces. */
    workspaces: boolean;
    /** The tool issues one key per workspace, so an agency adds a key for each. */
    multi_key: boolean;
    /** The API returns mailbox passwords, so SMTP and IMAP mailboxes connect without asking again. */
    mailbox_secrets: boolean;
    notes?: string[];
}

export interface ToolProviderList {
    data: ToolProviderInfo[];
}

/** Values for ToolProviderInfo.fields, keyed by field key. */
export type ToolCredentials = Record<string, string>;

/** Who a key acts as. */
export interface ToolAccount {
    name?: string;
    email?: string;
    plan?: string;
}

export interface ToolWorkspace {
    /** The provider's id, empty for the account itself. */
    id: string;
    name: string;
    /** Index into the migration's credential sets; absent means 0. */
    key?: number;
}

export type ToolCampaignStatus = "draft" | "active" | "paused" | "completed" | "archived";

export interface ToolCampaignSummary {
    id: string;
    name: string;
    status: ToolCampaignStatus;
    steps: number;
    leads: number;
}

/** What one workspace holds; a count the API cannot give cheaply is -1. */
export interface ToolInventory {
    mailboxes: number;
    campaigns: number;
    leads: number;
    blocklist: number;
    campaign_summaries?: ToolCampaignSummary[];
}

export interface ToolMigrationWorkspace {
    workspace: ToolWorkspace;
    inventory: ToolInventory;
    error?: string;
}

export type ToolMigrationStatus = "scanning" | "ready" | "running" | "completed" | "failed" | "canceled";

/** What happens to a lead the old tool was part way through a sequence with. */
export type ToolMigrationInFlight = "restart" | "skip";

export interface ToolMigrationOptions {
    kinds: ToolMigrationKind[];
    /** Limits campaigns (and their leads) to these source ids; empty brings every campaign. */
    campaign_ids?: string[];
    in_flight: ToolMigrationInFlight;
    skip_finished: boolean;
    warmup: boolean;
}

export type ToolMigrationTargetStatus = "pending" | "running" | "completed" | "failed" | "canceled";

export interface ToolMigrationCounts {
    blocklist_imported: number;
    mailboxes_queued: number;
    mailboxes_manual: number;
    campaigns_created: number;
    campaigns_skipped: number;
    steps_created: number;
    leads_read: number;
    contacts_imported: number;
    contacts_updated: number;
    leads_enrolled: number;
    leads_not_enrolled: number;
    leads_suppressed: number;
    leads_failed: number;
}

export type ToolMailboxKind = "google" | "microsoft" | "smtp_imap";

export interface ToolMigrationMailbox {
    email: string;
    kind: ToolMailboxKind;
}

export interface ToolMigrationCampaign {
    source_id: string;
    campaign_id: string;
    name: string;
    missing_senders?: string[];
}

export interface ToolMigrationFollowup {
    reconnect?: ToolMigrationMailbox[];
    notes?: string[];
    campaigns?: ToolMigrationCampaign[];
}

export interface ToolMigrationTarget {
    id: string;
    migration_id: string;
    workspace: ToolWorkspace;
    organization_id: string;
    organization_name?: string;
    status: ToolMigrationTargetStatus;
    /** The kind being imported now, absent before the first. */
    phase?: ToolMigrationKind;
    counts: ToolMigrationCounts;
    followup: ToolMigrationFollowup;
    error?: string;
    mailbox_import_id?: string;
    category_id?: string;
    started_at?: Date;
    finished_at?: Date;
    updated_at: Date;
}

export interface ToolMigration {
    id: string;
    organization_id?: string;
    agency_id?: string;
    created_by_user_id?: string;
    provider: ToolProviderId;
    label: string;
    status: ToolMigrationStatus;
    accounts: ToolAccount[] | null;
    inventory: ToolMigrationWorkspace[] | null;
    options?: ToolMigrationOptions;
    targets: ToolMigrationTarget[] | null;
    error?: string;
    /** False once the keys have been wiped. */
    credentials_held: boolean;
    started_at?: Date;
    finished_at?: Date;
    created_at: Date;
    updated_at: Date;
}

export interface NewToolMigration {
    provider: ToolProviderId;
    label?: string;
    credentials: ToolCredentials[];
}

/** Where one source workspace goes in an agency migration: exactly one of the three. */
export interface ToolMigrationTargetChoice {
    workspace_id: string;
    key: number;
    organization_id?: string;
    create_name?: string;
    skip?: boolean;
}

export interface StartToolMigration {
    options: ToolMigrationOptions;
    targets?: ToolMigrationTargetChoice[];
}

export interface ToolMigrationList {
    data: ToolMigration[];
    pagination: Pagination;
}

/** Whose migrations a call reaches: the current workspace, or one agency. */
export type ToolMigrationScope = { kind: "workspace" } | { kind: "agency"; agencyId: string };

export const isToolMigrationActive = (status: ToolMigrationStatus) => status === "scanning" || status === "running";
