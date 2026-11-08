import { expect, test } from "../lib/proof.ts";

test("the accounts strip shows today against the last two weeks", async ({ page, proof }) => {
  // Report the product tour as finished, so a freshly seeded account never covers the page with it.
  await page.route("**/v1/auth/me", async (route) => {
    const response = await route.fetch();
    await route.fulfill({ response, json: { ...await response.json(), product_tour_completed_at: "2026-01-01T00:00:00Z" } });
  });

  await page.goto("/app/emails");
  // A freshly seeded stack takes a while to answer its first page.
  await expect(page.getByRole("row", { name: /dev\.outbound@warmbly\.test/ })).toBeVisible({ timeout: 30_000 });
  const sent = page.getByRole("slider", { name: "Sent today over time" });
  await expect(sent).toBeVisible();
  await expect(page.getByText("Checking…")).toHaveCount(0);
  await proof.chapter("Accounts", "Today's sending, warmup, placement and replies, each with its trend");
  await proof.shot("accounts-trend-light", { caption: "Light theme" });

  const card = sent.locator("xpath=..");
  const box = await sent.boundingBox();
  if (!box) throw new Error("the Sent today chart has no box");
  await page.mouse.move(box.x + box.width * 0.45, box.y + box.height / 2);
  await expect(card).toContainText(/[A-Z][a-z]{2} \d{1,2}/);
  await expect(card).not.toContainText("/ 20");
  await proof.shot("accounts-trend-hover", { caption: "Hovering a day reads that day" });
  await page.mouse.move(5, 5);
  await expect(card).toContainText("/ 20");

  await page.evaluate(() => localStorage.setItem("theme", "dark"));
  await page.reload();
  await expect(page.getByRole("row", { name: /dev\.outbound@warmbly\.test/ })).toBeVisible();
  await expect(page.getByRole("slider", { name: "Sent today over time" })).toBeVisible();
  await expect(page.getByText("Checking…")).toHaveCount(0);
  await proof.shot("accounts-trend-dark", { caption: "Dark theme" });
  await page.evaluate(() => localStorage.setItem("theme", "light"));
});
