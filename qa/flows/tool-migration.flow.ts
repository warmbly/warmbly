import { env } from "../lib/env.ts";
import { expect, test } from "../lib/proof.ts";

// Settings > Migrate from another tool, run end to end against the sandbox's mock Smartlead. Needs the mock
// (QA_VENDOR_MOCK=1 pnpm stack up lite); any API key works there.

test("a workspace migrates its Smartlead account: key, preview, run and the drafts it created", async ({ page, proof }) => {
  test.skip(process.env.QA_VENDOR_MOCK !== "1", "Start the stack with QA_VENDOR_MOCK=1 so the mock Smartlead answers.");
  // A fresh seed opens the product tour over the page; mark it done the way skipping it does.
  await page.goto("/app/settings/migrate");
  await page.evaluate(async (api) => {
    const token = localStorage.getItem("access_token") ?? "";
    await fetch(`${api}/v1/auth/me/product-tour`, { method: "PUT", headers: { Authorization: `Bearer ${token}` } });
  }, env.apiURL);
  await page.reload();
  await expect(page.getByRole("heading", { name: /Migrate from another tool/ }).first()).toBeVisible();
  await expect(page.getByRole("navigation", { name: "Tour chapters" })).toBeHidden();
  await proof.chapter("Migrate from another tool", "Bring a Smartlead account over: blocklist, mailboxes, campaigns and leads");
  await proof.shot("migrate-settings", { caption: "Settings > Migrate from another tool" });

  await page.getByRole("button", { name: "Start a migration" }).click();
  const wizard = page.getByRole("dialog", { name: /Migrate from another tool/ });
  await expect(wizard.getByText("Which tool are you moving from?")).toBeVisible();
  await expect(wizard.getByRole("button", { name: /Instantly / })).toBeVisible();
  await proof.shot("migrate-pick-tool", { caption: "Ten tools, each with what its API can bring" });
  // Picking a tool goes straight to its key.
  await wizard.getByRole("button", { name: /Smartlead Blocklist/ }).click();
  await expect(wizard.getByRole("heading", { name: "Connect Smartlead" })).toBeVisible();

  await wizard.getByLabel("API key").pressSequentially("sandbox-smartlead-key", { delay: 30 });
  await proof.shot("migrate-connect", { caption: "Paste the key; it is kept encrypted only while the migration needs it" });
  await wizard.getByRole("button", { name: /Connect and scan/ }).click();

  // The scan reads every client and campaign, then the preview lists them.
  await expect(wizard.getByText("Leads part way through a sequence").first()).toBeVisible({ timeout: 60_000 });
  await expect(wizard.getByText("Northwind Studio").first()).toBeVisible();
  await proof.dwell(900);
  await proof.shot("migrate-preview", { caption: "Preview: what each Smartlead client holds, the campaigns to bring and the choices" });

  await wizard.getByRole("button", { name: /Start migration/ }).click();
  await expect(wizard.getByText("Draft campaigns to review").first()).toBeVisible({ timeout: 120_000 });
  await expect(wizard.getByText("Mailboxes to connect again").first()).toBeVisible();
  await proof.dwell(1200);
  await proof.shot("migrate-result", { caption: "Done: draft campaigns to review, mailboxes to connect again, and what did not carry over" });

  await wizard.getByRole("button", { name: "Done" }).click();
  await expect(page.getByText("Smartlead").first()).toBeVisible();
  await proof.shot("migrate-history", { caption: "The migration in the list, with what it brought over" });
});
