import { expect, test } from "../lib/proof.ts";

// The plan chooser popup, opened from Settings > Billing. Needs a stack that bills; see the README's billing plans proof.
test("the plan chooser popup", async ({ page, proof }) => {
  test.skip(process.env.QA_BILLING !== "1", "Start the stack with QA_STRIPE_SECRET_KEY so it bills, and set QA_BILLING=1.");
  await page.goto("/app/settings/billing");
  await page.getByRole("button", { name: /Choose a plan|Change plan|Upgrade/ }).first().click();
  const dialog = page.getByRole("dialog");
  await expect(dialog).toBeVisible();
  await proof.dwell(900);
  await proof.shot("plans-popup", { caption: "The plan chooser popup" });

  await page.keyboard.press("Escape");
  await expect(dialog).toBeHidden();
  await page.goto("/app/settings/billing/plans");
  await expect(page.getByText("Compare plans")).toBeVisible();
  await proof.dwell(900);
  await proof.shot("billing-plans-tab", { caption: "Settings > Billing > Plans: the same plan cards" });
});
