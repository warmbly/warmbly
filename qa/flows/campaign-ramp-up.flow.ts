import { expect, test } from "../lib/proof.ts";

// Requires ../fixtures/campaign-estimate.sql applied to this worktree's rich-seed database.
const FIXTURES = process.env.QA_CAMPAIGN_ESTIMATE_FIXTURES === "1";

test.describe("campaign ramp-up", () => {
  test.skip(!FIXTURES, "Apply qa/fixtures/campaign-estimate.sql to this stack's database and set QA_CAMPAIGN_ESTIMATE_FIXTURES=1.");

  test("choose how a new campaign's mailboxes ramp up and see the launch plan follow", async ({ page, proof }) => {
    await page.goto("/app/campaigns");
    await page.getByRole("button", { name: "New campaign" }).first().click();
    const dialog = page.getByRole("dialog", { name: "New campaign" });
    await expect(dialog).toBeVisible();
    await proof.chapter("Ramp-up", "Pick how each mailbox's daily volume grows in a new campaign");

    await dialog.getByText("Agencies", { exact: true }).click();
    await dialog.getByRole("button", { name: /^Continue/ }).click();

    await expect(dialog.getByText("What will they receive?")).toBeVisible();
    await dialog.getByRole("textbox").first().fill("There's room to grow");
    await dialog.getByText("Hi {{.FirstName}}, …", { exact: true }).click({ force: true });
    await page.keyboard.type("A quick idea for your agency.");
    await dialog.getByRole("button", { name: /^Continue/ }).click();

    const ramp = dialog.getByRole("radiogroup", { name: "Ramp-up" });
    await expect(ramp).toBeVisible();
    await ramp.scrollIntoViewIfNeeded();
    await expect(ramp.getByRole("radio", { name: /^As replies come in/ })).toHaveAttribute("aria-checked", "true");
    await proof.dwell();
    await proof.shot("ramp-up-replies", { caption: "The default: mailboxes leaving warmup earn volume from replies" });

    await ramp.getByRole("radio", { name: /^On a schedule/ }).click();
    await expect(dialog.getByText(/then steady/)).toBeVisible();
    await dialog.getByText(/then steady/).scrollIntoViewIfNeeded();
    await proof.dwell();
    await proof.shot("ramp-up-schedule", { caption: "On a schedule: start, daily increase and ceiling, with the day-by-day climb" });

    await ramp.getByRole("radio", { name: /^Full volume/ }).click();
    await expect(dialog.getByText(/fastest and the riskiest/)).toBeVisible();
    await proof.dwell();
    await proof.shot("ramp-up-full", { caption: "Full volume: every mailbox sends up to its own daily limit from day one" });

    await dialog.getByRole("button", { name: /^Continue/ }).click();
    await expect(dialog.getByText("Review and launch")).toBeVisible();
    await expect(dialog.getByText(/from day one/)).toBeVisible();
    await proof.dwell();
    await proof.shot("ramp-up-review-full", { caption: "The launch plan at full volume" });

    await dialog.getByRole("button", { name: /^Senders/ }).click();
    await ramp.getByRole("radio", { name: /^As replies come in/ }).click();
    await dialog.getByRole("button", { name: /^Continue/ }).click();
    await expect(dialog.getByText("Review and launch")).toBeVisible();
    await expect(dialog.getByRole("button", { name: /^Senders .*as leads reply/ })).toBeVisible();
    await proof.dwell();
    await proof.shot("ramp-up-review-replies", { caption: "The same campaign paced by replies" });
  });

  test("change a running campaign's ramp-up in its settings", async ({ page, proof }) => {
    await page.goto("/app/campaigns/dddd0d00-0000-0000-0000-000000000001/preferences");
    const ramp = page.getByRole("radiogroup", { name: "Ramp-up" });
    await expect(ramp).toBeVisible();
    await ramp.scrollIntoViewIfNeeded();
    await proof.chapter("Campaign settings", "The same choice on an existing campaign");
    await proof.shot("settings-ramp-up", { caption: "Ramp-up in campaign settings" });

    await ramp.getByRole("radio", { name: /^On a schedule/ }).click();
    await expect(page.getByText(/then steady/)).toBeVisible();
    await page.getByText(/then steady/).scrollIntoViewIfNeeded();
    await proof.dwell();
    await proof.shot("settings-ramp-up-schedule", { caption: "Switching it to a schedule" });
  });
});
