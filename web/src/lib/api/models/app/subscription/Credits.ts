// The workspace balance in US dollars: what is left, how it is topped up, and
// what it was spent on. Mirrors /subscription/balance and /subscription/credits
// (internal/api/handler/credits.go).

export interface PaymentMethod {
    brand: string;
    last4: string;
    exp_month: number;
    exp_year: number;
    wallet?: string;
}

export type AutoReloadFailure = "card_declined" | "authentication_required" | "no_payment_method" | "payment_failed" | "unavailable";

export interface AutoReload {
    enabled: boolean;
    threshold_usd: number;
    amount_usd: number;
    monthly_limit_usd: number;
    reloaded_this_month_usd: number;
    failure_code: AutoReloadFailure | null;
    failed_at: Date | null;
    last_reload: { amount_usd: number; at: Date } | null;
    // A soft decline waiting for its next try.
    retry_at: Date | null;
    retry_reason: string | null;
}

export interface TopUpBounds {
    min_usd: number;
    max_usd: number;
    presets_usd: number[];
    default_usd: number;
    /** The card processing fee charged on top of the amount, in basis points; absent from an older backend. */
    fee_bps?: number;
}

// GET /subscription/balance. With billing off only `unlimited: true` is sent.
export interface Balance {
    unlimited: boolean;
    /** Whether this workspace's plan lets it add money; absent from an older backend. */
    can_add_money?: boolean;
    balance_usd: number;
    /** What the workspace's agency balance adds on top of its own; absent outside an agency. */
    agency_usd?: number;
    monthly_usd: number;
    purchased_usd: number;
    monthly_allowance_usd: number;
    monthly_used_usd: number;
    next_reset_at: Date | null;
    top_up: TopUpBounds;
    auto_reload?: AutoReload;
    low_balance_usd?: number;
    payment_method?: PaymentMethod | null;
    charge_in_progress?: boolean;
    /** What the balance pays for a day; absent from an older backend. */
    burn?: BalanceBurn;
    /** Loads Stripe.js to save a card in the page; absent without Stripe billing. */
    stripe_publishable_key?: string;
}

/** The balance's daily draw: a month of bought mailboxes over 30 days, renewals due in 60 days, and AI's daily average. */
export interface BalanceBurn {
    per_day_usd: number;
    mailboxes_per_day_usd: number;
    renewals_per_day_usd: number;
    ai_per_day_usd: number;
    mailboxes_monthly_usd: number;
    renewals_due_usd: number;
    /** The day of the month bought mailboxes are billed; null before the first one. */
    billing_day: number | null;
}

export interface AutoReloadPatch {
    enabled: boolean;
    threshold_usd: number;
    amount_usd: number;
    monthly_limit_usd: number;
}

/** 200 is "paid"; 202 is "processing", the payment still going through. */
export interface TopUpResult {
    status: "paid" | "processing";
    amount_usd: number;
    balance_usd: number;
    charge_in_progress: boolean;
}

export interface BalanceInvoice {
    id: string;
    number: string;
    kind: "subscription" | "top_up" | "auto_reload" | string;
    status: string;
    amount_cents: number;
    currency: string;
    created_at: Date;
    hosted_url?: string;
    pdf_url?: string;
}

export interface CreditTransaction {
    id: string;
    org_id: string;
    reason: string;
    model_used: string;
    // Signed: negative for usage, positive for money added.
    amount_usd: number;
    // Provider cost before the markup.
    cost_usd: number;
    input_tokens: number;
    output_tokens: number;
    quantity: number;
    balance_after_usd: number;
    actor_user_id?: string | null;
    context?: CreditContext;
    created_at: Date;
}

export interface CreditTransactionsPage {
    data: CreditTransaction[];
    pagination: {
        next_cursor: string | null;
        has_more: boolean;
    };
}

// GET/PATCH /subscription/credits/settings. A null limit is no limit.
export interface AISpendSettings {
    spend_limit_daily_usd: number | null;
    spend_limit_weekly_usd: number | null;
    spend_limit_monthly_usd: number | null;
    member_limit_daily_usd: number | null;
    member_limit_weekly_usd: number | null;
    member_limit_monthly_usd: number | null;
    low_balance_usd: number;
    auto_reload_enabled: boolean;
    /** Whether AI may use money you added once the plan's included usage is spent; absent from an older backend. */
    ai_overage_enabled?: boolean;
    /** The most AI may take from that money per calendar month; null is no cap. */
    ai_overage_monthly_limit_usd?: number | null;
}

export interface CreditUsagePoint {
    date: string; // YYYY-MM-DD (UTC)
    amount_usd: number;
    tokens: number;
}

export interface CreditUsageBucket {
    key: string;
    amount_usd: number;
    tokens: number;
    count: number;
}

// GET /subscription/credits/usage: spend per window against the limits, a
// daily series, and breakdowns by feature and model.
export interface CreditUsageOverview {
    spent_today_usd: number;
    spent_week_usd: number;
    spent_month_usd: number;
    limit_daily_usd: number | null;
    limit_weekly_usd: number | null;
    limit_monthly_usd: number | null;
    /** What AI took from money you added this calendar month, past the included usage; absent from an older backend. */
    ai_from_balance_month_usd?: number;
    series: CreditUsagePoint[];
    by_reason: CreditUsageBucket[];
    by_model: CreditUsageBucket[];
}

// What a charge ran for; only the fields that apply are present.
export interface CreditContext {
    campaign_id?: string;
    campaign_name?: string;
    step_id?: string;
    contact_id?: string;
    contact_email?: string;
    automation_id?: string;
    automation_name?: string;
    node_id?: string;
    run_id?: string;
    thread_id?: string;
    session_id?: string;
    detail?: string;
}
