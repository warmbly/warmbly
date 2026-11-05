import { expect, test } from "../lib/proof.ts";

test("daily charts keep every day of the window, with sends in their own strip", async ({ page, proof }) => {
  const tour = page.getByRole("navigation", { name: "Tour chapters" });
  await page.addLocatorHandler(tour, async () => {
    while (await tour.isVisible()) await page.keyboard.press("Escape");
  });
  await page.goto("/app/campaigns");
  const href = await page.getByRole("link", { name: /RevOps outreach - July/ }).first().getAttribute("href");
  await page.goto(href!);
  const main = page.getByRole("main");
  const daily = main.getByText("Daily performance");
  await daily.evaluate((el) => el.scrollIntoView({ block: "start" }));
  await proof.chapter("Campaign chart", "Engagement lines on their own scale, sends as bars underneath");
  await proof.dwell();
  await proof.shot("campaign-daily-chart", { caption: "Every day of the window, sends in their own strip" });

  const chart = daily.locator("xpath=ancestor::div[contains(@class,'rounded-md')][1]");
  const box = await chart.boundingBox();
  if (box) await page.mouse.move(box.x + box.width * 0.6, box.y + box.height * 0.45);
  await proof.dwell();
  await proof.shot("campaign-daily-hover", { caption: "One crosshair lists the day's sends and each line as a share of them" });

  await page.goto("/app/analytics");
  await expect(page.getByText("Email performance")).toBeVisible();
  await page.getByRole("button", { name: "30d", exact: true }).click();
  await proof.chapter("Workspace analytics", "The same chart over the last 30 days");
  await proof.dwell();
  await proof.shot("analytics-daily-chart", { caption: "Last 30 days, quiet days kept on the axis" });

  await page.goto("/app/deliverability");
  await expect(page.getByText("Over time", { exact: true }).first()).toBeVisible();
  await proof.dwell();
  await proof.shot("deliverability-daily-chart", { caption: "Failure signals above, sends below" });
});
