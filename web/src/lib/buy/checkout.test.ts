import { describe, expect, it } from "vitest";
import type { QuotePayment } from "@/lib/api/models/app/store/Store";
import { autoReloadLine, balanceLine, monthlyLine, payBlocker, payChecklist, payLabel, paymentPlan, withTopUp, type PayGate } from "./checkout";

const visa = { brand: "visa", last4: "4242" };
const clear: PayGate = { cartIssue: null, planNeeded: false, canPay: true, registrantMissing: false, cardIncomplete: false, quoteProblem: null };

describe("checkout summary", () => {
    it("places the order where nothing is charged and names the total where it is", () => {
        expect(payLabel({ metered: false, due: 53_670_000, card: null, cardEntry: "inline" })).toBe("Place order · $53.67");
        expect(payLabel({ metered: false, due: null, card: null, cardEntry: "inline" })).toBe("Place order");
        expect(payLabel({ metered: true, due: null, card: visa, cardEntry: "inline" })).toBe("Pay");
        expect(payLabel({ metered: true, due: 53_670_000, card: visa, cardEntry: "inline" })).toBe("Pay $53.67");
        expect(payLabel({ metered: true, due: 53_670_000, card: null, cardEntry: "inline" })).toBe("Pay $53.67");
        // Without Stripe.js the card is added on Stripe's page, and the button says so.
        expect(payLabel({ metered: true, due: 53_670_000, card: null, cardEntry: "redirect" })).toBe("Add a card and pay $53.67");
        expect(payLabel({ metered: true, due: 7_900_000, card: null, cardEntry: "redirect", fromBalance: true })).toBe("Pay $7.90 from balance");
    });

    it("explains the first thing that stops paying, in the order a buyer fixes them", () => {
        expect(payBlocker(clear)).toBeNull();
        expect(payBlocker({ ...clear, cartIssue: "Add at least one domain to continue.", canPay: false })).toBe("Add at least one domain to continue.");
        expect(payBlocker({ ...clear, planNeeded: true, canPay: false })).toMatch(/plan/);
        expect(payBlocker({ ...clear, canPay: false, registrantMissing: true })).toMatch(/Manage billing/);
        expect(payBlocker({ ...clear, registrantMissing: true, cardIncomplete: true })).toMatch(/registrant/);
        expect(payBlocker({ ...clear, cardIncomplete: true })).toMatch(/card details/);
        expect(payBlocker({ ...clear, quoteProblem: "That domain was just taken. Pick another one." })).toMatch(/taken/);
    });

    it("says where the months after are charged", () => {
        expect(monthlyLine(9_000_000, true, visa)).toBe("Then $9/mo, charged automatically to Visa •••• 4242");
        expect(monthlyLine(9_000_000, true, "new_card")).toBe("Then $9/mo, charged automatically to your card");
        expect(monthlyLine(9_000_000, true, "balance")).toBe("Then $9/mo, taken from your balance automatically");
        expect(monthlyLine(9_000_000, false, visa)).toBeNull();
    });

    it("shows a minimum card charge as money added to the balance, and the button charges the card's amount", () => {
        const p: QuotePayment = {
            balance_micros: 0, from_balance_micros: 0, card_charge_micros: 10_000_000, card: visa, needs_payment_method: false,
            balance_after_micros: 7_000_000, auto_reload: { enabled: false, threshold_micros: 3_000_000, amount_micros: 10_000_000, store_managed: true },
        };
        const plan = paymentPlan(p, 0, 3_000_000);
        expect(balanceLine(plan, 3_000_000)).toBe("Adds $10.00 to your balance · $3.00 used now, $7.00 left for next month");
        expect(payLabel({ metered: true, due: plan.cardCharge, card: visa, cardEntry: "inline" })).toBe("Pay $10.00");
        expect(autoReloadLine({ kind: "offer", amount: plan.autoReload!.amount })).toBe("Keep my balance topped up · adds $10.00 (+3% processing fee) when it won't cover next month's mailboxes");
        expect(autoReloadLine({ kind: "on", amount: 10_000_000, threshold: 3_000_000 })).toBe("Balance tops up by $10.00 (+3% processing fee) when it falls below $3.00");
        // Rounding the card charge up to a preset leaves the difference on the balance.
        const rounded = withTopUp(plan, 100_000_000, 3_000_000);
        expect(balanceLine(rounded, 3_000_000)).toBe("Adds $100.00 to your balance · $3.00 used now, $97.00 left for next month");
        expect(withTopUp(plan, 5_000_000, 3_000_000)).toBe(plan);
    });

    it("pays from the balance with no card when it covers the order", () => {
        const p: QuotePayment = { balance_micros: 50_000_000, from_balance_micros: 7_900_000, card_charge_micros: 0, card: null, needs_payment_method: false };
        const plan = paymentPlan(p, 50_000_000, 7_900_000);
        expect(plan.balanceCovers).toBe(true);
        expect(balanceLine(plan, 7_900_000)).toBe("Paid from your balance ($42.10 left)");
        // Before a quote, the balance on hand is the estimate.
        expect(paymentPlan(undefined, 50_000_000, 7_900_000).balanceCovers).toBe(true);
        expect(paymentPlan(undefined, 1_000_000, 7_900_000)).toMatchObject({ balanceCovers: false, cardCharge: 6_900_000 });
    });

    it("says nothing extra when the card pays exactly what is due", () => {
        const p: QuotePayment = { balance_micros: 0, from_balance_micros: 0, card_charge_micros: 7_900_000, card: visa, needs_payment_method: false };
        expect(balanceLine(paymentPlan(p, 0, 7_900_000), 7_900_000)).toBeNull();
    });
});

describe("payChecklist", () => {
    const ok = { hasDomain: true, sendersOk: true, addressesOk: true, forwardOk: true, registrantOk: null, cardOk: null };

    it("lists what the order needs and ticks each off", () => {
        expect(payChecklist({ ...ok, hasDomain: false }).map((s) => [s.label, s.done])).toEqual([
            ["Add a domain", false],
            ["Name every sender", true],
        ]);
        expect(payChecklist({ ...ok, registrantOk: false, cardOk: true }).map((s) => [s.key, s.done])).toEqual([
            ["domain", true],
            ["senders", true],
            ["registrant", false],
            ["card", true],
        ]);
    });

    it("shows address and forward fixes only while one is needed", () => {
        expect(payChecklist(ok).some((s) => s.key === "addresses" || s.key === "forward")).toBe(false);
        expect(payChecklist({ ...ok, addressesOk: false, forwardOk: false }).filter((s) => !s.done).map((s) => s.key)).toEqual(["addresses", "forward"]);
    });
});
