import { expect, test } from "../lib/proof.ts";

// Agency > Billing: what extra sends cost and the automatic top-up. Needs fixtures/admin-agencies.sql with a plan in force; see the README.
test("agency billing explains extra sends and automatic top-up", async ({ page, proof }) => {
  test.skip(process.env.QA_ADMIN_AGENCY_FIXTURES !== "1", "Apply fixtures/admin-agencies.sql, give the agency a plan, and set QA_ADMIN_AGENCY_FIXTURES=1.");
  await page.goto("/app/agency/billing");
  await expect(page.getByText("50,000 extra sends cost $60")).toBeVisible();
  await expect(page.getByRole("switch", { name: "Automatic top-up of sends" }).or(page.getByLabel("Automatic top-up of sends"))).toBeVisible();
  await expect(page.getByText("2 × $60, plus tax where it applies")).toBeVisible();
  await proof.dwell(900);
  await proof.shot("agency-extra-sends", { caption: "Extra sends: what a pack costs, automatic top-up with its cost per top-up and the most a month can cost" });

  const ai = page.getByLabel("Automatic top-up of AI balance");
  await ai.scrollIntoViewIfNeeded();
  await expect(page.getByText("up to 4 top-ups of $25, plus tax where it applies")).toBeVisible();
  await proof.dwell(700);
  await proof.shot("agency-ai-auto-topup", { caption: "AI balance: automatic top-up with the amount added each time and a monthly limit" });
});
