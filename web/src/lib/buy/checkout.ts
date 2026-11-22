// What the order summary says: the Pay button's label, why it cannot be pressed yet, and how the months after are paid.
import type { QuotePayment, StoreCard } from "@/lib/api/models/app/store/Store";
import { topUpCharge } from "@/lib/money";
import { cardLabel, fmtMicros, fmtMonthly } from "./money";

/** How a workspace with no saved card adds one: in the page with Stripe.js, or on Stripe's page when that cannot load. */
export type CardEntry = "inline" | "redirect";

export interface PayView {
    /** False on a self-host or unmetered workspace: nothing is charged, so the button places the order. */
    metered: boolean;
    due: number | null;
    /** The saved card, or null when the workspace has none. */
    card: StoreCard | null;
    cardEntry: CardEntry;
    /** The balance covers it all, so no card is charged. */
    fromBalance?: boolean;
}

export function payLabel(v: PayView): string {
    if (!v.metered) return v.due === null ? "Place order" : `Place order · ${fmtMicros(v.due)}`;
    if (v.due === null) return "Pay";
    if (v.fromBalance) return `Pay ${fmtMicros(v.due)} from balance`;
    if (!v.card && v.cardEntry === "redirect") return `Add a card and pay ${fmtMicros(v.due)}`;
    return `Pay ${fmtMicros(v.due)}`;
}

export interface PayGate {
    cartIssue: string | null;
    planNeeded: boolean;
    canPay: boolean;
    /** The registry needs details the workspace has not given yet. */
    registrantMissing: boolean;
    /** A card is being entered and is not complete. */
    cardIncomplete: boolean;
    /** The order cannot be priced as it stands (a taken domain, an outage). */
    quoteProblem: string | null;
}

/** The one sentence under the Pay button saying why it cannot be pressed, or null when it can. */
export function payBlocker(g: PayGate): string | null {
    if (g.cartIssue) return g.cartIssue;
    if (g.planNeeded) return "Choose a plan to buy mailboxes.";
    if (!g.canPay) return "Paying needs the Manage billing permission. Ask a workspace owner or billing admin to place this order.";
    if (g.registrantMissing) return "Fill in the registrant details above.";
    if (g.cardIncomplete) return "Enter your card details above.";
    return g.quoteProblem;
}

/** Who pays the months after: the saved card, the card being added, or the balance alone. */
export type MonthlyPayer = StoreCard | "new_card" | "balance";

/** "Then $9/mo, charged automatically to Visa •••• 4242", or null on an unmetered workspace. */
export function monthlyLine(monthly: number, metered: boolean, payer: MonthlyPayer): string | null {
    if (!metered || monthly <= 0) return null;
    if (payer === "balance") return `Then ${fmtMonthly(monthly)}, taken from your balance automatically`;
    return `Then ${fmtMonthly(monthly)}, charged automatically to ${payer === "new_card" ? "your card" : cardLabel(payer)}`;
}

/** How today's charge is met: the balance first, then the card, which may add more than is due to reach its minimum. */
export interface PaymentPlan {
    /** What the card is charged today; 0 when the balance covers it. */
    cardCharge: number;
    /** The processing fee the card adds on a top-up the buyer chose, on top of cardCharge; absent otherwise. */
    topUpFee?: number;
    fromBalance: number;
    /** The balance once the order is paid, or null while there is no total yet. */
    balanceAfter: number | null;
    balanceCovers: boolean;
    /** The automatic reload as it will be after the order; storeManaged when the store sizes it to the mailboxes. */
    autoReload: { enabled: boolean; threshold: number; amount: number; storeManaged: boolean } | null;
}

