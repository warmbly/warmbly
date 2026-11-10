import { readFileSync } from "node:fs";
import type { Page } from "@playwright/test";
import { authFile, env } from "../lib/env.ts";
import { expect, test } from "../lib/proof.ts";

// Requires ../fixtures/agency.sql applied to this worktree's rich-seed database.
const FIXTURES = process.env.QA_AGENCY_FIXTURES === "1";
const AGENCY = "a9e00000-0000-0000-0000-000000000001";
const CLIENTS = ["Bluebird Dental", "Acme Robotics"];

async function api<T>(method: string, path: string, body?: object): Promise<T> {
  const state = JSON.parse(readFileSync(authFile(), "utf8")) as { origins: { localStorage: { name: string; value: string }[] }[] };
  const token = state.origins[0].localStorage.find((i) => i.name === "access_token")?.value;
  const res = await fetch(`${env.apiURL}/v1${path}`, {
    method,
    headers: { "Content-Type": "application/json", Authorization: `Bearer ${token}` },
    body: body ? JSON.stringify(body) : undefined,
  });
  if (!res.ok) throw new Error(`${method} ${path} answered ${res.status}: ${await res.text()}`);
  return (await res.json()) as T;
}

// The agency's client workspaces make the account a member of several, so the dashboard asks which to open.
async function openSeededWorkspace(page: Page) {
  await page.goto("/select-org");
  await page.getByRole("button", { name: /Dev's Organization/ }).click();
  await expect(page).toHaveURL(/\/app\//);
}

test.describe("agency", () => {
  test.skip(!FIXTURES, "Apply qa/fixtures/agency.sql to this stack's database and set QA_AGENCY_FIXTURES=1.");

  test.beforeAll(async () => {
    const res = await api<{ data?: { name?: string }[] } | { name?: string }[]>("GET", `/agency/${AGENCY}/clients`);
    const list = Array.isArray(res) ? res : (res.data ?? []);
    const names = new Set(list.map((c) => c.name));
    for (const name of CLIENTS) {
      if (!names.has(name)) await api("POST", `/agency/${AGENCY}/clients`, { name, timezone: "America/New_York" });
    }
  });

  test("open the agency home, the client inbox and the shared send pool", async ({ page, proof }) => {
    // Agency pages need no workspace picked first.
    await page.goto("/app/agency");
    await expect(page.getByText("Pool this period").first()).toBeVisible();
    await expect(page.getByText("Bluebird Dental").first()).toBeVisible();
    await proof.chapter("Agency home", "Every client in one table, the shared send pool on top");
    await proof.shot("agency-home", { caption: "Agency home: every client workspace, problems first, with the shared monthly send pool" });

    await page.goto("/app/agency/inbox");
    await expect(page.getByRole("main")).toBeVisible();
    await proof.dwell(1500);
    await proof.shot("agency-inbox", { caption: "One inbox across every client workspace" });

    await page.goto("/app/agency/usage");
    await expect(page.getByText("Send pool").first()).toBeVisible();
    await expect(page.getByText("AI balance").first()).toBeVisible();
    await proof.chapter("Usage", "One monthly send pool and one AI balance, shared by every client");
    await proof.shot("agency-send-pool", { caption: "The shared send pool and AI balance, and what each client used" });

    await page.goto("/app/agency/clients");
    await expect(page.getByText("Monthly cap").first()).toBeVisible();
    await proof.shot("agency-clients", { caption: "Clients, each with an optional monthly cap on sends and on AI" });

    await page.getByRole("button", { name: "Actions for Bluebird Dental", exact: true }).click();
    await page.getByText("Add balance", { exact: true }).click();
    const fund = page.getByRole("dialog");
    await expect(fund.getByText("Bluebird Dental").first()).toBeVisible();
    await proof.shot("agency-add-balance", { caption: "Move part of the agency's AI balance to one client" });
    await fund.getByRole("button", { name: "Cancel" }).click();
  });

  test("review agency billing, staff and white-label branding", async ({ page, proof }) => {
    await page.goto("/app/agency/billing");
    // The stack bills nothing unless QA_STRIPE_SECRET_KEY is set, so this shows either state.
    await expect(page.getByText(/Current plan|No billing on this instance/).first()).toBeVisible();
    await proof.chapter("Agency billing", "One invoice for every client, with top-up packs");
    await proof.shot("agency-billing", { caption: "Agency plan, renewal and top-up packs" });

    await page.goto("/app/agency/staff");
    await expect(page.getByText("dev@warmbly.com").first()).toBeVisible();
    await proof.shot("agency-staff", { caption: "Agency staff get access to every client workspace" });

    await page.goto("/app/agency/branding");
    await expect(page.getByText("Portal domain").first()).toBeVisible();
    await proof.chapter("White-label", "The agency's brand and domain on client pages, plus website embeds");
    await proof.shot("agency-branding", { caption: "Agency branding: identity, look, and a live preview of the sign-in page" });

    await page.getByRole("button", { name: "Dashboard", exact: true }).click();
    await page.getByRole("heading", { name: "Sign-in page" }).evaluate((el) => el.scrollIntoView({ block: "start" }));
    await proof.dwell(800);
    await proof.shot("agency-branding-sign-in", { caption: "The sign-in page's layout, background, image and wording, beside the dashboard preview" });

    await page.getByRole("button", { name: "Report", exact: true }).click();
    await page.getByRole("heading", { name: "Support and legal" }).evaluate((el) => el.scrollIntoView({ block: "center" }));
    await proof.dwell(800);
    await proof.shot("agency-branding-links", { caption: "The agency's own support, terms and privacy links, beside the report preview" });

    const signIn = page.getByText("Where clients sign in").first();
    await signIn.evaluate((el) => el.scrollIntoView({ block: "center" }));
    await proof.dwell(600);
    await proof.shot("agency-sign-in-setting", { caption: "Where clients sign in: on the agency's domain, or on a branded Warmbly page" });

    await page.goto("/app/agency/api-keys");
    await expect(page.getByRole("main").getByRole("button", { name: "Create key" }).first()).toBeVisible();
    await proof.chapter("Agency API", "One key for the agency and every client");
    await proof.shot("agency-api-keys", { caption: "Agency API keys, reaching any client workspace" });
  });

  test("move between the agency and its client workspaces", async ({ page, proof }) => {
    await page.goto("/select-org");
    await expect(page.getByText("Northwind Growth").first()).toBeVisible();
    await proof.chapter("Workspaces", "The agency and its clients, grouped");
    await proof.shot("workspace-picker", { caption: "The workspace picker groups each client under its agency" });

    await page.getByRole("button", { name: /Dev's Organization/ }).click();
    await expect(page).toHaveURL(/\/app\//);
    const back = page.getByRole("link", { name: "Back to Northwind Growth" }).filter({ visible: true }).first();
    await expect(back).toBeVisible();
    await proof.shot("client-workspace", { caption: "Inside a client workspace, the header leads back to the agency" });

    await page.getByRole("button", { name: "DO Dev's Organization", exact: true }).click();
    await expect(page.getByRole("menu").getByText("Northwind Growth").first()).toBeVisible();
    await proof.shot("workspace-switcher", { caption: "The switcher lists the agency first, with its clients under it" });
    await page.keyboard.press("Escape");

    await back.click();
    await expect(page).toHaveURL(/\/app\/agency/);
    await expect(page.getByText("Pool this period").first()).toBeVisible();
    await proof.shot("back-to-agency", { caption: "One click back to the agency view" });
  });

  test("invite a client to connect a mailbox and share a client report", async ({ page, proof }) => {
    await openSeededWorkspace(page);
    await page.goto("/app/emails");
    await page.getByRole("button", { name: "Invite client to connect" }).click();
    const invite = page.getByRole("dialog", { name: "Invite client to connect" });
    await expect(invite).toBeVisible();
    await proof.chapter("Client connect link", "The client connects their own mailbox from a link, no shared password");
    await proof.shot("connect-link-dialog", { caption: "A connect link for the client, with its expiry, mailbox limit and tags" });
    await invite.getByRole("button", { name: "Close" }).click();

    await page.goto("/app/reports");
    await page.getByRole("button", { name: /New report/ }).first().click();
    const dialog = page.getByRole("dialog", { name: "New report" });
    await expect(dialog).toBeVisible();
    await proof.chapter("Client reports", "A live read-only link and scheduled emails for the client");
    await dialog.getByPlaceholder("Acme outreach, monthly").pressSequentially("Dev's Organization, monthly", { delay: 40 });
    await proof.shot("report-dialog", { caption: "A client report: sections, date range and reply snippets" });
  });

  test("copy a campaign to a client workspace", async ({ page, proof }) => {
    await openSeededWorkspace(page);
    await page.goto("/app/campaigns");
    await page.getByRole("button", { name: /^Actions for / }).first().click();
    await page.getByText("Copy to workspace…").click();
    const dialog = page.getByRole("dialog", { name: /to another workspace/ });
    await expect(dialog).toBeVisible();
    await proof.chapter("Copy to workspace", "Reuse a proven sequence in another client's workspace");
    await dialog.getByText("Bluebird Dental").click();
    await proof.shot("copy-to-workspace", { caption: "Copy a campaign, and optionally its templates, to client workspaces" });
    await dialog.getByRole("button", { name: /^Copy$/ }).click();
    await expect(dialog.getByText(/Copied to 1 workspace/)).toBeVisible();
    await proof.shot("copy-to-workspace-done", { caption: "Copied, with a link to open it there" });
  });
});
