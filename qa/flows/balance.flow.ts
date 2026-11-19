import { expect, test } from "../lib/proof.ts";

// Requires a stack started with QA_STRIPE_SECRET_KEY (a Stripe test key) and ../fixtures/balance.sql
// applied with a test customer that has a saved card.
const FIXTURES = process.env.QA_BILLING === "1" || process.env.QA_BALANCE_FIXTURES === "1";

test.describe("balance", () => {
  test.skip(!FIXTURES, "Start the stack with QA_STRIPE_SECRET_KEY, apply qa/fixtures/balance.sql with -v customer=cus_..., and set QA_BILLING=1.");

  test("read the balance and how long it lasts, add funds with the saved card and set the automatic top-up", async ({ page, proof }) => {
    await page.goto("/app/settings/billing/balance");
    const card = page.getByRole("region", { name: "Balance" });
    await expect(card.getByRole("button", { name: "Add funds" })).toBeVisible();
    await expect(card.getByText(/^(Lasts|Runs out|Nothing is drawing)/)).toBeVisible();
    await proof.chapter("Balance", "One balance in dollars pays for mailboxes, renewals and AI, and says how long it lasts");
    await proof.dwell();
    await proof.shot("balance-card", { caption: "The balance, how long it lasts at today's spend, and the rates it pays" });

    await card.getByRole("button", { name: "Add funds" }).click();
    const dialog = page.getByRole("dialog", { name: "Add funds" });
    // $100 is the default only where the store is on; pick it so the flow reads the same either way.
    await dialog.getByRole("radio", { name: /^\$100/ }).click();
    await expect(dialog.getByRole("radio", { name: /^\$100/ })).toHaveAttribute("aria-checked", "true");
    await expect(dialog.getByText(/•••• \d{4}/)).toBeVisible();
    await proof.chapter("Add funds", "A preset or a custom amount, paid on the saved card in place");
    await proof.dwell();
    await proof.shot("add-funds", { caption: "Six presets, a custom amount, how long it lasts, the saved card and one Pay" });

    await dialog.getByRole("tab", { name: "Auto top-up" }).click();
    await expect(dialog.getByLabel("When balance drops below")).toBeVisible();
    await proof.dwell();
    await proof.shot("add-funds-auto", { caption: "The automatic top-up: below what, how much, and a monthly limit" });

    await dialog.getByRole("tab", { name: "One-time" }).click();
    await dialog.getByRole("button", { name: "Pay $100.00" }).click();
    await expect(dialog).toBeHidden({ timeout: 30_000 });
    await expect(card.getByText("$115.00")).toBeVisible({ timeout: 30_000 });
    await proof.dwell();
    await proof.shot("top-up-paid", { caption: "Paid as a Stripe invoice and added to the balance right away" });

    const invoices = page.getByText("Receipts for the plan and for money added.");
    await invoices.scrollIntoViewIfNeeded();
    await expect(page.getByRole("cell", { name: "$100.00" }).first()).toBeVisible({ timeout: 15_000 });
    await proof.dwell();
    await proof.shot("invoices", { caption: "The top-up's invoice in the list, with its receipt" });
  });
});
