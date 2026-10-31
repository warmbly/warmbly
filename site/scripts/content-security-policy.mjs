import { createHash } from 'node:crypto';
import { readFile, readdir, writeFile } from 'node:fs/promises';
import { parse } from 'parse5';
import { loadEnv } from 'vite';
import { fileURLToPath } from 'node:url';

export function inlineHashes(html) {
  const scripts = new Set();
  function visit(node) {
    const attributes = Object.fromEntries((node.attrs ?? []).map(({ name, value }) => [name, value]));
    if (Object.keys(attributes).some((name) => name.startsWith('on'))) {
      throw new Error('Marketing CSP requires event listeners instead of inline handlers');
    }
    if (node.tagName === 'script' && !attributes.src && !['application/ld+json', 'application/json'].includes(attributes.type)) {
      const text = (node.childNodes ?? []).map((child) => child.value ?? '').join('');
      if (text) {
        const hash = `'sha256-${createHash('sha256').update(text).digest('base64')}'`;
        scripts.add(hash);
      }
    }
    for (const child of node.childNodes ?? []) visit(child);
  }
  visit(parse(html));
  return scripts;
}

export function contentSecurityPolicy(scripts, analyticsHost) {
  const analytics = [];
  if (analyticsHost) {
    const url = new URL(analyticsHost);
    if (url.protocol !== 'https:' || url.username || url.password) throw new Error('Analytics CSP requires an HTTPS host');
    analytics.push(url.origin);
    if (/^(us|eu)\.i\.posthog\.com$/.test(url.hostname)) {
      analytics.push(`https://${url.hostname.replace('.i.', '-assets.i.')}`);
    }
  }
  return [
    "default-src 'none'",
    "base-uri 'none'",
    "object-src 'none'",
    "frame-ancestors 'none'",
    "form-action 'self'",
    `script-src 'self' https://static.cloudflareinsights.com ${[...analytics, ...scripts].join(' ')}`,
    "script-src-attr 'none'",
    "style-src 'self' 'unsafe-inline'",
    `img-src 'self' data: ${analytics.join(' ')}`,
    "font-src 'self'",
    `connect-src 'self' https://cloudflareinsights.com ${analytics.join(' ')}`,
    "worker-src 'self' blob:",
  ].join('; ');
}

export default function contentSecurityPolicyHeaders() {
  let analyticsHost;
  return {
    name: 'marketing-content-security-policy',
    hooks: {
      'astro:config:setup': ({ config, mode }) => {
        const env = loadEnv(mode, fileURLToPath(config.root), 'PUBLIC_POSTHOG_');
        analyticsHost = env.PUBLIC_POSTHOG_KEY ? env.PUBLIC_POSTHOG_HOST || 'https://us.i.posthog.com' : undefined;
      },
      'astro:build:done': async ({ dir, logger }) => {
        const hashes = new Set();
        for (const entry of await readdir(dir, { recursive: true })) {
          if (!entry.endsWith('.html')) continue;
          const page = inlineHashes(await readFile(new URL(entry, dir), 'utf8'));
          for (const hash of page) hashes.add(hash);
        }
        if (!hashes.size) throw new Error('No marketing page scripts found for CSP');
        const policy = contentSecurityPolicy(hashes, analyticsHost);
        const header = `  Content-Security-Policy: ${policy}`;
        // Pages rejects header lines longer than 2,000 characters.
        if (header.length > 2000) throw new Error(`Marketing CSP exceeds the Pages header limit (${header.length})`);
        const file = new URL('_headers', dir);
        const headers = await readFile(file, 'utf8');
        if (/Content-Security-Policy:/i.test(headers)) throw new Error('Marketing CSP header already configured');
        if (!headers.includes('/*\n')) throw new Error('Marketing headers require a catch-all rule');
        await writeFile(file, headers.replace('/*\n', `/*\n${header}\n`));
        logger.info(`Generated CSP for all marketing routes (${hashes.size} script hashes)`);
      },
    },
  };
}
