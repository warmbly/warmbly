import { expect, test } from "../lib/proof.ts";

// Requires ../fixtures/regions-and-timezones.sql applied to this worktree's rich-seed database.
const FIXTURES = process.env.QA_REGIONS_FIXTURES === "1";
const SEGMENT = "/app/contacts/segments/86300000-0000-0000-0000-000000000000";
const CAMPAIGN = "86300000-0000-0000-0000-00000000c001";
// The backend's location sweep places fixture contacts within a pass or two.
const SWEEP = { timeout: 90_000 };

test.describe("regions and timezones", () => {
    test.skip(!FIXTURES, "Apply qa/fixtures/regions-and-timezones.sql to this stack's database and set QA_REGIONS_FIXTURES=1.");

    test("split a segment into one live segment per world region", async ({ page, proof }) => {
        await page.goto(SEGMENT);
        await expect(page.getByRole("heading", { name: "Prospects worldwide" })).toBeVisible();
        await proof.chapter("Split by region", "120 leads imported with country, state, city and timezone columns");

        await page.getByRole("button", { name: /Split by region/ }).click();
        const dialog = page.getByRole("dialog", { name: "Split segment by region" });
        await expect(dialog).toBeVisible();
        await expect(dialog.getByRole("button", { name: /^Create 5 segments/ })).toBeVisible(SWEEP);
        await proof.dwell();
        await proof.shot("split-by-region", { caption: "Each region counted before it is created, plus the leads with no location" });

        await dialog.getByRole("button", { name: /^Create 5 segments/ }).click();
        await expect(dialog).toBeHidden();
        await page.goto("/app/contacts/segments");
        await expect(page.getByText("Prospects worldwide · Asia-Pacific")).toBeVisible();
        await proof.shot("region-segments", { caption: "One live segment per region, ready for its own campaign" });
    });

    test("read a campaign's schedule in each lead's local time and see where its leads are", async ({ page, proof }) => {
        await page.goto(`/app/campaigns/${CAMPAIGN}/schedule`);
        await expect(page.getByText("Where your leads are")).toBeVisible();
        await expect(page.getByText(/Chicago/).first()).toBeVisible(SWEEP);
        await proof.chapter("Whose time", "A weekday 9-5 campaign on New York time");
        await expect(page.getByText(/Lands .* their time/).first()).toBeVisible();
        const card = page.getByRole("region", { name: "Whose time" });
        await card.scrollIntoViewIfNeeded();
        await proof.shot("campaign-clock", {
            caption: "On the campaign's clock, where its next window lands for each region",
            target: card,
            ignore: [page.getByText(/there$/), page.getByText(/^Lands /)],
        });

        await page.getByRole("radio", { name: /Each lead's local time/ }).click();
        await expect(page.getByText(/inside their window right now/)).toBeVisible();
        await expect(page.getByText(/have no location/)).toBeVisible();
        await proof.dwell();
        await proof.shot("lead-clock", {
            caption: "Read in each lead's own day, with the leads nothing places called out",
            target: card,
            ignore: [page.getByText(/there$/), page.getByText(/their window|their time$/)],
        });

        await page.getByRole("button", { name: "Save changes" }).click();
        await expect(page.getByText("Schedule updated.")).toBeVisible();
    });
});
