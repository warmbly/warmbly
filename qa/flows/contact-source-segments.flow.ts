import { expect, test } from "../lib/proof.ts";

// Requires ../fixtures/contact-source-segments.sql applied to this worktree's rich-seed database.
const FIXTURES = process.env.QA_SOURCE_SEGMENTS_FIXTURES === "1";
const CONNECTION = "5e650000-0000-4000-8000-000000000001";

test.describe("contact source segments", () => {
  test.skip(!FIXTURES, "Apply qa/fixtures/contact-source-segments.sql to this stack's database and set QA_SOURCE_SEGMENTS_FIXTURES=1.");

  test("pin an Apollo import into a segment created from its name, then edit a saved one", async ({ page, proof }) => {
    // Apollo itself is not reachable from the stack, so its list and preview answers are stubbed.
    await page.route(`**/integrations/apollo/${CONNECTION}/lists`, (route) =>
      route.fulfill({ json: { data: [{ id: "apl-list-us", name: "VP Sales, US SaaS", count: 312 }] } }),
    );
    await page.route(`**/integrations/apollo/${CONNECTION}/import/preview`, (route) =>
      route.fulfill({
        json: {
          total: 312,
          sample: [
            { apollo_id: "a1", name: "Dana Whitfield", email: "dana@northwind.test", email_status: "verified", title: "VP Sales", company: "Northwind" },
            { apollo_id: "a2", name: "Leo Park", email: "leo@brightline.test", email_status: "verified", title: "VP Revenue", company: "Brightline" },
          ],
        },
      }),
    );

    await page.goto(`/app/integrations/apollo/${CONNECTION}`);
    await expect(page.getByText("Heads of sales, EMEA").first()).toBeVisible();
    await proof.chapter("Apollo imports", "Every import can pin its contacts into segments");
    await expect(page.getByText("Heads of sales", { exact: true }).first()).toBeVisible();
    await proof.dwell();
    await proof.shot("apollo-import-card", { caption: "A saved import shows the segments it pins contacts into next to its labels" });

    await page.getByRole("button", { name: "New import" }).first().click();
    const dialog = page.getByRole("dialog", { name: "New import" });
    await dialog.getByRole("button", { name: "Apollo list" }).click();
    await page.getByText("VP Sales, US SaaS").click();
    await dialog.getByRole("button", { name: "Continue" }).click();
    await expect(dialog.getByText("Dana Whitfield")).toBeVisible();
    await dialog.getByRole("button", { name: "Continue" }).click();
    await expect(dialog.getByText("Add to segments (optional)")).toBeVisible();

    await dialog.getByRole("button", { name: "Pick or create a segment…" }).click();
    await expect(dialog.getByText("New segment:")).toBeVisible();
    await proof.dwell();
    await proof.shot("apollo-segment-create", { caption: "The segment picker offers a new segment named after the import" });
    await dialog.getByRole("button", { name: /New segment: VP Sales, US SaaS/ }).click();
    await expect(dialog.getByText("VP Sales, US SaaS", { exact: true }).last()).toBeVisible();
    await proof.dwell();
    await proof.shot("apollo-options", { caption: "The Options step with the new segment picked, above labels and Keep in sync" });
    await dialog.getByRole("button", { name: "Close" }).click();
    await page.getByRole("alertdialog").getByRole("button", { name: /Confirm|Discard|Yes/ }).click();

    await page.getByRole("button", { name: "Actions for Heads of sales, EMEA", exact: true }).click();
    await page.getByRole("menuitem", { name: "Edit" }).click();
    const edit = page.getByRole("dialog", { name: "Edit import" });
    await expect(edit.getByText("Add to segments")).toBeVisible();
    await expect(edit.getByText("Heads of sales", { exact: true })).toBeVisible();
    await proof.dwell();
    await proof.shot("apollo-edit", { caption: "Editing a saved import changes the segments every later sync pins into" });
    await edit.getByRole("button", { name: "Close" }).click();
  });

  test("pin every form submission into a segment", async ({ page, proof }) => {
    await page.goto("/app/forms/33333333-0000-0000-0000-000000000001");
    await expect(page.getByRole("button", { name: "Settings", exact: true })).toBeVisible();
    await proof.chapter("Forms", "Submissions land in segments as well as labels");
    await page.getByRole("button", { name: "Settings", exact: true }).click();
    await expect(page.getByText("Add to segments")).toBeVisible();

    await page.getByRole("button", { name: "Pick or create a segment…" }).click();
    await page.getByPlaceholder("Search or name a new segment…").pressSequentially("Website leads", { delay: 50 });
    await page.getByRole("button", { name: /New segment: Website leads/ }).click();
    await expect(page.getByText("Website leads", { exact: true })).toBeVisible();
    await page.getByRole("button", { name: "Save", exact: true }).click();
    await expect(page.getByText("Saving…")).toBeHidden();
    await proof.dwell();
    await proof.shot("form-settings", { caption: "Form Settings: every submitted contact is pinned into the picked segments" });
  });
});
