import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import { mkdtemp, mkdir, readFile, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { pathToFileURL } from 'node:url';
import { test } from 'node:test';
import integration, { contentSecurityPolicy, inlineHashes } from './content-security-policy.mjs';

test('hashes browser-normalized script text without treating JSON data or remote scripts as code', () => {
  const hashes = inlineHashes('<script>window.ready = true;\r\n</script><script type="application/ld+json">{"x":1}</script><script src="/app.js"></script>');
  const digest = createHash('sha256').update('window.ready = true;\n').digest('base64');
  assert.deepEqual([...hashes], [`'sha256-${digest}'`]);
  assert.throws(() => inlineHashes('<button onclick="alert(1)">Go</button>'), /inline handlers/);
});

test('restricts executable sources while preserving configured analytics and inline presentation', () => {
  const directives = new Map(contentSecurityPolicy(new Set(["'sha256-known'"]), 'https://eu.i.posthog.com')
    .split('; ').map((value) => { const [name, ...sources] = value.split(' '); return [name, sources]; }));
  assert.deepEqual(directives.get('default-src'), ["'none'"]);
  assert.deepEqual(directives.get('script-src-attr'), ["'none'"]);
  assert.deepEqual(directives.get('frame-ancestors'), ["'none'"]);
  const scripts = directives.get('script-src');
  assert.ok(scripts.includes("'sha256-known'"));
  assert.ok(scripts.includes('https://eu-assets.i.posthog.com'));
  assert.ok(!scripts.includes("'unsafe-inline'") && !scripts.includes("'unsafe-eval'"));
  assert.ok(directives.get('style-src').includes("'unsafe-inline'"));
  assert.ok(!contentSecurityPolicy(new Set()).includes('posthog'));
  assert.throws(() => contentSecurityPolicy(new Set(), 'http://analytics.example.com'), /HTTPS/);
  assert.throws(() => contentSecurityPolicy(new Set(), 'https://user:password@example.com'), /HTTPS/);
});

test('emits one catch-all header from every generated page and refuses oversized or duplicate policies', async () => {
  const root = await mkdtemp(join(tmpdir(), 'marketing-csp-'));
  const dir = pathToFileURL(`${root}/`);
  try {
    await writeFile(join(root, '.env.production'), 'PUBLIC_POSTHOG_KEY=test\nPUBLIC_POSTHOG_HOST=https://analytics.example.com/ingest\n');
    await mkdir(join(root, 'terms'));
    await writeFile(join(root, 'index.html'), '<script>window.home=true</script>');
    await writeFile(join(root, 'terms/index.html'), '<script>window.legal=true</script>');
    const baseline = '/*\n  X-Frame-Options: DENY\n/_astro/*\n  Cache-Control: public\n';
    await writeFile(join(root, '_headers'), baseline);
    const { hooks } = integration();
    hooks['astro:config:setup']({ config: { root: dir }, mode: 'production' });
    const build = () => hooks['astro:build:done']({ dir, logger: { info() {} } });
    await build();
    const headers = await readFile(join(root, '_headers'), 'utf8');
    assert.ok(headers.startsWith('/*\n  Content-Security-Policy:'));
    for (const code of ['window.home=true', 'window.legal=true']) {
      assert.ok(headers.includes(createHash('sha256').update(code).digest('base64')));
    }
    assert.ok(headers.includes('https://analytics.example.com'));
    assert.ok(headers.includes('X-Frame-Options: DENY'));
    assert.ok(headers.includes('/_astro/*\n  Cache-Control: public'));
    await assert.rejects(build(), /already configured/);
    await writeFile(join(root, '_headers'), baseline);
    await writeFile(join(root, 'index.html'), Array.from({ length: 40 }, (_, i) => `<script>window.page=${i}</script>`).join(''));
    await assert.rejects(build(), /header limit/);
    assert.equal(await readFile(join(root, '_headers'), 'utf8'), baseline);
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});
