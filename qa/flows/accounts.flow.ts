import { expect, test } from "../lib/proof.ts";

test("the accounts list reads on a phone as well as on a desktop", async ({ page, proof }) => {
  await page.goto("/app/emails");
  const row = page.getByRole("row", { name: /dev\.outbound@warmbly\.test/ });
  await expect(row).toBeVisible();
  await proof.chapter("Accounts", "The same list on a desktop, then on a phone");
  await proof.shot("accounts-desktop", { caption: "Desktop: every metric has its own column" });

  // The list's columns follow its own width, so the phone layout is a viewport change away.
  await page.setViewportSize({ width: 390, height: 844 });
  await expect(row.getByText("dev.outbound@warmbly.test")).toBeVisible();
  await expect(row.getByText("Health", { exact: true })).toBeVisible();
  await proof.dwell();
  await proof.shot("accounts-phone", { caption: "Phone: address, sender and connection, then labelled warmup, inbox and health readings" });

  await page.getByRole("textbox", { name: /Search by email/ }).pressSequentially("outbound", { delay: 60 });
  await expect(page.getByRole("row", { name: /dev\.send@warmbly\.test/ })).toBeHidden();
  await expect(row).toBeVisible();
  await proof.shot("accounts-phone-search", { caption: "Search and the tag filter share one row" });

  // From the keyboard: on a phone-sized page the dev-only query devtools sit over the last row's actions.
  await row.getByRole("button", { name: "Mailbox actions" }).focus();
  await page.keyboard.press("Enter");
  await expect(page.getByRole("menuitem", { name: /Disconnect mailbox/ })).toBeVisible();
  await proof.shot("accounts-phone-actions", { caption: "Row actions stay reachable at touch size" });
});
