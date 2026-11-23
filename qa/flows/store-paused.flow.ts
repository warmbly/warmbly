import { expect, test } from "../lib/proof.ts";

// The paused state as the dashboard draws it: the status read reports the operator's pause.
test("buying new mailboxes is paused and says it will be back soon", async ({ page, proof }) => {
  // Report the product tour as finished, so a freshly seeded account never covers the page with it.
  await page.route("**/v1/auth/me", async (route) => {
    const response = await route.fetch();
    await route.fulfill({ response, json: { ...await response.json(), product_tour_completed_at: "2026-01-01T00:00:00Z" } });
  });
  await page.route("**/v1/store/status", async (route) => {
    const response = await route.fetch();
    await route.fulfill({ response, json: { ...await response.json(), sales_paused: true } });
  });

  await page.goto("/app/emails");
  await page.getByRole("button", { name: "Add account" }).first().click();
  const row = page.getByRole("menuitem", { name: /Buy new mailboxes/ });
  await expect(row).toBeVisible();
  await expect(row).toHaveAttribute("aria-disabled", "true");
  await expect(row.getByText("Will be available again soon")).toBeVisible();
  await proof.chapter("Paused", "Buy new mailboxes stays in the Add account menu, paused, and opens nothing");
  await proof.shot("paused-menu", { caption: "The buy row is marked Paused and says it will be available again soon" });
  await row.click({ force: true });
  await expect(page).toHaveURL(/\/app\/emails$/);
  await page.keyboard.press("Escape");

  await page.goto("/app/emails/buy");
  await expect(page.getByText("Buying new mailboxes is paused")).toBeVisible({ timeout: 20_000 });
  await proof.dwell();
  await proof.shot("paused-page", { caption: "The buy page says the same, and that what was already bought keeps working" });
});
