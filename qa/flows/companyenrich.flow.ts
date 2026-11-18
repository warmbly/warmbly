import { expect, test } from "../lib/proof.ts";

// Provider responses and the connection below are synthetic localhost fixtures.
// No CompanyEnrich key, paid API call, import, enrollment or email is used.
const connectionID = "c0e00000-0000-4000-8000-000000000001";
const contactID = "c0e00000-0000-4000-8000-000000000002";
const timestamp = "2026-01-01T00:00:00Z";

test.beforeEach(async ({ page }) => {
  await page.route("**/v1/auth/me", async (route) => {
    const response = await route.fetch();
    await route.fulfill({ response, json: { ...await response.json(), product_tour_completed_at: timestamp } });
  });
  await page.route(`**/v1/integrations/connections/${connectionID}`, (route) => route.fulfill({ json: {
    connection: {
      id: connectionID, provider: "companyenrich", label: "CompanyEnrich demo", organization_id: connectionID,
      status: "connected", auth_method: "api_key", health: "healthy", display_fields: {},
      created_at: timestamp, updated_at: timestamp,
    }, events: [], runs: [],
  } }));
});

test("CompanyEnrich research runs explicitly and keeps company results available for review", async ({ page, proof }) => {
  let calls = 0;
  await page.route(`**/v1/integrations/companyenrich/${connectionID}/query`, async (route) => {
    calls += 1;
    expect(route.request().postDataJSON()).toEqual({ operation: "search_companies", body: {
      query: "Example", page: 1, pageSize: 10,
    } });
    await route.fulfill({ json: { credits_consumed: 1, data: {
      items: [{ id: "c0e00000-0000-4000-8000-000000000003", name: "Example Systems", domain: "example.test", industry: "Software", employees: "51-200", description: "Synthetic company profile for the local walkthrough." }],
      page: 1, pageSize: 10, totalItems: 1, totalPages: 1,
    } } });
  });
  await page.goto(`/app/integrations/companyenrich/${connectionID}`);
  await expect(page.getByRole("heading", { name: "CompanyEnrich demo" })).toBeVisible();
  await proof.chapter("Company and people research", "Synthetic CompanyEnrich connection and responses; this walkthrough consumes no credits.");
  await page.getByRole("combobox", { name: "Research action" }).click();
  await expect(page.getByRole("option")).toHaveCount(12);
  await proof.shot("companyenrich-actions", { caption: "Twelve explicit research actions cover company and people discovery, enrichment, workforce, ID lookups and counts." });
  await page.getByRole("option", { name: "Search companies", exact: true }).click();
  await page.getByRole("textbox", { name: "Company name or domain" }).fill("Example");
  expect(calls).toBe(0);
  await page.getByRole("button", { name: "Run one request", exact: true }).click();
  await expect(page.getByRole("heading", { name: "Example Systems" })).toBeVisible();
  expect(calls).toBe(1);
  await proof.shot("companyenrich-company-results", { caption: "Readable company cards and pagination metadata. These are response fixtures; research does not import or enroll contacts." });
});

test("CompanyEnrich person lookup preserves large IDs and provider errors are not replayed", async ({ page, proof }) => {
  let calls = 0;
  await page.route(`**/v1/integrations/companyenrich/${connectionID}/query`, async (route) => {
    calls += 1;
    expect(route.request().postDataJSON()).toEqual({ operation: "get_person", query: { id: "9144415778169662936" } });
    if (calls === 1) {
      await route.fulfill({ json: { data: { id: "9144415778169662936", first_name: "Morgan", last_name: "Example", position: "Engineering lead" } } });
    } else {
      await route.fulfill({ status: 422, json: { error: "Unprocessable Entity", code: "companyenrich_credits_required", message: "This CompanyEnrich account has insufficient credits." } });
    }
  });
  await page.goto(`/app/integrations/companyenrich/${connectionID}`);
  await page.getByRole("combobox", { name: "Research action" }).click();
  await page.getByRole("option", { name: "Get person by ID", exact: true }).click();
  await page.getByRole("textbox", { name: "Person ID" }).fill("9144415778169662936");
  await page.getByRole("button", { name: "Run one request", exact: true }).click();
  await expect(page.getByRole("heading", { name: "Morgan Example" })).toBeVisible();
  await expect(page.getByText("9144415778169662936", { exact: true })).toBeVisible();
  await proof.chapter("People and provider errors", "Synthetic person and credit-error responses. Exact numeric identifiers are retained as text.");
  await proof.shot("companyenrich-person-result", { caption: "Person names and exact large IDs remain readable without rounding." });
  await page.getByRole("button", { name: "Run one request", exact: true }).click();
  await expect(page.getByText("This CompanyEnrich account has insufficient credits.", { exact: true })).toBeVisible();
  await expect(page.getByRole("heading", { name: "Morgan Example" })).toHaveCount(0);
  expect(calls).toBe(2);
  await proof.shot("companyenrich-provider-error", { caption: "An explicit second request shows the provider credit error, clears stale results, and is not automatically retried." });
});

test("CompanyEnrich fills an existing contact's missing profile fields", async ({ page, proof }) => {
  let calls = 0;
  const contact = { id: contactID, first_name: "Morgan", last_name: "Example", email: "morgan@example.test", company: "", subscribed: false, custom_fields: {}, categories: [], campaigns: [], created_at: timestamp, updated_at: timestamp };
  await page.route("**/v1/contacts/search*", (route) => route.fulfill({ json: {
    data: [contact], pagination: { total: 1, next_cursor: null, has_more: false },
  } }));
  await page.route(`**/v1/integrations/companyenrich/${connectionID}/enrich-contact`, async (route) => {
    calls += 1;
    expect(route.request().postDataJSON()).toEqual({ contact_id: contactID, kind: "person", overwrite: false });
    await route.fulfill({ json: { contact: { ...contact, company: "Example Systems" }, updated_fields: ["company"], data: {
      id: "9144415778169662936", first_name: "Morgan", last_name: "Example", position: "Engineering lead",
    } } });
  });
  await page.goto(`/app/integrations/companyenrich/${connectionID}`);
  await page.getByRole("button", { name: "Choose an existing contact", exact: true }).click();
  await page.getByRole("menuitem", { name: /Morgan Example.*morgan@example.test/ }).click();
  expect(calls).toBe(0);
  await page.getByRole("button", { name: "Enrich contact", exact: true }).click();
  await expect(page.getByRole("status").filter({ hasText: "Updated Morgan Example: company." })).toBeVisible();
  expect(calls).toBe(1);
  await proof.chapter("Fill missing contact details", "Synthetic saved contact and enrichment response. Backend tests separately cover atomic writes and unchanged consent.");
  await proof.shot("companyenrich-contact-enriched", { caption: "One explicit enrichment fills missing profile details. Address, subscription, verification and campaign membership are not part of the write." });
});
