import { expect, test } from "../lib/proof.ts";

// Runs against the GraphKit simulator (`GRAPHKIT_API_URL=fake`, the `make backend` default).

test.beforeEach(async ({ page }) => {
  // Report the product tour as finished, so a freshly seeded account never covers the page with it.
  await page.route("**/v1/auth/me", async (route) => {
    const response = await route.fetch();
    await route.fulfill({ response, json: { ...await response.json(), product_tour_completed_at: "2026-01-01T00:00:00Z" } });
  });
});

test("open LinkedIn from the sidebar and see the add-on before any account is connected", async ({ page, proof }) => {
  await page.goto("/app/linkedin");
  await expect(page.getByRole("heading", { name: "Add LinkedIn to your outreach" })).toBeVisible();
  await proof.chapter("LinkedIn", "A new page beside Mailboxes for connecting LinkedIn accounts");
  await proof.shot("linkedin-empty", { caption: "Before any account: what LinkedIn adds and linking a GraphKit workspace. This local instance does not bill, so it says seats are not charged" });
});

test("connect a LinkedIn account through the wizard, past a two-factor checkpoint", async ({ page, proof }) => {
  test.setTimeout(120_000);
  await page.goto("/app/linkedin");
  await page.getByRole("button", { name: "Connect a LinkedIn account" }).click();
  const dialog = page.getByRole("dialog");
  await expect(dialog.getByText("One LinkedIn account")).toBeVisible();
  await proof.chapter("Connect", "Price first, then the risk notice, then LinkedIn's own sign-in and its checkpoints");
  await proof.shot("connect-price", { caption: "The price step. A billing instance shows $49, $32 or $19 a month and what is due today; this local one does not bill", target: dialog });
  await dialog.getByRole("button", { name: /Continue/ }).click();

  await expect(dialog.getByText("Automating LinkedIn carries a risk to the account")).toBeVisible();
  await dialog.getByText(/I understand LinkedIn may restrict/).click();
  await proof.shot("connect-ack", { caption: "An explicit acknowledgement before any account is automated", target: dialog });
  await dialog.getByRole("button", { name: /continue/i }).last().click();

  await expect(dialog.getByLabel("LinkedIn email")).toBeVisible();
  await dialog.getByLabel("LinkedIn email").pressSequentially("grace.hopper.2fa.salesnav@example.com", { delay: 30 });
  await dialog.getByLabel("Password").fill("correct-horse-battery");
  await proof.shot("connect-sign-in", { caption: "Sign in with email and password, or a session cookie", target: dialog });
  await dialog.getByRole("button", { name: "Sign in" }).click();

  await expect(dialog.getByText("Enter the code LinkedIn sent")).toBeVisible({ timeout: 20_000 });
  await dialog.getByPlaceholder("123456").pressSequentially("123456", { delay: 60 });
  await proof.shot("connect-two-factor", { caption: "LinkedIn's two-factor checkpoint, answered inside the wizard", target: dialog });
  await dialog.getByRole("button", { name: "Verify" }).click();

  await expect(dialog.getByRole("button", { name: "Done" })).toBeVisible({ timeout: 20_000 });
  await proof.shot("connect-done", { caption: "Connected: Grace Hopper with Sales Navigator", target: dialog });
  await dialog.getByRole("button", { name: "Done" }).click();

  await expect(page.getByRole("row", { name: /Grace Hopper/ })).toBeVisible({ timeout: 20_000 });
  await proof.shot("linkedin-accounts", { caption: "The account list: status, today's usage against limits, and its billing" });
});

test("open the account drawer to tune limits, working hours and warm-up, and see its billing", async ({ page, proof }) => {
  await page.goto("/app/linkedin");
  await page.getByRole("row", { name: /Grace Hopper/ }).click();
  const drawer = page.getByRole("complementary", { name: "LinkedIn account" });
  await expect(drawer.getByRole("button", { name: "Overview" })).toBeVisible();
  await proof.chapter("Account", "Each account has its own pacing, schedule and billing");
  await proof.shot("drawer-overview", { caption: "Overview: today's usage, licences and InMail credits" });
  await drawer.getByRole("button", { name: "Settings" }).click();
  await expect(drawer.getByLabel("Invites per week")).toBeVisible();
  await proof.shot("drawer-settings", { caption: "Daily and weekly limits capped at the licence's ceiling, working hours and warm-up" });
  await drawer.getByRole("button", { name: "Billing" }).click();
  await proof.dwell(600);
  await proof.shot("drawer-billing", { caption: "Billing: a seat on an instance that does not bill says it is not charged" });
});

