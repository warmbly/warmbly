import type { Page } from "@playwright/test";
import { expect, test } from "../lib/proof.ts";

// The product tour opens a few seconds in and hides the page behind its dialog.
async function closeTour(page: Page) {
  const skip = page.getByRole("button", { name: /Skip tour/ });
  await skip.waitFor({ timeout: 6000 }).then(async () => {
    await skip.click();
    await page.getByRole("button", { name: "Done" }).click();
  }, () => undefined);
  await expect(skip).toBeHidden();
}

test("stat strips across the dashboard share one look", async ({ page, proof }) => {
  await page.goto("/app/campaigns");
  await closeTour(page);
  await expect(page.getByRole("button", { name: /^All/ }).first()).toBeVisible();
  await proof.chapter("Campaigns", "Status counts filter the list, the selected one underlined");
  await proof.shot("strip-campaigns", { caption: "Campaigns list" });
  await page.getByRole("button", { name: /^Paused/ }).first().click();
  await expect(page.getByRole("button", { name: /^Paused/ }).first()).toHaveAttribute("aria-pressed", "true");
  await proof.shot("strip-campaigns-paused", { caption: "Campaigns list filtered to paused" });
  await page.getByRole("button", { name: /^All/ }).first().click();

  await page.getByRole("link", { name: /RevOps outreach - July/ }).click();
  await expect(page.getByText("Open rate").first()).toBeVisible();
  await expect(page.getByText("Reply rate").first()).toBeVisible();
  await page.waitForLoadState("networkidle");
  await proof.chapter("Campaign", "Each rate over the daily activity behind it");
  await proof.shot("strip-campaign-detail", { caption: "Campaign detail" });

  for (const [path, name, text] of [
    ["/app/analytics", "strip-analytics", "Total sent"],
    ["/app/deliverability", "strip-deliverability", "Complaint rate"],
    ["/app/api-keys", "strip-api-keys", "Avg latency · 24h"],
    ["/app/templates", "strip-templates", "Edited this week"],
    ["/app/crm/tasks", "strip-tasks", "Overdue"],
  ] as const) {
    await page.goto(path);
    await expect(page.getByText(text).first()).toBeVisible();
    await page.waitForLoadState("networkidle");
    await proof.shot(name, { caption: path });
  }
});
