// The repository's star count, read once per build for the header. A build
// that cannot reach GitHub shows the link without a number rather than a stale
// or invented one.
export const REPO_URL = 'https://github.com/warmbly/warmbly';

let cache: Promise<string | null> | undefined;

export function getRepoStars(): Promise<string | null> {
  if (!cache) cache = load();
  return cache;
}

async function load(): Promise<string | null> {
  try {
    const token =
      (globalThis as { process?: { env?: Record<string, string | undefined> } }).process?.env
        ?.GITHUB_TOKEN ?? undefined;
    const res = await fetch('https://api.github.com/repos/warmbly/warmbly', {
      headers: {
        Accept: 'application/vnd.github+json',
        'User-Agent': 'warmbly-site-header',
        'X-GitHub-Api-Version': '2022-11-28',
        ...(token ? { Authorization: `Bearer ${token}` } : {}),
      },
      signal: AbortSignal.timeout(4000),
    });
    if (!res.ok) throw new Error(`GitHub API responded ${res.status}`);
    const n = Number(((await res.json()) as { stargazers_count?: number }).stargazers_count);
    if (!Number.isFinite(n)) return null;
    return n >= 1000 ? `${(n / 1000).toFixed(n >= 10000 ? 0 : 1).replace(/\.0$/, '')}k` : String(n);
  } catch (err) {
    console.warn(`[github] star count unavailable: ${(err as Error).message}`);
    return null;
  }
}
