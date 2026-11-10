import { expect, test } from "../lib/proof.ts";

// Run after flows/admin-agencies.flow.ts, against the dashboard: the agency it gave a plan to.
test("an agency on a plan arranged directly sees it on its billing page", async ({ page, proof }) => {
  test.skip(process.env.QA_ADMIN_AGENCY_FIXTURES !== "1", "Run admin-agencies first and set QA_ADMIN_AGENCY_FIXTURES=1.");
  await page.goto("/app/agency/billing");
  await expect(page.getByText("Arranged directly, not billed here")).toBeVisible();
  await expect(page.getByText("Agency Pro").first()).toBeVisible();
  await proof.shot("agency-billing-arranged", { caption: "The agency's own billing page: the plan is in force with no subscription to manage" });
});
