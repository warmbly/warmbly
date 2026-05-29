export type DiscountType = "percent" | "fixed" | "trial_extension";
export type DiscountDuration = "once" | "repeating" | "forever";

// Mirrors internal/models/discount.go DiscountPreview. Returned by
// POST /subscription/discount/validate. When `valid` is false, `reason`
// explains why and the discount fields are empty.
export default interface DiscountPreview {
    valid: boolean;
    reason?: string;

    code?: string;
    type?: DiscountType;
    percent_off?: number | null;
    amount_off?: number | null;
    currency?: string | null;
    trial_extension_days?: number | null;
    duration?: DiscountDuration;
    duration_in_months?: number | null;

    original_amount?: number | null;
    discounted_amount?: number | null;
    savings_amount?: number | null;
}
