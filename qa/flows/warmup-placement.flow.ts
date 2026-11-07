import { expect, test } from "../lib/proof.ts";

// Requires ../fixtures/warmup-placement.sql applied to this worktree's rich-seed database.
const FIXTURES = process.env.QA_WARMUP_PLACEMENT_FIXTURES === "1";

test.describe("warmup placement", () => {
  test.skip(!FIXTURES, "Apply qa/fixtures/warmup-placement.sql to this stack's database and set QA_WARMUP_PLACEMENT_FIXTURES=1.");

  test("see where a campaign step landed when warmup carried it, per A/B arm and provider", async ({ page, proof }) => {
    await page.goto("/app/campaigns");
    const href = await page.getByRole("link", { name: /RevOps outreach - July/ }).first().getAttribute("href");
    await page.goto(`${href}/steps`);
    await expect(page.getByText("Contact enters the campaign")).toBeVisible();
    await page.getByText("Step 1 - intro").first().click();
    const strip = page.getByText("Warmup placement");
    await expect(strip).toBeVisible();
    await strip.scrollIntoViewIfNeeded();
    await expect(page.getByText("Shorter ask").first()).toBeVisible();
    await proof.chapter("Warmup placement", "Warmup opens some threads with a running step's copy on its own; the step shows where it landed");
    await proof.shot("step-warmup-placement", { caption: "Each arm's inbox, tabs and spam at business inboxes on Google Workspace and Microsoft 365 over the last 30 days" });
    await proof.dwell();
  });
});