/** The plan from a quote's payment, or estimated from the balance and the total while no quote has come back. */
export function paymentPlan(p: QuotePayment | undefined, balance: number, due: number | null): PaymentPlan {
    if (p) {
        const after = p.balance_after_micros ?? (due !== null ? p.balance_micros + p.card_charge_micros - due : null);
        return {
            cardCharge: p.card_charge_micros,
            fromBalance: p.from_balance_micros,
            balanceAfter: after,
            balanceCovers: p.card_charge_micros === 0 && !p.needs_payment_method,
            autoReload: p.auto_reload
                ? { enabled: p.auto_reload.enabled, threshold: p.auto_reload.threshold_micros, amount: p.auto_reload.amount_micros, storeManaged: p.auto_reload.store_managed }
                : null,
        };
    }
    if (due === null) return { cardCharge: 0, fromBalance: 0, balanceAfter: null, balanceCovers: false, autoReload: null };
    const fromBalance = Math.min(Math.max(balance, 0), due);
    return { cardCharge: due - fromBalance, fromBalance, balanceAfter: balance - fromBalance, balanceCovers: fromBalance >= due, autoReload: null };
}

/** The plan when the buyer rounds today's card charge up to a larger amount, the rest left on the balance. */
export function withTopUp(plan: PaymentPlan, topUpMicros: number, due: number | null): PaymentPlan {
    if (due === null || topUpMicros <= plan.cardCharge) return plan;
    const before = (plan.balanceAfter ?? 0) - plan.cardCharge + due;
    const fee = Math.round(topUpCharge(topUpMicros / 1_000_000).feeUsd * 1_000_000);
    return { ...plan, cardCharge: topUpMicros, topUpFee: fee, balanceAfter: before + topUpMicros - due };
}

/** The one line under the total on how today is paid, or null when the card simply pays what is due. */
export function balanceLine(plan: PaymentPlan, due: number | null): string | null {
    if (due === null || due <= 0) return null;
    if (plan.balanceCovers) return plan.balanceAfter !== null ? `Paid from your balance (${fmtMicros(plan.balanceAfter)} left)` : "Paid from your balance";
    if (plan.cardCharge > 0 && plan.balanceAfter !== null && plan.balanceAfter > 0) {
        return `Adds ${fmtMicros(plan.cardCharge)} to your balance · ${fmtMicros(due)} used now, ${fmtMicros(plan.balanceAfter)} left for next month`;
    }
    if (plan.cardCharge > 0 && plan.fromBalance > 0) return `${fmtMicros(plan.fromBalance)} from your balance, ${fmtMicros(plan.cardCharge)} on the card`;
    return null;
}

/** The automatic top-up at checkout: already on (one line), offered with the first purchase (a toggle), or neither. */
export type ReloadView = { kind: "on"; amount: number; threshold: number } | { kind: "offer"; amount: number } | null;

/** What the automatic top-up already does, or what turning it on would. */
export function autoReloadLine(r: NonNullable<ReloadView>): string {
    if (r.kind === "on") return `Balance tops up by ${fmtMicros(r.amount)} (+3% processing fee) when it falls below ${fmtMicros(r.threshold)}`;
    return `Keep my balance topped up · adds ${fmtMicros(r.amount)} (+3% processing fee) when it won't cover next month's mailboxes`;
}

/** One thing the order still needs before it can be paid, ticked off as it is done. */
export interface PayStep {
    key: "domain" | "senders" | "addresses" | "forward" | "registrant" | "card";
    label: string;
    done: boolean;
}

export interface ChecklistInput {
    hasDomain: boolean;
    sendersOk: boolean;
    addressesOk: boolean;
    forwardOk: boolean;
    /** null when no registrant is asked for. */
    registrantOk: boolean | null;
    /** null when no card is asked for. */
    cardOk: boolean | null;
}

/** What the order needs, in the order a buyer fills it in. Addresses and the forward show only while one needs a fix. */
export function payChecklist(c: ChecklistInput): PayStep[] {
    const steps: PayStep[] = [
        { key: "domain", label: "Add a domain", done: c.hasDomain },
        { key: "senders", label: "Name every sender", done: c.sendersOk },
    ];
    if (!c.addressesOk) steps.push({ key: "addresses", label: "Fix the highlighted addresses", done: false });
    if (!c.forwardOk) steps.push({ key: "forward", label: "Fix the website forward", done: false });
    if (c.registrantOk !== null) steps.push({ key: "registrant", label: "Complete the registrant details", done: c.registrantOk });
    if (c.cardOk !== null) steps.push({ key: "card", label: "Enter your card", done: c.cardOk });
    return steps;
}
