import type { Page } from "@playwright/test";
import { expect, test } from "../lib/proof.ts";

// Requires ../fixtures/member-access.sql applied to this worktree's rich-seed database.
const FIXTURES = process.env.QA_MEMBER_ACCESS_FIXTURES === "1";
const CLIENT = { email: "client@acme-agency.test", password: "password123" };

// Saving access needs a recent sign-in; the dashboard asks for the password when the session is older.
async function confirmIfAsked(page: Page) {
  const dialog = page.getByRole("dialog").filter({ hasText: "Confirm it is you" });
  if (await dialog.isVisible({ timeout: 2000 }).catch(() => false)) {
    await dialog.getByPlaceholder("Password").fill("password123");
    await dialog.getByPlaceholder("Password").press("Enter");
  }
}

test.describe("member access scope", () => {
  test.skip(!FIXTURES, "Apply qa/fixtures/member-access.sql to this stack's database and set QA_MEMBER_ACCESS_FIXTURES=1.");

  test("restrict a teammate to one folder, a campaign and the mailboxes behind them", async ({ page, proof }) => {
    await page.goto("/app/settings/members");
    const row = page.getByRole("row").filter({ hasText: CLIENT.email });
    await expect(row.getByRole("button", { name: /1 folder, 1 mailbox/ })).toBeVisible();
    await proof.chapter("Access scope", "Roles say what a member may do; access says which campaigns and mailboxes it applies to");
    await proof.shot("members-access-column", { caption: "The roster shows each member's access next to their roles" });

    await row.getByRole("button", { name: /1 folder, 1 mailbox/ }).click();
    const dialog = page.getByRole("dialog", { name: "Member access" });
    await expect(dialog.getByText("Campaign folders")).toBeVisible();
    await expect(dialog.getByText("Outbound", { exact: true })).toBeVisible();
    await proof.shot("access-dialog", { caption: "Selected resources: a folder grants whatever campaigns it holds, mailboxes are granted one by one" });

    await dialog.getByText("Choose individual campaigns…").click();
    await page.getByPlaceholder("Search…").pressSequentially("Agency", { delay: 60 });
    await page.getByRole("button", { name: "Agency partnerships" }).click();
    await page.keyboard.press("Escape");
    await expect(dialog.getByText("Senders of the selected campaigns")).toBeVisible();
    await proof.shot("suggested-senders", { caption: "Campaign access never includes a mailbox; the campaigns' senders are suggested to review" });

    await dialog.getByRole("listitem").filter({ hasText: "dev.send@warmbly.test" }).getByRole("button", { name: "Add" }).click();
    await expect(dialog.getByText("dev.send@warmbly.test")).toBeVisible();
    await dialog.getByRole("button", { name: "Save access" }).click();
    await confirmIfAsked(page);
    await expect(row.getByRole("button", { name: /1 folder, 1 campaign, 2 mailboxes/ })).toBeVisible();
    await proof.dwell();
    await proof.shot("access-saved", { caption: "Saved: the member's dashboard, API keys and live updates follow the new scope on their next request" });
  });

  test.describe("as the restricted teammate", () => {
    test.use({ account: CLIENT });

    test("a restricted teammate sees only their campaigns and mailboxes, read-only", async ({ page, proof }) => {
      await page.goto("/app");
      await expect(page).toHaveURL(/\/app\/campaigns/);
      await expect(page.getByRole("link", { name: /RevOps outreach - July/ }).first()).toBeVisible();
      await expect(page.getByRole("link", { name: /Q1 Outreach/ })).toHaveCount(0);
      await expect(page.getByRole("link", { name: "Contacts" })).toHaveCount(0);
      await proof.chapter("Restricted teammate", "Only the granted campaigns and mailboxes, and only the pages that can be narrowed to them");
      await proof.shot("restricted-campaigns", { caption: "The sidebar keeps Inbox, Campaigns and Analytics; the list holds only the granted campaigns" });

      await page.getByRole("link", { name: /RevOps outreach - July/ }).first().click();
      const main = page.getByRole("main");
      await expect(main.getByRole("link", { name: "Steps" })).toBeVisible();
      await expect(main.getByRole("link", { name: "Leads" })).toHaveCount(0);
      await proof.shot("restricted-campaign", { caption: "Campaign analytics and steps, with no tab that reads contacts or settings" });

      await page.goto("/app/unibox");
      const thread = page.getByRole("button", { name: /Quick question about outbound at Fieldstone/ }).first();
      await expect(thread).toBeVisible();
      await thread.click();
      await expect(page.getByRole("heading", { name: /Quick question about outbound at Fieldstone/ })).toBeVisible();
      await expect(page.getByRole("button", { name: "Reply", exact: true })).toHaveCount(0);
      await proof.dwell();
      await proof.shot("restricted-inbox", { caption: "The inbox shows only the granted mailbox's conversations, to read without replying or changing them" });

      await page.goto("/app/contacts");
      await expect(page).toHaveURL(/\/app\/campaigns/);
    });
  });
});
