import { readFileSync } from "node:fs";
import { env } from "../lib/env.ts";
import { expect, test } from "../lib/proof.ts";

// Needs a test partner key on this stack (warmblyctl partner create + partner key), its path in QA_PARTNER_KEY_FILE.
test("a reseller's customer signs in to a managed workspace", async ({ page, proof }) => {
  test.skip(!process.env.QA_PARTNER_KEY_FILE, "Create a test partner key with warmblyctl and set QA_PARTNER_KEY_FILE.");
  test.setTimeout(120_000);
  const key = readFileSync(process.env.QA_PARTNER_KEY_FILE!, "utf8").trim();
  const api = `${env.apiURL}/v1/partner`;
  const call = async (method: "PUT" | "POST", path: string, data: unknown) => {
    const res = await page.request.fetch(`${api}${path}`, {
      method, data,
      headers: { Authorization: `Bearer ${key}`, "Idempotency-Key": `qa-${method}-${path}-${Date.now()}` },
    });
    expect(res.ok(), `${method} ${path}: ${res.status()} ${await res.text()}`).toBeTruthy();
    return res.json();
  };
  const ext = `acme-${Date.now()}`;
  const owner = { external_id: `u-${ext}`, email: `ada+${ext}@acme.example`, name: "Ada Lovelace", role: "owner" };
  await call("PUT", `/organizations/${ext}`, { name: "Acme Outreach", plan: "graphkit_managed", state: "active", owner });
  const link = await call("POST", `/organizations/${ext}/login-links`, { person: owner }) as { url: string };

  // Start signed out, as the reseller's customer would.
  await page.goto("/");
  await page.evaluate(() => localStorage.clear());
  await page.context().clearCookies();
  await page.goto(link.url);
  await expect(page).toHaveURL(/\/app\//, { timeout: 30_000 });
  await page.goto("/app/emails");
  await expect(page.getByText(/GraphKit/).first()).toBeVisible({ timeout: 30_000 });
  await proof.chapter("Managed workspace", "A reseller's customer, signed in from the reseller's portal with no Warmbly password");
  await proof.dwell();
  await proof.shot("partner-workspace", { caption: "The reseller's customer lands in their own workspace; buying happens in the reseller's portal, so the store is hidden" });

  // The billing page exists only on a stack with billing on (QA_STRIPE_SECRET_KEY).
  if (process.env.QA_BILLING === "1") {
    await page.goto("/app/settings/billing");
    await expect(page.getByText("Billed through GraphKit")).toBeVisible({ timeout: 20_000 });
    await proof.dwell();
    await proof.shot("partner-billing", { caption: "Billing reads \"Billed through GraphKit\": no checkout, plan change or top-up inside the workspace" });
  }

  await call("PUT", `/organizations/${ext}`, { name: "Acme Outreach", plan: "graphkit_managed", state: "suspended", suspended_reason: "billing" });
  await page.goto("/app/emails");
  await expect(page.getByText(/Paused/).first()).toBeVisible({ timeout: 30_000 });
  await proof.dwell();
  await proof.shot("partner-suspended", { caption: "Suspended by the reseller: everything stays readable, sending and warmup stop" });
});
