import { expect, test } from "../lib/proof.ts";

test.skip(process.env.QA_WARMUP_FILTERING_FIXTURES !== "1", "Apply qa/fixtures/warmup-filtering.sql to this stack's rich-seed database, clear its Redis and set QA_WARMUP_FILTERING_FIXTURES=1.");

test("hide warmup sent by other services from the inbox", async ({ page, proof }) => {
  await page.goto("/app/settings/sending");
  const section = page.getByText("Warmup from other services", { exact: true });
  await section.scrollIntoViewIfNeeded();
  const marker = page.getByRole("switch", { name: "Hide warmup from another Warmbly" });
  await expect(marker).toHaveAttribute("aria-checked", "false");
  await proof.chapter("Warmup from other services", "Off by default: nothing is hidden until the workspace asks");
  await proof.shot("external-warmup-off", { caption: "The new section under Settings > Sending, off by default." });

  await marker.click();
  await expect(marker).toHaveAttribute("aria-checked", "true");
  const field = page.getByPlaceholder("Paste a warmup tag");
  await field.pressSequentially("re", { delay: 60 });
  await expect(page.getByText(/Use at least 4 characters/)).toBeVisible();
  await proof.shot("external-warmup-short-tag", { caption: "A tag too short to be specific is explained before saving." });

  await field.fill("");
  await field.pressSequentially("wrm-7q2x", { delay: 60 });
  const saved = page.waitForResponse((r) => r.url().includes("/outreach/settings") && r.request().method() !== "GET" && (r.request().postData() ?? "").includes("wrm-7q2x"));
  await page.getByRole("button", { name: "Add", exact: true }).click();
  await expect(page.getByRole("button", { name: "Remove wrm-7q2x" })).toBeVisible();
  expect((await saved).ok()).toBe(true);
  await proof.shot("external-warmup-on", { caption: "Another Warmbly's warmup and a tool's warmup tag are now kept out of the inbox." });
});

test("turn on a warmup filter tag for one mailbox", async ({ page, proof }) => {
  await page.goto("/app/emails");
  await page.getByRole("row", { name: /dev\.outbound@warmbly\.test/ }).getByText("dev.outbound@warmbly.test", { exact: true }).click();
  await page.getByRole("button", { name: "Warmup", exact: true }).click();
  const toggle = page.getByRole("switch", { name: "Add a filter tag to warmup mail this mailbox receives" });
  await toggle.scrollIntoViewIfNeeded();
  await expect(toggle).toHaveAttribute("aria-checked", "false");
  await proof.chapter("Filter tag", "Opt-in, per mailbox: a code on every warmup email it receives");
  await proof.shot("filter-tag-off", { caption: "Off by default, below where warmup mail is filed." });

  await toggle.click();
  await expect(toggle).toHaveAttribute("aria-checked", "true");
  await expect(page.getByText(/^[a-z2-9]{4}-[a-z2-9]{4}-[a-z2-9]{4}$/)).toBeVisible();
  await expect(page.getByText(/add a rule for mail whose body contains|In Gmail, search for|In Outlook, add a rule/)).toBeVisible();
  await proof.shot("filter-tag-on", { caption: "A random code with copy, a new-code action and filter steps for the mailbox's provider.", ignore: [page.getByText(/^[a-z2-9]{4}-[a-z2-9]{4}-[a-z2-9]{4}$/)] });

  await toggle.click();
  await expect(toggle).toHaveAttribute("aria-checked", "false");
});
