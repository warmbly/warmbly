import { env } from "../lib/env.ts";
import { expect, test } from "../lib/proof.ts";

test("the dashboard boots into its own frame drawn in outline", async ({ page, proof }) => {
  // Holds every API call so the boot state stays on screen until the shot is taken.
  let release = () => {};
  const held = new Promise<void>((resolve) => (release = resolve));
  await page.route(`${env.apiURL}/**`, async (route) => {
    await held;
    await route.continue();
  });

  await page.goto("/app/emails");
  await expect(page.getByRole("status", { name: "Loading" }).first()).toBeVisible();
  await proof.chapter("Loading", "Header, sidebar and a list page in outline, lit by one shared sweep");
  await proof.dwell(3200);
  await proof.shot("loading-frame", { caption: "The boot state while the session loads" });

  release();
  await expect(page.getByRole("row", { name: /dev\.outbound@warmbly\.test/ })).toBeVisible();
  await proof.chapter("Loaded", "The real page fills the same frame");
  await proof.shot("mailboxes-loaded", { caption: "Mailboxes once the session has loaded" });
});
