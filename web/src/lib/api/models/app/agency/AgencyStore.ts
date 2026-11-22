// GET /agency/:agencyId/store: domains and mailboxes bought in the store across the clients the caller manages mailboxes in. Money is micro-USD.

export type AgencyStoreAttentionKind = "connect_failed" | "billing_failed" | "renewal_failed" | "auto_renew_off" | "order_failed";

export interface AgencyStoreAttention {
    kind: AgencyStoreAttentionKind | string;
    count: number;
}

export interface AgencyStoreRenewalRef {
    domain_id: string;
    domain: string;
    expires_at: Date | string;
    auto_renew: boolean;
}

export interface AgencyStoreClient {
    organization_id: string;
    name: string;
    avatar_url?: string;
    domains: number;
    mailboxes_live: number;
    mailboxes_connected: number;
    monthly_spend_micros: number;
    renewals_30d: number;
    next_renewal: AgencyStoreRenewalRef | null;
    orders_open: number;
    attention: AgencyStoreAttention[];
}

export interface AgencyStoreRenewal extends AgencyStoreRenewalRef {
    organization_id: string;
    organization_name: string;
    renew_micros: number;
}

export interface AgencyStoreOverview {
    enabled: boolean;
    clients: AgencyStoreClient[];
    /** The next 30 days, soonest first, at most 50. */
    renewals: AgencyStoreRenewal[];
    totals: { domains: number; mailboxes_live: number; monthly_spend_micros: number; renewals_30d: number; attention: number };
}
