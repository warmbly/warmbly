import { expect, test } from "../lib/proof.ts";

// Requires ../fixtures/agency.sql, then ../fixtures/agency-client-view.sql, applied to this worktree's rich-seed database.
const FIXTURES = process.env.QA_AGENCY_FIXTURES === "1";
const CLIENT = "Dev's Organization";

test.describe("agency client view", () => {
  test.skip(!FIXTURES, "Apply qa/fixtures/agency.sql and qa/fixtures/agency-client-view.sql, then set QA_AGENCY_FIXTURES=1.");

  test("see a client workspace the way its members do", async ({ page, proof }) => {
    await page.goto("/app/agency");
    await expect(page.getByText("Pool this period").first()).toBeVisible();
    await proof.chapter("Agency home", "Pages settle in, figures fill from outlines, the sidebar highlight follows");
    await proof.shot("agency-home", { caption: "Agency home, loaded" });

    await page.getByRole("button", { name: `Actions for ${CLIENT}`, exact: true }).click();
    await expect(page.getByText("View as client", { exact: true })).toBeVisible();
    await proof.shot("client-row-menu", { caption: "A client's row menu: Open workspace, and View as client" });
    await page.keyboard.press("Escape");

    await page.goto("/app/agency/branding");
    await expect(page.getByText("Portal domain").first()).toBeVisible();
    // The preview's Dashboard is the real one: it opens a client's workspace as its members see it.
    await page.getByRole("button", { name: "Dashboard", exact: true }).click();
    await expect(page.getByRole("menu").getByText(CLIENT)).toBeVisible();
    await proof.chapter("Branding", "No drawn dashboard: the real one, opened as a client, with the saved brand");
    await proof.shot("branding-view-as-client", { caption: "The preview's Dashboard: save, then open a client's real dashboard as its members see it" });
    await page.getByRole("menu").getByText(CLIENT).click();
    const bar = page.getByRole("status").filter({ hasText: "as a client" });
    await expect(bar).toBeVisible();
    await expect(page).toHaveURL(/\/app\/campaigns/);
    await proof.chapter("Client view", "The client's workspace in the client's brand, cut down to one role");
    await proof.dwell(1200);
    await proof.shot("client-view", { caption: "Viewing the client's workspace as its members do, opened on the role most of them hold, on a page that role can use" });

    await bar.getByRole("button", { name: "Client team" }).click();
    await expect(page.getByRole("menu").getByText("Client manager")).toBeVisible();
    await proof.shot("client-view-roles", { caption: "Switch the role the workspace is drawn for" });
    await page.getByRole("menu").getByText("Client manager").click();
    await expect(bar.getByRole("button", { name: "Client manager" })).toBeVisible();
    await proof.dwell(800);
    await proof.shot("client-view-manager", { caption: "The same workspace drawn for the Client manager role" });

    await bar.getByRole("button", { name: "Branding" }).click();
    await expect(bar).toBeHidden();
    await expect(page).toHaveURL(/\/app\/agency\/branding/);
    await expect(page.getByText("Portal domain").first()).toBeVisible();
    await proof.dwell(800);
    await proof.shot("client-view-back-to-branding", { caption: "The way back returns to Branding, where the view was opened" });

    await page.goto("/app/agency");
    await page.getByRole("button", { name: `Actions for ${CLIENT}`, exact: true }).click();
    await page.getByText("View as client", { exact: true }).click();
    await expect(bar).toBeVisible();
    await bar.getByRole("button", { name: "Exit client view" }).click();
    await expect(bar).toBeHidden();
    await proof.dwell(800);
    await proof.shot("client-view-exit", { caption: "Out of the client view: back to the staff view, with the way back to the agency" });
  });
});
