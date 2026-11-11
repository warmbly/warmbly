import type { SourceStatus, VendorCatalogEntry } from "@/lib/api/models/app/emails/MailboxSources";

/** One client mailbox as the agency's mailbox list shows it. */
export interface AgencyMailbox {
    id: string;
    organization_id: string;
    client: string;
    email: string;
    name: string;
    provider: string;
    status: string;
    /** Connected, with an unresolved sign-in or sending error. */
    erroring: boolean;
    warmup: boolean;
    /** watch, throttled, quarantined or blocked; absent in good standing. */
    warmup_state?: "watch" | "throttled" | "quarantined" | "blocked";
    /** The inbox vendor it was imported from. */
    vendor?: string;
    campaign_limit: number;
    created_at: Date;
}

export interface AgencyMailboxPage {
    data: AgencyMailbox[];
    pagination: { next_cursor: string | null; has_more: boolean };
}

export interface AgencyMailboxFilters {
    clients: string[];
    attention: boolean;
    q: string;
}

/** A client workspace an agency's vendor account is used in. */
export interface AgencyVendorAccountClient {
    organization_id: string;
    name: string;
    connection_id: string;
    mailboxes: number;
    status: SourceStatus;
    last_error?: string;
}

/** An inbox vendor account the agency holds once and uses in any client. */
export interface AgencyVendorAccount {
    id: string;
    vendor: string;
    label: string;
    created_at: Date;
    status: SourceStatus;
    last_error?: string;
    mailboxes: number;
    clients: AgencyVendorAccountClient[];
}

export interface AgencyVendorAccountList {
    data: AgencyVendorAccount[];
    catalog: VendorCatalogEntry[];
}

export interface AgencyVendorAccountInput {
    vendor?: string;
    label?: string;
    fields?: Record<string, string>;
}
