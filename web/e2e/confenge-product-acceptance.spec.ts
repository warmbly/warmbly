import { test, expect, type Page } from "@playwright/test";
import * as fs from "node:fs";
import * as path from "node:path";
import { fileURLToPath } from "node:url";

/**
 * Full Phase L/M path against a live stack with HARD asserts:
 * healthchecks → auth → import → confenge → approve (content_hash + APPROVED)
 * → edit (clears ApprovedContentHash, NEEDS_REVIEW)
 * → re-approve → queue (QUEUED)
 * → needs attention
 *
 * Opt-in: CONFENGE_E2E=1
 * Paths are repo-relative / env-based (no ephemeral /tmp/grok-* defaults).
 */
const enabled = process.env.CONFENGE_E2E === "1";
const API = process.env.CONFENGE_E2E_API || "http://127.0.0.1:18080";
const MAILPIT = process.env.CONFENGE_E2E_MAILPIT || "http://127.0.0.1:18025";
const ORG = process.env.CONFENGE_E2E_ORG || "22222222-0000-0000-0000-000000000001";
const WEB_BASE = process.env.CONFENGE_E2E_BASE_URL || "http://127.0.0.1:5173";
// web/e2e → repo root is ../.. (ESM-safe; no __dirname)
const REPO_ROOT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../..");
function resolveFeedPath(): string {
  const candidates = [
    process.env.CONFENGE_E2E_FEED,
    process.env.CONFENGE_E2E_FEED_FALLBACK,
    // CI uses the current contract fixture. Historical real slices intentionally
    // fail closed when they predate authoritative target-fit fields.
    path.join(REPO_ROOT, "internal/app/confenge/testdata/demo_3_companies.json"),
  ].filter((p): p is string => !!p);
  for (const p of candidates) {
    if (fs.existsSync(p)) return p;
  }
  return candidates[candidates.length - 1]!;
}
const FEED_PATH = resolveFeedPath();
const PROOF_DIR =
  process.env.CONFENGE_E2E_PROOF_DIR ||
  path.join(REPO_ROOT, "data/confenge-evidence");

let cachedFeedPayload: string | null = null;

function feedPayloadForImport(): string {
  if (cachedFeedPayload) return cachedFeedPayload;
  const raw = fs.readFileSync(FEED_PATH, "utf8");
  if (path.basename(FEED_PATH) !== "demo_3_companies.json") {
    cachedFeedPayload = raw;
    return raw;
  }

  const now = new Date().toISOString();
  const nonce = Date.now().toString(36);
  const feed = JSON.parse(raw) as {
    generated_at: string;
    source: { run_id: string; snapshot_hash: string };
    leads: Array<{
      target_fit_computed_at?: string;
      target_fit_source_watermark?: string;
      company?: { cnpj14?: string };
      contacts?: Array<{
        email?: string;
        source_url?: string;
        source_date?: string;
        mailbox_purpose?: string;
        ownership_status?: string;
        recipient_commercial_suitability?: string;
        provenance_chain_valid?: boolean;
        derived_from_fixture?: boolean;
      }>;
    }>;
  };
  feed.generated_at = now;
  feed.source.run_id = `confenge-e2e-${nonce}`;
  feed.source.snapshot_hash = `confenge-e2e-snapshot-${nonce}`;
  for (const [leadIndex, lead] of feed.leads.entries()) {
    lead.target_fit_computed_at = now;
    lead.target_fit_source_watermark = now;
    if (lead.company?.cnpj14) {
      // 77-prefix never collides with seed 11… ACME leftovers.
      const stamp = Date.now().toString().slice(-8).padStart(8, "0");
      lead.company.cnpj14 = `77${stamp}${String(leadIndex).padStart(4, "0")}`;
    }
    for (const [contactIndex, contact] of (lead.contacts || []).entries()) {
      const local = contactIndex === 0 ? `confenge-ci-${leadIndex + 1}` : `confenge-ci-${leadIndex + 1}-${contactIndex + 1}`;
      contact.email = `${local}@acmeobras.com.br`;
      contact.source_url = `https://acmeobras.com.br/equipe/${local}`;
      contact.source_date = now.slice(0, 10);
      contact.mailbox_purpose = contact.name ? "COMERCIAL" : "GENERIC_CONTACT";
      contact.ownership_status = "COMPANY_OWNED";
      contact.recipient_commercial_suitability = contact.name ? "SUITABLE" : "SUITABLE_GENERIC";
      contact.provenance_chain_valid = true;
      contact.derived_from_fixture = false;
    }
  }
  cachedFeedPayload = JSON.stringify(feed);
  return cachedFeedPayload;
}

type Touchpoint = {
  id: string;
  state?: string;
  content_hash?: string;
  approved_content_hash?: string;
  body_text?: string;
  recipient?: string;
  subject?: string;
  account_id?: string;
  draft_id?: string;
};

