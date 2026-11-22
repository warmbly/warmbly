import { expect, test } from "../lib/proof.ts";

// Admin origin with fixtures/admin-agencies.sql applied (admin permissions and a session marked as
// having passed a second factor: local auth fixtures, not MFA proof). Runs after buy-mailboxes.
test("operators run the store and partners from the admin panel", async ({ page, proof }) => {
  test.skip(process.env.QA_ADMIN_AGENCY_FIXTURES !== "1", "Prepare the localhost admin auth fixtures and set QA_ADMIN_AGENCY_FIXTURES=1.");
  test.setTimeout(150_000);
  await page.addInitScript(() => {
    const token = localStorage.getItem("auth_token");
    if (token) localStorage.setItem("warmbly_admin_token", token);
  });

  await page.goto("/store?tab=health");
  await expect(page.getByRole("tab", { name: "Health" }).or(page.getByRole("button", { name: "Health" })).first()).toBeVisible({ timeout: 20_000 });
  await proof.chapter("Store health", "Real localhost API. Admin permissions and the verified session are local auth fixtures, not MFA proof.");
  await proof.dwell();
  await proof.shot("admin-store-health", { caption: "Store health: live or test mode, partner balance, last webhook, loop lag, stuck orders and failed connects" });

  await page.goto("/store?tab=orders");
  await expect(page.getByText("northwind.com").first()).toBeVisible({ timeout: 20_000 });
  await proof.dwell();
  await proof.shot("admin-store-orders", { caption: "Every order on the instance, with its workspace, status, steps and failure code" });

  await page.getByText("northwind.com").first().click();
  await proof.dwell();
  await proof.shot("admin-store-order-detail", { caption: "An order's detail with the operator actions: refresh, retry placing, refund a line or an amount" });
  await page.keyboard.press("Escape");

  await page.goto("/store?tab=mailboxes");
  await expect(page.getByText(/@northwind\.com/).first()).toBeVisible({ timeout: 20_000 });
  await proof.dwell();
  await proof.shot("admin-store-mailboxes", { caption: "Every purchased mailbox: connect state, paid through, billing failures" });

  await page.goto("/store?tab=economics");
  await proof.dwell();
  await proof.shot("admin-store-economics", { caption: "Unit economics: cost against retail per mailbox provider and per domain ending" });

  await page.goto("/partners");
  await page.getByRole("button", { name: /New partner/ }).click();
  const dialog = page.getByRole("dialog", { name: "New partner" });
  await dialog.locator("#new-partner-slug").fill("graphkit");
  await dialog.locator("#new-partner-name").fill("GraphKit");
  await dialog.locator("#new-partner-hosts").fill("app.graphkit.com");
  await proof.chapter("Partners", "Resellers that create and manage Warmbly workspaces for their own customers");
  await proof.shot("admin-partner-new", { caption: "A new reseller partner: its name, the display name customers see and the hosts it may send people back to" });
  await dialog.getByRole("button", { name: "Create partner" }).click();
  await expect(page.getByText("GraphKit").first()).toBeVisible({ timeout: 20_000 });
  await proof.dwell();
  await proof.shot("admin-partner-detail", { caption: "The partner: API keys, webhook, allowed hosts, wholesale prices, its workspaces and the monthly statement" });
});
