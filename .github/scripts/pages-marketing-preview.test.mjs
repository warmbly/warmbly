import assert from 'node:assert/strict';
import { mkdtemp, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { pathToFileURL } from 'node:url';
import { test } from 'node:test';
import { previewURL, verifyPreview } from './pages-marketing-preview.mjs';

test('accepts only a versioned Wrangler preview receipt for the reviewed commit and project', () => {
  const deployment = { type: 'pages-deploy-detailed', version: 1, pages_project: 'marketing',
    deployment_id: 'deployment-id', environment: 'preview', url: 'https://abc.marketing.pages.dev',
    deployment_trigger: { metadata: { commit_hash: 'sha' } } };
  const select = value => previewURL([value], 'marketing', 'sha');
  assert.equal(select(deployment).origin, 'https://abc.marketing.pages.dev');
  for (const override of [
    { environment: 'production' }, { url: 'https://abc.other.pages.dev' },
    { type: 'pages-deploy' }, { version: 2 }, { pages_project: 'other' }, { deployment_id: null },
    { deployment_trigger: { metadata: { commit_hash: 'old' } } },
  ]) assert.throws(() => select({ ...deployment, ...override }));
  assert.throws(() => previewURL([deployment, deployment], 'marketing', 'sha'));
  assert.equal(previewURL([{ type: 'pages-deploy' }, deployment], 'marketing', 'sha').origin, 'https://abc.marketing.pages.dev');
});

test('verifies page, error and byte-identical installer responses with the built policy', async () => {
  const root = await mkdtemp(join(tmpdir(), 'marketing-preview-'));
  try {
    for (const file of ['install.sh', 'cli.sh']) await writeFile(join(root, file), `# ${file}\n`);
    const paths = [];
    const request = async url => {
      paths.push(url.pathname);
      const script = url.pathname.endsWith('.sh');
      return new Response(script ? `# ${url.pathname.slice(1)}\n` : '<html>Page</html>', {
        status: url.pathname.includes('missing-page') ? 404 : 200,
        headers: { 'content-security-policy': 'policy', 'x-content-type-options': 'nosniff',
          'content-type': script ? 'text/plain' : 'text/html; charset=utf-8' },
      });
    };
    await verifyPreview('https://abc.marketing.pages.dev', 'policy', request, pathToFileURL(`${root}/`));
    assert.deepEqual(paths, ['/', '/terms/', '/privacy/', '/marketing-csp-missing-page', '/install.sh', '/cli.sh']);
    await writeFile(join(root, 'cli.sh'), 'different bytes');
    await assert.rejects(verifyPreview('https://abc.marketing.pages.dev', 'policy', request, pathToFileURL(`${root}/`)));
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

test('blocks promotion for absent or different policy, redirects and unexpected responses', async () => {
  for (const response of [
    new Response('', { status: 200 }),
    new Response('', { status: 200, headers: { 'content-security-policy': 'other' } }),
    new Response('', { status: 503 }),
  ]) await assert.rejects(verifyPreview('https://abc.marketing.pages.dev', 'policy', async () => response));
  await assert.rejects(verifyPreview('https://abc.marketing.pages.dev', 'policy', async (url, options) => {
    assert.equal(options.redirect, 'error');
    throw new Error('redirect refused');
  }), /redirect refused/);
});
