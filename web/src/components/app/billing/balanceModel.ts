// Shared rules for the balance surfaces: when to warn, and how to name a card.

import type { AutoReload, Balance, PaymentMethod } from "@/lib/api/models/app/subscription/Credits";
import { MIN_TOP_UP_USD, OUT_OF_BALANCE_BELOW, roundCents } from "@/lib/money";

export type BalanceAlert = "out" | "low";

/** Out (AI paused) or running low; null when there is nothing to say. */
export function balanceAlert(b: Balance | null | undefined): BalanceAlert | null {
    if (!b || b.unlimited) return null;
    // An agency client spends the agency's balance once its own runs out.
    if (b.balance_usd + (b.agency_usd ?? 0) < OUT_OF_BALANCE_BELOW) return "out";
    if (b.agency_usd) return null;
    const lowAt = b.auto_reload?.enabled ? b.auto_reload.threshold_usd : (b.low_balance_usd ?? 0);
    return b.balance_usd < lowAt ? "low" : null;
}

/** Automatic reload is on, has a card and room left this month, so a low balance refills itself. */
export function reloadCovers(b: Balance | null | undefined): boolean {
    const ar = b?.auto_reload;
    if (!ar?.enabled || !b?.payment_method) return false;
    return roundCents(ar.monthly_limit_usd - ar.reloaded_this_month_usd) >= MIN_TOP_UP_USD;
}

/** The app-wide banner: out or low, and automatic reload will not refill it. */
export function balanceBanner(b: Balance | null | undefined): BalanceAlert | null {
    const alert = balanceAlert(b);
    if (!alert || reloadCovers(b)) return null;
    return alert;
}

const BRANDS: Record<string, string> = {
    visa: "Visa",
    mastercard: "Mastercard",
    amex: "American Express",
    discover: "Discover",
    diners: "Diners Club",
    jcb: "JCB",
    unionpay: "UnionPay",
    cartes_bancaires: "Cartes Bancaires",
    link: "Link",
    sepa_debit: "SEPA Direct Debit",
    us_bank_account: "Bank account",
};

const WALLETS: Record<string, string> = { apple_pay: "Apple Pay", google_pay: "Google Pay", link: "Link" };

/** "Visa •••• 4242", "Apple Pay (Visa •••• 4242)". */
export function paymentMethodLabel(pm: PaymentMethod): string {
    const brand = BRANDS[pm.brand] ?? pm.brand.replace(/_/g, " ").replace(/^./, (c) => c.toUpperCase());
    const card = pm.last4 ? `${brand} •••• ${pm.last4}` : brand;
    const wallet = pm.wallet ? WALLETS[pm.wallet] : undefined;
    return wallet ? `${wallet} (${card})` : card;
}

/** "08/30", or null when it does not expire. */
export function paymentMethodExpiry(pm: PaymentMethod): string | null {
    if (!pm.exp_month || !pm.exp_year) return null;
    return `${String(pm.exp_month).padStart(2, "0")}/${String(pm.exp_year % 100).padStart(2, "0")}`;
}

/** Expires before the end of next month: worth replacing before a reload fails. */
export function expiresSoon(pm: PaymentMethod, now = new Date()): boolean {
    if (!pm.exp_month || !pm.exp_year) return false;
    const months = (pm.exp_year - now.getFullYear()) * 12 + (pm.exp_month - (now.getMonth() + 1));
    return months <= 1;
}

export const RELOAD_FAILURE_COPY: Record<NonNullable<AutoReload["failure_code"]>, string> = {
    card_declined: "The card was declined, so nothing was charged. Update the card, then turn it back on.",
    authentication_required: "The bank asked to confirm the payment, which can't happen automatically. Use another card, then turn it back on.",
    no_payment_method: "There's no saved card. Add one, then turn it back on.",
    payment_failed: "The payment didn't go through. Update the card, then turn it back on.",
    unavailable: "Adding money isn't available right now. Try again later.",
};

export function fmtDay(value: Date | string | null | undefined): string {
    if (!value) return "";
    const d = new Date(value);
    if (Number.isNaN(d.getTime())) return "";
    return d.toLocaleDateString("en-US", { month: "short", day: "numeric" });
}

export function fmtClock(value: Date | string): string {
    return new Date(value).toLocaleTimeString("en-US", { hour: "numeric", minute: "2-digit" });
}

// Ledger reason codes as people read them.
const REASONS: Record<string, string> = {
    writing_assistant: "Writing assistant",
    writing_edit: "Selection edit",
    agent_iteration: "Remie",
    reply_draft: "Reply draft",
    compose_draft: "Compose draft",
    research_run: "Contact research",
    automation_ai: "Automation AI step",
    campaign_ai: "Campaign switch",
    campaign_ai_search: "Campaign switch web search",
    inbox_agent_draft: "Inbox agent",
    spam_analysis: "Spam analysis",
    placement_test: "Placement test",
    ai_variable: "AI variable",
    advisor_fix: "Advisor fix",
    balance_top_up: "Money added",
    credit_topup: "Money added",
    auto_reload: "Automatic reload",
    credit_auto_topup: "Automatic reload",
    monthly_reset: "Monthly included usage",
    agency_transfer: "Added by your agency",
    trial_grant: "Trial balance",
    tester_grant: "Test balance",
};

export function reasonLabel(reason: string): string {
    if (REASONS[reason]) return REASONS[reason];
    const refund = reason.match(/^(.*)_(refund|release)$/);
    if (refund && REASONS[refund[1]]) return `${REASONS[refund[1]]} refund`;
    const s = reason.replace(/_/g, " ");
    return s.charAt(0).toUpperCase() + s.slice(1);
}
