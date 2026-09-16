// One place that turns a discount code into the sentence an operator reads.
// The list, the builder preview and the detail pane all render the same words,
// so a code never describes itself two ways.

import type { DiscountCode, DiscountDuration, DiscountType } from "@/lib/api/models/admin";

/** The shape the builder holds mid-edit: every field optional, nothing saved. */
export interface DiscountShape {
    type: DiscountType;
    percent_off?: number | null;
    amount_off?: number | null;
    currency?: string | null;
    trial_extension_days?: number | null;
    duration: DiscountDuration;
    duration_in_months?: number | null;
}

export function formatMoney(amount: number, currency?: string | null): string {
    const code = (currency || "USD").toUpperCase();
    try {
        return new Intl.NumberFormat(undefined, { style: "currency", currency: code }).format(amount);
    } catch {
        // An unknown or half-typed currency must not blank the preview.
        return `${amount.toFixed(2)} ${code}`;
    }
}

/** "50% off", "$10.00 off", "14 extra trial days". */
export function describeValue(d: DiscountShape): string {
    switch (d.type) {
        case "percent":
            return d.percent_off ? `${d.percent_off}% off` : "a percentage off";
        case "fixed":
            return d.amount_off ? `${formatMoney(d.amount_off, d.currency)} off` : "a fixed amount off";
        case "trial_extension":
            return d.trial_extension_days
                ? `${d.trial_extension_days} extra trial ${d.trial_extension_days === 1 ? "day" : "days"}`
                : "extra trial days";
    }
}

/** "on the first invoice", "for 3 months", "on every invoice". Trial codes have
 *  no duration: they add days once, at checkout. */
export function describeDuration(d: DiscountShape): string {
    if (d.type === "trial_extension") return "";
    switch (d.duration) {
        case "once":
            return "on the first invoice";
        case "forever":
            return "on every invoice";
        case "repeating":
            return d.duration_in_months
                ? `for ${d.duration_in_months} ${d.duration_in_months === 1 ? "month" : "months"}`
                : "for a number of months";
    }
}

export function describeDiscount(d: DiscountShape): string {
    const tail = describeDuration(d);
    return tail ? `${describeValue(d)} ${tail}` : describeValue(d);
}

/** What a repeating discount actually does on each billing interval. This is
 *  the part that surprises people: the window is months, not invoices, so a
 *  yearly plan has exactly one invoice inside it and the whole first year is
 *  discounted. Returns "" when the code has no such asymmetry. */
export function describeIntervalEffect(d: DiscountShape): string {
    if (d.type === "trial_extension" || d.duration !== "repeating" || !d.duration_in_months) {
        return "";
    }
    const n = d.duration_in_months;
    const monthly = `${n} discounted ${n === 1 ? "invoice" : "invoices"}`;
    if (n >= 12) {
        // The window is a clock, so an annual plan bills at month 0, 12, 24 and
        // so on: ceil, not floor. 18 months covers two annual invoices, not one.
        const years = Math.ceil(n / 12);
        return `A monthly plan gets ${monthly}. An annual plan gets the first ${years === 1 ? "year" : `${years} years`} discounted.`;
    }
    return `A monthly plan gets ${monthly}. An annual plan bills once inside that window, so it gets the whole first year discounted.`;
}

export function describeScope(d: DiscountCode, planNames: Map<string, string>): string {
    if (d.applies_to_all_plans) return "All plans";
    if (!d.plan_ids?.length) return "No plans";
    return d.plan_ids.map((id) => planNames.get(id) ?? id.slice(0, 8)).join(", ");
}

/** A code can be active and still unusable. The list shows the real state. */
export function effectiveState(d: DiscountCode): { label: string; tone: string } {
    if (d.status === "disabled") return { label: "disabled", tone: "border-zinc-300 text-zinc-600 bg-zinc-50" };
    if (d.status === "expired") return { label: "expired", tone: "border-zinc-300 text-zinc-600 bg-zinc-50" };

    const now = Date.now();
    if (d.expires_at && new Date(d.expires_at).getTime() < now) {
        return { label: "expired", tone: "border-zinc-300 text-zinc-600 bg-zinc-50" };
    }
    if (d.starts_at && new Date(d.starts_at).getTime() > now) {
        return { label: "scheduled", tone: "border-sky-300 text-sky-700 bg-sky-50" };
    }
    if (d.max_redemptions != null && d.times_redeemed >= d.max_redemptions) {
        return { label: "exhausted", tone: "border-amber-300 text-amber-700 bg-amber-50" };
    }
    return { label: "active", tone: "border-emerald-300 text-emerald-700 bg-emerald-50" };
}
