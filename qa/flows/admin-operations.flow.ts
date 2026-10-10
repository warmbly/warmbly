import { expect, test } from "../lib/proof.ts";
import { env } from "../lib/env.ts";

test.use({ signedIn: false, timezoneId: "UTC" });

const worker = "11111111-1111-4111-8111-111111111111";
const consumer = "22222222-2222-4222-8222-222222222222";
const mailbox = "33333333-3333-4333-8333-333333333333";
const admin = "44444444-4444-4444-8444-444444444444";

// Parent-run only: the worktree's localhost admin Vite origin with VITE_API_URL=QA_API_URL.
test("admin CLI consent and redacted node diagnostics with disclosed local response fixtures", async ({ page, proof }) => {
  test.skip(process.env.QA_ADMIN_OPERATIONS_FIXTURES !== "1", "Set QA_ADMIN_OPERATIONS_FIXTURES=1 on this worktree's localhost admin origin.");
  for (const origin of [env.webURL, env.apiURL]) {
    expect(["localhost", "127.0.0.1", "[::1]"].includes(new URL(origin).hostname)).toBe(true);
  }
  const at = new Date().toISOString();
  const started = new Date(Date.now() - 30000).toISOString();
  const expires = new Date(Date.now() + 5 * 60000).toISOString();
  const before = new Date(Date.now() + 60000).toISOString().slice(0, 16);
  let mfa = false;
  let confirmed = false;
  let decisions = 0;
  let descriptions = 0;
  let unsupportedResults = false;
  let consumerBrokerReads = 0;
  const node = (id: string, role: string) => ({ id, role, name: role === "worker" ? "fixture-operations-worker" : "fixture-operations-consumer", notes: "Synthetic UI fixture", region: "local", address: "", capacity_target: 100, version: "fixture", active: true, last_seen_at: at, enrolled_at: started, usage: {}, mailbox_count: role === "worker" ? 1 : 0, created_at: started, updated_at: at });
  const metric = (scope: string, id: string, value: number) => ({ id, title: `Broker ${id}`, unit: id === "members" ? "consumers" : "messages", scope_id: scope, availability: "fresh", condition: "unknown", severity: "info", note: "Synthetic committed-offset observation, not delivery evidence", count: value, affected_mailboxes: null, affected_organizations: null, unknown_organization_rows: null, observed_at: at, evidence_at: at, latest_evidence_at: at, evidence_age_seconds: null, next_eligible_at: null, window_start: null, window_end: null, threshold_seconds: null });
  const source = (scope: string, group: string, topic: string, lag: number) => ({ id: scope, availability: "fresh", coverage: "complete", checked_at: at, observed_at: at, measured_scopes: 1, expected_scopes: 1, metrics: [{ ...metric(scope, "committed_lag", lag), broker: { consumer_group: group, topics: [topic], partitions: [{ topic, partition: 0, earliest: 10, committed: 16, latest: 16 + lag, committed_lag: lag }] } }, metric(scope, "members", 1)] });
  const held = { id: "55555555-5555-4555-8555-555555555555", observed_at: at, event: "sync_control_plane_held", level: "warn", category: "control_plane", mailbox_id: mailbox, http_status: 503, count: 1 };
  const runtime = { id: "66666666-6666-4666-8666-666666666666", observed_at: at, event: "worker_error", level: "error", category: "worker_runtime", count: 1 };

  await page.route(`${env.apiURL}/**`, async (route) => {
    const request = route.request();
    const url = new URL(request.url());
    const path = url.pathname;
    const headers = { "Cache-Control": "no-store", "Access-Control-Allow-Origin": new URL(env.webURL).origin, "Access-Control-Allow-Headers": "Authorization, Content-Type", "Access-Control-Allow-Methods": "GET, POST, OPTIONS" };
    const json = (body: unknown, status = 200) => route.fulfill({ status, contentType: "application/json", headers, body: JSON.stringify(body) });
    if (request.method() === "OPTIONS") return route.fulfill({ status: 204, headers });
    if (path === "/v1/auth/config") return json({ captcha: false });
    if (path === "/v1/auth/me") return json({ id: admin, email: "fixture-admin@example.test", is_admin: true, admin_permissions: (1 << 0) | (1 << 4) | (1 << 11), session_mfa_verified: mfa });
    if (path === "/v1/auth/reauth") { confirmed = true; return json({ valid_for_seconds: 300 }); }
    if (path === "/admin/auth/device/describe") {
      descriptions++;
      const body = request.postDataJSON() as { user_code: string };
      if (body.user_code === "JKLM-2345") return json({ error: "expired", message: "Request expired or already consumed", code: "admin_device_expired", request_id: "fixture-expired" }, 404);
      return json({ request: { client_name: "Local fixture laptop (unverified name)", instance_url: env.apiURL, expires_at: expires, status: "pending", consent_token: "c".repeat(43) }, requested_access: "Your live platform administrator permissions through a separate revocable session", admin_permissions: (1 << 0) | (1 << 4) | (1 << 11) });
    }
    if (path === "/admin/auth/device/decide") {
      decisions++;
      const body = request.postDataJSON() as { user_code: string; consent_token: string; decision: string };
      expect(body.user_code).toBe("ABCD-EFGH"); expect(body.consent_token).toBe("c".repeat(43));
      if (!confirmed) return json({ error: "forbidden", message: "Fresh authentication required", code: "reauth_required", request_id: "fixture-reauth" }, 403);
      return json({ status: body.decision });
    }
    if (path === "/admin/fleet/nodes") return json({ data: [node(worker, "worker"), node(consumer, "consumer")] });
    if (path === `/admin/workers/${worker}/stats`) return json({ worker_id: worker, total_emails_sent: 0, emails_sent_today: 0, emails_sent_this_week: 0, average_delivery_time_ms: 0, success_rate: 0, queue_depth: 0 });
    if (path === `/admin/workers/${worker}/emails`) return json({ data: [{ id: mailbox, email: "fixture-mailbox@example.test", provider: "gmail", user_id: admin }], pagination: { has_more: false, total: 1 } });
    if (path === `/admin/fleet/nodes/${worker}/logs`) {
      const events = [held, runtime].filter((event) => (!url.searchParams.get("level") || event.level === url.searchParams.get("level")) && (!url.searchParams.get("mailbox_id") || ("mailbox_id" in event && event.mailbox_id === url.searchParams.get("mailbox_id"))) && (!url.searchParams.get("after") || Date.parse(event.observed_at) >= Date.parse(url.searchParams.get("after")!)) && (!url.searchParams.get("before") || Date.parse(event.observed_at) <= Date.parse(url.searchParams.get("before")!)));
      return json({ node_id: worker, availability: "fresh", coverage: "partial", reason: "allowlisted_evidence_only", observed_at: at, capture: { protocol: 1, run_id: "77777777-7777-4777-8777-777777777777", started_at: started, observed_at: at, received_at: at, dropped: 3 }, events, oldest_retained_at: at, newest_retained_at: at, retained_count: 2, truncated: false, retention_seconds: 3600, max_events: 1000 });
    }
    if (path === `/admin/fleet/nodes/${consumer}/logs`) return json({ node_id: consumer, availability: "unavailable", coverage: "unavailable", reason: "log_credential_unavailable", observed_at: at, events: [], retained_count: 0, truncated: false, retention_seconds: 3600, max_events: 1000 });
    if (path === `/admin/fleet/nodes/${consumer}/broker`) { consumerBrokerReads++; return json({ code: "bad_request", message: "Worker required" }, 400); }
    if (path === `/admin/fleet/nodes/${worker}/broker`) return json({ node_id: worker, observed_at: at, commands: source(worker, `worker-${worker}`, `w.${worker}`, 6), results: unsupportedResults ? { id: "worker_events", availability: "unavailable", coverage: "unavailable", reason: "unsupported", checked_at: at, observed_at: at, measured_scopes: null, expected_scopes: null, metrics: [] } : source("worker_events", "consumer-group", "jobs.worker-events", 3), note: "Shared results are not this worker's delivery count; lag is not outage evidence." });
    if (path === "/admin/instance/health") return json({ checks: [], checked_at: at });
    if (path === "/admin/instance/monitoring") return json({ version: "fixture", checked_at: at, refresh_after: expires, coverage: "unavailable", sources: [] });
    return json({ code: "service_unavailable", message: "Not included in the local UI fixture", request_id: "fixture-only" }, 503);
  });

  await page.goto("/device?user_code=ABCD-EFGH&approve=true");
  await expect(page).toHaveURL(/\/auth\/login$/);
  await expect(page.getByRole("heading", { name: "Sign in to Warmbly", exact: true })).toBeVisible();
  expect(decisions).toBe(0); expect(descriptions).toBe(0);
  await proof.chapter("Native sign-in boundary", "Local response fixtures only. This flow does not prove actual MFA, a real CLI session, Redis retention or live Kafka observations.");
  await proof.shot("admin-cli-sign-in-boundary", { caption: "Unauthenticated /device redirects to the unchanged login screen. Synthetic fixtures are used throughout; no production credentials or real device secrets." });

  await page.addInitScript(() => localStorage.setItem("warmbly_admin_token", JSON.stringify({ access_token: "fixture-not-a-credential", refresh_token: "fixture-not-a-credential", access_token_expires_at: "2099-01-01T00:00:00Z", refresh_token_expires_at: "2099-01-01T00:00:00Z" })));
  await page.goto("/device");
  await expect(page.getByRole("heading", { name: "Two-factor authentication required", exact: true })).toBeVisible();
  expect(descriptions).toBe(0);
  mfa = true;
  await page.reload();
  await expect(page.getByRole("button", { name: "Review request", exact: true })).toBeVisible();
  await page.getByRole("textbox", { name: "User code", exact: true }).fill("ABCD-EFGH");
  await page.getByRole("button", { name: "Review request", exact: true }).click();
  await expect(page.getByText("Pending explicit approval", { exact: true })).toBeVisible();
  await expect(page.getByText(env.apiURL, { exact: true })).toBeVisible();
  await expect(page.getByText("Local fixture laptop (unverified name)", { exact: true })).toBeVisible();
  expect(decisions).toBe(0);
  await proof.chapter("Review is not approval", "Synthetic client/instance/access/expiry response. Opening the link, passing the MFA guard fixture and reviewing do not authorize the CLI.");
  await proof.shot("admin-cli-pending-consent", { caption: "Disclosed local fixture: explicit pending consent shows instance, requester-supplied client name, administrator capability and expiry. No automatic decision." });
  await page.getByRole("button", { name: "Approve CLI sign-in", exact: true }).click();
  await page.getByRole("button", { name: "Confirm identity", exact: true }).click();
  const confirmation = page.getByRole("alertdialog");
  await expect(confirmation).toBeVisible();
  await confirmation.getByPlaceholder("Two-factor or recovery code", { exact: true }).fill("123456");
  await confirmation.getByRole("button", { name: "Confirm", exact: true }).click();
  await expect(confirmation).not.toBeVisible();
  await expect(page.getByRole("button", { name: "Approve CLI sign-in", exact: true })).toBeEnabled();
  expect(decisions).toBe(1);
  await page.getByRole("button", { name: "Approve CLI sign-in", exact: true }).click();
  await expect(page.getByText("Request approved", { exact: true })).toBeVisible();
  expect(decisions).toBe(2);
  await proof.shot("admin-cli-explicit-approved", { caption: "Synthetic decision and reauth responses, not real MFA or session issuance. Native identity confirmation alone never retries approval; a second explicit click is required." });

  await page.getByRole("button", { name: "Enter another code", exact: true }).click();
  await page.getByRole("textbox", { name: "User code", exact: true }).fill("ABCD-EFGH");
  await page.getByRole("button", { name: "Review request", exact: true }).click();
  await page.getByRole("button", { name: "Deny request", exact: true }).click();
  await expect(page.getByText("Request denied", { exact: true })).toBeVisible();
  await proof.shot("admin-cli-explicit-denied", { caption: "Local decision fixture: explicit denial resolves the request without returning credentials." });
  await page.getByRole("button", { name: "Enter another code", exact: true }).click();
  await page.getByRole("textbox", { name: "User code", exact: true }).fill("JKLM-2345");
  await page.getByRole("button", { name: "Review request", exact: true }).click();
  await expect(page.getByText("Request expired or unavailable", { exact: true })).toBeVisible();
  await proof.shot("admin-cli-expired-request", { caption: "Local 404 admin_device_expired fixture keeps unknown/expired/consumed codes fail-closed." });

  await page.goto(`/workers/${worker}`);
  const logs = page.locator("section").filter({ has: page.getByRole("heading", { name: "Recent redacted node logs", exact: true }) });
  await logs.getByRole("heading").scrollIntoViewIfNeeded();
  await expect(logs.getByRole("table", { name: "Redacted node events", exact: true })).toBeVisible();
  await expect(logs.getByText("Control-plane sync held", { exact: true })).toBeVisible();
  await expect(logs).toContainText("HTTP 503"); await expect(logs).toContainText("Partial coverage"); await expect(logs).toContainText("per process run");
  await expect(logs.getByRole("link", { name: mailbox, exact: true })).toHaveAttribute("href", new RegExp("/mailboxes\\?q=fixture-mailbox%40example.test"));
  await proof.chapter("Redacted operational evidence", "All node capture/log/offset responses are synthetic localhost fixtures. They do not establish production sampling, provider health, delivery or the cause of an arrival 503.");
  await logs.getByRole("table", { name: "Redacted node events", exact: true }).scrollIntoViewIfNeeded();
  await proof.shot("admin-node-redacted-evidence", { caption: "Synthetic capture and allowlisted events: timestamps, partial coverage, retention bounds, three known per-run drops, numeric control-plane HTTP 503 and native mailbox link." });
  await logs.getByRole("combobox", { name: "Log level", exact: true }).click();
  await page.getByRole("option", { name: "Warn", exact: true }).click();
  await logs.getByRole("textbox", { name: "Mailbox UUID", exact: true }).fill(mailbox);
  await logs.getByLabel("Before (local time)", { exact: true }).fill(before);
  await expect(logs.getByText("Coarse worker runtime error", { exact: true })).toHaveCount(0);
  await page.reload();
  await expect(logs.getByRole("textbox", { name: "Mailbox UUID", exact: true })).toHaveValue(mailbox);
  await expect(logs.getByLabel("Before (local time)", { exact: true })).toHaveValue(before);
  await expect(logs.getByRole("combobox", { name: "Log level", exact: true })).toContainText("Warn");
  await logs.getByRole("heading").scrollIntoViewIfNeeded();
  await proof.shot("admin-node-persistent-filters", { caption: "Local fixture filters survive refresh under the current admin and node. Window counts cover all retained events, not just the filtered subset." });

  const broker = page.locator("section").filter({ has: page.getByRole("heading", { name: "Broker committed-offset observations", exact: true }) });
  await broker.getByRole("heading").scrollIntoViewIfNeeded();
  await expect(broker).toContainText("Worker command scope"); await expect(broker).toContainText("Shared result-consumer scope");
  await expect(broker).toContainText("6 messages"); await expect(broker).toContainText("3 messages");
  await expect(broker).toContainText("consumer-group"); await expect(broker).toContainText("jobs.worker-events");
  await broker.getByText("Validated partition offsets", { exact: true }).first().click();
  await expect(broker.getByRole("table", { name: "Worker command scope partition offsets", exact: true })).toBeVisible();
  await proof.shot("admin-node-separate-broker-scopes", { caption: "Synthetic retained offsets earliest 10 / committed 16 / latest 22 yield command lag 6. Shared result lag 3 is separate, never added, attributed to this worker or labeled an outage/send rate." });
  unsupportedResults = true;
  await broker.getByRole("button", { name: "Refresh broker", exact: true }).click();
  await expect(broker).toContainText("This broker does not support"); await expect(broker).toContainText("Unknown / unavailable");
  await proof.shot("admin-node-unavailable-broker", { caption: "Synthetic unsupported shared-result observation remains unknown/unavailable while independent worker command evidence stays visible. No fabricated zero lag or healthy state." });

  await page.goto(`/workers/${consumer}`);
  await expect(logs.getByText("Log evidence unavailable", { exact: true })).toBeVisible();
  await expect(logs).toContainText("upgrade and re-enrollment");
  await expect(broker).toContainText("Command-group broker observations require a worker node");
  expect(consumerBrokerReads).toBe(0);
  await logs.getByRole("heading").scrollIntoViewIfNeeded();
  await proof.shot("admin-consumer-generic-evidence", { caption: "Local older-enrollment consumer fixture: generic log capture unavailable, retained event count unknown, no worker-command query and no inferred consumer lag." });
  await page.goto(`/workers/${worker}`);
  await expect(logs.getByRole("textbox", { name: "Mailbox UUID", exact: true })).toHaveValue(mailbox);
  await expect(logs.getByRole("combobox", { name: "Log level", exact: true })).toContainText("Warn");
});
