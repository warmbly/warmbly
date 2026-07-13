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
