import { mkdirSync, rmSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { test as base, expect, type Locator, type Page, type TestInfo } from "@playwright/test";
import { ensureSession, type Account } from "./auth.ts";
import { env, RUN_DIR } from "./env.ts";
import { Recorder } from "./recorder.ts";

export type Box = { x: number; y: number; width: number; height: number };
// `ignore` holds regions, in the still's own pixels, that comparisons skip.
export type Shot = { name: string; file: string; caption: string; ignore?: Box[] };

export type FlowMeta = {
  slug: string;
  title: string;
  file: string;
  status: TestInfo["status"];
  // Spooled frames waiting for the end-of-run encode; `video` replaces it once encoded.
  frames?: string;
  video?: string;
  shots: Shot[];
};

// Pages a signed-in flow must never land on: each means the session or the account setup is wrong.
const GATES: [RegExp, string][] = [
  [/^\/auth\//, "the sign-in page: the saved session was refused. Delete qa/.artifacts/auth and run again"],
  [/^\/onboarding/, "onboarding: the account has not finished setup. Delete qa/.artifacts/auth so global setup completes it"],
  [/^\/select-org/, "the workspace picker: the account belongs to several workspaces; open the one the flow needs first"],
];

// Dev-only chrome that would otherwise sit in every frame (the React Query devtools button).
const HIDE_DEV_CHROME = `.tsqd-parent-container, .tsqd-open-btn-container { display: none !important; }`;

export class Proof {
  readonly shots: Shot[] = [];
  readonly dir: string;
  private readonly page: Page;

  constructor(page: Page, dir: string) {
    this.page = page;
    this.dir = dir;
  }

  // A title card in the video, so a reviewer knows what the next few seconds show.
  async chapter(title: string, description?: string): Promise<void> {
    await this.page.screencast.showChapter(title, { description, duration: 1600 });
  }

  // A named full-resolution still. Keep names stable: follow-up comments diff stills by name.
  // `ignore` names content that changes on its own (relative times, live counters): the still is
  // published as is, and those regions are skipped when it is compared with the previous publish.
  async shot(name: string, opts: { caption?: string; target?: Locator; ignore?: Locator[] } = {}): Promise<void> {
    if (!/^[a-z0-9][a-z0-9-]*$/.test(name)) throw new Error(`shot name "${name}" must be kebab-case`);
    if (this.shots.some((s) => s.name === name)) throw new Error(`shot "${name}" taken twice in one flow`);
    const file = join(this.dir, "shots", `${name}.png`);
    const origin = opts.target ? await opts.target.boundingBox() : { x: 0, y: 0 };
    const ignore: Box[] = [];
    for (const locator of opts.ignore ?? []) {
      for (const el of await locator.all()) {
        const b = await el.boundingBox();
        if (b && origin) ignore.push({ x: Math.floor(b.x - origin.x), y: Math.floor(b.y - origin.y), width: Math.ceil(b.width) + 1, height: Math.ceil(b.height) + 1 });
      }
    }
    const options = { path: file, animations: "disabled" as const, caret: "hide" as const };
    await this.page.screencast.hideOverlays();
    try {
      if (opts.target) await opts.target.screenshot(options);
      else await this.page.screenshot(options);
    } finally {
      await this.page.screencast.showOverlays();
    }
    this.shots.push({ name, file, caption: opts.caption ?? name, ignore: ignore.length ? ignore : undefined });
  }

  // Lets a result sit on screen long enough to be read in the video.
  async dwell(ms = 1200): Promise<void> {
    await this.page.waitForTimeout(ms);
  }
}

export function slugOf(info: TestInfo): string {
  return info.titlePath
    .slice(1)
    .join(" ")
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-|-$/g, "");
}

export type Seed = "rich" | "sandbox";

// The seed each stack mode loads; a stack the harness did not start ("external") is trusted as is.
const SEED_OF_MODE: Record<string, Seed | undefined> = { lite: "rich", full: "rich", sandbox: "sandbox" };

export const test = base.extend<{ proof: Proof; signedIn: boolean; seed: Seed; account: Account | undefined }>({
  // `test.use({ signedIn: false })` for a flow that starts on the sign-in pages.
  signedIn: [true, { option: true }],
  // `test.use({ account: { email, password } })` for a flow recorded as another seeded or fixture account.
  account: [undefined, { option: true }],
  // `test.use({ seed: "sandbox" })` for a flow written against the Sunrise Labs sandbox data.
  seed: ["rich", { option: true }],
  storageState: async ({ signedIn, account, storageState }, use) => {
    if (!signedIn) return use({ cookies: [], origins: [] });
    await use(account ? await ensureSession(account) : storageState);
  },
  proof: [
    async ({ page, signedIn, seed }, use, info) => {
      const loaded = SEED_OF_MODE[env.mode];
      const fix = seed === "sandbox" ? "pnpm stack reset-data sandbox" : "pnpm stack reset-data lite";
      if (loaded !== undefined && loaded !== seed) {
        const why = `written for the ${seed} seed, but the stack has the ${loaded} seed (${fix})`;
        console.log(`skipped "${info.title}": ${why}`);
        info.skip(true, why);
      }
      const slug = slugOf(info);
      const dir = join(RUN_DIR, slug);
      mkdirSync(join(dir, "shots"), { recursive: true });
      await page.addInitScript((css) => {
        const add = () => document.head.appendChild(Object.assign(document.createElement("style"), { textContent: css }));
        if (document.head) add();
        else document.addEventListener("DOMContentLoaded", add);
      }, HIDE_DEV_CHROME);

      const frames = join(dir, "frames");
      const recorder = new Recorder(page, frames);
      await recorder.start();
      await page.screencast.showActions({ cursor: "pointer", duration: 700, fontSize: 20, position: "top-right" });
      const proof = new Proof(page, dir);

      await use(proof);

      const captured = await recorder.stop();
      // A failed flow's video is never published, so its frames are not worth encoding unless asked for.
      const keep = captured && (info.status === "passed" || process.env.QA_VIDEO_ON_FAIL === "1");
      if (!keep) rmSync(frames, { recursive: true, force: true });
      const meta: FlowMeta = {
        slug,
        title: info.title,
        file: info.file,
        status: info.status,
        frames: keep ? frames : undefined,
        shots: proof.shots,
      };
      writeFileSync(join(dir, "meta.json"), JSON.stringify(meta, null, 2));
      const path = new URL(page.url()).pathname;
      const gate = signedIn ? GATES.find(([re]) => re.test(path)) : undefined;
      if (gate) throw new Error(`the dashboard sent this flow to ${gate[1]}.`);
    },
    { auto: true },
  ],
});

export { expect };
