import { expect, test } from "../lib/proof.ts";

// Needs a stack with no GraphKit key (GRAPHKIT_API_URL and GRAPHKIT_API_KEY unset).
test("the store is locked until an operator adds a GraphKit API key", async ({ page, proof }) => {
  test.skip(process.env.QA_STORE_LOCKED !== "1", "Run against a stack with no GraphKit key and set QA_STORE_LOCKED=1.");
  await page.goto("/app/emails");
  await page.getByRole("button", { name: "Add account" }).first().click();
  await expect(page.getByText("Set up").first()).toBeVisible();
  await proof.chapter("Locked", "Without a GraphKit API key the store shows, locked, instead of disappearing");
  await proof.shot("locked-menu", { caption: "Buy new mailboxes stays in the menu, locked, with a Set up badge" });
  await page.keyboard.press("Escape");

  await page.goto("/app/emails/buy");
  await expect(page.getByText(/add a GraphKit API key/i).first()).toBeVisible({ timeout: 20_000 });
  await proof.dwell();
  await proof.shot("locked-admin", { caption: "An instance admin sees what it does and the three steps to add the key" });

  // How a member who can't configure the instance sees the same page.
  await page.route("**/v1/store/status", async (route) => {
    const response = await route.fetch();
    await route.fulfill({ response, json: { ...await response.json(), can_configure: false, configure_url: null } });
  });
  await page.reload();
  await expect(page.getByText("Ask your instance operator to add a GraphKit API key")).toBeVisible({ timeout: 20_000 });
  await proof.dwell();
  await proof.shot("locked-member", { caption: "A member is asked to send their operator a ready-made note" });
});
