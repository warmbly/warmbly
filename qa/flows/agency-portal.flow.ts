import type { Page } from "@playwright/test";
import { env } from "../lib/env.ts";
import { expect, test } from "../lib/proof.ts";

// Requires fixtures/agency.sql and fixtures/agency-clients.sql, plus the portal setup in qa/README.md
// (a built dashboard behind the portal service at QA_PORTAL_URL, and links in QA_PORTAL_CONNECT / QA_PORTAL_REPORT).
const PORTAL = process.env.QA_PORTAL_URL ?? "";
const CONNECT = process.env.QA_PORTAL_CONNECT ?? "";
const REPORT = process.env.QA_PORTAL_REPORT ?? "";
const CLIENT = { email: "casey@client.example", password: "password123" };

async function loginCode(email: string, since: number): Promise<string> {
  const query = encodeURIComponent(`to:"${email}" subject:"Your Login Code"`);
  for (let i = 0; i < 40; i++) {
    const res = await fetch(`${env.mailpitURL}/api/v1/search?query=${query}&limit=5`);
    if (res.ok) {
      const { messages } = (await res.json()) as { messages: { ID: string; Created: string }[] };
      const fresh = messages.find((m) => Date.parse(m.Created) >= since - 2000);
      if (fresh) {
        const msg = (await (await fetch(`${env.mailpitURL}/api/v1/message/${fresh.ID}`)).json()) as { Text: string };
        const code = msg.Text.match(/\b(\d{6})\b/)?.[1];
        if (code) return code;
      }
    }
    await new Promise((r) => setTimeout(r, 500));
  }
  throw new Error(`no login code for ${email}`);
}

async function signIn(page: Page) {
  await page.getByPlaceholder("name@company.com").fill(CLIENT.email);
  const cont = page.getByRole("button", { name: "Continue" });
  if (await cont.isVisible()) await cont.click();
  await page.getByPlaceholder("Enter your password").fill(CLIENT.password);
  const since = Date.now();
  await page.getByRole("button", { name: "Sign in" }).click();
  const code = await loginCode(CLIENT.email, since);
  await expect(page.getByRole("heading", { name: "Check your email" })).toBeVisible();
  await page.getByRole("textbox").first().pressSequentially(code, { delay: 60 });
  const verify = page.getByRole("button", { name: "Verify" });
  if (await verify.isVisible()) await verify.click();
}

test.describe("agency clients", () => {
  test.skip(process.env.QA_AGENCY_FIXTURES !== "1", "Apply qa/fixtures/agency.sql and agency-clients.sql and set QA_AGENCY_FIXTURES=1.");

  test.describe("on the main address", () => {
    test.use({ account: CLIENT });

    test("a client's own teammate sees the agency's brand in the dashboard", async ({ page, proof }) => {
      await page.goto("/app/campaigns");
      await expect(page.getByText("Northwind Growth").first()).toBeVisible();
      await proof.chapter("The client's dashboard", "Casey works at the client; the agency's brand replaces Warmbly's");
      await proof.shot("client-dashboard", { caption: "A client's teammate sees the agency's name and colour, with no Warmbly plan or trial prompts" });
      await page.getByRole("button", { name: "DO Dev's Organization", exact: true }).click();
      await expect(page.getByText("Managed by").first()).toBeVisible();
      await proof.shot("client-workspace-menu", { caption: "The workspace menu says who manages it" });
      await page.keyboard.press("Escape");
    });
  });

  test.describe("on the agency's own domain", () => {
    test.skip(!PORTAL || !CONNECT || !REPORT, "Set QA_PORTAL_URL, QA_PORTAL_CONNECT and QA_PORTAL_REPORT (see qa/README.md).");
    test.use({ signedIn: false });

    test("the client opens a connect link and a live report on the agency's domain", async ({ page, proof }) => {
      await page.goto(`${PORTAL}/connect/${CONNECT}`);
      await expect(page.getByText("Northwind Growth").first()).toBeVisible();
      await proof.chapter("Client pages", "Links the agency sends, on the agency's domain and in its brand");
      await proof.shot("portal-connect", { caption: "The client connects a mailbox from the agency's link" });

      await page.goto(`${PORTAL}/r/${REPORT}`);
      await expect(page.getByText("Monthly results").first()).toBeVisible();
      await proof.dwell(1000);
      await proof.shot("portal-report", { caption: "The client's live report" });
    });

    test("the client signs in to the branded dashboard on the agency's domain", async ({ page, proof }) => {
      await page.goto(`${PORTAL}/auth/login`);
      await expect(page.getByPlaceholder("name@company.com")).toBeVisible();
      await proof.chapter("Branded dashboard", "The whole dashboard on portal.northwindgrowth.com, with no Warmbly name");
      await proof.shot("portal-sign-in", { caption: "Sign-in on the agency's domain" });

      await signIn(page);
      await expect(page).toHaveURL(/\/app\//, { timeout: 30_000 });
      await expect(page.getByText("Northwind Growth").first()).toBeVisible();
      // The agency's name is in the loading frame too, so wait for the page itself.
      await expect(page.getByRole("link", { name: "Campaigns" }).first()).toBeVisible({ timeout: 30_000 });
      await page.goto(`${PORTAL}/app/campaigns`);
      await expect(page.getByRole("button", { name: /New campaign/ }).first()).toBeVisible({ timeout: 30_000 });
      await proof.dwell(1000);
      await proof.shot("portal-dashboard", { caption: "The client's dashboard on the agency's domain, in the agency's colour, font and corners" });
    });
  });
});
