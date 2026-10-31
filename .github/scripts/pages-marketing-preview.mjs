import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';

export function previewURL(deployments, project, branch, sha) {
  const matches = deployments.filter(deployment => deployment.environment === 'preview'
    && deployment.deployment_trigger?.metadata?.branch === branch
    && deployment.deployment_trigger?.metadata?.commit_hash === sha
    && deployment.latest_stage?.name === 'deploy'
    && deployment.latest_stage?.status === 'success');
  assert.equal(matches.length, 1, 'Require one successful preview for this run and commit');
  const url = new URL(matches[0].url);
  assert.equal(url.protocol, 'https:');
  assert.ok(url.hostname.endsWith(`.${project}.pages.dev`), 'Require the configured Pages project');
  assert.ok(!url.username && !url.password && url.pathname === '/' && !url.search && !url.hash);
  return url;
}

export async function verifyPreview(url, policy, request = fetch, directory = new URL('../../site/dist/', import.meta.url)) {
  for (const [path, status, type] of [
    ['/', 200, 'text/html'], ['/terms/', 200, 'text/html'], ['/privacy/', 200, 'text/html'],
    ['/marketing-csp-missing-page', 404, 'text/html'],
    ['/install.sh', 200, null], ['/cli.sh', 200, null],
  ]) {
    const response = await request(new URL(path, url), { redirect: 'error', signal: AbortSignal.timeout(30000) });
    assert.equal(response.status, status, `Unexpected preview status for ${path}`);
    assert.equal(response.headers.get('content-security-policy'), policy, `Unexpected preview CSP for ${path}`);
    assert.equal(response.headers.get('x-content-type-options'), 'nosniff');
    if (type) assert.ok(response.headers.get('content-type')?.startsWith(type));
    else assert.equal(await response.text(), await readFile(new URL(path.slice(1), directory), 'utf8'));
  }
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  const { CLOUDFLARE_ACCOUNT_ID: account, CLOUDFLARE_PAGES_PROJECT_NAME: project,
    CLOUDFLARE_API_TOKEN: token, PREVIEW_BRANCH: branch, GITHUB_SHA: sha } = process.env;
  assert.ok(account && project && token && branch && sha, 'Require preview deployment configuration');
  const response = await fetch(`https://api.cloudflare.com/client/v4/accounts/${account}/pages/projects/${project}/deployments?env=preview&per_page=100`, {
    headers: { Authorization: `Bearer ${token}` }, signal: AbortSignal.timeout(30000),
  });
  assert.ok(response.ok, `Pages deployment lookup returned ${response.status}`);
  const data = await response.json();
  assert.equal(data.success, true, 'Pages deployment lookup must succeed');
  const url = previewURL(data.result, project, branch, sha);
  const headers = await readFile(new URL('../../site/dist/_headers', import.meta.url), 'utf8');
  const policies = [...headers.matchAll(/^\s+Content-Security-Policy: (.+)$/gm)];
  assert.equal(policies.length, 1, 'Require one built policy');
  await verifyPreview(url, policies[0][1]);
  console.log('Marketing preview headers, routes and published installers verified');
}
