import type { Page } from "@playwright/test";
import { expect, test } from "../lib/proof.ts";

const CAMPAIGN = "44444444-0000-0000-0000-000000000001";
const FOLLOW_UP = "55555555-0000-0000-0000-000000000002";

// Report the product tour as finished, so a freshly seeded account never covers the page with it.
async function skipTour(page: Page) {
  await page.route("**/v1/auth/me", async (route) => {
    const response = await route.fetch();
    await route.fulfill({ response, json: { ...await response.json(), product_tour_completed_at: "2026-01-01T00:00:00Z" } });
  });
}

// The stack has no AI provider, so writing AI copy ahead is answered here: the
// lead's real emails after a pause, with the opener filled in. Everything else
// on screen is the real dashboard and API.
const OPENERS: Record<string, string> = {
  "jonas@acme-robotics.test": "Saw Acme Robotics opened its second assembly line in Lyon this spring, congrats on the growth.",
  "leila@cedarfinance.test": "Your post on cutting month-end close to three days was a great read.",
  "marco@fiorestudio.test": "The Fiore Studio rebrand for the Milan design week caught my eye.",
};
const REWRITE = "Congrats on the Lyon line; doubling capacity in a year is no small thing.";

async function standInForAI(page: Page) {
  // Leads whose write has been answered, so a shot can tell a background write from one started by opening the lead.
  const written = new Set<string>();
  await page.route(/\/v1\/campaigns\/[^/]+\/leads\/[^/]+\/emails\/[^/]+\/write$/, async (route) => {
    const url = route.request().url();
    const rewrite = (route.request().postData() ?? "").includes("rewrite");
    await new Promise((r) => setTimeout(r, 4500));
    const response = await route.fetch({ url: url.replace(/\/[^/]+\/write$/, ""), method: "GET", postData: undefined });
    const data = await response.json();
    const text = rewrite ? REWRITE : (OPENERS[data.lead.email] ?? `Loved what ${data.lead.company} has been shipping lately.`);
    for (const step of data.steps) {
      step.body_html = step.body_html.replace(/<span data-ai-pending="([^"]+)"[^>]*><\/span>/g, `<span data-ai-block="$1">${text}</span>`);
      for (const b of step.ai_blocks ?? []) b.written = true;
    }
    await route.fulfill({ status: 200, json: data });
    written.add(data.lead.email);
  });
  return written;
}

test.skip(process.env.QA_CAMPAIGN_REVIEW_FIXTURES !== "1", "Apply qa/fixtures/campaign-review.sql to this stack's rich-seed database and set QA_CAMPAIGN_REVIEW_FIXTURES=1.");

