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

test("keep bulk warmup visible while ownership refreshes and the original start finishes", async ({ page, context, proof }) => {
  let holdOwnership = false;
  let releaseOwnership = () => {};
  let releaseStart = () => {};
  const ownershipReady = new Promise<void>((resolve) => { releaseOwnership = resolve; });
  const startReady = new Promise<void>((resolve) => { releaseStart = resolve; });
  await page.route("**/v1/cloud-link", async (route) => {
    if (holdOwnership) await ownershipReady;
    await route.continue();
  });
  await page.route("**/v1/emails/*/warmup/start", async (route) => {
    await startReady;
    await route.continue();
  });

  await page.goto("/app/emails");
  await page.getByRole("row", { name: /dev\.outbound@warmbly\.test/ }).getByRole("checkbox").check();
  await page.getByRole("button", { name: "Start warmup", exact: true }).click();
  const dialog = page.getByRole("dialog", { name: "Start warmup", exact: true });
  await dialog.getByRole("button", { name: "Weekdays", exact: true }).click();
  await expect(dialog.getByRole("button", { name: "Weekdays", exact: true })).toHaveAttribute("aria-pressed", "true");
  await proof.chapter("Bulk warmup during a connection refresh", "Local seed data with delayed ownership and start responses. No sending worker runs in this stack.");
  await proof.shot("bulk-warmup-settings", { caption: "The bulk-start dialog keeps the chosen weekdays and original mailbox selection." });
  // Cloud ownership reads become stale after ten seconds and refetch on reconnection.
  await proof.dwell(11_000);

  try {
    const starting = page.waitForRequest((request) => request.url().endsWith("/warmup/start") && request.method() === "POST");
    await dialog.getByRole("button", { name: "Start warmup", exact: true }).click();
    await starting;
    holdOwnership = true;
    const refreshing = page.waitForRequest((request) => request.url().endsWith("/v1/cloud-link") && request.method() === "GET");
    await context.setOffline(true);
    await context.setOffline(false);
    await refreshing;
    await expect(dialog.getByRole("status")).toHaveText("Waiting for mailbox ownership to be confirmed.");
    await expect(dialog.getByRole("button", { name: "Start warmup", exact: true })).toBeDisabled();
    await expect(dialog.getByRole("button", { name: "Close", exact: true })).toBeDisabled();
    await expect(dialog).toContainText("1 mailbox selected.");
    await proof.shot("bulk-warmup-ownership-refresh", { caption: "A connection refresh keeps the in-flight dialog visible and prevents another submission." });

    holdOwnership = false;
    releaseOwnership();
    await expect(dialog.getByRole("status")).toBeHidden();
    await expect(dialog.getByRole("button", { name: "Start warmup", exact: true })).toBeDisabled();
    await expect(dialog.getByRole("button", { name: "Weekdays", exact: true })).toHaveAttribute("aria-pressed", "true");
    await proof.shot("bulk-warmup-still-starting", { caption: "After ownership is confirmed, the original start remains in progress with the same settings." });
    releaseStart();
    await expect(dialog).toBeHidden();
    await expect(page.getByText("Warmup started for 1 mailbox", { exact: true })).toBeVisible();
  } finally {
    releaseOwnership();
    releaseStart();
    await context.setOffline(false);
  }
});
