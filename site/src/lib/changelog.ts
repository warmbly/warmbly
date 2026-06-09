/**
 * Build-time changelog, sourced from real merged GitHub pull requests.
 *
 * There are no tagged releases or a CHANGELOG.md in this repo, so merged PRs
 * into `main` are the source of truth. We use PR granularity (not commits)
 * because a single PR aggregates many commits into one shippable change.
 *
 * This runs at build time inside Astro's component script. The result is
 * fetched once per build (module-level cache) and falls back to a small
 * curated list if the GitHub API is unreachable, so the build never breaks
 * and the page is never blank.
 *
 * Curation knobs live here on purpose, so the rules sit with the code:
 *   SKIP_TYPES   conventional-commit types that are real work but not
 *                customer-facing (chore, ci, refactor, ...)
 *   SKIP_LABELS  PR labels that always hide a PR. Add `skip-changelog` to a
 *                pull request to keep it off the changelog.
 *   SKIP_TITLE   title patterns that always hide a PR (marketing-site and
 *                pure-infra work that is not product changelog material)
 *   MAX_ENTRIES  how many entries to surface
 *
 * Set GITHUB_TOKEN in the build environment to lift the unauthenticated API
 * rate limit. It is optional; public PRs are readable without it.
 */

const REPO = 'warmbly/warmbly';
const MAX_ENTRIES = 24;

export type ChangelogTag = 'shipped' | 'changed' | 'fixed';

export interface ChangelogEntry {
  number: number;
  url: string;
  date: string; // YYYY-MM-DD, the merge date
  title: string; // human title, conventional-commit prefix stripped
  tag: ChangelogTag;
  body: string; // one-paragraph summary
  bullets: string[]; // up to 3 summary bullets, for the homepage timeline
}

// Conventional-commit type -> changelog tag. Unknown/untyped defaults to shipped.
const TAG_BY_TYPE: Record<string, ChangelogTag> = {
  feat: 'shipped',
  fix: 'fixed',
  perf: 'changed',
};

// Recognised conventional-commit types. A leading word is only treated as a
// type when it is one of these, so a scope prefix like "Contacts:" is kept as
// part of the title instead of being stripped.
const KNOWN_TYPES = new Set([
  'feat',
  'fix',
  'perf',
  'refactor',
  'chore',
  'ci',
  'build',
  'test',
  'docs',
  'style',
  'revert',
  'deps',
]);

// Real work, but not changelog material.
const SKIP_TYPES = new Set(['chore', 'ci', 'build', 'test', 'docs', 'style', 'refactor']);

// Labels that always hide a PR. Add one of these to a PR to keep it off here.
const SKIP_LABELS = new Set(['skip-changelog', 'no-changelog', 'internal']);

// Marketing-site and pure-infra titles that are not product changelog material.
// Conservative and easy to amend. The `skip-changelog` label is the durable
// way to hide a one-off; these patterns keep the historical backlog clean.
const SKIP_TITLE: RegExp[] = [
  /\bpage\b/i, // "redesign X page", "use case page"
  /marketing/i,
  /landing/i,
  /feature showcase/i,
  /\blicense\b/i,
  /dev stack/i,
  /dev infra/i,
];

interface GhPull {
  number: number;
  title: string;
  body: string | null;
  html_url: string;
  merged_at: string | null;
  labels: { name: string }[];
}

let cache: Promise<ChangelogEntry[]> | null = null;

/** Fetched once per build; safe to call from multiple components. */
export function getChangelog(): Promise<ChangelogEntry[]> {
  if (!cache) cache = load();
  return cache;
}

async function load(): Promise<ChangelogEntry[]> {
  try {
    // process is only typed with @types/node; reach it defensively so this
    // stays valid without node types and yields undefined off-Node.
    const token =
      (globalThis as { process?: { env?: Record<string, string | undefined> } }).process?.env
        ?.GITHUB_TOKEN ?? undefined;

    const res = await fetch(
      `https://api.github.com/repos/${REPO}/pulls?state=closed&base=main&sort=updated&direction=desc&per_page=100`,
      {
        headers: {
          Accept: 'application/vnd.github+json',
          'User-Agent': 'warmbly-site-changelog',
          'X-GitHub-Api-Version': '2022-11-28',
          ...(token ? { Authorization: `Bearer ${token}` } : {}),
        },
      },
    );
    if (!res.ok) throw new Error(`GitHub API responded ${res.status}`);

    const pulls = (await res.json()) as GhPull[];
    const entries = pulls
      .filter((p) => p.merged_at)
      .map(toEntry)
      .filter((e): e is ChangelogEntry => e !== null)
      .sort((a, b) => (a.date < b.date ? 1 : a.date > b.date ? -1 : b.number - a.number))
      .slice(0, MAX_ENTRIES);

    return entries.length ? entries : FALLBACK;
  } catch (err) {
    console.warn(`[changelog] using curated fallback: ${(err as Error).message}`);
    return FALLBACK;
  }
}

function toEntry(p: GhPull): ChangelogEntry | null {
  const labels = p.labels.map((l) => l.name.toLowerCase());
  if (labels.some((l) => SKIP_LABELS.has(l))) return null;

  const { type, summary } = parseTitle(p.title);
  if (type && SKIP_TYPES.has(type)) return null;
  if (SKIP_TITLE.some((re) => re.test(summary))) return null;

  const bullets = summaryBullets(p.body).map((b) => titleCase(b));
  const body = bullets.length ? bullets.join('. ') : firstParagraph(p.body) || `${titleCase(summary)}.`;

  return {
    number: p.number,
    url: p.html_url,
    date: p.merged_at!.slice(0, 10),
    title: titleCase(summary),
    tag: type ? (TAG_BY_TYPE[type] ?? 'shipped') : 'shipped',
    body: clamp(ensureStop(body), 320),
    bullets: bullets.slice(0, 3).map((b) => clamp(b, 120)),
  };
}

