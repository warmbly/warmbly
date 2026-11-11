import type { Page } from "@playwright/test";

import { expect, test } from "../lib/proof.ts";

// Saving a top-up setting needs a recent sign-in; the dashboard asks for the password when the session is older.
async function confirmIfAsked(page: Page) {
  const dialog = page.getByRole("alertdialog").filter({ hasText: "Confirm it is you" });
  // The prompt opens once the request has been refused, so it is waited for, not sampled.
  const asked = await dialog.waitFor({ state: "visible", timeout: 4000 }).then(() => true, () => false);
  if (asked) {
    await dialog.getByPlaceholder("Password").fill("password123");
    await dialog.getByPlaceholder("Password").press("Enter");
  }
}

// Agency > Billing: what extra sends cost and the automatic top-up. Needs fixtures/admin-agencies.sql with Agency Pro in force (its pack is $60); see the README.
test("agency billing explains extra sends and automatic top-up", async ({ page, proof }) => {
  test.skip(process.env.QA_ADMIN_AGENCY_FIXTURES !== "1", "Apply fixtures/admin-agencies.sql, put the agency on Agency Pro, and set QA_ADMIN_AGENCY_FIXTURES=1.");
  await page.goto("/app/agency/billing");
  await expect(page.getByText("50,000 extra sends cost $60")).toBeVisible();
  await expect(page.getByRole("switch", { name: "Automatic top-up of sends" }).or(page.getByLabel("Automatic top-up of sends"))).toBeVisible();
  await expect(page.getByText("2 × $60, plus tax where it applies")).toBeVisible();
  await proof.dwell(900);
  await proof.shot("agency-extra-sends", { caption: "Extra sends: what a pack costs, automatic top-up with its cost per top-up and the most a month can cost" });

  // Changing a number charges nothing and saves nothing until Save changes.
  await page.getByLabel("Stop after this many top-ups in a month").fill("3");
  await expect(page.getByText("3 × $60, plus tax where it applies")).toBeVisible();
  await expect(page.getByText("Nothing is charged while automatic top-up is off.")).toBeVisible();
  await proof.dwell(900);
  await proof.shot("agency-auto-topup-unsaved", { caption: "A changed number waits for Save changes; nothing is sent or charged before it" });
  // Saved while off, it is kept; then put back so the recording can run again.
  await page.getByRole("button", { name: "Save changes" }).click();
  await confirmIfAsked(page);
  await expect(page.getByRole("button", { name: "Save changes" })).toBeHidden();
  await page.reload();
  await expect(page.getByLabel("Stop after this many top-ups in a month")).toHaveValue("3");
  await page.getByLabel("Stop after this many top-ups in a month").fill("2");
  await page.getByRole("button", { name: "Save changes" }).click();
  await confirmIfAsked(page);
  await expect(page.getByRole("button", { name: "Save changes" })).toBeHidden();

  const ai = page.getByLabel("Automatic top-up of AI balance");
  await ai.scrollIntoViewIfNeeded();
  await expect(page.getByText("up to 4 top-ups of $25, plus tax where it applies")).toBeVisible();
  await proof.dwell(700);
  await proof.shot("agency-ai-auto-topup", { caption: "AI balance: automatic top-up with the amount added each time and a monthly limit" });
});
