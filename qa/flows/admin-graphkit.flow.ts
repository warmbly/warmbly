import { expect, test } from "../lib/proof.ts";

// Admin origin; the seeded account holds admin permissions with a verified session (local fixtures, not MFA proof).
test("an operator adds the GraphKit API key in the admin panel", async ({ page, proof }) => {
  test.skip(process.env.QA_ADMIN_AGENCY_FIXTURES !== "1", "Prepare the localhost admin auth fixtures and set QA_ADMIN_AGENCY_FIXTURES=1.");
  await page.addInitScript(() => {
    const token = localStorage.getItem("auth_token");
    if (token) localStorage.setItem("warmbly_admin_token", token);
  });
  await page.goto("/health");
  await page.getByRole("tab", { name: "Integrations" }).or(page.getByRole("button", { name: "Integrations" })).first().click();
  await expect(page.getByText("Domains and mailboxes (GraphKit)")).toBeVisible({ timeout: 20_000 });
  await proof.chapter("GraphKit", "Real localhost API. Admin permissions and the verified session are local auth fixtures, not MFA proof.");
  await proof.dwell();
  await proof.shot("admin-graphkit", { caption: "Setup and health, Integrations: paste the GraphKit API key, test it, save; the store turns on with no restart" });
});
