// Pricing helpers shared by the billing page and the in-app upgrade dialog so
// a discounted price is computed and described the same way on both.

import type DiscountPreview from "@/lib/api/models/app/subscription/DiscountPreview";

export type BillingInterval = "monthly" | "annual";

/** Short human summary of an applied promo code ("20% off, forever"). */
export function describeDiscount(d: DiscountPreview): string {
    if (d.type === "trial_extension") {
        return `+${d.trial_extension_days ?? 0} trial days`;
    }
    let base: string;
    if (d.type === "percent") {
        base = `${d.percent_off ?? 0}% off`;
    } else {
        base = `${(d.currency ?? "usd").toUpperCase()} ${fmtMoney(d.amount_off ?? 0)} off`;
    }
    if (d.duration === "forever") return `${base}, forever`;
    if (d.duration === "repeating" && d.duration_in_months) {
        return `${base} for ${d.duration_in_months} months`;
    }
    return `${base} on your first invoice`;
}

/** Discounted price for a money discount, or null when the code does not
 *  change the price (trial extensions, custom plans, no code).
 *
 *  `price` is always the monthly display figure. A percentage applies to any
 *  billing period unchanged, but a fixed amount comes off the invoice, and an
 *  annual invoice is one payment covering twelve months. Subtracting
 *  `amount_off` from the monthly figure would multiply the discount by twelve,
 *  so the fixed case works on the invoice total and converts back for display. */
export function discountedPrice(
    price: number | null,
    d: DiscountPreview | null | undefined,
    interval: BillingInterval = "monthly",
): number | null {
    if (price == null || !d || !d.valid) return null;
    if (d.type === "percent" && d.percent_off != null) {
        return roundMoney(Math.max(0, price * (1 - d.percent_off / 100)));
    }
    if (d.type === "fixed" && d.amount_off != null) {
        const months = interval === "annual" ? 12 : 1;
        const invoice = price * months;
        return roundMoney(Math.max(0, invoice - d.amount_off) / months);
    }
    return null;
}

export function roundMoney(n: number): number {
    return Math.round(n * 100) / 100;
}

export function fmtMoney(n: number): string {
    return Number.isInteger(n) ? String(n) : n.toFixed(2);
}

// Currencies with no minor unit, so Stripe reports whole units already.
// https://docs.stripe.com/currencies
const ZERO_DECIMAL = new Set([
    "bif", "clp", "djf", "gnf", "jpy", "kmf", "krw", "mga",
    "pyg", "rwf", "ugx", "vnd", "vuv", "xaf", "xof", "xpf",
]);

/** Convert a Stripe amount from its minor unit to whole units for display. */
export function fromMinorUnits(amount: number, currency?: string | null): number {
    const code = (currency ?? "usd").toLowerCase().trim();
    return ZERO_DECIMAL.has(code) ? amount : amount / 100;
}
