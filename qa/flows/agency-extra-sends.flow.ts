import { expect, test } from "../lib/proof.ts";

// Agency > Billing: what extra sends cost and the automatic top-up. Needs fixtures/admin-agencies.sql with a plan in force; see the README.
test("agency billing explains extra sends and automatic top-up", async ({ page, proof }) => {
  test.skip(process.env.QA_ADMIN_AGENCY_FIXTURES !== "1", "Apply fixtures/admin-agencies.sql, give the agency a plan, and set QA_ADMIN_AGENCY_FIXTURES=1.");
  await page.goto("/app/agency/billing");
  await expect(page.getByText("Automatic top-up", { exact: true })).toBeVisible();
  await expect(page.getByText(/50,000 more sends for \$60/)).toBeVisible();
  await expect(page.getByText(/up to \$120 a period/)).toBeVisible();
  await proof.dwell(900);
  await proof.shot("agency-extra-sends", { caption: "Agency billing: the price of a pack, automatic top-up and what happens when sends run out" });
});
