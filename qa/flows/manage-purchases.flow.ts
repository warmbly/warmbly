import { expect, test } from "../lib/proof.ts";

// Runs after buy-mailboxes on the same data: it opens what that flow bought.
test("manage purchased domains, mailboxes and orders", async ({ page, proof }) => {
  test.setTimeout(120_000);
  // Report the product tour as finished, so a freshly seeded account never covers the page with it.
  await page.route("**/v1/auth/me", async (route) => {
    const response = await route.fetch();
    await route.fulfill({ response, json: { ...await response.json(), product_tour_completed_at: "2026-01-01T00:00:00Z" } });
  });

  await page.goto("/app/emails/buy/orders");
  await expect(page.getByText("northwind.com").first()).toBeVisible({ timeout: 20_000 });
  await proof.chapter("Orders", "Every order with its status, total and refunds");
  await proof.dwell();
  await proof.shot("orders-history", { caption: "Order history: completed orders, what was charged, and the live track for any still open" });

  await page.goto("/app/emails/domains?tab=purchased");
  const row = page.getByRole("row", { name: /northwind\.com/ }).or(page.getByText("northwind.com", { exact: true })).first();
  await expect(row).toBeVisible({ timeout: 20_000 });
  await row.click();
  const drawer = page.getByRole("dialog", { name: "northwind.com" }).or(page.locator('[aria-label="northwind.com"]')).first();
  await expect(drawer.getByText("Renew automatically")).toBeVisible();
  await proof.chapter("Domain", "Renewal, forwarding, mail authentication and DNS for a bought domain");
  await proof.dwell();
  await proof.shot("domain-drawer", { caption: "A purchased domain: renewal date and current price, auto-renew, website forward and mail authentication" });

  const dns = drawer.getByText("DNS records");
  await dns.scrollIntoViewIfNeeded();
  await proof.dwell();
  await proof.shot("domain-dns", { caption: "DNS records: Warmbly's own mail and tracking records are locked, anything else can be added, edited or removed" });
  await page.keyboard.press("Escape");

  await page.goto("/app/emails");
  await page.getByRole("row", { name: /^dev@northwind\.com/ }).click();
  const settingsTab = page.getByRole("tab", { name: "Settings" }).or(page.getByRole("button", { name: "Settings", exact: true })).first();
  await settingsTab.click();
  const subscription = page.getByText("Subscription", { exact: true }).first();
  await expect(subscription).toBeVisible({ timeout: 15_000 });
  await subscription.scrollIntoViewIfNeeded();
  await proof.chapter("Mailbox", "Each bought mailbox carries its subscription");
  await proof.dwell();
  await proof.shot("mailbox-subscription", { caption: "The mailbox drawer: $3 a month, paid through date, cancel at the end of the month or resume" });

  // Needs an agency with this workspace as a client (fixtures/admin-agencies.sql); skipped otherwise.
  if (process.env.QA_ADMIN_AGENCY_FIXTURES === "1") {
    await page.goto("/app/agency/store");
    await expect(page.getByText("Live mailboxes").first()).toBeVisible({ timeout: 20_000 });
    await expect(page.getByText("$18.00").first()).toBeVisible();
    await proof.chapter("Agency", "An agency sees every client's purchased domains and mailboxes in one place");
    await proof.dwell();
    await proof.shot("agency-store", { caption: "Agency view: each client's domains, mailboxes, monthly spend and renewals coming up" });
  }
});
