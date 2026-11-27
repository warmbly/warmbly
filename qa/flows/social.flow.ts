import type { Locator, Page } from "@playwright/test";

import { env } from "../lib/env.ts";
import { expect, test } from "../lib/proof.ts";

// Runs against the GraphKit simulator (`GRAPHKIT_API_URL=fake`, the `make backend` default), which serves every channel.

// Starting a connection needs a recent sign-in; an older session confirms the password first.
async function acceptTerms(page: Page, dialog: Locator) {
  await dialog.getByText(/I understand/).click();
  await dialog.getByRole("button", { name: "Continue" }).click();
  const reauth = page.getByRole("alertdialog", { name: "Confirm it is you" });
  const signIn = dialog.getByRole("button", { name: /^(Sign in|Show the QR code|Get a pairing code)$/ });
  await expect(reauth.or(signIn)).toBeVisible({ timeout: 20_000 });
  if (await reauth.isVisible()) {
    await reauth.getByRole("textbox", { name: "Password" }).fill(env.password);
    await reauth.getByRole("button", { name: "Confirm" }).click();
  }
  await expect(signIn).toBeVisible({ timeout: 20_000 });
}

test.beforeEach(async ({ page }) => {
  // Report the product tour as finished, so a freshly seeded account never covers the page with it.
  await page.route("**/v1/auth/me", async (route) => {
    const response = await route.fetch();
    await route.fulfill({ response, json: { ...await response.json(), product_tour_completed_at: "2026-01-01T00:00:00Z" } });
  });
});

test("find social accounts under Settings, with nothing added to the sidebar", async ({ page, proof }) => {
  await page.goto("/app/settings/social");
  await expect(page.getByRole("heading", { name: "Social accounts" }).first()).toBeVisible();
  await proof.chapter("Social accounts", "LinkedIn, Instagram, WhatsApp, Telegram and Facebook accounts live in Settings");
  await proof.shot("social-settings-empty", { caption: "Settings > Social accounts before any account is connected" });
});

test("connect a LinkedIn account from Settings, past a two-factor checkpoint", async ({ page, proof }) => {
  test.setTimeout(120_000);
  await page.goto("/app/settings/social");
  await page.getByRole("button", { name: /^LinkedIn LinkedIn Connect/ }).click();
  const dialog = page.getByRole("dialog");
  await expect(dialog.getByRole("button", { name: /Continue/ })).toBeVisible();
  await proof.chapter("Connect LinkedIn", "Pick the channel, then price, terms and LinkedIn's own sign-in");
  await proof.shot("connect-linkedin-price", { caption: "The price step for a LinkedIn account. This local instance does not bill", target: dialog });
  await dialog.getByRole("button", { name: /Continue/ }).click();
  await acceptTerms(page, dialog);

  await expect(dialog.getByLabel("LinkedIn email")).toBeVisible();
  await dialog.getByLabel("LinkedIn email").pressSequentially("grace.hopper.2fa.salesnav@example.com", { delay: 30 });
  await dialog.getByLabel("Password").fill("correct-horse-battery");
  await proof.shot("connect-linkedin-sign-in", { caption: "Email and password, or a session cookie", target: dialog });
  await dialog.getByRole("button", { name: "Sign in" }).click();

  await expect(dialog.getByPlaceholder("123456")).toBeVisible({ timeout: 20_000 });
  await dialog.getByPlaceholder("123456").pressSequentially("123456", { delay: 60 });
  await proof.shot("connect-linkedin-two-factor", { caption: "LinkedIn's two-factor checkpoint, answered in the dialog", target: dialog });
  await dialog.getByRole("button", { name: "Verify" }).click();
  await expect(dialog.getByRole("button", { name: "Done" })).toBeVisible({ timeout: 20_000 });
  await dialog.getByRole("button", { name: "Done" }).click();
  await expect(page.getByRole("row", { name: /Grace Hopper/ })).toBeVisible({ timeout: 20_000 });
});

