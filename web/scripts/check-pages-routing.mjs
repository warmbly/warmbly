import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import ts from 'typescript';

const read = (path) => readFileSync(new URL(path, import.meta.url), 'utf8');
const rules = read('../public/_redirects').split('\n').filter((line) => line.trim() && !line.startsWith('#')).map((line) => {
  const [source, target, code] = line.trim().split(/\s+/);
  return { source, target, code };
});
const match = (path) => rules.find(({ source }) => source.endsWith('*') ? path.startsWith(source.slice(0, -1)) : path === source);
const routes = ts.createSourceFile('router.tsx', read('../src/router.tsx'), ts.ScriptTarget.Latest, true, ts.ScriptKind.TSX);
const roots = [];
const visit = (node) => {
  if (ts.isCallExpression(node) && ts.isIdentifier(node.expression)) {
    if (node.expression.text === 'standalone' && ts.isStringLiteral(node.arguments[0])) roots.push('/' + node.arguments[0].text);
    if (node.expression.text === 'createRoute' && ts.isObjectLiteralExpression(node.arguments[0])) {
      const properties = node.arguments[0].properties;
      const parent = properties.find((p) => p.name?.getText(routes) === 'getParentRoute');
      const path = properties.find((p) => p.name?.getText(routes) === 'path');
      if (parent && ts.isPropertyAssignment(parent) && ts.isArrowFunction(parent.initializer)
          && parent.initializer.body.getText(routes) === 'rootRoute'
          && path && ts.isPropertyAssignment(path) && ts.isStringLiteral(path.initializer)) {
        roots.push(path.initializer.text === '/' ? '/' : '/' + path.initializer.text);
      }
    }
  }
  ts.forEachChild(node, visit);
};
visit(routes);
assert.ok(roots.length > 10, 'root route discovery did not find the route table');
// Pages serves index.html only through "/": with a 404.html present, a rewrite to /index.html 404s every route.
assert.ok(rules.every(({ target }) => target === '/'), 'every Pages rewrite must target "/", not /index.html');
for (const path of [...roots.filter((root) => root !== '/'), '/auth/login/confirm', '/auth/reset-password/confirm', '/app/campaigns/a/preferences', '/app/unibox/inbox/thread', '/app/settings/security']) {
  const rule = match(path);
  assert.equal(rule?.target, '/', `Pages does not serve SPA route ${path}`);
  assert.equal(rule?.code, '200');
}
for (const path of ['/assets/missing.js', '/assets/layout-aTnJVElG.js', '/config.js', '/favicon.ico', '/mail-preview.html', '/mail-preview', '/missing-resource']) {
  assert.equal(match(path), undefined, `${path} must not rewrite to HTML`);
}
assert.match(read('../public/404.html'), /<!doctype html>/i);
assert.doesNotMatch(read('../public/_headers'), /Cache-Control:.*immutable/);

const headerRules = [];
for (const line of read('../public/_headers').split('\n')) {
  if (!line.trim() || line.trim().startsWith('#')) continue;
  if (line.startsWith('/')) {
    headerRules.push({ source: line.trim(), headers: [] });
  } else {
    headerRules.at(-1).headers.push(line.trim());
  }
}
const headersFor = (path) => {
  const headers = new Map();
  for (const { source, headers: lines } of headerRules) {
    if (!(source.endsWith('*') ? path.startsWith(source.slice(0, -1)) : path === source)) continue;
    for (const line of lines) {
      if (line.startsWith('! ')) {
        headers.delete(line.slice(2).toLowerCase());
      } else {
        const colon = line.indexOf(':');
        const name = line.slice(0, colon).toLowerCase();
        const value = line.slice(colon + 1).trim();
        headers.set(name, headers.has(name) ? `${headers.get(name)}, ${value}` : value);
      }
    }
  }
  return headers;
};
const previewHeaders = headersFor('/mail-preview.html');
// Pages redirects .html assets to extensionless URLs; both must carry the same isolated policy.
assert.deepEqual(headersFor('/mail-preview'), previewHeaders, 'Pages canonical preview URL must retain the .html preview headers');
assert.equal(previewHeaders.get('x-frame-options'), 'SAMEORIGIN');
assert.equal(previewHeaders.get('referrer-policy'), 'no-referrer');
assert.equal(previewHeaders.get('cache-control'), 'no-store');
assert.match(previewHeaders.get('content-security-policy'), /(?:^|; )script-src 'none';/);
assert.match(previewHeaders.get('content-security-policy'), /(?:^|; )frame-ancestors 'self';/);
assert.match(previewHeaders.get('content-security-policy'), /(?:^|; )img-src data: https: http:;/);
assert.ok(!previewHeaders.get('content-security-policy').includes(','), 'preview must have one policy, not an intersection with the dashboard policy');
for (const path of ['/', '/app/unibox/inbox', '/mail-preview-other', '/mail-preview/other', '/assets/missing.js']) {
  const headers = headersFor(path);
  assert.equal(headers.get('x-frame-options'), 'DENY', `${path} must retain dashboard clickjacking protection`);
  assert.match(headers.get('content-security-policy'), /(?:^|; )frame-ancestors 'none'$/, `${path} must not use the preview policy`);
}
console.log('Pages SPA deep links and missing-asset separation passed');
