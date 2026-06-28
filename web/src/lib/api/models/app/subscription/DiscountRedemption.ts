// Mirrors a row from GET /subscription/discounts — the org's promo redemption
// history. `amount_off` is in major currency units (matches DiscountPreview).
export interface DiscountRedemption {
    id: string;
    code: string;
    type: string;
    percent_off?: number | null;
    amount_off?: number | null;
    currency?: string | null;
    trial_extension_days?: number | null;
    status: string;
    redeemed_at: string;
    applied_at?: string | null;
}

export interface DiscountRedemptionsResult {
    data: DiscountRedemption[];
}
