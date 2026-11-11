export type AgencyRole = "owner" | "admin" | "member";

export type AgencyStatus =
    | "trialing"
    | "active"
    | "past_due"
    | "canceled"
    | "unpaid"
    | "incomplete"
    | "incomplete_expired"
    | "paused";

export interface AgencyPlan {
    id: string;
    name?: string;
    price: number;
    price_yearly?: number;
    stripe_price_id?: string;
    stripe_price_id_yearly?: string;
    dedicated_workers: number;
    // Sends shared by every client each period; 0 is no pool cap.
    monthly_send_pool?: number;
    // What the shared AI balance refills to each period.
    agency_ai_allowance_usd?: number;
}

export interface Agency {
    id: string;
    name: string;
    owner_user_id: string;
    plan_id?: string;
    status: AgencyStatus;
    current_period_start?: string;
    current_period_end?: string;
    cancel_at_period_end: boolean;
    created_at: string;
    updated_at: string;
    plan?: AgencyPlan;
    role?: AgencyRole;
    client_count: number;
    has_billing: boolean;
    /** A plan the agency holds without a subscription, while it is in force; `plan` is then that plan. */
    granted?: { plan_id: string; until?: string };
    /** The agency's own billing period while its subscription is in force. */
    billing_interval?: "month" | "year";
}

/** What switching the agency's subscription to another plan or billing period would bill, in the currency's minor unit. */
export interface AgencyPlanChangePreview {
    plan_id: string;
    interval: "month" | "year";
    proration_cents: number;
    amount_due_cents: number;
    currency: string;
    /** When the amounts were calculated; sent back with the switch so it bills exactly them. */
    proration_date: number;
    /** The price the subscription was on; sent back so a switch priced from another plan is refused. */
    from_price_id: string;
}

export interface AgenciesResult {
    data: Agency[];
    billing_enabled: boolean;
}

export interface AgencyPlansResult {
    data: AgencyPlan[];
    billing_enabled: boolean;
}

export interface AgencyMember {
    agency_id: string;
    user_id: string;
    role: AgencyRole;
    created_at: string;
    email: string;
    first_name: string;
    last_name: string;
}

export interface AgencyInvitation {
    id: string;
    agency_id: string;
    email: string;
    role: AgencyRole;
    expires_at: string;
    created_at: string;
}

export interface AgencyStaffResult {
    data: AgencyMember[];
    invitations: AgencyInvitation[];
}

export interface AgencyClient {
    organization_id: string;
    agency_id: string;
    monthly_send_limit?: number | null;
    // Cap on the client's spend from the agency's AI balance per period.
    monthly_ai_limit_usd?: number | null;
    added_at: string;
    name: string;
    slug?: string;
    avatar_url?: string;
    owner_user_id: string;
}

export interface AgencyClientUsage {
    organization_id: string;
    name: string;
    used: number;
    monthly_send_limit?: number | null;
}

export interface AgencyUsage {
    unlimited: boolean;
    period_start: string;
    period_end: string;
    days_left: number;
    pool_size: number;
    topups: number;
    total: number;
    used: number;
    remaining: number;
    projected: number;
    clients: AgencyClientUsage[];
    topup_sends: number;
    topup_price_cents: number;
}

export interface AgencyAIClient {
    organization_id: string;
    name: string;
    spent_usd: number;
    moved_usd: number;
    monthly_ai_limit_usd?: number | null;
}

// GET /agency/:id/ai-balance. Unmetered (a self-host) sends zeros for the rest.
export interface AgencyAIBalance {
    unmetered: boolean;
    period_start: string;
    period_end: string;
    allowance_usd: number;
    allowance_left_usd: number;
    purchased_usd: number;
    bought_this_period_usd: number;
    spent_usd: number;
    moved_usd: number;
    balance_usd: number;
    clients: AgencyAIClient[];
    top_up: { min_usd: number; max_usd: number; presets_usd: number[] | null; default_usd: number };
}

export interface AgencyAITransferResult {
    amount_usd: number;
    agency_balance_usd: number;
    workspace_balance_usd: number;
}

export interface AgencyClientPatch {
    monthly_send_limit?: number | null;
    monthly_ai_limit_usd?: number | null;
}

export interface AgencyRef {
    id: string;
    name: string;
}
