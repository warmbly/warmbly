import { expect, test } from "../lib/proof.ts";

// Agency > Branding from nothing set to a finished look. Needs fixtures/admin-agencies.sql; see the README.
test("an agency sets its portal's look", async ({ page, proof }) => {
  test.skip(process.env.QA_ADMIN_AGENCY_FIXTURES !== "1", "Apply fixtures/admin-agencies.sql and set QA_ADMIN_AGENCY_FIXTURES=1.");
  await page.goto("/app/agency/branding");
  await expect(page.getByRole("heading", { name: "Identity" })).toBeVisible();
  await proof.dwell(900);
  await proof.shot("agency-look-empty", { caption: "Agency branding before anything is set: controls on the left, the live page on the right" });

  await page.getByRole("button", { name: "Connect page", exact: true }).click();
  await expect(page.getByText("The page a client opens to connect a mailbox.")).toBeVisible();
  await proof.dwell(500);
  await proof.shot("agency-look-connect", { caption: "Preview: the page a client opens to connect a mailbox" });

  await page.getByPlaceholder("Meridian Outbound").first().fill("Northwind Growth");
  await page.getByRole("button", { name: "Teal", exact: true }).click();
  await page.getByRole("radiogroup", { name: "Font" }).getByRole("radio", { name: "Geometric" }).click();
  await page.getByRole("radiogroup", { name: "Corners" }).getByRole("radio", { name: "Round" }).click();
  await expect(page.getByRole("button", { name: "Dashboard", exact: true })).toHaveAttribute("aria-pressed", "true");
  await expect(page.getByRole("radiogroup", { name: "Corners" }).getByRole("radio", { name: "Round" })).toBeChecked();
  await expect(page.getByText("The dashboard your clients work in.")).toBeVisible();
  await proof.dwell(700);
  await proof.shot("agency-look-dashboard", { caption: "A colour, a font and round corners, shown on the client dashboard as they are picked" });

  await page.getByRole("radiogroup", { name: "Sign-in layout" }).getByRole("radio", { name: "Split" }).scrollIntoViewIfNeeded();
  await page.getByRole("radiogroup", { name: "Sign-in layout" }).getByRole("radio", { name: "Split" }).click();
  await page.getByRole("radiogroup", { name: "Sign-in background" }).getByRole("radio", { name: "Accent" }).click();
  await page.getByPlaceholder("Welcome to Northwind Growth").fill("Welcome back to Northwind Growth");
  await expect(page.getByRole("button", { name: "Sign-in", exact: true })).toHaveAttribute("aria-pressed", "true");
  await expect(page.getByRole("radiogroup", { name: "Sign-in layout" }).getByRole("radio", { name: "Split" })).toBeChecked();
  await proof.dwell(900);
  await proof.shot("agency-look-sign-in", { caption: "The sign-in page with a split layout on the accent colour" });

  await page.getByRole("radio", { name: "Phone" }).click();
  await expect(page.getByRole("radio", { name: "Phone" })).toBeChecked();
  await proof.dwell(600);
  await proof.shot("agency-look-phone", { caption: "The same page at a phone's width" });
});