/** Split a conventional-commit title: "feat(scope): summary" -> {type, summary}. */
function parseTitle(raw: string): { type: string | null; summary: string } {
  const m = /^([a-z]+)(?:\([^)]*\))?!?:\s*(.+)$/i.exec(raw.trim());
  if (m && KNOWN_TYPES.has(m[1].toLowerCase())) {
    return { type: m[1].toLowerCase(), summary: m[2].trim() };
  }
  return { type: null, summary: raw.trim() };
}

/** Pull the bullets under a "## Summary" heading, else top-level bullets. */
function summaryBullets(body: string | null): string[] {
  if (!body) return [];
  const text = body.replace(/<!--[\s\S]*?-->/g, '');
  const lines = text.split(/\r?\n/);

  const start = lines.findIndex((l) => /^#{1,6}\s*summary\b/i.test(l.trim()));
  const scope = start >= 0 ? lines.slice(start + 1) : lines;

  const bullets: string[] = [];
  for (const line of scope) {
    const t = line.trim();
    if (start >= 0 && /^#{1,6}\s/.test(t)) break; // next heading ends the section
    const m = /^[-*]\s+(.+)$/.exec(t);
    if (m) bullets.push(cleanInline(m[1]));
    else if (bullets.length && t === '') continue;
  }
  return bullets.filter(Boolean);
}

function firstParagraph(body: string | null): string {
  if (!body) return '';
  const text = body.replace(/<!--[\s\S]*?-->/g, '');
  for (const block of text.split(/\r?\n\s*\r?\n/)) {
    const t = block.trim();
    if (!t || /^#{1,6}\s/.test(t) || /^[-*]\s/.test(t)) continue;
    return cleanInline(t.replace(/\s*\r?\n\s*/g, ' '));
  }
  return '';
}

/** Strip light markdown so copy reads cleanly as plain text. */
function cleanInline(s: string): string {
  return s
    .replace(/\[([^\]]+)\]\([^)]+\)/g, '$1') // links -> text
    .replace(/[`*_]/g, '')
    .replace(/\s+/g, ' ')
    .trim();
}

function titleCase(s: string): string {
  return s ? s.charAt(0).toUpperCase() + s.slice(1) : s;
}

function ensureStop(s: string): string {
  return /[.!?]$/.test(s) ? s : `${s}.`;
}

function clamp(s: string, n: number): string {
  if (s.length <= n) return s;
  const cut = s.slice(0, n);
  const sp = cut.lastIndexOf(' ');
  return `${(sp > 40 ? cut.slice(0, sp) : cut).replace(/[.,;:]$/, '')}…`;
}

// Curated fallback, shown only when the GitHub API is unreachable at build
// time. Mirrors recent real merged PRs so the page is never blank.
const FALLBACK: ChangelogEntry[] = [
  {
    number: 40,
    url: `https://github.com/${REPO}/pull/40`,
    date: '2026-06-09',
    title: 'Improve automation action flows',
    tag: 'shipped',
    body: 'Safeguards for campaign-launched and nested automation actions. Automations referenced by a campaign step can no longer be deleted. Team assignment for CRM tasks.',
    bullets: [
      'Safeguards for campaign-launched and nested automation actions',
      'Automations referenced by a campaign step cannot be deleted',
      'Team assignment for CRM tasks',
    ],
  },
  {
    number: 30,
    url: `https://github.com/${REPO}/pull/30`,
    date: '2026-06-07',
    title: 'Expand CRM campaign automation',
    tag: 'shipped',
    body: 'Deeper CRM and campaign automation: deal sequence actions, reply-driven branches, and instant actions fired from tracking events.',
    bullets: [
      'Deal sequence actions in the CRM',
      'Reply-driven campaign branches',
      'Instant actions fired from tracking events',
    ],
  },
  {
    number: 27,
    url: `https://github.com/${REPO}/pull/27`,
    date: '2026-06-06',
    title: 'Campaign experience overhaul',
    tag: 'shipped',
    body: 'A reworked campaign builder with reply classification, A/B variant reporting, and stop-on-reply defaults.',
    bullets: [
      'Reworked campaign builder',
      'Reply classification and A/B variant reporting',
      'Stop-on-reply on by default',
    ],
  },
  {
    number: 26,
    url: `https://github.com/${REPO}/pull/26`,
    date: '2026-06-03',
    title: 'Add warmup deliverability controls',
    tag: 'shipped',
    body: 'A deliverability dashboard and warmup controls surfacing placement, complaints, bounces, and warmup health per mailbox.',
    bullets: [
      'Deliverability dashboard',
      'Warmup health surfaced per mailbox',
    ],
  },
  {
    number: 24,
    url: `https://github.com/${REPO}/pull/24`,
    date: '2026-06-01',
    title: 'Improve email warmup scheduling and safety',
    tag: 'changed',
    body: 'Steadier warmup scheduling with stronger per-mailbox safety and spacing between sends.',
    bullets: [
      'Steadier warmup scheduling',
      'Stronger per-mailbox safety and spacing',
    ],
  },
];
