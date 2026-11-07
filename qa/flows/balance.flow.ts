import { expect, test } from "../lib/proof.ts";

// Requires a stack started with QA_STRIPE_SECRET_KEY (a Stripe test key) and ../fixtures/balance.sql
// applied with a test customer that has a saved card.
const FIXTURES = process.env.QA_BALANCE_FIXTURES === "1";

test.describe("balance", () => {
  test.skip(!FIXTURES, "Start the stack with QA_STRIPE_SECRET_KEY, apply qa/fixtures/balance.sql with -v customer=cus_..., and set QA_BALANCE_FIXTURES=1.");

  test("read the dollar balance, add money with the saved card and turn on automatic reload", async ({ page, proof }) => {
    await page.goto("/app/settings/billing/balance");
    await expect(page.getByText("Available", { exact: true })).toBeVisible();
    await expect(page.getByText("Included this cycle")).toBeVisible();
    await proof.chapter("Balance", "AI is charged in dollars on actual usage, from the plan's included amount first");
    await proof.dwell();
    await proof.shot("balance", { caption: "The balance in dollars: what is left of this month's included usage and the money added" });

    await page.getByRole("radio", { name: "$25", exact: true }).click();
    await page.getByRole("button", { name: "Pay $25.00" }).click();
    const dialog = page.getByRole("alertdialog");
    await expect(dialog.getByText("Add $25.00 to the balance?")).toBeVisible();
    await proof.shot("top-up-confirm", { caption: "One click on the saved card, confirmed before anything is charged" });
    await dialog.getByRole("button", { name: "Pay $25.00" }).click();
    await expect(page.getByText("$40.00").first()).toBeVisible({ timeout: 30_000 });
    await proof.dwell();
    await proof.shot("top-up-paid", { caption: "Paid as a Stripe invoice and added to the balance right away" });

    await page.getByRole("switch", { name: "Automatic reload" }).click();
    await expect(page.getByRole("switch", { name: "Automatic reload" })).toHaveAttribute("aria-checked", "true");
    await expect(page.getByText("No reloads yet")).toBeVisible();
    await page.getByText("Reload below").scrollIntoViewIfNeeded();
    await proof.dwell();
    await proof.shot("auto-reload", { caption: "Automatic reload on the saved card: a threshold, an amount and a monthly limit" });

    const invoices = page.getByText("Receipts for the plan and for money added.");
    await invoices.scrollIntoViewIfNeeded();
    await expect(page.getByRole("cell", { name: "$25.00" }).first()).toBeVisible({ timeout: 15_000 });
    await proof.dwell();
    await proof.shot("invoices", { caption: "The top-up's invoice in the list, with its receipt" });
  });
});
