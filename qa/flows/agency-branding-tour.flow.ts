import { expect, test } from "../lib/proof.ts";

// Requires ../fixtures/agency.sql and ../fixtures/agency-client-view.sql, as agency-client-view.flow.ts does.
const FIXTURES = process.env.QA_AGENCY_FIXTURES === "1";
const CLIENT = "Dev's Organization";

// A square logo mark, drawn by the browser so it needs no file in the repository.
const LOGO_HTML = `<div style="width:256px;height:256px;border-radius:64px;background:linear-gradient(145deg,#14b8a6,#0f766e);
  display:flex;align-items:center;justify-content:center;font:800 150px/1 system-ui,sans-serif;color:#fff;letter-spacing:-8px">N</div>`;

test.describe("agency branding tour", () => {
  test.skip(!FIXTURES, "Apply qa/fixtures/agency.sql and qa/fixtures/agency-client-view.sql, then set QA_AGENCY_FIXTURES=1.");

  test("brand the portal, preview every page, then see the real dashboard in it", async ({ page, browser, proof }) => {
    const drawing = await browser.newPage({ viewport: { width: 256, height: 256 } });
    await drawing.setContent(`<body style="margin:0;background:transparent">${LOGO_HTML}</body>`);
    const logo = await drawing.screenshot({ omitBackground: true });
    await drawing.close();

    await page.goto("/app/agency/branding");
    await expect(page.getByRole("heading", { name: "Identity" })).toBeVisible();
    await proof.chapter("Identity", "A logo, a colour, a font and corners");
    await page.getByLabel("Upload the logo").setInputFiles({ name: "northwind.png", mimeType: "image/png", buffer: logo });
    await page.getByRole("button", { name: "Teal", exact: true }).click();
    await page.getByRole("radiogroup", { name: "Font" }).getByRole("radio", { name: "Geometric" }).click();
    await page.getByRole("radiogroup", { name: "Corners" }).getByRole("radio", { name: "Round" }).click();
    await expect(page.getByRole("button", { name: "Sign-in", exact: true })).toHaveAttribute("aria-pressed", "true");
    await proof.dwell(900);
    await proof.shot("brand-sign-in", { caption: "Logo, teal accent, geometric font and round corners on the portal's sign-in page" });

    const layout = page.getByRole("radiogroup", { name: "Sign-in layout" });
    await layout.scrollIntoViewIfNeeded();
    await layout.getByRole("radio", { name: "Split" }).click();
    await page.getByRole("radiogroup", { name: "Sign-in background" }).getByRole("radio", { name: "Accent" }).click();
    await page.getByPlaceholder("Welcome to Northwind Growth").fill("Your outreach, in one place");
    await proof.dwell(1000);
    await proof.shot("brand-sign-in-split", { caption: "A split sign-in page on the accent colour, with the agency's own heading" });

    await proof.chapter("Client pages", "The connect page and a shared report in the same brand");
    await page.getByRole("button", { name: "Connect page", exact: true }).click();
    await expect(page.getByText("The page a client opens to connect a mailbox.")).toBeVisible();
    await proof.dwell(700);
    await proof.shot("brand-connect", { caption: "The page a client opens to connect a mailbox" });

    await page.getByRole("button", { name: "Report", exact: true }).click();
    await expect(page.getByText("A report shared with a client.")).toBeVisible();
    await proof.dwell(700);
    await proof.shot("brand-report", { caption: "A report shared with a client" });

    const appearance = page.getByRole("radiogroup", { name: "Appearance", exact: true });
    await appearance.getByRole("radio", { name: "Dark" }).click();
    await proof.dwell(700);
    await proof.shot("brand-report-dark", { caption: "The same report in dark mode" });

    await page.getByRole("radiogroup", { name: "Screen size", exact: true }).getByRole("radio", { name: "Phone" }).click();
    await page.getByRole("button", { name: "Sign-in", exact: true }).click();
    await proof.dwell(800);
    await proof.shot("brand-phone-dark", { caption: "The sign-in page on a phone, in dark mode" });
    await page.getByRole("radiogroup", { name: "Screen size", exact: true }).getByRole("radio", { name: "Desktop" }).click();
    await appearance.getByRole("radio", { name: "Light" }).click();

    await proof.chapter("Portal and embeds", "One domain for every client page, and embeds for the agency's site");
    await page.getByText("Portal domain").first().evaluate((el) => el.scrollIntoView({ block: "start" }));
    await proof.dwell(700);
    await proof.shot("brand-portal-domain", { caption: "The portal domain every client's connect links and reports live on" });
    await page.getByText("Embed on your website").first().evaluate((el) => el.scrollIntoView({ block: "start" }));
    await proof.dwell(700);
    await proof.shot("brand-embeds", { caption: "Embed a connect page or a report on the agency's own website" });

    await proof.chapter("The real dashboard", "The saved brand, in a client's workspace, as its members see it");
    await page.getByRole("heading", { name: "Identity" }).evaluate((el) => el.scrollIntoView({ block: "start" }));
    await page.getByRole("button", { name: "Dashboard", exact: true }).click();
    await page.getByRole("menu").getByText(CLIENT).click();
    const bar = page.getByRole("status").filter({ hasText: "as a client" });
    await expect(bar).toBeVisible();
    await proof.dwell(1500);
    await proof.shot("brand-dashboard", { caption: "The client's real dashboard in the agency's brand" });

    await page.goto("/app/analytics");
    await expect(bar).toBeVisible();
    await proof.dwell(1500);
    await proof.shot("brand-dashboard-analytics", { caption: "Analytics in the same brand" });

    await bar.getByRole("button", { name: "Branding" }).click();
    await expect(page).toHaveURL(/\/app\/agency\/branding/);
    await expect(page.getByRole("heading", { name: "Identity" })).toBeVisible();
  });
});
