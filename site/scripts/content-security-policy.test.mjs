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
  const inherited = Object.entries(process.env).filter(([name]) => name.startsWith('PUBLIC_POSTHOG_'));
  for (const [name] of inherited) delete process.env[name];
  try {
    await writeFile(join(root, '.env.production'), 'PUBLIC_POSTHOG_KEY=test\nPUBLIC_POSTHOG_HOST=https://analytics.example.com/ingest\n');
    await mkdir(join(root, 'terms'));
    await writeFile(join(root, 'index.html'), '<script>window.home=true</script>');
    await writeFile(join(root, 'terms/index.html'), '<script>window.legal=true</script>');
    const baseline = '/*\n  X-Frame-Options: DENY\n/_astro/*\n  Cache-Control: public\n';
    await writeFile(join(root, '_headers'), baseline);
    const { hooks } = integration();
    let vite;
    hooks['astro:config:setup']({ updateConfig: config => { vite = config.vite; } });
    vite.plugins[0].configResolved({ mode: 'production', envDir: root });
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
    const assetsOnly = '/_astro/*\n  Cache-Control: public\n';
    await writeFile(join(root, '_headers'), assetsOnly);
    await assert.rejects(build(), /catch-all rule/);
    assert.equal(await readFile(join(root, '_headers'), 'utf8'), assetsOnly);
    const reordered = '/_astro/*\r\n  Cache-Control: public\r\n/* \t\r\n  X-Frame-Options: DENY\r\n';
    await writeFile(join(root, '_headers'), reordered);
    await build();
    const reorderedHeaders = await readFile(join(root, '_headers'), 'utf8');
    assert.ok(reorderedHeaders.startsWith('/_astro/*\r\n  Cache-Control: public\r\n/* \t\r\n  Content-Security-Policy:'));
    assert.ok(reorderedHeaders.includes('\r\n  X-Frame-Options: DENY\r\n'));
    await mkdir(join(root, 'config'));
    await writeFile(join(root, 'config/.env.staging'), 'PUBLIC_POSTHOG_KEY=test\nPUBLIC_POSTHOG_HOST=https://staging-analytics.example.com\n');
    vite.plugins[0].configResolved({ mode: 'staging', envDir: join(root, 'config') });
    await writeFile(join(root, '_headers'), baseline);
    await build();
    const stagingHeaders = await readFile(join(root, '_headers'), 'utf8');
    assert.ok(stagingHeaders.includes('https://staging-analytics.example.com'));
    assert.ok(!stagingHeaders.includes('https://analytics.example.com'));
    process.env.PUBLIC_POSTHOG_KEY = 'test';
    process.env.PUBLIC_POSTHOG_HOST = 'https://configured-analytics.example.com';
    vite.plugins[0].configResolved({ mode: 'production', envDir: root });
    await writeFile(join(root, '_headers'), baseline);
    await build();
    assert.ok((await readFile(join(root, '_headers'), 'utf8')).includes('https://configured-analytics.example.com'));
    await writeFile(join(root, '_headers'), baseline);
    await writeFile(join(root, 'index.html'), Array.from({ length: 40 }, (_, i) => `<script>window.page=${i}</script>`).join(''));
    await assert.rejects(build(), /header limit/);
    assert.equal(await readFile(join(root, '_headers'), 'utf8'), baseline);
  } finally {
    for (const name of Object.keys(process.env).filter(name => name.startsWith('PUBLIC_POSTHOG_'))) delete process.env[name];
    for (const [name, value] of inherited) process.env[name] = value;
    await rm(root, { recursive: true, force: true });
  }
});
