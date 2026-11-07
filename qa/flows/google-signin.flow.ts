import type { Page } from "@playwright/test";
import { expect, test } from "../lib/proof.ts";

// Synthetic sign-in, grant and vendor responses keep this proof local; nothing reaches Google, InboxKit or Zapmail.
const CLIENT_ID = "000000000000-fixture.apps.googleusercontent.com";
const MAILBOX_ID = "33333333-0000-0000-0000-0000dddd0002";
const VENDOR_ID = "00000000-0000-4000-8000-00000000c0de";
const at = (local: string, domain: string) => `${local}@${domain}`;
const MAILBOX_EMAIL = at("dev.outbound", "warmbly.test");

async function enableGoogleSignin(page: Page) {
  await page.route("**/v1/auth/config", async (route) => {
    const response = await route.fetch();
    await route.fulfill({ response, json: { ...await response.json(), gmail_oauth_connect: true } });
  });
  await page.route("**/v1/emails/grants/config", (route) => route.fulfill({ json: {
    google_enabled: false, google_scopes: [], microsoft_enabled: false, google_mailbox_client_id: CLIENT_ID,
  } }));
}

test("a Google Workspace mailbox signs in after a trusted-app step, and personal Gmail goes to an app password", async ({ page, proof }) => {
  await enableGoogleSignin(page);
  await page.goto("/app/emails");
  await page.getByRole("button", { name: "Add account", exact: true }).first().click();
  await page.getByRole("button", { name: /Gmail and Google Workspace/ }).click();
  await expect(page.getByText("Personal Gmail", { exact: true })).toBeVisible();
  await proof.dwell();
  await proof.chapter("Connect a Google mailbox", "Workspace signs in with Google once the admin trusts Warmbly; personal Gmail uses an app password");
  await proof.shot("google-method-chooser", { caption: "Google splits into Google Workspace and Personal Gmail; no domain-wide grant setup is offered" });

  await page.getByRole("button", { name: /Sign in with Google/ }).click();
  await expect(page.getByText("Your Workspace admin trusts Warmbly, once per domain")).toBeVisible();
  await expect(page.getByText(CLIENT_ID)).toBeVisible();
  await proof.dwell();
  await proof.shot("workspace-trusted-app-step", { caption: "Step 1: the Admin console path to mark Warmbly Trusted, with the client ID to copy" });

  const address = page.getByPlaceholder(at("alex", "yourcompany.com"));
  await address.pressSequentially(at("alex", "acme.test"), { delay: 40 });
  await expect(page.getByRole("button", { name: "Continue with Google" })).toBeEnabled();
  await proof.shot("workspace-signin-ready", { caption: "Step 2: a Workspace address continues to Google sign-in" });

  await address.fill("");
  await address.pressSequentially(at("alex.morgan", "gmail.com"), { delay: 40 });
  await expect(page.getByText("This is a personal Gmail address.")).toBeVisible();
  await expect(page.getByRole("button", { name: "Continue with an app password" })).toBeVisible();
  await proof.shot("personal-gmail-redirect", { caption: "A personal Gmail address is sent to the app-password walkthrough instead" });
});

test("an existing Google mailbox switches how it signs in from the drawer, and stays the same mailbox", async ({ page, proof }) => {
  let reconnecting = false;
  await enableGoogleSignin(page);
  await page.route("**/v1/emails/grants/migration", (route) => route.fulfill({ json: {
    total: 1,
    data: [{ domain: "warmbly.test", kind: "workspace", mailboxes: [
      { id: MAILBOX_ID, email: MAILBOX_EMAIL, name: "Dev Outbound", status: "active", vendor_connection_id: VENDOR_ID, vendor: "inboxkit" },
    ] }],
  } }));
  await page.route(`**/v1/emails/${MAILBOX_ID}/signin`, (route) => route.fulfill({ json: {
    method: "google_signin", full_mail_access: false, personal: false, managed: false, auto_switch: false,
    options: {
      google_signin: { client_id: CLIENT_ID }, app_password: true, grant: null,
      vendor: { connection_id: VENDOR_ID, vendor: "inboxkit", label: "InboxKit" },
    },
    switch: reconnecting ? { status: "signing_in", automatic: false, at: new Date().toISOString() } : null,
  } }));
  await page.route(`**/v1/emails/vendors/${VENDOR_ID}/reconnect`, async (route) => {
    if (route.request().method() === "POST") reconnecting = true;
    await route.fulfill({ json: { data: [{ email_account_id: MAILBOX_ID, email: MAILBOX_EMAIL, status: reconnecting ? "signing_in" : "queued" }] } });
  });

  await page.goto("/app/emails");
  await expect(page.getByText("1 mailbox connects with the older Google sign-in.", { exact: false })).toBeVisible();
  await proof.chapter("Switch an existing mailbox", "Synthetic sign-in state for one seeded mailbox; the switch keeps its history, campaigns and warmup");
  await proof.shot("signin-banner", { caption: "The banner lists mailboxes on the older sign-in, with a one-click reconnect through their vendor" });

  await page.getByRole("button", { name: "Review mailboxes" }).click();
  const migration = page.getByRole("dialog", { name: "Move off Google sign-in" });
  await expect(migration.getByText(CLIENT_ID)).toBeVisible();
  await proof.dwell();
  await proof.shot("signin-migration-dialog", { caption: "Review mailboxes: the trusted-app step, then each mailbox's switch, all keeping their history" });
  await migration.getByRole("button", { name: "Close" }).click();
  await expect(migration).toBeHidden();

  await page.getByRole("row", { name: /dev\.outbound@warmbly\.test/ }).getByText(MAILBOX_EMAIL).first().click();
  await page.getByRole("button", { name: "Settings", exact: true }).click();
  const section = page.getByText("Sign-in", { exact: true }).locator("..");
  await expect(section.getByText("Google sign-in, older permissions")).toBeVisible();
  await section.scrollIntoViewIfNeeded();
  await proof.shot("drawer-signin-options", { caption: "The drawer's Sign-in section offers only the switches that can succeed for this mailbox" });

  await section.getByRole("button", { name: /Set up/ }).first().click();
  await expect(section.getByText("First, your Workspace admin trusts Warmbly, once per domain")).toBeVisible();
  await proof.dwell();
  await proof.shot("drawer-reconnect-google", { caption: "Reconnect with Google repeats the trusted-app step before signing in again" });
  await section.getByRole("button", { name: /Set up/ }).first().click();

  await section.getByRole("button", { name: "Reconnect", exact: true }).click();
  await expect(section.getByRole("status")).toContainText("InboxKit is signing in as the mailbox");
  await proof.dwell();
  await proof.shot("drawer-vendor-reconnecting", { caption: "Reconnect through InboxKit shows its progress live; the mailbox keeps working meanwhile" });
});
