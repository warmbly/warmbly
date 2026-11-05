import { expect, test } from "../lib/proof.ts";

// Requires ../fixtures/warmup-copy.sql applied to this worktree's rich-seed database.
const FIXTURES = process.env.QA_WARMUP_COPY_FIXTURES === "1";

test.describe("warmup copy", () => {
  test.skip(!FIXTURES, "Apply qa/fixtures/warmup-copy.sql to this stack's database and set QA_WARMUP_COPY_FIXTURES=1.");

  test("see where each running campaign step lands in warmup, by provider, and why one cannot be warmed", async ({ page, proof }) => {
    await page.goto("/app/warmup-copy");
    const intro = page.getByRole("button", { name: /Step 1 - intro/ }).first();
    await expect(intro).toBeVisible();
    await expect(page.getByText(/AI blocks/).first()).toBeVisible();
    await proof.chapter("Warmup copy", "Running campaign steps and templates the warmup pool carries, with where each landed");
    await proof.shot("warmup-copy-overview", { caption: "Settings, running campaign steps with their placement, and templates" });

    await intro.click();
    await expect(page.getByText("Microsoft").first()).toBeVisible();
    await expect(page.getByText("not in the headline").first()).toBeVisible();
    await proof.shot("campaign-step-by-provider", { caption: "One step split by provider; other hosts are listed but kept out of the headline" });
    await proof.dwell();
  });

  test("check a new warmup template before adding it, then open an existing one", async ({ page, proof }) => {
    await page.goto("/app/warmup-copy");
    await expect(page.getByText("Q4 intro copy")).toBeVisible();
    await page.getByRole("button", { name: "New template" }).first().click();
    const dialog = page.getByRole("dialog", { name: "New warmup template" });
    await expect(dialog).toBeVisible();
    await proof.chapter("New template", "The live check says what a save would refuse");

    await dialog.getByPlaceholder("Q3 intro").fill("Hiring follow-up");
    const subject = dialog.getByPlaceholder("Quick question about your onboarding");
    const body = dialog.getByPlaceholder("Write it the way you would send it to a prospect.");
    await subject.pressSequentially("Free cash bonus", { delay: 40 });
    await body.pressSequentially("Hi {{.FirstName}}, act now to claim your guaranteed bonus.", { delay: 25 });
    await expect(dialog.getByText("Would be blocked")).toBeVisible();
    await expect(dialog.getByText(/merge fields/)).toBeVisible();
    await proof.shot("template-check-blocked", { caption: "Spammy copy and a merge field are refused before saving" });

    await subject.fill("Are you still hiring SDRs?");
    await body.fill("Hi there,\n\nSaw the SDR openings on your careers page. Are you still building out the team this quarter?\n\nDev");
    await expect(dialog.getByText("Passes every check a save runs.")).toBeVisible();
    await proof.shot("template-check-passes", { caption: "Clean copy passes every check a save runs" });
    await dialog.getByRole("button", { name: "Add template" }).click();
    await expect(dialog).toBeHidden();
    await expect(page.getByText("Hiring follow-up")).toBeVisible();

    await page.getByRole("button", { name: /Q4 intro copy/ }).first().click();
    const drawer = page.getByRole("dialog", { name: /Q4 intro copy/ });
    await expect(drawer).toBeVisible();
    await expect(drawer.getByText("Google").first()).toBeVisible();
    await proof.shot("template-drawer", { caption: "A template's placement by provider" });
    await proof.dwell();
  });
});
