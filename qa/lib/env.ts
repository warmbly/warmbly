import { existsSync, readFileSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

export const QA_DIR = join(dirname(fileURLToPath(import.meta.url)), "..");
export const ARTIFACTS = join(QA_DIR, ".artifacts");
export const RUN_DIR = join(ARTIFACTS, "run");
export const AUTH_DIR = join(ARTIFACTS, "auth");
export const STACK_DIR = join(ARTIFACTS, "stack");

export const VIDEO = {
  width: 1920,
  height: 1080,
  fps: Number(process.env.QA_FPS ?? 30),
  crf: Number(process.env.QA_CRF ?? 18),
  preset: process.env.QA_PRESET ?? "slow",
};

type StackEnv = { webURL?: string; apiURL?: string; mailpitURL?: string; email?: string; password?: string; mode?: string };

function stackEnv(): StackEnv {
  const file = join(STACK_DIR, "env.json");
  if (!existsSync(file)) return {};
  return JSON.parse(readFileSync(file, "utf8")) as StackEnv;
}

const stack = stackEnv();

export const env = {
  webURL: trimSlash(process.env.QA_WEB_URL ?? stack.webURL ?? "http://localhost:5173"),
  apiURL: trimSlash(process.env.QA_API_URL ?? stack.apiURL ?? "http://localhost:8080"),
  mailpitURL: trimSlash(process.env.QA_MAILPIT_URL ?? stack.mailpitURL ?? "http://localhost:18025"),
  email: process.env.QA_EMAIL ?? stack.email ?? "dev@warmbly.com",
  password: process.env.QA_PASSWORD ?? stack.password ?? "password123",
  // "external": QA_WEB_URL names a stack the harness did not start. "none": nothing to record.
  mode: process.env.QA_WEB_URL ? "external" : (stack.mode ?? "none"),
  turnstile: "warmbly-local-turnstile-bypass",
};

function trimSlash(url: string): string {
  return url.replace(/\/+$/, "");
}

// The repo is public and recordings are uploaded to it, so only a local stack with seed data may be recorded.
export function assertLocal(url: string): void {
  const host = new URL(url).hostname;
  const allowed = (process.env.QA_ALLOW_HOST ?? "").split(",").filter(Boolean);
  const local = ["localhost", "127.0.0.1", "[::1]", "::1"].includes(host) || host.endsWith(".localhost");
  if (!local && !allowed.includes(host)) {
    throw new Error(
      `refusing to record ${url}: recordings are published to a public repo, so only a local dev stack is allowed ` +
        `(set QA_ALLOW_HOST=${host} if this host is a dev machine running seed data)`,
    );
  }
}

// The stack's idle watchdog stops it after a stretch with no recording; every use resets the clock.
export function touchStack(): void {
  if (existsSync(STACK_DIR)) writeFileSync(join(STACK_DIR, "last-used"), new Date().toISOString());
}

// A second account a flow signs in as gets its own session file beside the stack's.
export function authFile(email: string = env.email): string {
  const host = new URL(env.webURL).host.replace(/[^a-z0-9]+/gi, "_");
  const who = email === env.email ? "" : `__${email.replace(/[^a-z0-9]+/gi, "_")}`;
  return join(AUTH_DIR, `${host}${who}.json`);
}
