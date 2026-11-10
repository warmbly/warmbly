import type { Locator, Page } from "@playwright/test";
import { expect, test } from "../lib/proof.ts";

// Run against the marketing site: QA_WEB_URL=http://127.0.0.1:<port> QA_SITE=1 pnpm proof site-agencies
async function centre(page: Page, el: Locator) {
  await el.evaluate((node) => node.scrollIntoView({ block: "center" }));
  await page.waitForTimeout(700);
}

test.describe("marketing site agencies", () => {
  test.skip(process.env.QA_SITE !== "1", "Start site/ with pnpm dev and set QA_WEB_URL to it and QA_SITE=1.");
  test.use({ signedIn: false });

  test("agency pricing and the agencies page", async ({ page, proof }) => {
    await page.goto("/");
    await page.locator(".pricing-grid").evaluate((node) => node.scrollIntoView({ block: "center", behavior: "instant" }));
    await proof.dwell(1200);
    await proof.shot("site-home-pricing", { caption: "Landing page: regular plans, each with its daily and monthly volume" });
    const band = page.getByRole("heading", { name: /One plan covers all of them/ });
    await expect(band).toBeVisible();
    await page.locator(".agency-band").evaluate((node) => node.scrollIntoView({ block: "center", behavior: "instant" }));
    await proof.dwell(1200);
    await proof.shot("site-home-agencies", { caption: "Landing page: the agency plans under the regular ones" });

    await page.goto("/pricing/");
    await centre(page, page.getByText("cold emails a day").first());
    await proof.shot("site-pricing-regular", { caption: "Pricing page: regular plans" });
    await page.goto("/pricing/#agencies");
    const section = page.locator("#agencies");
    await expect(section.getByText("Agency Pro").first()).toBeVisible();
    await centre(page, section.getByRole("heading", { name: /Every client on one plan/ }).first());
    await page.evaluate(() => window.scrollBy(0, 250));
    await proof.dwell(600);
    await proof.shot("site-pricing-agencies", { caption: "Pricing page: the agency plans" });

    const calc = page.getByRole("heading", { name: "Cost per client" });
    await centre(page, calc);
    await proof.shot("site-cost-per-client", { caption: "Cost per client at 10 clients, billed yearly" });
    const slider = page.locator("#client-count");
    await slider.fill("25");
    await proof.dwell(500);
    await proof.shot("site-cost-per-client-25", { caption: "The same at 25 clients" });

    await centre(page, page.getByRole("heading", { name: "Included on every agency plan" }));
    await proof.shot("site-agency-included", { caption: "Included on every agency plan" });

    await page.goto("/use-cases/agencies/");
    await expect(page.getByRole("heading", { level: 1 })).toBeVisible();
    await proof.shot("site-agencies-hero", { caption: "Agencies page: hero" });
    await centre(page, page.getByText("One plan for every client.").first());
    await proof.shot("site-agencies-billing", { caption: "Agencies page: one plan for every client" });
    await centre(page, page.getByText(/Ten clients on Agency cost/).first());
    await proof.shot("site-agencies-pricing-teaser", { caption: "Agencies page: pricing teaser" });
  });
});
