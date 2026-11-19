// Store prices arrive in micro-USD (1_000_000 = $1.00); these render them.
import { paymentMethodLabel } from "@/components/app/billing/balanceModel";
import { fmtUsd } from "@/lib/money";

export const MICROS_PER_USD = 1_000_000;

/** Whole cents, rounded half up, the unit a card is charged in. */
export function microsToCents(micros: number): number {
    return Math.round(micros / 10_000);
}

/** "$12.00". */
export function fmtMicros(micros: number): string {
    return fmtUsd(microsToCents(micros) / 100);
}

/** "$3" for whole dollars, "$3.50" otherwise. Short labels and menu items. */
export function fmtMicrosShort(micros: number): string {
    const cents = microsToCents(micros);
    if (cents % 100 === 0) return `${cents < 0 ? "-" : ""}$${Math.abs(cents / 100).toLocaleString("en-US")}`;
    return fmtUsd(cents / 100);
}

/** "$3/mo". */
export function fmtMonthly(micros: number): string {
    return `${fmtMicrosShort(micros)}/mo`;
}

/** "$12.00/yr". */
export function fmtYearly(micros: number): string {
    return `${fmtMicros(micros)}/yr`;
}

/** "Visa •••• 4242", named exactly as the billing page names the same card. */
export function cardLabel(card: { brand: string; last4: string }): string {
    if (!card.brand) return `Card •••• ${card.last4}`;
    return paymentMethodLabel({ brand: card.brand.toLowerCase(), last4: card.last4, exp_month: 0, exp_year: 0 });
}
