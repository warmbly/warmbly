import { expect, test } from "../lib/proof.ts";

test.skip(process.env.QA_WARMUP_SCHEDULE_FIXTURES !== "1", "Apply qa/fixtures/warmup-schedule.sql to this stack's rich-seed database and set QA_WARMUP_SCHEDULE_FIXTURES=1.");

test("choose weekend warmup while campaigns keep weekday working days", async ({ page, proof }) => {
  await page.goto("/app/emails");
  const openMailbox = async () => {
    await page.getByRole("row", { name: /dev\.outbound@warmbly\.test/ }).getByText("dev.outbound@warmbly.test", { exact: true }).click();
    await page.getByRole("button", { name: "Warmup", exact: true }).click();
  };
  await openMailbox();
  await page.getByRole("button", { name: "Follow sending plan", exact: true }).scrollIntoViewIfNeeded();
  await expect(page.getByText("Following sending plan: Monday to Friday.")).toBeVisible();
  await expect(page.getByRole("button", { name: "Saturday", exact: true })).toHaveAttribute("aria-pressed", "false");
  await proof.chapter("Warmup sending days", "Local seed data: warmup starts by following a weekday sending plan");
  await proof.shot("warmup-inherited-days", { caption: "The inherited days and the remaining workday hours are explained beside the controls." });

  await page.getByRole("button", { name: "Every day", exact: true }).click();
  for (const day of ["Saturday", "Sunday"]) {
    await expect(page.getByRole("button", { name: day, exact: true })).toHaveAttribute("aria-pressed", "true");
  }
  const saved = page.waitForResponse((response) => response.url().includes("/emails/") && response.request().method() === "PATCH");
  await page.getByRole("button", { name: "Save changes", exact: true }).click();
  expect((await saved).ok()).toBe(true);
  await expect(page.getByRole("button", { name: "Save changes", exact: true })).toBeHidden();
  await proof.shot("warmup-every-day", { caption: "Every day explicitly selects both weekend days for warmup." });

  await page.getByRole("button", { name: "Close", exact: true }).click();
  await page.reload();
  await openMailbox();
  await expect(page.getByRole("button", { name: "Every day", exact: true })).toHaveAttribute("aria-pressed", "true");
  await page.getByRole("button", { name: "Every day", exact: true }).click();
  await page.getByRole("button", { name: "Save changes", exact: true }).click();
  await expect(page.getByRole("button", { name: "Save changes", exact: true })).toBeHidden();
  await page.getByRole("button", { name: "Sending", exact: true }).click();
  await expect(page.getByRole("button", { name: "Sat", exact: true })).toHaveAttribute("aria-pressed", "false");
  await expect(page.getByRole("button", { name: "Sun", exact: true })).toHaveAttribute("aria-pressed", "false");
  await proof.shot("warmup-campaign-days", { caption: "Campaign working days remain Monday to Friday after saving weekend warmup." });

  await page.getByRole("button", { name: "Warmup", exact: true }).click();
  await page.setViewportSize({ width: 390, height: 844 });
  await page.getByRole("button", { name: "Every day", exact: true }).scrollIntoViewIfNeeded();
  await expect(page.getByRole("button", { name: "Every day", exact: true })).toHaveAttribute("aria-pressed", "true");
  for (const day of ["Saturday", "Sunday"]) {
    await expect(page.getByRole("button", { name: day, exact: true })).toHaveAttribute("aria-pressed", "true");
  }
  await proof.shot("warmup-days-phone", { caption: "The same presets, selected days and explanation on a phone." });
});
