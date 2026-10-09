import { expect, test } from "../lib/proof.ts";

test("read a campaign's positive replies, interested leads and reply quality", async ({ page, proof }) => {
  await page.goto("/app/campaigns");
  const href = await page.getByRole("link", { name: /RevOps outreach - July/ }).first().getAttribute("href");
  await page.goto(href!);
  const main = page.getByRole("main");
  await expect(main.getByText(/^\d+ interested$/)).toBeVisible();
  await proof.chapter("Campaign analytics", "Positive replies sit next to the reply rate, counted on the same sends");
  await proof.shot("campaign-positive-rate", { caption: "Positive rate in the headline strip, with the interested leads behind it" });

  await main.getByText("Reply quality").filter({ visible: true }).evaluate((el) => el.scrollIntoView({ block: "center" }));
  await expect(main.getByText("Out of office").filter({ visible: true })).toBeVisible();
  await proof.dwell();
  await proof.shot("campaign-reply-quality", {
    caption: "Totals gain positive replies and interested leads; reply quality splits the replies, automated answers listed apart",
  });

  await main.getByText("Daily performance").evaluate((el) => el.scrollIntoView({ block: "start" }));
  for (const metric of ["Sent", "Opens", "Clicks"]) {
    await main.getByRole("button", { name: metric, exact: true }).click();
  }
  await expect(main.getByRole("button", { name: "Positive", exact: true })).toBeVisible();
  await proof.dwell();
  await proof.shot("campaign-positive-steps", {
    caption: "The chart plots positive replies against replies, and each step shows its positive count and rate",
  });
});
