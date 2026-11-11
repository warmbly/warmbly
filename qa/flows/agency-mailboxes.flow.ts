import type { Page } from "@playwright/test";

import { env } from "../lib/env.ts";
import { expect, test } from "../lib/proof.ts";

// Agency > Mailboxes: every client's mailboxes on one list, a vendor account the agency connects once, and
// that account opened in a client's own import. Needs fixtures/admin-agencies.sql and the mock vendors; see the README.

// Connecting a vendor account needs a recent sign-in; the dashboard asks for the password when the session is older.
// Either the account is connected or the prompt opens, so whichever comes first is waited for.
async function confirmIfAsked(page: Page) {
  const dialog = page.getByRole("alertdialog").filter({ hasText: "Confirm it is you" });
  const asked = await Promise.race([
    dialog.waitFor({ state: "visible" }).then(() => true),
    page.getByText("Not used for a client yet").waitFor({ state: "visible" }).then(() => false),
  ]);
  if (asked) {
    await dialog.getByPlaceholder("Password").fill(env.password);
    await dialog.getByPlaceholder("Password").press("Enter");
  }
}

test("an agency sees every client's mailboxes and hands a vendor account to a client", async ({ page, proof }) => {
  test.skip(process.env.QA_ADMIN_AGENCY_FIXTURES !== "1", "Apply fixtures/admin-agencies.sql, start the stack with QA_VENDOR_MOCK=1, and set QA_ADMIN_AGENCY_FIXTURES=1.");
  await page.goto("/app/agency/mailboxes");
  await expect(page.getByText("Inbox vendor accounts")).toBeVisible();
  await expect(page.getByRole("button", { name: /Open in|@/ }).first()).toBeVisible();
  await proof.dwell(900);
  await proof.shot("agency-mailboxes", { caption: "Agency > Mailboxes: every client's mailboxes with their state, and the vendor accounts the agency holds" });

  // The ways to add mailboxes for a client, by the agency or by the client itself.
  await page.getByRole("button", { name: "Add mailboxes" }).first().click();
  const add = page.getByRole("dialog", { name: /Add mailboxes/ });
  await expect(add.getByText("Let the client do it")).toBeVisible();
  await proof.dwell(900);
  await proof.shot("agency-add-mailboxes", { caption: "Add mailboxes: from a vendor, a file, one at a time, or a link the client uses itself" });
  await add.getByRole("button", { name: "Cancel" }).click();

  // The vendor account is connected once, at the agency.
  await page.getByRole("button", { name: "Connect a vendor" }).click();
  const connect = page.getByRole("dialog", { name: "Connect a vendor account" });
  await connect.getByRole("radio", { name: "Mailforge" }).click();
  await connect.getByLabel("API key").fill("qa-agency-key");
  await connect.getByLabel("Name, for your team").fill("Agency Mailforge");
  await proof.dwell(700);
  await proof.shot("agency-vendor-connect", { caption: "A vendor account connected once for the whole agency" });
  await connect.getByRole("button", { name: "Connect", exact: true }).click();
  await confirmIfAsked(page);
  await expect(page.getByText("Not used for a client yet")).toBeVisible();

  // Handing it to a client opens that client's own import on the account, with no key to paste.
  await page.getByRole("button", { name: "Add to a client" }).click();
  await page.getByRole("dialog", { name: /Add mailboxes from Agency Mailforge/ }).getByRole("button", { name: /Dev's Organization/ }).click();
  await page.waitForURL(/\/app\/emails/);
  await expect(page.getByText("Agency Mailforge")).toBeVisible();
  // Marked as the agency's, so the client's own team knows whose key it is.
  await expect(page.getByTitle("Your agency holds this account for its clients. Only the agency's staff can import from it.")).toBeVisible();
  await proof.dwell(900);
  await proof.shot("agency-vendor-in-client", { caption: "Inside the client's workspace: the agency's account is there to import from" });

  // Its mailboxes are listed with the agency's key, and the ones picked connect in this client.
  await page.getByRole("button", { name: "Continue" }).click();
  await expect(page.getByText(/@sunrisehq\.test/).first()).toBeVisible();
  await proof.dwell(900);
  await proof.shot("agency-vendor-mailboxes", { caption: "The vendor's mailboxes, ready to pick for this client" });
});
