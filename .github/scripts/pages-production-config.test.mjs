import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { mkdtempSync, readFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { test } from "node:test";
import { fileURLToPath } from "node:url";
import { runInNewContext } from "node:vm";

const script = fileURLToPath(new URL("./pages-production-config.mjs", import.meta.url));
const entrypoint = readFileSync(new URL("../../web/docker-entrypoint.sh", import.meta.url), "utf8");
const names = [...new Set([...entrypoint.matchAll(/\$\{(WARMBLY_[A-Z_]+)/g)].map((match) => match[1]))]
  .filter((name) => name !== "WARMBLY_CONFIG_OUT");

function fixture() {
  return {
    success: true,
    result: {
      production_branch: "production",
      deployment_configs: {
        production: {
          env_vars: Object.fromEntries(names.map((name) => [name, { type: "plain_text", value: `production-${name}` }])),
        },
        preview: { env_vars: { WARMBLY_API_URL: { type: "plain_text", value: "preview-only" } } },
      },
    },
  };
}

function run(project, app = "web") {
  const work = mkdtempSync(join(tmpdir(), "pages-config-"));
  try {
    const envFile = join(work, "env");
    const outputFile = join(work, "output");
    const result = spawnSync(process.execPath, [script], {
      input: typeof project === "string" ? project : JSON.stringify(project),
      env: { ...process.env, PAGES_APP: app, GITHUB_ENV: envFile, GITHUB_OUTPUT: outputFile },
      encoding: "utf8",
    });
    const read = (file) => {
      try { return readFileSync(file, "utf8"); } catch { return ""; }
    };
    return { ...result, env: read(envFile), output: read(outputFile) };
  } finally {
    rmSync(work, { recursive: true, force: true });
  }
}

function decode(text) {
  const lines = text.split("\n");
  const values = {};
  while (lines[0]) {
    const [name, delimiter] = lines.shift().split("<<");
    const end = lines.indexOf(delimiter);
    assert.ok(end >= 0);
    values[name] = lines.splice(0, end).join("\n");
    lines.shift();
  }
  return values;
}

test("imports every runtime key from production only and discovers the production branch", () => {
  const project = fixture();
  project.result.deployment_configs.production.env_vars.NODE_OPTIONS = { type: "plain_text", value: "untrusted" };
  project.result.deployment_configs.production.env_vars.SENTRY_AUTH_TOKEN = { type: "secret_text", value: "private-value" };
  const result = run(project);
  assert.equal(result.status, 0, result.stderr);
  assert.equal(result.output, "production_branch=production\n");
  const values = decode(result.env);
  assert.deepEqual(Object.keys(values).sort(), names.sort());
  for (const name of names) assert.equal(values[name], `production-${name}`);
  assert.equal(result.stdout + result.stderr, "");
});

test("preserves multiline values without injecting additional runner variables", () => {
  const project = fixture();
  const value = 'logos "quoted"\\path\r\nNODE_OPTIONS=untrusted\n::error::not-a-command';
  project.result.deployment_configs.production.env_vars.WARMBLY_COMPANY_LOGOS.value = value;
  const result = run(project);
  assert.equal(result.status, 0, result.stderr);
  assert.equal(decode(result.env).WARMBLY_COMPANY_LOGOS, value);
  assert.equal(result.stdout + result.stderr, "");
});

test("defaults absent optional runtime settings to empty strings", () => {
  const project = fixture();
  delete project.result.deployment_configs.production.env_vars.WARMBLY_POSTHOG_KEY;
  const result = run(project);
  assert.equal(result.status, 0, result.stderr);
  assert.equal(decode(result.env).WARMBLY_POSTHOG_KEY, "");
});

test("renders the imported production settings with the real dashboard entrypoint", () => {
  const project = fixture();
  project.result.deployment_configs.production.env_vars.WARMBLY_COMPANY_LOGOS.value = 'logos "quoted"\\path';
  const imported = run(project);
  assert.equal(imported.status, 0, imported.stderr);
  const work = mkdtempSync(join(tmpdir(), "pages-render-"));
  try {
    const output = join(work, "config.js");
    const rendered = spawnSync("sh", [fileURLToPath(new URL("../../web/docker-entrypoint.sh", import.meta.url))], {
      env: { ...process.env, ...decode(imported.env), WARMBLY_CONFIG_OUT: output },
      encoding: "utf8",
    });
    assert.equal(rendered.status, 0, rendered.stderr);
    const window = {};
    runInNewContext(readFileSync(output, "utf8"), { window });
    assert.equal(window.__WARMBLY_ENV__.API_URL, "production-WARMBLY_API_URL");
    assert.equal(window.__WARMBLY_ENV__.TURNSTILE_KEY, "production-WARMBLY_TURNSTILE_KEY");
    assert.equal(window.__WARMBLY_ENV__.COMPANY_LOGOS, 'logos "quoted"\\path');
  } finally {
    rmSync(work, { recursive: true, force: true });
  }
});

test("rejects missing required variables and encrypted runtime settings without writing config", () => {
  for (const name of ["WARMBLY_API_URL", "WARMBLY_APP_URL", "WARMBLY_TURNSTILE_KEY"]) {
    for (const setting of [undefined, { type: "plain_text", value: " " }, { type: "secret_text", value: "hidden" }]) {
      const project = fixture();
      project.result.deployment_configs.production.env_vars[name] = setting;
      const result = run(project);
      assert.equal(result.status, 1);
      assert.ok(result.stderr.includes(name));
      assert.ok(!result.stderr.includes("hidden"));
      assert.equal(result.env + result.output, "");
    }
  }
  const project = fixture();
  project.result.deployment_configs.production.env_vars.WARMBLY_POSTHOG_KEY.type = "secret_text";
  assert.equal(run(project).status, 1);
});

test("rejects malformed API responses, missing production config, and runner output injection", () => {
  const inputs = ["invalid-private-response", { success: false }, { success: true, result: {} }, null];
  for (const branch of ["", "main\nother=value", "main\rother=value"]) {
    const project = fixture();
    project.result.production_branch = branch;
    inputs.push(project);
  }
  const noProduction = fixture();
  delete noProduction.result.deployment_configs.production;
  inputs.push(noProduction);
  for (const input of inputs) {
    const result = run(input);
    assert.equal(result.status, 1);
    assert.equal(result.env + result.output, "");
    assert.ok(!result.stderr.includes("invalid-private-response"));
  }
});

test("imports and renders the admin's distinct runtime configuration", () => {
  const entry = new URL("../../admin/docker-entrypoint.sh", import.meta.url);
  const adminNames = [...new Set([...readFileSync(entry, "utf8").matchAll(/\$\{(WARMBLY_[A-Z_]+)/g)].map((match) => match[1]))]
    .filter((name) => name !== "WARMBLY_CONFIG_OUT");
  const project = fixture();
  project.result.production_branch = "admin-release";
  project.result.deployment_configs.production.env_vars = Object.fromEntries(adminNames.map((name) => [name, { type: "plain_text", value: `admin-${name}` }]));
  const imported = run(project, "admin");
  assert.equal(imported.status, 0, imported.stderr);
  assert.equal(imported.output, "production_branch=admin-release\n");
  assert.deepEqual(Object.keys(decode(imported.env)).sort(), adminNames.sort());
  const work = mkdtempSync(join(tmpdir(), "pages-admin-"));
  try {
    const output = join(work, "config.js");
    const rendered = spawnSync("sh", [fileURLToPath(entry)], {
      env: { ...process.env, ...decode(imported.env), WARMBLY_CONFIG_OUT: output },
      encoding: "utf8",
    });
    assert.equal(rendered.status, 0, rendered.stderr);
    const window = {};
    runInNewContext(readFileSync(output, "utf8"), { window });
    assert.equal(window.__WARMBLY_ENV__.DASHBOARD_URL, "admin-WARMBLY_DASHBOARD_URL");
    assert.equal(window.__WARMBLY_ENV__.ENV_LABEL, "admin-WARMBLY_ENV_LABEL");
  } finally {
    rmSync(work, { recursive: true, force: true });
  }
  delete project.result.deployment_configs.production.env_vars.WARMBLY_DASHBOARD_URL;
  assert.equal(run(project, "admin").status, 1);
});

test("site and docs import only their public build variables, without dashboard requirements", () => {
  for (const [app, name] of [["site", "PUBLIC_POSTHOG_KEY"], ["docs", "NEXT_PUBLIC_ANALYTICS_KEY"]]) {
    const project = fixture();
    project.result.deployment_configs.production.env_vars = {
      [name]: { type: "plain_text", value: `${app}-public` },
      API_KEY: { type: "secret_text", value: "private" },
      NODE_OPTIONS: { type: "plain_text", value: "untrusted" },
      WARMBLY_API_URL: { type: "plain_text", value: "not-this-app" },
    };
    const imported = run(project, app);
    assert.equal(imported.status, 0, imported.stderr);
    assert.deepEqual(decode(imported.env), { [name]: `${app}-public` });
    project.result.deployment_configs.production.env_vars[name].type = "secret_text";
    assert.equal(run(project, app).status, 1);
    project.result.deployment_configs.production.env_vars = {};
    assert.equal(run(project, app).status, 0);
  }
  assert.equal(run(fixture(), "unsupported").status, 1);
});