test("review every lead's emails, then approve them before they go out", async ({ page, proof }) => {
  await skipTour(page);
  const written = await standInForAI(page);
  // The email renders in its own frame; only the visible one is the open lead's.
  const emailBody = page.frameLocator('iframe[title="Message body"]:visible');

  // Hold the first readiness read so the loading frame stays on screen for its shot.
  let release = () => {};
  const held = new Promise<void>((r) => (release = r));
  let first = true;
  await page.route(/\/v1\/campaigns\/[^/]+\/review$/, async (route) => {
    if (first && route.request().method() === "GET") {
      first = false;
      await held;
    }
    await route.continue();
  });
  await page.goto(`/app/campaigns/${CAMPAIGN}/review`);
  // Wait for the step panel first, so the outline asserted next is the Review tab's, not the page's.
  await expect(page.getByTitle("Read this email as each lead gets it, and approve leads")).toBeVisible();
  await expect(page.getByRole("status", { name: "Loading", exact: true })).toBeVisible();
  await proof.chapter("Review tab", "It opens in the dashboard's own outline while the data loads");
  await proof.dwell(1600);
  await proof.shot("review-loading", { caption: "Loading: the tab's own frame in outline, lit by the shared sweep" });
  release();

  const toggle = page.getByRole("switch", { name: "Approve before sending" });
  await expect(toggle).toBeVisible();
  await expect(page.getByText(/Personalized for this lead/)).toBeVisible();
  await proof.dwell();
  await proof.shot("review-readiness", { caption: "One line of checks on top; the leads and each one's emails exactly as they will be sent below" });

  await toggle.click();
  await expect(page.getByText(/Review is on/)).toBeVisible();
  await expect(page.getByRole("button", { name: /^Approve all/ })).toBeVisible();
  await proof.dwell();
  await proof.shot("review-on", { caption: "Review on: every lead not yet emailed waits here for approval" });

  await proof.chapter("AI writes as you read", "Opening a lead writes its AI copy on the spot");
  await page.getByRole("button", { name: /Jonas Weber/ }).first().click();
  await expect(page.getByText(/Writing Jonas's opener|Reading about Acme Robotics/)).toBeVisible();
  await proof.dwell(1600);
  await proof.shot("ai-writing", { caption: "Opening Jonas writes his opener right away, where it will sit in the email" });
  await expect(page.getByText(/AI wrote the highlighted part for Jonas/)).toBeVisible({ timeout: 15_000 });
  await proof.dwell(1400);
  await proof.shot("ai-written", { caption: "It fades in, highlighted so you can see what AI wrote; Rewrite asks for another" });

  await page.getByRole("button", { name: "Rewrite" }).click();
  await expect(page.getByText(/Writing Jonas's opener|Reading about Acme Robotics/)).toBeVisible();
  await expect(emailBody.getByText(/Congrats on the Lyon line/)).toHaveCount(0);
  await expect(page.getByText(/AI wrote the highlighted part for Jonas/)).toBeVisible({ timeout: 15_000 });
  await expect(emailBody.getByText(/Congrats on the Lyon line/)).toBeVisible();
  await proof.dwell(1200);

  // Leila's opener was written in the background while Jonas was open, before J is pressed.
  await expect.poll(() => written.has("leila@cedarfinance.test"), { timeout: 15_000 }).toBe(true);
  await page.keyboard.press("j");
  await expect(page.getByText("leila@cedarfinance.test").last()).toBeVisible();
  await expect(page.getByText(/AI wrote the highlighted part for Leila/)).toBeVisible();
  await expect(emailBody.getByText(/cutting month-end close to three days/)).toBeVisible();
  await proof.dwell(800);
  await proof.shot("ai-prewritten", { caption: "Press J: the next leads were written in the background, so Leila's is already there" });

  await page.getByRole("button", { name: /to check$/ }).click();
  await page.getByRole("button", { name: "Show them" }).first().click();
  await expect(page.getByText("No first name", { exact: true })).toBeVisible();
  await proof.dwell(600);
  await proof.shot("missing-first-name", { caption: "Show them lists the leads whose emails would print a blank first name" });
  await page.getByRole("button", { name: "Clear filter" }).click();

  // Each email step has its own Review tab, so the follow-up is read on the follow-up's.
  await page.goto(`/app/campaigns/${CAMPAIGN}/review?step=${FOLLOW_UP}`);
  await page.getByRole("button", { name: /Jonas Weber/ }).first().click();
  await expect(page.getByText("jonas@acme-robotics.test").last()).toBeVisible();
  await expect(page.getByText("replies in the thread", { exact: true })).toBeVisible();
  await proof.dwell(600);
  await proof.shot("lead-follow-up", { caption: "The follow-up replies in the thread, so it carries the first email's subject" });

  await page.goto(`/app/campaigns/${CAMPAIGN}/review`);
  await expect(toggle).toBeVisible();
  await page.getByRole("button", { name: /Ana Ruiz/ }).first().click();
  await expect(page.getByText(/Personalized for this lead/)).toBeVisible();
  await page.getByRole("button", { name: "Edit for this lead" }).click();
  await expect(page.getByRole("button", { name: "Save for this lead" })).toBeVisible();
  await proof.dwell();
  await proof.shot("lead-edit", { caption: "Edit for this lead: the step's full editor, saved for one lead" });
  await page.getByRole("button", { name: "Cancel", exact: true }).click();

  await page.getByRole("button", { name: /Jonas Weber/ }).first().click();
  await expect(page.getByText("jonas@acme-robotics.test").last()).toBeVisible();
  await page.getByRole("button", { name: "Approve", exact: true }).first().click();
  await expect(page.getByText(/Approved\. Their first email goes out on schedule/)).toBeVisible();
  await proof.dwell();
  await proof.shot("approved", { caption: "Approve (or press A): Jonas leaves the list and the next waiting lead slides in" });

  await page.getByRole("checkbox", { name: /Select Priya Natarajan/ }).check();
  await page.getByRole("checkbox", { name: /Select Sofia Lindqvist/ }).check();
  await page.getByRole("checkbox", { name: /Select Tom Fischer/ }).check();
  await expect(page.getByText("3 selected")).toBeVisible();
  await proof.shot("bulk-select", { caption: "Approve several at once, write their AI copy, or send them back to review" });

  await page.getByRole("link", { name: "Overview" }).click();
  await expect(page.getByText(/waiting for review\. Nothing goes to them/)).toBeVisible();
  await proof.chapter("Everywhere else", "Waiting leads are visible from every tab of the campaign");
  await proof.shot("waiting-banner", { caption: "Every other tab says how many leads wait, with a way back to Review" });
});
