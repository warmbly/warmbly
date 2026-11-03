import type { Page } from "@playwright/test";
import { expect, test } from "../lib/proof.ts";

// Synthetic Cloud responses keep this proof local; no Cloud workspace or subscription is used.
async function asSelfHostAdmin(page: Page) {
  await page.route("**/v1/auth/me", async (route) => {
    const response = await route.fetch();
    const user = await response.json();
    await route.fulfill({ response, json: { ...user, admin_permissions: user.admin_permissions | (1 << 14), session_mfa_verified: true, product_tour_completed_at: "2026-01-01T00:00:00Z" } });
  });
  await page.route("**/v1/auth/config", async (route) => {
    const response = await route.fetch();
    await route.fulfill({ response, json: { ...await response.json(), self_hosted: true } });
  });
}

test("an unlinked self-hosted instance is told local warmup is not effective and pointed at Warmbly Cloud", async ({ page, proof }) => {
  await asSelfHostAdmin(page);
  await page.route("**/v1/cloud-link", (route) => route.fulfill({ json: { connected: false, reachable: true, default_cloud_url: "https://cloud.example.invalid" } }));

  await page.goto("/app/emails");
  await expect(page.getByText("Warmup on this instance is not effective.")).toBeVisible();
  await expect(page.getByRole("link", { name: "Community on Discord" })).toHaveAttribute("href", "https://dc.warmbly.com");
  await proof.chapter("Self-hosted notice", "The Mailboxes banner on an instance that is not linked to Warmbly Cloud.");
  await proof.shot("self-host-notice", { caption: "Local warmup only pairs the instance's own mailboxes, Warmbly gives no support for self-hosting, and Cloud warms in a pool of thousands." });
});

test("a free Cloud workspace keeps its enrolled mailboxes and warms new ones on the instance", async ({ page, proof }) => {
  await asSelfHostAdmin(page);
  await page.route("**/v1/cloud-link", (route) => route.fulfill({ json: {
    connected: true, reachable: true, default_cloud_url: "https://cloud.example.invalid",
    link: { instance_id: "fixture", organization_id: "fixture", organization_name: "Cloud workspace", cloud_url: "https://cloud.example.invalid", connected_at: "2026-01-01T00:00:00Z" },
    info: {
      instance: { id: "fixture", organization_id: "fixture", name: "Self-hosted", url: "http://localhost", version: "dev", created_at: "2026-01-01T00:00:00Z", mailbox_count: 2 },
      organization: { id: "fixture", name: "Cloud workspace" },
      plan: { tier: "free", mailbox_limit: 10, enrolled: 2, warming: 2, price_usd: 15, upgrade_url: "https://cloud.example.invalid/app/settings/billing", warmup_entitled: true, subscription_required: true },
    },
  } }));
  await page.route("**/v1/cloud-link/mailboxes", (route) => route.fulfill({ json: { data: ["a", "b"].map((n, i) => ({
    id: `00000000-0000-4000-8000-00000000000${i + 1}`, email: `kept-${n}@workspace.test`, name: `Kept ${n}`, provider: "smtp_imap", status: "active", enrolled: true, managed: false,
  })) } }));

  await page.goto("/app/emails");
  await expect(page.getByText("New mailboxes warm on this instance, which is not effective.")).toBeVisible();
  await expect(page.getByRole("link", { name: "See plans" })).toBeVisible();
  await proof.chapter("Free workspace linked before the change", "Its two enrolled mailboxes keep warming in Cloud; nothing new is added without a paid plan.");
  await proof.shot("self-host-free-linked", { caption: "A free workspace keeps what it enrolled. New mailboxes warm on this instance, or the workspace moves to a paid plan." });
});

test("an unlinked instance shows no Warmbly Cloud settings", async ({ page, proof }) => {
  await asSelfHostAdmin(page);
  await page.route("**/v1/cloud-link", (route) => route.fulfill({ json: { connected: false, reachable: true, default_cloud_url: "https://cloud.example.invalid" } }));

  await page.goto("/app/settings/warmbly-cloud");
  await expect(page).toHaveURL(/\/app\/settings\/profile$/);
  await expect(page.getByRole("link", { name: "Warmbly Cloud", exact: true })).toHaveCount(0);
  await proof.chapter("No Cloud settings", "An instance with no link has no Warmbly Cloud section; its old address opens the profile.");
  await proof.shot("self-host-settings-hidden", { caption: "Settings on an unlinked instance: no Warmbly Cloud entry under Advanced." });
});

test("the Cloud approval page says new self-hosted links are closed", async ({ page, proof }) => {
  await page.goto("/connect?code=ABCDEFGH");
  await expect(page.getByRole("heading", { name: "Warmbly Cloud no longer links self-hosted instances" })).toBeVisible();
  await proof.chapter("Approval page", "Older instances still send people here; Cloud refuses every code.");
  await proof.shot("connect-closed", { caption: "Existing connections keep working; new ones are not offered." });
});