test("connect WhatsApp by scanning a QR code", async ({ page, proof }) => {
  test.setTimeout(120_000);
  await page.goto("/app/settings/social?connect=whatsapp");
  const dialog = page.getByRole("dialog");
  await expect(dialog.getByRole("button", { name: /Continue/ })).toBeVisible({ timeout: 15_000 });
  await proof.chapter("Connect WhatsApp", "A setup link opens the dialog on its channel; WhatsApp signs in by QR code or pairing code");
  await dialog.getByRole("button", { name: /Continue/ }).click();
  await acceptTerms(page, dialog);
  await expect(dialog.getByRole("tab", { name: /QR code/ })).toBeVisible();
  await proof.shot("connect-whatsapp-methods", { caption: "WhatsApp offers a QR code or a pairing code", target: dialog });
  await dialog.getByRole("button", { name: "Show the QR code" }).click();
  await expect(dialog.getByLabel(/sign-in QR code/)).toBeVisible({ timeout: 20_000 });
  await proof.shot("connect-whatsapp-qr", { caption: "The QR code to scan from WhatsApp on the phone; the dialog moves on once it is scanned", target: dialog });
  await expect(dialog.getByRole("button", { name: "Done" })).toBeVisible({ timeout: 30_000 });
  await proof.shot("connect-whatsapp-done", { caption: "Connected", target: dialog });
  await dialog.getByRole("button", { name: "Done" }).click();
  await expect(page.getByRole("tablist", { name: "Filter by channel" })).toBeVisible({ timeout: 20_000 });
  await proof.dwell(600);
  await proof.shot("social-accounts", { caption: "Accounts across channels, filtered by channel, each with today's usage and billing" });
});

test("open an account to tune its limits, working hours and warm-up", async ({ page, proof }) => {
  await page.goto("/app/settings/social");
  await page.getByRole("row", { name: /Grace Hopper/ }).click();
  const drawer = page.getByRole("complementary", { name: "LinkedIn account" });
  await expect(drawer.getByRole("button", { name: "Overview" })).toBeVisible();
  await proof.chapter("An account", "Today's usage, health, limits and billing for one account");
  await proof.dwell(600);
  await proof.shot("drawer-overview", { caption: "Overview: today's usage per action, licences and InMail credits", target: drawer });
  await drawer.getByRole("button", { name: "Settings" }).click();
  await expect(drawer.getByText("Daily limits")).toBeVisible();
  await proof.shot("drawer-settings", { caption: "Daily and weekly limits held under the account's ceilings, working hours and warm-up", target: drawer });
});

test("search LinkedIn from the lead finder", async ({ page, proof }) => {
  test.setTimeout(90_000);
  await page.goto("/app/leads?source=linkedin");
  await expect(page.getByRole("button", { name: "Heads of growth, 2nd degree" })).toBeVisible({ timeout: 20_000 });
  await proof.chapter("Lead finder", "LinkedIn and Sales Navigator search, opened from the lead finder's source switch");
  await page.getByRole("button", { name: "Heads of growth, 2nd degree" }).click();
  await expect(page.getByRole("list", { name: "Search results" })).toBeVisible({ timeout: 20_000 });
  await proof.dwell(900);
  await proof.shot("finder-results", { caption: "Results with reach signals, Premium and Open Profile badges" });
});

