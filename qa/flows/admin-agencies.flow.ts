import { readFileSync } from "node:fs";
import { authFile, env } from "../lib/env.ts";
import { expect, test } from "../lib/proof.ts";

const AGENCY = "a9e00000-0000-0000-0000-000000000002";
const CLIENT = "22222222-0000-0000-0000-000000000001";

// Use the isolated admin origin with fixtures/admin-agencies.sql applied; see the README.
test("an operator gives an agency a plan, a pool of its own and extra sends", async ({ page, proof }) => {
  test.skip(process.env.QA_ADMIN_AGENCY_FIXTURES !== "1", "Prepare the localhost admin auth fixtures and set QA_ADMIN_AGENCY_FIXTURES=1.");
  const state = JSON.parse(readFileSync(authFile(), "utf8")) as {
    origins: { origin: string; localStorage: { name: string; value: string }[] }[];
  };
  const stored = state.origins.find((origin) => origin.origin === env.webURL)?.localStorage.find((item) => item.name === "auth_token");
  if (!stored) throw new Error("No local admin session prepared");
  const token = JSON.parse(stored.value) as { access_token: string };
  const headers = { Authorization: `Bearer ${token.access_token}` };
  await page.addInitScript(() => {
    const token = localStorage.getItem("auth_token");
    if (token) localStorage.setItem("warmbly_admin_token", token);
  });
  const clientPlan = async () => (await (await page.request.get(`${env.apiURL}/admin/organizations/${CLIENT}/managed-plan`, { headers })).json()) as { managed: boolean; plan_id: string };

  await page.goto("/agencies");
  await expect(page.getByText("Meridian Outbound")).toBeVisible();
  await proof.chapter("Agencies", "Real localhost API. Admin permissions and the verified session are local auth fixtures, not MFA proof.");
  await proof.shot("admin-agencies-list", { caption: "Every agency, with its plan, clients and shared pool" });

  await page.goto(`/agencies/${AGENCY}`);
  await expect(page.getByText("No plan in force.")).toBeVisible();
  expect((await clientPlan()).managed).toBe(false);
  await proof.shot("admin-agency-no-plan", { caption: "An agency with a client workspace and no plan yet" });

  await page.getByRole("button", { name: "Give a plan" }).click();
  const give = page.getByRole("dialog", { name: "Give Meridian Outbound a plan" });
  await give.getByRole("combobox").click();
  await page.getByRole("option", { name: /Agency Pro/ }).click();
  await give.getByLabel("Reason").fill("Annual contract, paid by invoice");
  await proof.shot("admin-agency-give-plan", { caption: "Give an agency plan without Stripe: plan, reason and an optional end date" });
  await give.getByRole("button", { name: "Give plan" }).click();
  await expect(give).toBeHidden();
  await expect(page.getByText("Given by an operator, not bought through Stripe.")).toBeVisible();
  // The client workspace holds the agency's plan the moment it is given.
  await expect.poll(async () => (await clientPlan()).plan_id).toBe("00000000-0000-0000-0000-0000000000a2");
  await proof.shot("admin-agency-plan-given", { caption: "The plan is in force and the client workspace has received it" });

  await page.getByRole("button", { name: "Edit" }).click();
  const custom = page.getByRole("dialog", { name: "Custom pool and AI allowance" });
  await custom.getByLabel("Monthly send pool").fill("2000000");
  await custom.getByLabel("Monthly AI allowance (USD)").fill("250");
  await custom.getByLabel("Reason").fill("Agency Scale contract");
  await proof.shot("admin-agency-custom-pool", { caption: "A pool and AI allowance for this agency alone, in place of its plan's" });
  await custom.getByRole("button", { name: "Save" }).click();
  await expect(custom).toBeHidden();
  await expect(page.getByText("2,000,000 sends").first()).toBeVisible();

  await page.getByRole("button", { name: "Add sends" }).click();
  const sends = page.getByRole("dialog", { name: "Add sends" });
  await sends.locator("#agency-credit-amount").fill("50000");
  await sends.locator("#agency-credit-reason").fill("Goodwill after a delayed onboarding");
  await sends.getByRole("button", { name: /^Add 50,000 sends$/ }).click();
  await expect(sends).toBeHidden();

  const detail = (await (await page.request.get(`${env.apiURL}/admin/agencies/${AGENCY}`, { headers })).json()) as {
    usage: { pool_size: number; topups: number; total: number };
  };
  expect(detail.usage).toMatchObject({ pool_size: 2_000_000, topups: 50_000, total: 2_050_000 });
  await page.evaluate(() => window.scrollTo({ top: 0, behavior: "instant" }));
  await proof.dwell(800);
  await proof.shot("admin-agency-scale", { caption: "The agency on a given plan with its own 2,000,000 pool and 50,000 added sends" });

  await page.goto(`/organizations/${CLIENT}`);
  await expect(page.getByRole("link", { name: "Meridian Outbound" })).toBeVisible();
  await proof.shot("admin-agency-client-workspace", { caption: "A client workspace names its agency and links to it" });
});
