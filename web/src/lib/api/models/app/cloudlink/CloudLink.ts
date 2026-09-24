// Shapes for the self-hosted warmup pool link, both sides.
//
// Self-hosted side:  GET/POST /cloud-link/*   (Settings > Warmbly Cloud)
// Cloud side:        GET/POST /pool-link/*    (the /connect approval page and
//                    the workspace's linked instances)

export type PoolLinkCodeStatus = "pending" | "approved" | "claimed" | "denied";

export interface PoolLinkOrgInfo {
    id: string;
    name: string;
}

export interface PoolLinkInstance {
    id: string;
    organization_id: string;
    name: string;
    url: string;
    version: string;
    created_at: Date;
    last_seen_at?: Date | null;
    revoked_at?: Date | null;
    mailbox_count: number;
}

export interface PoolLinkPlan {
    tier: "free" | "paid";
    /** null when unlimited */
    mailbox_limit: number | null;
    enrolled: number;
    /** Mailboxes with warmup on and not paused; older servers do not send it. */
    warming?: number;
    price_usd: number;
    /** The cloud's billing page with the warmup plan checkout open; only on the free tier. */
    upgrade_url?: string;
    /** The cloud's billing page; absent when the cloud runs without billing. */
    manage_url?: string;
    warmup_entitled: boolean;
}

/** GET /pool-link/offer — the pool plan's price, and whether it can be bought
 *  here at all. Unavailable means billing is off or no Stripe price is set. */
export interface PoolLinkOffer {
    available: boolean;
    monthly_available: boolean;
    yearly_available: boolean;
    monthly_usd: number;
    yearly_usd: number;
    currency: string;
}

export interface PoolLinkInstanceInfo {
    instance: PoolLinkInstance;
    organization: PoolLinkOrgInfo;
    plan: PoolLinkPlan;
}

export interface PoolLinkCode {
    id: string;
    user_code: string;
    instance_name: string;
    instance_url: string;
    instance_version: string;
    status: PoolLinkCodeStatus;
    organization_id?: string;
    expires_at: Date;
    created_at: Date;
}

export interface PoolLinkWarmupSettings {
    base: number;
    max: number;
    increase: number;
    reply_rate: number;
    start_time: string;
    end_time: string;
    days: number;
    timezone: string;
}

export interface PoolLinkWarmupStatus {
    enabled: boolean;
    paused: boolean;
    paused_at?: Date | null;
    started_at: Date;
    current_volume: number;
    target_volume: number;
    max_volume: number;
    reply_rate: number;
    days_active: number;
}

export interface PoolLinkWarmupHealth {
    state: "healthy" | "watch" | "throttled" | "quarantined" | "blocked";
    score: number;
    reason?: string;
    /** @deprecated Always 0 since the warmup spam score was retired; read score and reason. */
    spam_score: number;
    blocked_until?: Date | null;
    evaluated_at?: Date | null;
    partner_mailboxes_7d?: number;
    partner_domains_7d?: number;
    partner_organizations_7d?: number;
    received_7d?: number;
    senders_7d?: number;
}

export interface PoolLinkMailboxError {
    id: string;
    error_code: string;
    severity: string;
    title: string;
    message: string;
    action_required?: string | null;
    created_at: Date;
}

export interface PoolLinkMailboxState {
    remote_id: string;
    email_account_id: string;
    email: string;
    name: string;
    provider: string;
    status: string;
    enrolled_at: Date;
    /** The cloud holds the only credential; the instance sends with brokered tokens. */
    managed: boolean;
    warmup?: PoolLinkWarmupStatus | null;
    health?: PoolLinkWarmupHealth | null;
    sent_today: number;
    sent_7d: number;
    replied_7d: number;
    spam_placed_7d: number;
    errors?: PoolLinkMailboxError[];
    auth_state: string;
    settings: PoolLinkWarmupSettings;
}

export interface CloudLink {
    cloud_url: string;
    instance_id: string;
    organization_name: string;
    connected_at: Date;
    last_synced_at?: Date | null;
    last_error?: string;
}

export interface CloudLinkStatus {
    connected: boolean;
    link?: CloudLink | null;
    info?: PoolLinkInstanceInfo | null;
    reachable: boolean;
    error?: string;
    default_cloud_url: string;
}

export interface CloudLinkPendingConnect {
    user_code: string;
    verification_url: string;
    cloud_url: string;
    expires_at: Date;
    interval: number;
}

export interface CloudLinkPollResult {
    status: PoolLinkCodeStatus;
    link?: CloudLink | null;
    info?: PoolLinkInstanceInfo | null;
}

export interface CloudLinkMailboxRow {
    id: string;
    email: string;
    name: string;
    provider: string;
    status: string;
    enrolled: boolean;
    enrolled_at?: Date | null;
    managed: boolean;
    cloud?: PoolLinkMailboxState | null;
}

/** A Google or Microsoft consent started through Warmbly Cloud. */
export interface CloudLinkOAuthStart {
    url: string;
    session: string;
}

/** A mailbox connected directly on the cloud workspace that this instance can adopt. */
export interface PoolLinkWorkspaceMailbox {
    id: string;
    email: string;
    name: string;
    provider: string;
    status: string;
}
