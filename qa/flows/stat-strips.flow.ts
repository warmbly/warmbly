import { expect, test } from "../lib/proof.ts";

test("stat strips across the dashboard share one look", async ({ page, proof }) => {
  // Report the product tour as finished, so a freshly seeded account never covers the page with it.
  await page.route("**/v1/auth/me", async (route) => {
    const response = await route.fetch();
    await route.fulfill({ response, json: { ...await response.json(), product_tour_completed_at: "2026-01-01T00:00:00Z" } });
  });
  const settled = async () => {
    await expect(page.getByText("Checking…")).toHaveCount(0);
    await expect(page.getByText("Unavailable", { exact: true })).toHaveCount(0);
  };

  await page.goto("/app/campaigns");
  const main = page.getByRole("main");
  await expect(main.getByText("RevOps outreach - July")).toBeVisible({ timeout: 30_000 });
  await expect(main.getByText("Agency partnerships")).toBeVisible();
  await proof.chapter("Campaigns", "Status counts filter the list, the selected one underlined");
  await proof.shot("strip-campaigns", { caption: "Campaigns list" });

  // The seed's draft stays a draft; the other campaign's status moves with the scheduler.
  await page.getByRole("button", { name: /^Draft/ }).click();
  await expect(page.getByRole("button", { name: /^Draft/ })).toHaveAttribute("aria-pressed", "true");
  await expect(main.getByText("Agency partnerships")).toBeVisible();
  await expect(main.getByText("RevOps outreach - July")).toBeHidden();
  await proof.shot("strip-campaigns-draft", { caption: "Campaigns list filtered to drafts" });
  await page.getByRole("button", { name: /^All \d/ }).click();

  await main.getByText("RevOps outreach - July").click();
  const sent = page.getByRole("slider", { name: "Sent over time" });
  await expect(sent).toBeVisible();
  await expect(page.getByRole("slider", { name: "Reply rate over time" })).toBeVisible();
  await settled();
  await proof.chapter("Campaign", "Each rate over the daily activity behind it");
  await proof.shot("strip-campaign-detail", { caption: "Campaign detail" });

  for (const [path, name, ready] of [
    ["/app/analytics", "strip-analytics", page.getByRole("slider", { name: "Total sent over time" })],
    ["/app/deliverability", "strip-deliverability", page.getByRole("slider", { name: "Bounce rate over time" })],
    ["/app/api-keys", "strip-api-keys", page.getByRole("slider", { name: "Requests · 24h over time" })],
    ["/app/templates", "strip-templates", page.getByText("Quick yes")],
    ["/app/crm/tasks", "strip-tasks", page.getByText("Demo with Emberly")],
  ] as const) {
    await page.goto(path);
    await expect(ready).toBeVisible();
    await settled();
    await proof.shot(name, { caption: path });
  }
});
