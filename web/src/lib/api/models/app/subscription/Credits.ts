// AI credit ledger, transaction log, and top-up packs. Mirrors the backend
// /subscription/credits responses (internal/api/handler/credits.go).

export interface CreditPack {
    key: string;
    credits: number;
}

export interface CreditBalance {
    // Spendable total across both pools.
    balance: number;
    // Current monthly (plan allowance) pool.
    monthly_balance: number;
    // Top-up pool (never expires, survives resets).
    purchased_balance: number;
    // Plan grant per billing cycle.
    monthly_allowance: number;
    // Lifetime purchased credits.
    total_purchased: number;
    // When the monthly pool was last reset.
    monthly_reset_at: string;
    // When the monthly pool next resets (subscription period end), if any.
    next_reset_at: string | null;
    packs: CreditPack[];
}

export interface CreditTransaction {
    id: string;
    org_id: string;
    // Signed: negative for consumption, positive for grants/purchases.
    amount: number;
    reason: string;
    model_used: string;
    tokens_used: number;
    balance_after: number;
    purchased_delta: number;
    purchased_balance_after: number;
    idempotency_key?: string | null;
    created_at: string;
}

export interface CreditTransactionsPage {
    data: CreditTransaction[];
    pagination: {
        next_cursor: string | null;
        has_more: boolean;
    };
}

// Org AI spend controls: hard spend limits per window (null = off), the
// low-balance alert threshold, and auto top-up configuration. Mirrors
// GET/PATCH /subscription/credits/settings.
export interface AISpendSettings {
    org_id: string;
    spend_limit_daily: number | null;
    spend_limit_weekly: number | null;
    spend_limit_monthly: number | null;
    low_balance_threshold: number;
    low_balance_notified_at?: string | null;
    auto_topup_enabled: boolean;
    auto_topup_pack: string;
    auto_topup_threshold: number;
    auto_topup_max_per_month: number;
}

export interface CreditUsagePoint {
    date: string; // YYYY-MM-DD (UTC)
    credits: number;
    tokens: number;
}

export interface CreditUsageBucket {
    key: string;
    credits: number;
    tokens: number;
    count: number;
}

// GET /subscription/credits/usage — spend per window vs configured limits,
// a daily series, and breakdowns by feature and model.
export interface CreditUsageOverview {
    spent_today: number;
    spent_week: number;
    spent_month: number;
    limit_daily: number | null;
    limit_weekly: number | null;
    limit_monthly: number | null;
    series: CreditUsagePoint[];
    by_reason: CreditUsageBucket[];
    by_model: CreditUsageBucket[];
}