test("add a Social step to a campaign: pick a channel, then an action and its fields", async ({ page, proof }) => {
  test.setTimeout(120_000);
  await page.goto("/app/campaigns");
  const href = await page.getByRole("link", { name: /RevOps outreach - July/ }).first().getAttribute("href");
  await page.goto(`${href}/steps`);
  await page.getByRole("button", { name: "Add a step here" }).last().click();
  const picker = page.getByRole("dialog", { name: "Add a step" });
  await expect(picker).toBeVisible();
  await proof.chapter("Social step", "One step type for every channel, set up the way GraphKit's builder does it");
  await picker.getByRole("textbox").pressSequentially("social", { delay: 60 });
  await expect(picker.getByRole("button", { name: /^Social/ }).first()).toBeVisible();
  await proof.shot("step-picker-social", { caption: "One Social step, plus ready-made paths and social conditions" });
  await picker.getByRole("button", { name: /^Social/ }).first().click();

  const channels = page.getByRole("radiogroup", { name: "Channel" });
  await expect(channels).toBeVisible({ timeout: 15_000 });
  const actions = page.getByRole("radiogroup", { name: "Action" });
  await proof.dwell(600);
  await proof.shot("social-step-linkedin", { caption: "LinkedIn: invite, message, InMail, voice note, visit, follow, react, comment, endorse and withdraw" });
  await actions.getByRole("radio", { name: /Voice note/ }).click();
  await proof.dwell(600);
  await proof.shot("social-step-voice", { caption: "A voice note, recorded or written and read in the account's voice" });
  await channels.getByRole("radio", { name: /WhatsApp/ }).click();
  await proof.dwell(600);
  await proof.shot("social-step-whatsapp", { caption: "WhatsApp offers only what WhatsApp can do, and reaches contacts with a consent basis" });
  await actions.getByRole("radio", { name: /^Message/ }).click();
  await page.getByLabel("Message", { exact: true }).pressSequentially("Hi {{.FirstName}}, following up on my email here. Is now a good time?", { delay: 20 });
  await page.getByRole("button", { name: "Save action" }).click();
  await expect(page.getByText(/on WhatsApp/).first()).toBeVisible({ timeout: 15_000 });
  await proof.dwell(600);
  await proof.shot("social-step-saved", { caption: "Saved: the canvas shows a WhatsApp message step, and the editor says which account it runs from" });
});

test("read social conversations in the unibox", async ({ page, proof }) => {
  await page.goto("/app/unibox/social");
  await expect(page.getByRole("heading", { name: "Social" })).toBeVisible({ timeout: 20_000 });
  await proof.chapter("Unibox", "A Social scope next to the mail folders, across every connected account");
  await proof.dwell(900);
  await proof.shot("unibox-social", { caption: "Conversations from every channel in one scope, filtered by channel" });
  await page.getByRole("button").filter({ hasText: /Thursday/ }).first().click();
  const reply = page.getByRole("textbox", { name: /^Reply on / });
  await expect(reply).toBeVisible({ timeout: 15_000 });
  await reply.pressSequentially("Thursday at 2pm works. I will send an invite.", { delay: 25 });
  await proof.shot("unibox-social-thread", { caption: "The thread, answered from the account that holds it" });
});

test("give a contact a LinkedIn profile and a WhatsApp number with its consent basis", async ({ page, proof }) => {
  await page.goto("/app/contacts");
  await page.getByRole("row", { name: /Beth Chen/ }).click();
  await page.getByRole("button", { name: "Social", exact: true }).click();
  await expect(page.getByRole("heading", { name: "Profiles" })).toBeVisible();
  await proof.chapter("Contact", "A contact's handle on each channel, the consent basis where a channel needs one, and its conversations");
  await page.getByRole("button", { name: /LinkedIn$/ }).click();
  await page.getByRole("textbox", { name: "LinkedIn handle" }).pressSequentially("linkedin.com/in/beth-chen", { delay: 25 });
  await page.getByRole("button", { name: "Save", exact: true }).click();
  await expect(page.getByRole("button", { name: "Edit the LinkedIn handle" })).toBeAttached({ timeout: 15_000 });
  await page.getByRole("button", { name: /WhatsApp$/ }).click();
  await page.getByRole("textbox", { name: "WhatsApp handle" }).pressSequentially("+44 7700 900123", { delay: 25 });
  await proof.dwell(500);
  await proof.shot("contact-social", { caption: "WhatsApp asks for the consent basis before campaigns can reach the number" });
});