async function loginViaAPIAndMailpit(): Promise<Record<string, string>> {
  const email = process.env.CONFENGE_E2E_EMAIL || "dev@warmbly.com";
  const password = process.env.CONFENGE_E2E_PASSWORD || "password123";

  const startRes = await fetch(`${API}/v1/auth/login`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ email, password }),
  });
  if (!startRes.ok) {
    throw new Error(`login start ${startRes.status}: ${await startRes.text()}`);
  }
  const start = (await startRes.json()) as {
    session?: string;
    code_required?: boolean;
    token?: Record<string, string>;
    access_token?: string;
  };

  // After #99, self-host / BILLING_PROVIDER=none defaults AUTH_LOGIN_CODE=off,
  // so LoginStart already returns the token pair and never mails a code.
  let tokens: Record<string, string>;
  if (start.token?.access_token) {
    tokens = start.token;
  } else if (start.access_token) {
    tokens = start as Record<string, string>;
  } else {
    let code = "";
    for (let i = 0; i < 40; i++) {
      const listRes = await fetch(`${MAILPIT}/api/v1/messages`);
      const list = (await listRes.json()) as {
        messages?: Array<{ ID: string; Subject?: string }>;
      };
      const msg = (list.messages || []).find((m) =>
        (m.Subject || "").includes("Login Code"),
      );
      if (msg) {
        const bodyRes = await fetch(`${MAILPIT}/api/v1/message/${msg.ID}`);
        const body = (await bodyRes.json()) as { Text?: string; HTML?: string };
        const text = body.Text || body.HTML || "";
        const m = text.match(/\b(\d{6})\b/);
        if (m) {
          code = m[1];
          break;
        }
      }
      await new Promise((r) => setTimeout(r, 300));
    }
    if (!code) throw new Error("no login code in Mailpit");

    const confirmRes = await fetch(`${API}/v1/auth/login/confirm`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ code, session: start.session }),
    });
    if (!confirmRes.ok) {
      throw new Error(`login confirm ${confirmRes.status}: ${await confirmRes.text()}`);
    }
    tokens = (await confirmRes.json()) as Record<string, string>;
  }

  const sw = await fetch(`${API}/v1/organization/switch/${ORG}`, {
    method: "POST",
    headers: { Authorization: `Bearer ${tokens.access_token}` },
  });
  if (!sw.ok) {
    throw new Error(`org switch ${sw.status}: ${await sw.text()}`);
  }
  return tokens;
}

async function injectTokens(page: Page, tokens: Record<string, string>) {
  // Use /login so we have an origin for localStorage, then stamp auth + org.
  // Full seed creates multiple orgs; without currentOrganization in
  // warmbly-storage, OrgGate redirects to /select-org and /app/confenge never
  // mounts (CI multi-org; local single-org can mask this).
  await page.goto("/login");
  await page.evaluate(
    ({ t, orgId }) => {
      localStorage.setItem("access_token", t.access_token);
      localStorage.setItem("access_token_expires_at", t.access_token_expires_at);
      localStorage.setItem("refresh_token", t.refresh_token);
      localStorage.setItem("refresh_token_expires_at", t.refresh_token_expires_at);
      localStorage.setItem(
        "auth_token",
        JSON.stringify({
          access_token: t.access_token,
          access_token_expires_at: t.access_token_expires_at,
          refresh_token: t.refresh_token,
          refresh_token_expires_at: t.refresh_token_expires_at,
        }),
      );
      // zustand persist shape for organization selection
      const storageKey = "warmbly-storage";
      let prev: Record<string, unknown> = {};
      try {
        prev = JSON.parse(localStorage.getItem(storageKey) || "{}") || {};
      } catch {
        prev = {};
      }
      const state = (prev.state as Record<string, unknown>) || prev;
      const nextState = {
        ...state,
        currentOrganization: {
          id: orgId,
          name: "CONFENGE E2E",
        },
      };
      localStorage.setItem(
        storageKey,
        JSON.stringify({ state: nextState, version: prev.version ?? 0 }),
      );
    },
    { t: tokens, orgId: ORG },
  );
}

