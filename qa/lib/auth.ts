import { existsSync, mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { dirname } from "node:path";
import { authFile, env } from "./env.ts";

type Tokens = {
  access_token: string;
  access_token_expires_at: string;
  refresh_token: string;
  refresh_token_expires_at: string;
};

type StorageState = {
  cookies: [];
  origins: { origin: string; localStorage: { name: string; value: string }[] }[];
};

const TOKEN_KEYS = ["access_token", "access_token_expires_at", "refresh_token", "refresh_token_expires_at"] as const;

export type Account = { email: string; password: string };

// Reuses the saved session while it still works: every fresh login sends a code email, and those are budgeted.
export async function ensureSession(account: Account = { email: env.email, password: env.password }): Promise<string> {
  const file = authFile(account.email);
  const saved = readTokens(file);
  if (saved && (await sessionWorks(saved))) {
    await finishOnboarding(saved.access_token);
    return file;
  }

  const tokens = await login(account);
  await finishOnboarding(tokens.access_token);
  mkdirSync(dirname(file), { recursive: true });
  const state: StorageState = {
    cookies: [],
    origins: [
      {
        origin: env.webURL,
        // The dashboard reads `auth_token` (one JSON object) and keeps the four flat keys alongside it.
        localStorage: [
          { name: "auth_token", value: JSON.stringify(Object.fromEntries(TOKEN_KEYS.map((k) => [k, tokens[k]]))) },
          ...TOKEN_KEYS.map((k) => ({ name: k, value: tokens[k] })),
        ],
      },
    ],
  };
  writeFileSync(file, JSON.stringify(state, null, 2));
  return file;
}

function readTokens(file: string): Tokens | undefined {
  if (!existsSync(file)) return undefined;
  const state = JSON.parse(readFileSync(file, "utf8")) as StorageState;
  const items = state.origins.find((o) => o.origin === env.webURL)?.localStorage ?? [];
  const get = (k: string) => items.find((i) => i.name === k)?.value ?? "";
  const tokens = Object.fromEntries(TOKEN_KEYS.map((k) => [k, get(k)])) as Tokens;
  if (!tokens.refresh_token || Date.parse(tokens.refresh_token_expires_at) < Date.now() + 3600_000) return undefined;
  return tokens;
}

async function sessionWorks(tokens: Tokens): Promise<boolean> {
  if (Date.parse(tokens.access_token_expires_at) > Date.now() + 60_000) {
    const res = await fetch(`${env.apiURL}/v1/auth/me`, { headers: { Authorization: `Bearer ${tokens.access_token}` } });
    return res.ok;
  }
  // An expired access token is fine: the dashboard refreshes it on its first request.
  return true;
}

async function call<T>(method: string, path: string, body?: object, token?: string): Promise<T> {
  const res = await fetch(`${env.apiURL}/v1${path}`, {
    method,
    headers: { "Content-Type": "application/json", ...(token ? { Authorization: `Bearer ${token}` } : {}) },
    body: body ? JSON.stringify(body) : undefined,
  });
  const json = (await res.json().catch(() => ({}))) as T & { error?: string; code?: string };
  if (!res.ok) throw new Error(`${method} ${path} answered ${res.status}: ${json.code ?? ""} ${json.error ?? ""}`.trim());
  return json;
}

function post<T>(path: string, body: object): Promise<T> {
  return call<T>("POST", path, body);
}

// Seeded accounts have not been through the first-run wizard, and the dashboard sends them there
// before anything else. Answer it once through the same endpoint the wizard uses.
async function finishOnboarding(token: string): Promise<void> {
  if (!token) return;
  const me = await call<{ first_name?: string; last_name?: string; onboarding_completed_at?: string | null }>("GET", "/auth/me", undefined, token).catch(() => undefined);
  if (!me || me.onboarding_completed_at) return;
  await call("PATCH", "/auth/me/onboarding", {
    first_name: me.first_name || "Dev",
    last_name: me.last_name || "User",
    referral_source: "other",
  }, token);
}

async function login(account: Account): Promise<Tokens> {
  const startedAt = Date.now();
  const start = await post<{ session?: string; code_required: boolean; token?: Tokens; two_fa_required?: boolean }>(
    "/auth/login",
    { email: account.email, password: account.password, turnstile: env.turnstile },
  );
  if (start.token) return start.token;
  if (start.two_fa_required) throw new Error(`${account.email} has 2FA enabled; use an account without it for QA`);
  if (!start.session) throw new Error("login answered with neither a token nor a code session");

  const code = await loginCode(account.email, startedAt);
  const confirm = await post<Partial<Tokens> & { two_fa_required?: boolean }>("/auth/login/confirm", {
    session: start.session,
    code,
    turnstile: env.turnstile,
  });
  if (confirm.two_fa_required) throw new Error(`${account.email} has 2FA enabled; use an account without it for QA`);
  if (!confirm.access_token) throw new Error("login confirm returned no token");
  return confirm as Tokens;
}

type MailpitSummary = { ID: string; Created: string };

async function loginCode(email: string, since: number): Promise<string> {
  const query = encodeURIComponent(`to:"${email}" subject:"Your Login Code"`);
  for (let i = 0; i < 40; i++) {
    const res = await fetch(`${env.mailpitURL}/api/v1/search?query=${query}&limit=5`);
    if (res.ok) {
      const { messages } = (await res.json()) as { messages: MailpitSummary[] };
      const fresh = messages.find((m) => Date.parse(m.Created) >= since - 2000);
      if (fresh) {
        const msg = (await (await fetch(`${env.mailpitURL}/api/v1/message/${fresh.ID}`)).json()) as { Text: string };
        const code = msg.Text.match(/\b(\d{6})\b/)?.[1];
        if (code) return code;
      }
    }
    await new Promise((r) => setTimeout(r, 500));
  }
  throw new Error(`no login code for ${email} reached Mailpit at ${env.mailpitURL}`);
}
