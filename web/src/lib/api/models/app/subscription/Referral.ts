// Mirrors the backend referral program contract returned under
// /subscription/referral and its sub-resources.

// GET /subscription/referral — the GET mints the code on first view so it
// always returns a ready code + share_url.
export interface ReferralSummary {
    code: string;
    share_url: string;
    currency: string;
    invitee_percent_off: number;
    invitee_months: number;
    balance_cents: number;
    lifetime_earned_cents: number;
    total_referred: number;
    pending: number;
    qualified: number;
    rewarded: number;
}

// POST /subscription/referral — idempotent "ensure" of the owner's code.
export interface ReferralCode {
    id: string;
    code: string;
    owner_user_id: string;
    owner_org_id: string;
    discount_code_id: string;
    created_at: string;
    updated_at: string;
}

export type ReferralAttributionStatus = "pending" | "qualified" | "rewarded" | "void";

export interface ReferralAttribution {
    id: string;
    status: ReferralAttributionStatus;
    reward_cents: number;
    reward_currency: string;
    invitee_org_id: string;
    qualified_at?: string;
    rewarded_at?: string;
    created_at: string;
}

// Positive amount = reward, negative = clawback.
export interface ReferralEarningsTransaction {
    id: string;
    amount_cents: number;
    currency: string;
    reason: string;
    balance_after_cents: number;
    created_at: string;
}

export interface ReferralPagination {
    next_cursor: string | null;
    has_more: boolean;
}

export interface ReferralAttributionsPage {
    data: ReferralAttribution[];
    pagination: ReferralPagination;
}

export interface ReferralEarningsPage {
    data: ReferralEarningsTransaction[];
    pagination: ReferralPagination;
}