async function apiJSON<T>(
  token: string,
  method: string,
  urlPath: string,
  body?: unknown,
): Promise<T> {
  const res = await fetch(`${API}${urlPath}`, {
    method,
    headers: {
      Authorization: `Bearer ${token}`,
      "Content-Type": "application/json",
    },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  const text = await res.text();
  if (!res.ok) {
    throw new Error(`${method} ${urlPath} → ${res.status}: ${text.slice(0, 400)}`);
  }
  return (text ? JSON.parse(text) : {}) as T;
}

async function getTouchpoint(token: string, id: string): Promise<Touchpoint> {
  const j = await apiJSON<{ data: Touchpoint }>(
    token,
    "GET",
    `/v1/confenge/touchpoints/${id}`,
  );
  return j.data;
}

function usableTP(t: Touchpoint | undefined): boolean {
  if (!t?.id) return false;
  const st = (t.state || "").toUpperCase();
  // Open for human review/edit (APPROVED can be re-edited to invalidate).
  if (!["DUE", "DRAFTED", "NEEDS_REVIEW", "APPROVED", "PLANNED"].includes(st)) {
    return false;
  }
  if (st === "PLANNED") return !!(t.recipient || "").trim();
  // Body + recipient are enough for approve/edit invalidation. draft_id is
  // required only for queue/transport (ensureReviewTouchpoint links it).
  return !!(t.body_text || "").trim() && !!(t.recipient || "").trim();
}

/** Ensure touchpoint has a linked draft so queue/transport can enroll. */
async function ensureDraftLinked(token: string, tp: Touchpoint): Promise<Touchpoint> {
  let full = await getTouchpoint(token, tp.id);
  if (full.draft_id) return full;
  let st = (full.state || "").toUpperCase();
  // APPROVED cannot generate until re-opened via edit.
  if (st === "APPROVED" && (full.body_text || "").trim()) {
    const ed = await apiJSON<{ data: Touchpoint }>(
      token,
      "POST",
      `/v1/confenge/touchpoints/${tp.id}/edit`,
      {
        subject: full.subject || "CONFENGE",
        body_text: (full.body_text || "").replace(/\s+$/, "") + " [draft-link]",
        recipient: full.recipient,
      },
    );
    full = ed.data;
    st = (full.state || "").toUpperCase();
    if (full.draft_id) return full;
  }
  if (["DUE", "DRAFTED", "NEEDS_REVIEW", "PLANNED"].includes(st)) {
    const gen = await fetch(`${API}/v1/confenge/touchpoints/${tp.id}/generate`, {
      method: "POST",
      headers: {
        Authorization: `Bearer ${token}`,
        "Content-Type": "application/json",
      },
      body: "{}",
    });
    if (gen.ok) {
      full = ((await gen.json()) as { data: Touchpoint }).data;
      if (full.draft_id) return full;
    }
  }
  return getTouchpoint(token, tp.id);
}

async function preparePilotReviewTouchpoint(token: string): Promise<{
  accountId: string;
  touchpointId: string;
}> {
  const feed = JSON.parse(feedPayloadForImport()) as {
    leads?: Array<{ company?: { cnpj14?: string } }>;
  };
  const importedCNPJs = new Set(
    (feed.leads || []).map((lead) => lead.company?.cnpj14 || "").filter(Boolean),
  );
  const accounts = await apiJSON<{ data?: Array<{ id: string; cnpj14?: string }> }>(
    token,
    "GET",
    "/v1/confenge/accounts?limit=100",
  );
  let lastBlock = "no account returned by the current feed";
  const blocks: string[] = [];
  for (const account of (accounts.data || []).filter((item) => importedCNPJs.has(item.cnpj14 || ""))) {
    const res = await fetch(`${API}/v1/confenge/pilot/cohort/prepare`, {
      method: "POST",
      headers: {
        Authorization: `Bearer ${token}`,
        "Content-Type": "application/json",
        "Idempotency-Key": `confenge-e2e-${account.id}`,
      },
      body: JSON.stringify({ account_ids: [account.id] }),
    });
    const text = await res.text();
    if (!res.ok) {
      lastBlock = `account ${account.id}: HTTP ${res.status} ${text.slice(0, 200)}`;
      blocks.push(lastBlock);
      continue;
    }
    const cohort = JSON.parse(text) as {
      data?: {
        results?: Array<{
          status?: string;
          reason_code?: string;
          touchpoint_id?: string;
        }>;
      };
    };
    const result = cohort.data?.results?.[0];
    if ((result?.status || "").toUpperCase() !== "PREPARED" || !result?.touchpoint_id) {
      lastBlock = `account ${account.id}: ${result?.reason_code || "not_prepared"}`;
      blocks.push(lastBlock);
      continue;
    }
    let touchpoint = await getTouchpoint(token, result.touchpoint_id);
    if ((touchpoint.state || "").toUpperCase() !== "NEEDS_REVIEW") {
      try {
        const gen = await apiJSON<{ data: Touchpoint }>(
          token,
          "POST",
          `/v1/confenge/touchpoints/${result.touchpoint_id}/generate`,
          {},
        );
        touchpoint = gen.data;
      } catch {
        // generate is illegal on QUEUED/SENT; try the next imported account
      }
    }
    if ((touchpoint.state || "").toUpperCase() !== "NEEDS_REVIEW") {
      lastBlock = `account ${account.id}: prepared touchpoint is ${touchpoint.state || "unknown"}`;
      blocks.push(lastBlock);
      continue;
    }
    await isolateReviewTouchpoint(token, result.touchpoint_id);
    return { accountId: account.id, touchpointId: result.touchpoint_id };
  }
  throw new Error(
    `pilot cohort could not prepare a review touchpoint: ${blocks.join("; ") || lastBlock}`,
  );
}

/** Plan + generate until a NEEDS_REVIEW touch sits alone at the front of the review queue. */
async function ensureReviewTouchpoint(token: string): Promise<{
  accountId: string;
  touchpointId: string;
}> {
  // Approval and dispatch are pilot-only. Always enter through the durable
  // cohort transaction so stale pre-import touchpoints cannot seed acceptance.
  try {
    return await preparePilotReviewTouchpoint(token);
  } catch (error) {
    if (process.env.CONFENGE_E2E_LEGACY_FALLBACK !== "1") throw error;
  }

  // This opt-in fallback is diagnostic only. CI and product acceptance never
  // approve touchpoints that were not claimed by the pilot cohort transaction.
  // Reuse an already-reviewable touch if present (with body).
  const existing = await apiJSON<{ data?: Touchpoint[] }>(
    token,
    "GET",
    "/v1/confenge/touchpoints/review?limit=50",
  );
  const reusable = (existing.data || []).find((t) => usableTP(t));
  if (reusable) {
    await ensureDraftLinked(token, reusable);
    await isolateReviewTouchpoint(token, reusable.id);
    let accountId = reusable.account_id || "";
    if (!accountId) {
      const full = await getTouchpoint(token, reusable.id);
      accountId = full.account_id || "";
    }
    return {
      accountId,
      touchpointId: reusable.id,
    };
  }

  const preferred = process.env.CONFENGE_E2E_ACCOUNT_ID || "";
  const candidates: Array<{ id: string; queue_state?: string }> = preferred
    ? [{ id: preferred }]
    : [];
  // Prefer open review work; READY_TO_GENERATE is ideal but NEEDS_REVIEW /
  // APPROVED accounts with re-editable drafts also seed the path.
  for (const qs of [
    "READY_TO_GENERATE",
    "NEEDS_CONTACT",
    "NEEDS_REVIEW",
    "APPROVED",
    "ENROLLED",
  ]) {
    if (candidates.length >= 30) break;
    const res = await apiJSON<{ data?: Array<{ id: string; queue_state?: string }> }>(
      token,
      "GET",
      `/v1/confenge/accounts?queue_state=${qs}&limit=30`,
    );
    for (const a of res.data || []) {
      if (!candidates.some((c) => c.id === a.id)) candidates.push(a);
    }
  }
  if (!candidates.length) {
    const all = await apiJSON<{ data?: Array<{ id: string; queue_state?: string }> }>(
      token,
      "GET",
      "/v1/confenge/accounts?limit=100",
    );
    for (const a of all.data || []) {
      const qs = (a.queue_state || "").toUpperCase();
      if (
        [
          "READY_TO_GENERATE",
          "NEEDS_CONTACT",
          "NEEDS_REVIEW",
          "APPROVED",
          "ENROLLED",
        ].includes(qs)
      ) {
        candidates.push(a);
      }
    }
  }
  if (!candidates.length) {
    throw new Error(
      "no confenge account available for review seeding (READY/NEEDS_REVIEW/APPROVED/ENROLLED empty)",
    );
  }

  let lastErr = "no candidate worked";
  for (const acc of candidates.slice(0, 15)) {
    const accountId = acc.id;
    try {
      await apiJSON(token, "POST", `/v1/confenge/accounts/${accountId}/plan`, {
        channel: "EMAIL",
      });
      const tps = await apiJSON<{ data?: Touchpoint[] }>(
        token,
        "GET",
        `/v1/confenge/accounts/${accountId}/touchpoints`,
      );
      const list = tps.data || [];
      const first =
        list.find((t) => {
          const st = (t.state || "").toUpperCase();
          return (
            st === "DUE" ||
            st === "NEEDS_REVIEW" ||
            st === "DRAFTED" ||
            st === "APPROVED" ||
            st === "PLANNED"
          );
        }) || list[0];
      if (!first?.id) {
        lastErr = `account ${accountId}: no touchpoint after plan`;
        continue;
      }

      let tp = await getTouchpoint(token, first.id);
      // Resolve an enrollable recipient from account candidates when touchpoint has none.
      let seedRecipient = (tp.recipient || "").trim();
      if (!seedRecipient) {
        try {
          const detail = await apiJSON<{
            data?: { contacts?: Array<{ email?: string; verification_status?: string }> };
          }>(token, "GET", `/v1/confenge/accounts/${accountId}`);
          const contacts = detail.data?.contacts || [];
          const enrollable = contacts.find(
            (c) =>
              (c.email || "").includes("@") &&
              !["CANDIDATE_UNVERIFIED", "NOT_FOUND", "INVALID", "BOUNCED", "DO_NOT_CONTACT"].includes(
                (c.verification_status || "").toUpperCase(),
              ),
          );
          seedRecipient =
            enrollable?.email ||
            contacts.find((c) => (c.email || "").includes("@"))?.email ||
            "";
        } catch {
          /* best-effort */
        }
      }
      if (!(tp.body_text || "").trim()) {
        const gen = await fetch(`${API}/v1/confenge/touchpoints/${first.id}/generate`, {
          method: "POST",
          headers: {
            Authorization: `Bearer ${token}`,
            "Content-Type": "application/json",
          },
          body: "{}",
        });
        if (gen.ok) {
          tp = ((await gen.json()) as { data: Touchpoint }).data;
        } else {
          lastErr = `generate ${first.id}: ${gen.status} ${(await gen.text()).slice(0, 120)}`;
        }
      }
      // Always force a human-editable body+recipient when generate leaves them empty
      // (template/AI path may no-op in CI without provider; edit is the product path).
      if (!(tp.body_text || "").trim() || !(tp.recipient || "").trim()) {
        try {
          const forcedBody =
            (tp.body_text || "").trim() ||
            "Mensagem CONFENGE com fato público e CTA para revisão humana. Podemos conversar sobre o recorte contratual observado?";
          const forcedRecipient =
            (tp.recipient || "").trim() ||
            seedRecipient ||
            `confenge-pilot+${accountId.slice(0, 8)}@warmbly.local`;
          tp = (
            await apiJSON<{ data: Touchpoint }>(
              token,
              "POST",
              `/v1/confenge/touchpoints/${first.id}/edit`,
              {
                subject: tp.subject || "CONFENGE",
                body_text: forcedBody,
                recipient: forcedRecipient,
              },
            )
          ).data;
        } catch (e) {
          lastErr = `force-edit empty touchpoint ${first.id}: ${e}`;
          continue;
        }
      }

      if (!(tp.body_text || "").trim() || !(tp.recipient || "").trim()) {
        lastErr = `touchpoint ${first.id} still empty after generate+force-edit`;
        continue;
      }
      tp = await ensureDraftLinked(token, tp);
      if (!tp.draft_id) {
        lastErr = `touchpoint ${first.id} has body but no draft_id after generate`;
        // Still usable for approve/edit; queue may fail — prefer next candidate.
        // Fall through only if we must prove queue; keep searching for drafted TPs.
        continue;
      }

      // Confirm it surfaces on the review list.
      let onReview = false;
      for (let i = 0; i < 10; i++) {
        const rev = await apiJSON<{ data?: Touchpoint[] }>(
          token,
          "GET",
          "/v1/confenge/touchpoints/review?limit=50",
        );
        onReview = !!(rev.data || []).some((t) => t.id === first.id);
        if (onReview) break;
        await new Promise((r) => setTimeout(r, 200));
      }
      if (!onReview) {
        lastErr = `touchpoint ${first.id} not in review queue after generate`;
        continue;
      }

      await isolateReviewTouchpoint(token, first.id);
      return { accountId, touchpointId: first.id };
    } catch (e) {
      lastErr = String(e);
    }
  }
  throw new Error(`ensureReviewTouchpoint failed: ${lastErr}`);
}

/** Skip other open review items so the UI index-0 editor is our seeded touchpoint. */
async function isolateReviewTouchpoint(token: string, keepId: string) {
  for (let pass = 0; pass < 3; pass++) {
    const rev = await apiJSON<{ data?: Touchpoint[] }>(
      token,
      "GET",
      "/v1/confenge/touchpoints/review?limit=50",
    );
    const others = (rev.data || []).filter((t) => t.id !== keepId);
    if (!others.length) {
      // Ensure keep is first (only).
      const first = (rev.data || [])[0];
      if (first && first.id === keepId) return;
      if (!first) throw new Error("review queue empty after isolate");
    }
    for (const t of others) {
      try {
        await apiJSON(token, "POST", `/v1/confenge/touchpoints/${t.id}/decision`, {
          action: "skip",
        });
      } catch {
        // Best-effort; non-skippable states are ignored.
      }
    }
  }
  const final = await apiJSON<{ data?: Touchpoint[] }>(
    token,
    "GET",
    "/v1/confenge/touchpoints/review?limit=10",
  );
  const list = final.data || [];
  if (!list.some((t) => t.id === keepId)) {
    throw new Error(`seeded touchpoint ${keepId} missing from review after isolate`);
  }
  // Prefer keep at index 0; if not, skip anything before it once more.
  while (list.length && list[0].id !== keepId) {
    try {
      await apiJSON(token, "POST", `/v1/confenge/touchpoints/${list[0].id}/decision`, {
        action: "skip",
      });
    } catch {
      break;
    }
    const again = await apiJSON<{ data?: Touchpoint[] }>(
      token,
      "GET",
      "/v1/confenge/touchpoints/review?limit=10",
    );
    list.splice(0, list.length, ...(again.data || []));
    if (!list.length) break;
  }
}

async function healthcheckOrThrow(): Promise<void> {
  /** Fail closed before browser work if stack is not actually up. */
  const checks: Array<{ name: string; url: string; ok: (r: Response) => boolean }> = [
    {
      name: "api",
      url: `${API}/v1/confenge/status`,
      ok: (r) => r.status < 500,
    },
    {
      name: "mailpit",
      url: `${MAILPIT}/api/v1/info`,
      ok: (r) => r.ok,
    },
    {
      name: "web",
      url: WEB_BASE,
      ok: (r) => r.status < 500,
    },
  ];
  const failures: string[] = [];
  for (const c of checks) {
    try {
      const r = await fetch(c.url, { signal: AbortSignal.timeout(8_000) });
      if (!c.ok(r)) failures.push(`${c.name}=HTTP ${r.status} (${c.url})`);
    } catch (e) {
      failures.push(`${c.name}=unreachable (${c.url}): ${e}`);
    }
  }
  if (failures.length) {
    throw new Error(
      `CONFENGE E2E healthchecks failed (do not wait for browser timeout):\n  - ${failures.join("\n  - ")}`,
    );
  }
}

test.describe("CONFENGE product acceptance UI", () => {
  test.skip(!enabled, "Set CONFENGE_E2E=1 with backend + web + CONFENGE enabled");

  test("import, approve hash, edit invalidates, re-approve queues", async ({ page }) => {
    await healthcheckOrThrow();

    const tokens = await loginViaAPIAndMailpit();
    // Complete onboarding via API (referral_source is required server-side).
    // Without this, UserProvider permanently redirects to /onboarding and the
    // CONFENGE UI never mounts in multi-step CI seed environments.
    const onboardRes = await fetch(`${API}/v1/auth/me/onboarding`, {
      method: "PATCH",
      headers: {
        Authorization: `Bearer ${tokens.access_token}`,
        "Content-Type": "application/json",
      },
      body: JSON.stringify({
        first_name: "Dev",
        last_name: "User",
        referral_source: "other",
        role: "founder",
        team_size: "just_me",
      }),
    });
    if (!onboardRes.ok && onboardRes.status !== 204) {
      // Already completed is fine; surface other failures for CI debugging.
      const t = await onboardRes.text();
      if (!/already|completed/i.test(t)) {
        throw new Error(`onboarding ${onboardRes.status}: ${t.slice(0, 300)}`);
      }
    }

    // Import real feed (idempotent)
    if (!fs.existsSync(FEED_PATH)) {
      throw new Error(`feed missing: ${FEED_PATH}`);
    }
    const importRes = await fetch(`${API}/v1/confenge/import`, {
      method: "POST",
      headers: {
        Authorization: `Bearer ${tokens.access_token}`,
        "Content-Type": "application/json",
        "Idempotency-Key": `e2e-import-hard-${Date.now()}`,
      },
      body: feedPayloadForImport(),
    });
    const importText = await importRes.text();
    expect(importRes.ok, `import failed: ${importRes.status} ${importText.slice(0, 400)}`).toBeTruthy();
    // Sanity: at least one confenge account exists after real-feed import.
    const postImport = await apiJSON<{ data?: Array<{ id: string; queue_state?: string }> }>(
      tokens.access_token,
      "GET",
      "/v1/confenge/accounts?limit=50",
    );
    expect(
      (postImport.data || []).length,
      `import returned ok but no accounts; body=${importText.slice(0, 300)}`,
    ).toBeGreaterThan(0);

    const seeded = await ensureReviewTouchpoint(tokens.access_token);
    expect(seeded.touchpointId).toBeTruthy();

    // Precondition: API review queue exposes seeded TP with body.
    const preReview = await apiJSON<{ data?: Touchpoint[] }>(
      tokens.access_token,
      "GET",
      "/v1/confenge/touchpoints/review?limit=10",
    );
    expect((preReview.data || []).some((t) => t.id === seeded.touchpointId)).toBeTruthy();
    const preTP = await getTouchpoint(tokens.access_token, seeded.touchpointId);
    expect((preTP.body_text || "").trim().length).toBeGreaterThan(10);
    expect((preTP.recipient || "").trim().length).toBeGreaterThan(3);

    await injectTokens(page, tokens);
    await page.goto("/app");
    // Onboarding first-name gate (CI seed user often still needs this; API PATCH alone is not enough).
    const welcome = page.getByRole("heading", { name: /Welcome to Warmbly/i });
    const firstName = page.getByPlaceholder("John");
    try {
      await welcome.or(firstName).first().waitFor({ state: "visible", timeout: 15_000 });
    } catch {
      /* already past onboarding */
    }
    if (await firstName.isVisible().catch(() => false)) {
      await firstName.fill("Dev");
      await page.getByPlaceholder("Doe").fill("User");
      await page.getByRole("button", { name: /Continue/i }).click();
      // Wait until onboarding leaves (welcome heading gone).
      await welcome.waitFor({ state: "hidden", timeout: 20_000 }).catch(() => undefined);
    }
    // Multi-org select-org fallback if OrgGate still redirected
    if (page.url().includes("select-org")) {
      const orgRow = page.getByText(/CONFENGE|Sunrise|Dev|Organization/i).first();
      if ((await orgRow.count()) > 0) {
        await orgRow.click();
      }
      const continueBtn = page.getByRole("button", { name: /Continue|Select|Open/i }).first();
      if ((await continueBtn.count()) > 0 && (await continueBtn.isVisible().catch(() => false))) {
        await continueBtn.click();
      }
    }

    await page.goto("/app/confenge");
    // If onboarding re-intercepts, complete it again then re-enter confenge.
    if (await firstName.isVisible().catch(() => false)) {
      await firstName.fill("Dev");
      await page.getByPlaceholder("Doe").fill("User");
      await page.getByRole("button", { name: /Continue/i }).click();
      await welcome.waitFor({ state: "hidden", timeout: 20_000 }).catch(() => undefined);
      await page.goto("/app/confenge");
    }
    await expect(
      page.getByText(/CONFENGE/i).or(page.getByTestId("confenge-dispatch-quota")).first(),
    ).toBeVisible({ timeout: 45_000 });
    await expect(page.getByTestId("confenge-dispatch-quota")).toBeVisible();
    await expect(page.getByTestId("confenge-review-queue")).toBeVisible();
    await expect(page.getByTestId("confenge-needs-attention")).toBeVisible();
    // Summary strip (CI static gate requires this testid string in the E2E spec).
    await expect(page.getByTestId("confenge-stat-sent")).toBeVisible();

    const body = page.getByTestId("confenge-body-input");
    await expect(body).toBeVisible({ timeout: 45_000 });
    await expect(page.getByTestId("confenge-evidence")).toBeVisible();
    await expect(page.getByTestId("confenge-recipient")).toBeVisible();
    await expect(page.getByTestId("confenge-company")).toBeVisible();
    await expect(page.getByText("Por que esta conta", { exact: true })).toBeVisible();

    const beforeEdit = await body.inputValue();
    expect(beforeEdit.trim().length).toBeGreaterThan(10);
    // Keep body under MaxEmailParagraphs (5): append on last line, no blank lines.
    const markPass1 = " [pass1]";
    const bodyPass1 = beforeEdit.replace(/\s+$/, "") + markPass1;

    // ── Hard path A: API approve → edit clears hash → re-approve → queue ──
    // (authoritative SM proof; independent of UI transport toast noise)
    let tp = await ensureDraftLinked(
      tokens.access_token,
      await getTouchpoint(tokens.access_token, seeded.touchpointId),
    );
    expect(tp.draft_id, "queue requires draft_id; generate must link a draft").toBeTruthy();

    tp = (
      await apiJSON<{ data: Touchpoint }>(
        tokens.access_token,
        "POST",
        `/v1/confenge/touchpoints/${seeded.touchpointId}/edit`,
        {
          subject: tp.subject,
          body_text: bodyPass1,
          recipient: tp.recipient,
        },
      )
    ).data;
    expect((tp.state || "").toUpperCase()).toBe("NEEDS_REVIEW");
    expect(tp.draft_id).toBeTruthy();

    tp = (
      await apiJSON<{ data: Touchpoint }>(
        tokens.access_token,
        "POST",
        `/v1/confenge/touchpoints/${seeded.touchpointId}/approve`,
        { generic_recipient_acknowledged: true },
      )
    ).data;
    const st1 = (tp.state || "").toUpperCase();
    expect(st1).toBe("APPROVED");
    expect((tp.content_hash || "").length).toBeGreaterThanOrEqual(32);
    expect((tp.approved_content_hash || "").length).toBeGreaterThanOrEqual(32);
    expect(tp.approved_content_hash).toBe(tp.content_hash);
    const hashAfterApprove = tp.approved_content_hash as string;

    const editedBody = bodyPass1.replace(/\s+$/, "") + " [edited]";
    tp = (
      await apiJSON<{ data: Touchpoint }>(
        tokens.access_token,
        "POST",
        `/v1/confenge/touchpoints/${seeded.touchpointId}/edit`,
        { body_text: editedBody },
      )
    ).data;
    expect((tp.state || "").toUpperCase()).toBe("NEEDS_REVIEW");
    expect(tp.approved_content_hash || "").toBe("");
    expect(tp.content_hash).not.toBe(hashAfterApprove);
    expect((tp.content_hash || "").length).toBeGreaterThanOrEqual(32);
    // Edit must not wipe draft_id (transport needs it after re-approve).
    expect(tp.draft_id).toBeTruthy();

    tp = (
      await apiJSON<{ data: Touchpoint }>(
        tokens.access_token,
        "POST",
        `/v1/confenge/touchpoints/${seeded.touchpointId}/approve`,
        { generic_recipient_acknowledged: true },
      )
    ).data;
    expect((tp.state || "").toUpperCase()).toBe("APPROVED");
    expect(tp.approved_content_hash).toBe(tp.content_hash);

    const approvedSubject = (tp.subject || "").trim();
    const approvedBody = (tp.body_text || "").trim();
    const approvedRecipient = (tp.recipient || "").trim();
    const approvedHash = tp.approved_content_hash as string;

    tp = (
      await apiJSON<{ data: Touchpoint }>(
        tokens.access_token,
        "POST",
        `/v1/confenge/touchpoints/${seeded.touchpointId}/queue`,
        {},
      )
    ).data;
    const st3 = (tp.state || "").toUpperCase();
    // Local enroll may land SENT immediately; governor may leave QUEUED.
    expect(["QUEUED", "SENT"]).toContain(st3);

    // ── Phase H: product queue→SMTP→Mailpit exact delivery ──
    // Product path: dispatchEmailTouch enrolls then deliverApprovedSMTP when
    // SMTP_HOST is set (CI confenge job configures Mailpit). No test-side SMTP.
    const normalize = (s: string) =>
      s.replace(/\r\n/g, "\n").replace(/\s+$/gm, "").trim();
    const wantBody = normalize(approvedBody);
    const wantSubject = normalize(approvedSubject);
    type MailpitMsg = {
      ID: string;
      Subject?: string;
      To?: Array<{ Address?: string }>;
    };
    let mailpitMatch: {
      id: string;
      subject: string;
      body: string;
      to: string;
    } | null = null;
    // After queue, product may have already SMTP'd; poll for exact approved payload.
    for (let i = 0; i < 50; i++) {
      const listRes = await fetch(`${MAILPIT}/api/v1/messages`);
      const list = (await listRes.json()) as { messages?: MailpitMsg[] };
      for (const m of list.messages || []) {
        const subj = normalize(m.Subject || "");
        if (/Login Code|password|verify/i.test(subj)) continue;
        if (wantSubject && subj !== wantSubject) continue;
        const bodyRes = await fetch(`${MAILPIT}/api/v1/message/${m.ID}`);
        const bodyJ = (await bodyRes.json()) as { Text?: string; HTML?: string };
        const got = normalize(bodyJ.Text || bodyJ.HTML || "");
        // Exact body after CRLF normalize (product MIME text/plain).
        if (wantBody.length > 10 && got === wantBody) {
          const to = (m.To || []).map((t) => t.Address || "").join(",");
          // Recipient is approved recipient or CONFENGE_SMTP_SINK_EMAIL rewrite.
          const expectTo = (
            process.env.CONFENGE_SMTP_SINK_EMAIL ||
            process.env.CONFENGE_E2E_SINK_EMAIL ||
            approvedRecipient
          ).toLowerCase();
          if (expectTo && !to.toLowerCase().includes(expectTo.split("@")[0] || expectTo)) {
            // still accept if body/subject exact (sink rewrite may change host)
            if (!to) continue;
          }
          mailpitMatch = { id: m.ID, subject: subj, body: got, to };
          break;
        }
      }
      if (mailpitMatch) break;
      await new Promise((r) => setTimeout(r, 300));
    }
    expect(
      mailpitMatch,
      "product queue must SMTP approved content to Mailpit (set SMTP_HOST; no test-side SMTP)",
    ).toBeTruthy();
    expect(mailpitMatch!.subject).toBe(wantSubject);
    expect(mailpitMatch!.body).toBe(wantBody);

    // ── UI path B: surface still works (second isolated review TP) ──
    const uiSeed = await ensureReviewTouchpoint(tokens.access_token);
    await page.reload();
    await expect(page.getByTestId("confenge-body-input")).toBeVisible({
      timeout: 45_000,
    });
    const uiBody = page.getByTestId("confenge-body-input");
    const uiText = (await uiBody.inputValue()).replace(/\s+$/, "") + " [ui]";
    await uiBody.fill(uiText);
    const genericAcknowledgement = page.getByText(
      /Confirmei que a mensagem trata esta caixa como canal institucional/i,
    );
    if (await genericAcknowledgement.isVisible().catch(() => false)) {
      await genericAcknowledgement.click();
    }
    const approve = page.getByTestId("confenge-approve");
    await expect(approve).toBeEnabled({ timeout: 10_000 });
    await approve.click();
    await expect
      .poll(
        async () => {
          const t = await getTouchpoint(tokens.access_token, uiSeed.touchpointId);
          return (t.state || "").toUpperCase();
        },
        { timeout: 25_000 },
      )
      .toMatch(/^(APPROVED|QUEUED|SENT)$/);
    const uiTP = await getTouchpoint(tokens.access_token, uiSeed.touchpointId);
    expect((uiTP.approved_content_hash || "").length).toBeGreaterThanOrEqual(32);
    expect(uiTP.approved_content_hash).toBe(uiTP.content_hash);

    // Needs attention surface
    await expect(page.getByTestId("confenge-needs-attention")).toBeVisible();
    const needsBtn = page.getByRole("button", { name: /Precisa de atenção/i });
    if ((await needsBtn.count()) > 0) {
      await needsBtn.click();
    }

    // Reply path (status < 500)
    if (seeded.accountId) {
      const reply = await page.request.post(
        `${API}/v1/confenge/accounts/${seeded.accountId}/generate-reply`,
        {
          headers: { Authorization: `Bearer ${tokens.access_token}` },
          data: {},
        },
      );
      expect(reply.status()).toBeLessThan(500);
    }

    // Persist hard-assert proof (current-run evidence for readiness gate)
    fs.mkdirSync(PROOF_DIR, { recursive: true });
    const codeSha =
      process.env.CONFENGE_GATE_CODE_SHA ||
      process.env.GITHUB_SHA ||
      "";
    const proof = {
      pass: true,
      result: "PASS",
      hard_asserts: true,
      generated_at: new Date().toISOString(),
      at: new Date().toISOString(),
      code_sha: codeSha || undefined,
      tested_sha: codeSha || undefined,
      command: "playwright confenge-product-acceptance",
      test_id: "import-approve-edit-invalidate-queue-mailpit",
      touchpoint_id: seeded.touchpointId,
      account_id: seeded.accountId,
      after_approve: {
        state: st1,
        content_hash_len: (hashAfterApprove || "").length,
        approved_matches_content: true,
        approved_content_hash: approvedHash,
        subject: approvedSubject,
        body_len: approvedBody.length,
        recipient: approvedRecipient,
      },
      after_edit: {
        state: "NEEDS_REVIEW",
        approved_content_hash_cleared: true,
        content_hash_changed: true,
        not_valid_approved_for_send: true,
      },
      after_reapprove_queue: {
        state: st3,
        approved_matches_content: true,
        queued_or_sent: true,
      },
      mailpit_exact_delivery: {
        required_when_sent: true,
        transport_state: st3,
        matched: !!mailpitMatch,
        mailpit_message_id: mailpitMatch?.id || null,
        approved_subject: approvedSubject,
        mailpit_subject: mailpitMatch?.subject || null,
        approved_body_prefix: approvedBody.slice(0, 120),
        body_match: !!mailpitMatch,
        recipient_meta: approvedRecipient,
        mailpit_to: mailpitMatch?.to || null,
        note: "Body must match approved content; recipient may be controlled sink",
      },
      ui_approve_queue: {
        touchpoint_id: uiSeed.touchpointId,
        state: (uiTP.state || "").toUpperCase(),
        approved_matches_content: uiTP.approved_content_hash === uiTP.content_hash,
      },
      imported_feed: FEED_PATH,
      healthchecks: { api: API, mailpit: MAILPIT, web: WEB_BASE },
    };
    fs.writeFileSync(
      path.join(PROOF_DIR, "playwright_live.json"),
      JSON.stringify(proof, null, 2),
    );
    fs.writeFileSync(
      path.join(PROOF_DIR, "playwright-live-pass.json"),
      JSON.stringify(proof, null, 2),
    );
    // Dedicated mailpit gate evidence (mechanical, same run as Playwright).
    if (mailpitMatch && st3 === "SENT") {
      fs.writeFileSync(
        path.join(PROOF_DIR, "mailpit_exact_delivery.json"),
        JSON.stringify(
          {
            result: "PASS",
            pass: true,
            gate: "PASS",
            hard_asserts: true,
            generated_at: new Date().toISOString(),
            at: new Date().toISOString(),
            code_sha: codeSha || undefined,
            tested_sha: codeSha || undefined,
            command: "playwright confenge-product-acceptance mailpit compare",
            test_id: "mailpit-exact-body-match",
            touchpoint_id: seeded.touchpointId,
            approved_content_hash: approvedHash,
            approved_subject: approvedSubject,
            approved_body: approvedBody,
            approved_recipient_meta: approvedRecipient,
            mailpit_message_id: mailpitMatch.id,
            mailpit_subject: mailpitMatch.subject,
            mailpit_to: mailpitMatch.to,
            body_match: true,
            subject_present: mailpitMatch.subject.length > 0,
          },
          null,
          2,
        ),
      );
    }
  });
});
