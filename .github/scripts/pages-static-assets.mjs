import { appendFileSync, cpSync, existsSync, lstatSync, mkdirSync, readFileSync, readdirSync, rmSync, writeFileSync } from "node:fs";
import { resolve, join } from "node:path";
import { fileURLToPath } from "node:url";
import { deploymentMatrix } from "./pages-projects.mjs";

export function staticAssetsMatrix(projects, targets) {
  const pages = deploymentMatrix(projects).include;
  const configured = JSON.parse(targets || "[]");
  if (!Array.isArray(configured) || configured.length > pages.length) throw new Error("Invalid static asset targets");
  const seenProjects = new Set();
  const seenWorkers = new Set();
  const seenDomains = new Set();
  return { include: configured.map((target) => {
    if (!target || Object.keys(target).some((key) => !["project", "worker", "domains"].includes(key))) throw new Error("Invalid static asset target");
    const page = pages.find((entry) => entry.project === target.project);
    if (!page || typeof target.worker !== "string" || !/^[a-z0-9][a-z0-9-]{0,62}$/.test(target.worker)) throw new Error("Unknown project or invalid Worker name");
    if (!Array.isArray(target.domains) || target.domains.length > 10) throw new Error("Explicit domains required");
    for (const domain of target.domains) {
      if (typeof domain !== "string" || domain.length > 253 || !/^(?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,63}$/.test(domain) || seenDomains.has(domain)) throw new Error("Invalid or duplicate custom domain");
      seenDomains.add(domain);
    }
    if (seenProjects.has(target.project) || seenWorkers.has(target.worker)) throw new Error("Duplicate static asset target");
    seenProjects.add(target.project);
    seenWorkers.add(target.worker);
    return { ...page, worker: target.worker, domains: target.domains };
  }) };
}

export function prepareStaticAssets({ target, sourceRoot, destination, sourceSha, release }) {
  if (!/^[a-f0-9]{40}$/.test(sourceSha) || !/^v\d+\.\d+\.\d+$/.test(release)) throw new Error("Exact release identity required");
  const output = resolve(sourceRoot, target.output_dir);
  const dest = resolve(destination);
  if (existsSync(dest)) throw new Error("Use a new artifact directory");
  let files = 0;
  function check(path) {
    const stat = lstatSync(path);
    if (stat.isSymbolicLink()) throw new Error("Static artifacts must not contain symlinks");
    if (stat.isDirectory()) {
      for (const entry of readdirSync(path)) check(join(path, entry));
    } else if (!stat.isFile() || stat.size > 25 * 1024 * 1024 || ++files > 19999) {
      throw new Error("Artifact exceeds Workers Free asset limits");
    }
  }
  check(output);
  const isRegularFile = (name) => existsSync(join(output, name)) && lstatSync(join(output, name)).isFile();
  if (!isRegularFile("index.html") || !isRegularFile("_headers")) throw new Error("Frontend index and response headers required");
  if (existsSync(join(output, "_worker.js")) || existsSync(join(output, "_routes.json"))) throw new Error("Only static artifacts are supported");
  mkdirSync(dest, { recursive: true });
  cpSync(output, join(dest, "assets"), { recursive: true });
  if (target.app === "admin") {
    const redirects = join(dest, "assets", "_redirects");
    if (existsSync(redirects)) {
      const rules = readFileSync(redirects, "utf8").split("\n").map((line) => line.trim()).filter((line) => line && !line.startsWith("#"));
      if (rules.length !== 1 || !/^\/\*\s+\/index\.html\s+200$/.test(rules[0])) throw new Error("Review changed admin redirect rules before publishing static assets");
      // The asset platform's SPA mode replaces the Pages-only catch-all rewrite.
      rmSync(redirects);
    }
  }
  mkdirSync(join(dest, "assets", ".well-known"), { recursive: true });
  writeFileSync(join(dest, "assets", ".well-known", "warmbly-release.json"), JSON.stringify({ release, sourceSha, app: target.app, project: target.project }) + "\n");
  const config = {
    name: target.worker,
    compatibility_date: "2025-10-11",
    workers_dev: false,
    preview_urls: false,
    assets: { directory: "./assets", html_handling: "auto-trailing-slash", not_found_handling: target.app === "admin" ? "single-page-application" : "404-page" },
    routes: target.domains.map((pattern) => ({ pattern, custom_domain: true })),
  };
  writeFileSync(join(dest, "wrangler.json"), JSON.stringify(config, null, 2) + "\n");
  return config;
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  try {
    const matrix = staticAssetsMatrix(process.env.CLOUDFLARE_PAGES_PROJECTS, process.env.CLOUDFLARE_STATIC_ASSETS_TARGETS);
    if (["prepare", "prepare-if-configured"].includes(process.argv[2])) {
      const target = matrix.include.find((entry) => entry.project === process.env.CLOUDFLARE_PAGES_PROJECT_NAME);
      if (!target && process.argv[2] === "prepare") throw new Error("Project is not configured for static assets");
      if (process.env.GITHUB_OUTPUT) appendFileSync(process.env.GITHUB_OUTPUT, `enabled=${Boolean(target)}\n`);
      if (target) prepareStaticAssets({ target, sourceRoot: process.cwd(), destination: process.env.STATIC_ASSETS_ARTIFACT_DIR, sourceSha: process.env.SOURCE_SHA, release: process.env.RELEASE_TAG });
    } else {
      appendFileSync(process.env.GITHUB_OUTPUT, `matrix=${JSON.stringify(matrix)}\nenabled=${matrix.include.length > 0}\n`);
    }
  } catch {
    console.error("::error::Invalid static asset configuration or artifact. Require unique configured Pages projects, Worker names, explicit domains and an exact release identity.");
    process.exitCode = 1;
  }
}
