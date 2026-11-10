import type { AgencyUsage } from "./Agency";

export type AgencyClientAccess = "full" | "restricted" | "none";

export interface AgencyClientCampaigns {
    active: number;
    paused_by_limits: number;
    paused_by_billing: number;
    sent_today: number;
    sent_7d: number;
    bounced_7d: number;
    bounce_rate_7d: number;
    positive_replies_7d: number;
}

export interface AgencyClientMailboxes {
    total: number;
    disconnected: number;
    erroring: number;
    watch: number;
    throttled: number;
    quarantined: number;
    blocked: number;
}

export interface AgencyClientAdvisor {
    critical: number;
    high: number;
    medium: number;
    low: number;
}

/** One client on the agency home. A section is absent when the caller's membership does not include its permission. */
export interface AgencyOverviewClient {
    organization_id: string;
    name: string;
    avatar_url?: string;
    access: AgencyClientAccess;
    pool_used: number;
    monthly_send_limit?: number | null;
    campaigns?: AgencyClientCampaigns;
    inbox?: { replies_waiting: number };
    mailboxes?: AgencyClientMailboxes;
    advisor?: AgencyClientAdvisor;
}

export type AgencyAttentionKind =
    | "mailbox_disconnected"
    | "mailbox_held"
    | "bounce_spike"
    | "campaigns_paused_limits"
    | "campaigns_paused_billing"
    | "client_cap_near"
    | "pool_low";

export interface AgencyAttention {
    kind: AgencyAttentionKind;
    severity: "critical" | "warning";
    organization_id?: string;
    client_name?: string;
    count: number;
    rate?: number;
}

export interface AgencyOverview {
    generated_at: Date;
    usage: AgencyUsage;
    totals: {
        clients: number;
        sent_today: number;
        replies_waiting: number;
        positive_replies_7d: number;
        active_campaigns: number;
        mailboxes: number;
        mailbox_problems: number;
    };
    attention: AgencyAttention[];
    clients: AgencyOverviewClient[];
}

export interface AgencyInboxThread {
    organization_id: string;
    id: string;
    email_id: string;
    thread_id: string;
    from_addr: string[];
    to_addr: string[];
    subject: string;
    snippet: string;
    internal_date: Date;
    seen: boolean;
    message_count: number;
    has_unread: boolean;
}

export interface AgencyInboxPage {
    data: AgencyInboxThread[];
    pagination: { next_cursor: string | null; has_more: boolean };
}

export interface AgencyInboxFilters {
    clients: string[];
    unread: boolean;
    needsReply: boolean;
    positive: boolean;
    q: string;
}
