import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import vm from 'node:vm';

const layout = readFileSync(new URL('../src/layouts/Layout.astro', import.meta.url), 'utf8');
const script = layout.match(/<script is:inline>\s*(\(function \(\) \{\s*var DASHBOARD_HOST = 'app\.warmbly\.com';[\s\S]*?)<\/script>/)[1];
assert.match(layout, /cookieless_mode: 'always'/);
assert.match(layout, /person_profiles: 'never'/);
assert.doesNotMatch(script, /localStorage|sessionStorage|document\.cookie/);

function visit(href, referrer = '', targets = [], loading = false) {
  const location = new URL(href);
  const links = targets.map((target) => ({ href: new URL(target, href).href }));
  const listeners = {};
  vm.runInNewContext(script, {
    URL, URLSearchParams, location,
    document: {
      referrer,
      readyState: loading ? 'loading' : 'complete',
      querySelectorAll: () => links,
      addEventListener: (name, callback) => { listeners[name] = callback; },
    },
  });
  if (loading) listeners.DOMContentLoaded();
  return { links: links.map((link) => new URL(link.href)), listeners };
}

const first = visit('https://warmbly.com/?utm_source=reddit&utm_medium=social&utm_campaign=launch&utm_term=email&utm_content=hero', 'https://www.reddit.com/r/email', ['/pricing', 'https://app.warmbly.com/auth/register?plan=pro&utm_source=other', '/install.sh', 'https://example.com/'], true);
const pricing = first.links[0];
assert.equal(pricing.searchParams.get('wb_ref'), 'www.reddit.com');
assert.equal(pricing.searchParams.get('wb_lp'), '/');
const second = visit(pricing.href, 'https://warmbly.com/', ['https://app.warmbly.com/']);
for (const [name, value] of pricing.searchParams) assert.equal(second.links[0].searchParams.get(name), value);
assert.equal(first.links[1].searchParams.get('utm_source'), 'reddit');
assert.equal(first.links[1].searchParams.get('plan'), 'pro');
assert.equal(first.links[2].search, '');
assert.equal(first.links[3].search, '');

const google = visit('https://warmbly.com/pricing', 'https://www.google.com/search?q=private', ['https://app.warmbly.com/']).links[0];
assert.equal(google.searchParams.get('wb_ref'), 'www.google.com');
assert.equal(google.searchParams.get('wb_lp'), '/pricing');
assert.equal(google.searchParams.has('utm_source'), false);

const direct = visit('https://warmbly.com/pricing', '', ['/features']);
assert.equal(direct.links[0].searchParams.get('wb_lp'), '/pricing');
const directSignup = visit(direct.links[0].href, 'https://warmbly.com/pricing', ['https://app.warmbly.com/auth/register']).links[0];
assert.equal(directSignup.searchParams.get('wb_lp'), '/pricing');
assert.equal(directSignup.searchParams.has('wb_ref'), false);
for (const host of ['warmbly.com', 'www.warmbly.com', 'app.warmbly.com']) {
  const own = visit('https://warmbly.com/', `https://${host}/`, ['https://app.warmbly.com/']).links[0];
  assert.equal(own.searchParams.has('wb_ref'), false);
}

const { listeners } = visit('https://warmbly.com/pricing?utm_source=reddit&wb_lp=/first', 'https://www.reddit.com/');
for (const name of ['click', 'auxclick', 'contextmenu']) {
  const link = { href: 'https://app.warmbly.com/auth/register?utm_source=later&plan=pro' };
  listeners[name]({ target: { closest: () => link } });
  const url = new URL(link.href);
  assert.equal(url.searchParams.get('utm_source'), 'reddit');
  assert.equal(url.searchParams.get('wb_lp'), '/first');
  assert.equal(url.searchParams.get('plan'), 'pro');
}
const privatePath = visit('https://warmbly.com/?wb_lp=/pricing%3Femail=private%23section&wb_ref=https%3A%2F%2FWWW.REDDIT.COM%2Fr%2Femail', '', ['https://app.warmbly.com/']).links[0];
assert.equal(privatePath.searchParams.get('wb_lp'), '/pricing');
assert.equal(privatePath.searchParams.get('wb_ref'), 'www.reddit.com');
console.log('Cookieless marketing acquisition regressions passed');
