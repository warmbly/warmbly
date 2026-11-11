import { expect, test } from "../lib/proof.ts";

// The workspace Plans tab lists the agency plans below the regular ones. Needs a stack that bills (QA_STRIPE_SECRET_KEY).
test("a workspace's Plans tab lists the agency plans", async ({ page, proof }) => {
  await page.goto("/app/settings/billing/plans");
  const section = page.getByText("Agency plans", { exact: true }).first();
  await expect(section).toBeVisible();
  await expect(page.getByText("150,000 shared sends a month")).toBeVisible();
  await expect(page.getByRole("link", { name: "Set up an agency" })).toBeVisible();
  await section.evaluate((node) => node.scrollIntoView({ block: "start", behavior: "instant" }));
  await proof.dwell(800);
  await proof.shot("billing-agency-plans", { caption: "Settings > Billing > Plans: the agency plans, leading to the agency" });
});
