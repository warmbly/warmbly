import { expect, test } from "../lib/proof.ts";

// A client connects many mailboxes at once from its own inbox vendor account, through a connect link.
// Needs fixtures/admin-agencies.sql, the portal service in front of this stack and the mock vendors; see the README.
const PORTAL = process.env.QA_PORTAL_URL ?? "";

test("a client connects mailboxes from its vendor account through a connect link", async ({ page, proof }) => {
  test.skip(!PORTAL, "Run the portal service against this stack, start it with QA_VENDOR_MOCK=1, and set QA_PORTAL_URL.");

  await page.goto("/app/emails");

  // The agency makes the link; a vendor account is one of the ways in.
  await page.getByRole("button", { name: "Invite client to connect" }).click();
  const dialog = page.getByRole("dialog", { name: "Invite client to connect" });
  await expect(dialog.getByText("Inbox vendor account")).toBeVisible();
  await proof.dwell(900);
  await proof.shot("connect-link-vendor-option", { caption: "The link's ways to connect now include an inbox vendor account" });
  await dialog.getByRole("button", { name: "Create link" }).click();
  const address = await dialog.locator("input[readonly]").inputValue();
  expect(address).toContain("/connect/");

  // The client opens it with no account.
  await page.goto(address.replace(/^https?:\/\/[^/]+/, PORTAL));
  await page.getByRole("button", { name: "Connect many from my inbox vendor" }).click();
  await page.getByLabel("Vendor").selectOption({ label: "Mailforge" });
  await page.getByLabel("API key").fill("qa-client-key");
  await proof.dwell(900);
  await proof.shot("client-vendor-key", { caption: "The client names its vendor account once" });
  await page.getByRole("button", { name: "Find my mailboxes" }).click();

  await expect(page.getByRole("heading", { name: "Pick the mailboxes to connect" })).toBeVisible();
  await expect(page.getByText(/@sunrisehq\.test/).first()).toBeVisible();
  await proof.dwell(900);
  await proof.shot("client-vendor-pick", { caption: "The account's mailboxes, picked up to what the link allows" });
  await page.getByRole("button", { name: /^Connect \d+ mailbox/ }).click();

  await expect(page.getByRole("heading", { name: /being connected/ })).toBeVisible();
  await proof.dwell(900);
  await proof.shot("client-vendor-started", { caption: "The import runs in the workspace; the client is done" });
});
