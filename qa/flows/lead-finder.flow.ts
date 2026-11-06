import { expect, test } from "../lib/proof.ts";

// Requires ../fixtures/lead-finder.sql applied to this worktree's rich-seed database.
const FIXTURES = process.env.QA_LEAD_FINDER_FIXTURES === "1";

test.describe("lead finder", () => {
  test.skip(!FIXTURES, "Apply qa/fixtures/lead-finder.sql to this stack's database and set QA_LEAD_FINDER_FIXTURES=1.");

  test("open a saved search on the map, restyle the pins and read one business's details", async ({ page, proof }) => {
    await page.goto("/app/leads");
    await expect(page.getByRole("heading", { name: "Lead finder" })).toBeVisible();
    await proof.chapter("Lead finder", "Local businesses on a map, enriched from their own websites");

    await page.getByText("Dentists in downtown Austin").click();
    await expect(page.getByText("Bright Smiles Dental").first()).toBeVisible();
    await expect(page.getByText(/36 on map/)).toBeVisible();
    await proof.dwell();
    await proof.shot("saved-search", { caption: "A saved search: its area on the map, pins colored by how each business can be reached, the list sorted best first" });

    await page.getByRole("button", { name: "List", exact: true }).click();
    await expect(page.getByText("Finding people and emails")).toBeVisible();
    await expect(page.getByText("Reading the website").first()).toBeVisible();
    await proof.dwell();
    await proof.shot("list-progress", { caption: "A website run going in the background: progress above the list, and each business being read marked in its row" });
    await page.getByRole("button", { name: "Split", exact: true }).click();

    await page.getByRole("button", { name: "Map layers" }).click();
    await page.getByRole("menuitem", { name: "Score" }).click();
    await expect(page.getByText("100", { exact: true }).last()).toBeVisible();
    await page.getByRole("button", { name: "Map layers" }).click();
    await page.getByRole("menuitem", { name: "Heatmap" }).click();
    await expect(page.getByText("Brighter where leads are denser and better")).toBeVisible();
    await proof.dwell();
    await proof.shot("heatmap", { caption: "The same leads as a heatmap weighted by score" });
    await page.getByRole("button", { name: "Map layers" }).click();
    await page.getByRole("menuitem", { name: "Pins" }).click();

    await page.getByText("Bright Smiles Dental").first().click();
    const drawer = page.getByRole("complementary", { name: "Lead details" });
    await expect(drawer.getByRole("heading", { name: "Bright Smiles Dental" })).toBeVisible();
    await expect(drawer.getByText("Primary")).toBeVisible();
    await proof.dwell();
    await proof.shot("lead-details", { caption: "A lead's addresses with their verification, phone, address, rating, socials and hours" });

    await drawer.getByRole("button", { name: "Website" }).click();
    await expect(drawer.getByText(/Built with/)).toBeVisible();
    await proof.dwell();
    await proof.shot("lead-website", { caption: "What reading the website found: platform, HTTPS and the gaps worth pitching" });
    await drawer.getByRole("button", { name: "Close" }).click();
  });

  test("filter to verified, well rated leads, select them and export or add them to contacts", async ({ page, proof }) => {
    await page.goto("/app/leads");
    await page.getByText("Dentists in downtown Austin").click();
    await expect(page.getByText("Bright Smiles Dental").first()).toBeVisible();
    await proof.chapter("Refine and act", "Filter the list, then work the whole selection at once");

    await page.getByRole("button", { name: "Has email" }).click();
    await page.getByRole("button", { name: "4+", exact: true }).click();
    await expect(page.getByText(/\d+ leads?$/).first()).toBeVisible();
    await proof.dwell();
    await proof.shot("filtered", { caption: "Only leads with an email and four stars or more, on the map and in the list" });

    await page.getByRole("button", { name: /^Filters/ }).click();
    await expect(page.getByRole("dialog", { name: "Filters" }).getByText("Reachability")).toBeVisible();
    await proof.dwell();
    await proof.shot("filter-panel", { caption: "Every other filter one click away above the results, no switching away from the search" });
    await page.keyboard.press("Escape");

    await page.getByRole("checkbox", { name: "Select every lead on screen" }).check();
    await expect(page.getByText(/selected$/)).toBeVisible();
    await proof.shot("selection-bar", { caption: "Find emails, add to contacts, export, tag, set a status or delete, for the whole selection" });

    await page.getByRole("button", { name: "Export" }).click();
    const exportDialog = page.getByRole("dialog", { name: /leads?$/ });
    await expect(exportDialog.getByText("Columns", { exact: true })).toBeVisible();
    await proof.dwell();
    await proof.shot("export-dialog", { caption: "Export as CSV, Excel or JSON with the columns you pick" });
    await exportDialog.getByRole("button", { name: "Cancel" }).click();

    await page.getByRole("button", { name: "Add to contacts" }).click();
    const addDialog = page.getByRole("dialog", { name: /leads?$/ });
    await expect(addDialog.getByText("Only verified addresses")).toBeVisible();
    await expect(addDialog.getByText("Segments", { exact: true })).toBeVisible();
    await expect(addDialog.getByText("Contacts you already have")).toBeVisible();
    await proof.dwell();
    await proof.shot("add-to-contacts", { caption: "Add the selection to contacts, with segments, labels and campaigns in the same step, and what to do with contacts you already have" });
    await addDialog.getByRole("button", { name: "Cancel" }).click();
  });

  test("see who runs a business, ask for someone's email and pick where businesses come from", async ({ page, proof }) => {
    await page.goto("/app/leads");
    await page.getByText("Dentists in downtown Austin").click();
    await expect(page.getByText("Bright Smiles Dental").first()).toBeVisible();
    await proof.chapter("People", "Who runs each business, and how to reach them");

    await page.getByText("Bright Smiles Dental").first().click();
    const drawer = page.getByRole("complementary", { name: "Lead details" });
    await expect(drawer.getByText("Decision makers")).toBeVisible();
    await drawer.getByRole("button", { name: /^People/ }).click();
    await expect(drawer.getByText("Owner & Lead Dentist")).toBeVisible();
    await expect(drawer.getByText("Confirmed").first()).toBeVisible();
    await proof.dwell();
    await proof.shot("lead-people", { caption: "The people the website names, most senior first, with each address and how far to trust it" });

    await drawer.getByRole("textbox", { name: "First name" }).last().pressSequentially("Ava", { delay: 60 });
    await drawer.getByRole("textbox", { name: "Last name" }).last().pressSequentially("Romano", { delay: 60 });
    await drawer.getByRole("textbox", { name: "Title (optional)" }).pressSequentially("Office manager", { delay: 40 });
    await expect(drawer.getByRole("button", { name: "Find email" })).toBeEnabled();
    await proof.dwell();
    await proof.shot("find-someone", { caption: "Name anyone at the business and their address is worked out from the domain and checked" });
    await drawer.getByRole("button", { name: "Close" }).click();

    await page.getByRole("button", { name: /OpenStreetMap/ }).click();
    await expect(page.getByRole("menuitem", { name: /Google Places/ })).toBeVisible();
    await proof.dwell();
    await proof.shot("source-picker", { caption: "Every source with its logo and what it costs; the keyed ones link to Integrations" });
    await page.keyboard.press("Escape");
  });

  test("pick a category, go to a city and draw the exact area to search", async ({ page, proof }) => {
    await page.goto("/app/leads");
    await expect(page.getByRole("heading", { name: "Lead finder" })).toBeVisible();
    await proof.chapter("A new search", "What, where, and the exact area");

    await page.getByRole("button", { name: "Restaurants" }).click();
    await page.getByRole("button", { name: /^All \d+ categories/ }).click();
    await expect(page.getByPlaceholder("Search categories…")).toBeVisible();
    await page.getByPlaceholder("Search categories…").pressSequentially("vet", { delay: 60 });
    await page.getByRole("menuitem", { name: "Veterinarians" }).click();

    await page.getByRole("textbox", { name: "Location" }).pressSequentially("Austin, Tex", { delay: 60 });
    await page.getByRole("option", { name: /Austin/ }).first().click();
    await expect(page.getByText("The visible map, or draw an area")).toBeVisible();

    await page.getByRole("button", { name: "Draw a circle to search" }).click();
    await expect(page.getByText("Drag from the center outwards")).toBeVisible();
    const map = page.locator(".maplibregl-canvas");
    const box = await map.boundingBox();
    if (!box) throw new Error("map canvas has no size");
    const cx = box.x + box.width / 2;
    const cy = box.y + box.height / 2;
    await page.mouse.move(cx, cy);
    await page.mouse.down();
    await page.mouse.move(cx + 140, cy + 40, { steps: 12 });
    await page.mouse.up();
    await expect(page.getByText(/^Circle, /)).toBeVisible();
    await proof.dwell();
    await proof.shot("draw-area", { caption: "A drawn circle becomes the search area; the source and its cost are picked below" });
  });
});
