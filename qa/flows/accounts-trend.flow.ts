import { expect, test } from "../lib/proof.ts";

test("the accounts strip shows today against the last two weeks", async ({ page, proof }) => {
  await page.goto("/app/emails");
  // The product tour opens a few seconds in and hides the page behind its dialog.
  const skipTour = page.getByRole("button", { name: /Skip tour/ });
  await skipTour.waitFor({ timeout: 6000 }).then(async () => {
    await skipTour.click();
    await page.getByRole("button", { name: "Done" }).click();
  }, () => undefined);
  await expect(skipTour).toBeHidden();
  await expect(page.getByRole("row", { name: /dev\.outbound@warmbly\.test/ })).toBeVisible();
  await expect(page.getByText("Checking…")).toHaveCount(0);
  await proof.chapter("Accounts", "Today's sending, warmup, placement and replies, each with its trend");
  await proof.shot("accounts-trend-light", { caption: "Light theme" });

  const warmup = page.getByText("Sent today").locator("xpath=../..");
  const chart = warmup.locator("div.cursor-crosshair");
  const box = await chart.boundingBox();
  if (box) await page.mouse.move(box.x + box.width * 0.45, box.y + box.height / 2);
  await proof.dwell(400);
  await proof.shot("accounts-trend-hover", { caption: "Hovering a day reads that day" });
  await page.mouse.move(5, 5);

  await page.evaluate(() => localStorage.setItem("theme", "dark"));
  await page.reload();
  await expect(page.getByRole("row", { name: /dev\.outbound@warmbly\.test/ })).toBeVisible();
  await expect(page.getByText("Checking…")).toHaveCount(0);
  await proof.shot("accounts-trend-dark", { caption: "Dark theme" });
  await page.evaluate(() => localStorage.setItem("theme", "light"));
});
