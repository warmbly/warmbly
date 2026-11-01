import { expect, test } from "../lib/proof.ts";

test.skip(process.env.QA_UNIBOX_DRAFTS_FIXTURES !== "1", "Apply qa/fixtures/unibox-drafts.sql to this stack's rich-seed database and set QA_UNIBOX_DRAFTS_FIXTURES=1.");

test("review an agent reply alongside mailbox drafts", async ({ page, proof }) => {
  await page.goto("/app/unibox");
  await page.getByRole("button", { name: /^Drafts(?: \d+)?$/ }).click();
  await expect(page.getByRole("heading", { name: "Drafts", exact: true })).toBeVisible();
  const agent = page.getByRole("button", { name: /Mira Kovacs.*Quick question about outbound at Lumina Labs/ });
  const mailbox = page.getByRole("button", { name: /Partnership follow-up draft/ });
  await expect(agent).toBeVisible();
  await expect(mailbox).toBeVisible();
  await expect(page.getByText("Agent drafts", { exact: true }).last()).toBeVisible();
  await proof.chapter("Drafts", "Pending agent replies appear first, above drafts saved in the mailbox");
  await proof.shot("drafts-list", { caption: "Agent replies and mailbox drafts in one folder" });

  await agent.click();
  await expect(page.getByText("AI reply draft", { exact: true })).toBeVisible();
  await expect(page.getByRole("button", { name: "Approve & send" })).toBeVisible();
  await expect(page.getByRole("button", { name: "Discard", exact: true })).toBeVisible();
  await proof.dwell();
  await proof.shot("draft-review", { caption: "Opening an agent draft keeps the existing edit, approve and discard controls" });
});
