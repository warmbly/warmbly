import { expect, test } from "../lib/proof.ts";

// The seeded local mailboxes use synthetic Cloud reports; no external mailbox is contacted.
test("Cloud warmup counts and each mailbox's health stay independent", async ({ page, proof }) => {
  const sender = "33333333-0000-0000-0000-0000dddd0001";
  const outbound = "33333333-0000-0000-0000-0000dddd0002";
  let resumed = false;
  const report = (id: string) => ({
    id,
    enrolled: true,
    enrollment_state: "active",
    cloud: {
      status: "active",
      sent_today: id === sender ? 9 : 6,
      sent_7d: 25,
      replied_7d: 3,
      spam_placed_7d: 0,
      settings: { base: 20 },
      warmup: { enabled: true, paused: id === outbound && !resumed, current_volume: 0, target_volume: 20, max_volume: 40, reply_rate: 30, days_active: 5 },
      health: id === outbound && resumed ? null : { state: "healthy", score: 100 },
    },
  });

  await page.route("**/v1/auth/me", async (route) => {
    const response = await route.fetch();
    const user = await response.json();
    await route.fulfill({ response, json: { ...user, admin_permissions: user.admin_permissions | (1 << 14), session_mfa_verified: true, product_tour_completed_at: "2026-01-01T00:00:00Z" } });
  });
  await page.route("**/v1/auth/config", async (route) => {
    const response = await route.fetch();
    await route.fulfill({ response, json: { ...await response.json(), self_hosted: true } });
  });
  await page.route("**/v1/cloud-link", (route) => route.fulfill({ json: {
    connected: true, reachable: true, default_cloud_url: "https://cloud.example.invalid",
    link: { instance_id: "fixture", organization_id: "fixture", organization_name: "Cloud workspace", cloud_url: "https://cloud.example.invalid", connected_at: "2026-01-01T00:00:00Z" },
  } }));
  await page.route("**/v1/cloud-link/mailboxes", (route) => route.fulfill({ json: { data: [report(sender), report(outbound)] } }));
  await page.route(`**/v1/cloud-link/mailboxes/${outbound}/resume`, (route) => {
    resumed = true;
    return route.fulfill({ json: report(outbound) });
  });
  await page.route("**/v1/analytics/accounts?*", (route) => {
    const ids = new URL(route.request().url()).searchParams.get("email_ids")?.split(",") ?? [];
    return route.fulfill({ json: { data: ids.map((id) => ({
      id, status: "active", health: { status: "healthy", score: 100 }, errors: [],
      daily_usage: { date: "2026-01-01", campaign_sent: 0, campaign_limit: 50 }, in_campaign: false,
    })) } });
  });

  await page.goto("/app/emails");
  const sentRow = page.getByRole("row", { name: /dev\.send@warmbly\.test/ });
  const outboundRow = page.getByRole("row", { name: /dev\.outbound@warmbly\.test/ });
  await expect(sentRow).toBeVisible();
  await expect(outboundRow).toBeVisible();
  await expect(page.getByText(/2 of 4 mailboxes connected to Warmbly Cloud\. 1 warming\./)).toBeVisible();
  await expect(sentRow.getByTitle(/9 of 20 warmup emails sent today/).last()).toBeVisible();
  await proof.chapter("Cloud warmup by mailbox", "Synthetic Cloud reports distinguish connected mailboxes from active warmup.");
  await proof.shot("cloud-warmup-counts", { caption: "Two Cloud connections, one actively warming; each row shows its Cloud-reported sends." });

  await outboundRow.getByRole("button", { name: "Warmup actions" }).click();
  await page.getByRole("menuitem", { name: "Resume in Warmbly Cloud" }).click();
  await expect(page.getByText(/2 of 4 mailboxes connected to Warmbly Cloud\. 2 warming\./)).toBeVisible();
  await expect(outboundRow.getByText("Unavailable", { exact: true }).last()).toBeVisible();
  await expect(sentRow.getByTitle(/9 of 20 warmup emails sent today/).last()).toBeVisible();
  await proof.shot("cloud-health-isolation", { caption: "A missing synthetic Cloud health report affects only its mailbox; the other row retains its reading." });
});