test("search LinkedIn from the lead finder with Warmbly's own filters", async ({ page, proof }) => {
  test.setTimeout(90_000);
  await page.goto("/app/leads?source=linkedin");
  await expect(page.getByRole("heading", { name: "Lead finder" })).toBeVisible();
  await expect(page.getByText("Or start from")).toBeVisible({ timeout: 20_000 });
  await proof.chapter("Lead finder", "Sales Navigator and LinkedIn search, annotated with what Warmbly already knows");
  await proof.shot("finder-start", { caption: "Start from a pasted search, an example, or the filters" });
  await page.getByRole("button", { name: "Heads of growth, 2nd degree" }).click();
  await expect(page.getByRole("list", { name: "Search results" })).toBeVisible({ timeout: 20_000 });
  await proof.dwell(800);
  await proof.shot("finder-results", { caption: "Results with reach signals, Premium and Open Profile badges" });
  await page.getByRole("list", { name: "Search results" }).getByRole("listitem").first().click();
  await expect(page.getByRole("button", { name: "Close the preview" })).toBeVisible();
  await proof.shot("finder-preview", { caption: "The person preview: experience, reach and what your accounts already did" });
});

test("add LinkedIn steps to a campaign: invite, then message or email depending on the answer", async ({ page, proof }) => {
  test.setTimeout(90_000);
  await page.goto("/app/campaigns");
  const href = await page.getByRole("link", { name: /RevOps outreach - July/ }).first().getAttribute("href");
  await page.goto(`${href}/steps`);
  await expect(page.getByText("Contact enters the campaign")).toBeVisible();
  await proof.chapter("Sequences", "LinkedIn actions and LinkedIn branches sit in the same flow as email steps");

  const picker = page.getByRole("dialog", { name: "Add a step" });
  await page.getByRole("button", { name: "Add a step here" }).last().click();
  await expect(picker).toBeVisible();
  await picker.getByRole("textbox").pressSequentially("linkedin", { delay: 60 });
  await expect(picker.getByRole("button", { name: /^Send invite/ })).toBeVisible();
  await proof.shot("step-picker-linkedin", { caption: "Invite, message, InMail, visit, follow, like and withdraw, plus a ready-made invite path" });
  await picker.getByRole("button", { name: /^Invite, then message or email/ }).click();
  await expect(page.getByRole("button", { name: /Accepted the LinkedIn invite within/ })).toBeVisible({ timeout: 15_000 });
  await proof.dwell(800);
  await proof.shot("linkedin-path", { caption: "Invite, wait for the answer, then a LinkedIn message if accepted or an email if not" });
  const note = page.getByRole("textbox", { name: "Send invite" });
  await expect(note).toBeVisible();
  await note.pressSequentially("Hi {{.FirstName}}, saw your post on outbound and would like to connect.", { delay: 30 });
  await proof.shot("linkedin-invite-note", { caption: "The invite's note takes variables and is checked against LinkedIn's 300-character cap" });
});

test("read and answer LinkedIn conversations in the unibox", async ({ page, proof }) => {
  await page.goto("/app/unibox/linkedin");
  await expect(page.getByRole("heading", { name: "LinkedIn" })).toBeVisible();
  const first = page.getByRole("listitem").getByRole("button").filter({ hasText: /Thursday/ }).first();
  await expect(first).toBeVisible({ timeout: 20_000 });
  await proof.chapter("Unibox", "LinkedIn conversations get their own scope next to the mail folders");
  await proof.shot("unibox-linkedin", { caption: "A LinkedIn scope in the unibox, across every connected account" });
  await first.click();
  const reply = page.getByRole("textbox", { name: "Reply on LinkedIn" });
  await expect(reply).toBeVisible();
  await reply.pressSequentially("Thursday at 3pm works. I'll send an invite.", { delay: 35 });
  await proof.shot("unibox-linkedin-thread", { caption: "The thread, answered from the account that holds it" });
  await page.getByRole("button", { name: "Send", exact: true }).click();
  await expect(page.getByText("Thursday at 3pm works. I'll send an invite.").first()).toBeVisible({ timeout: 15_000 });
  await proof.dwell(600);
  await proof.shot("unibox-linkedin-sent", { caption: "Handed to GraphKit and shown at once, marked Sending until GraphKit confirms the delivery" });
});
