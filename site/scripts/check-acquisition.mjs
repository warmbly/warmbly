import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import vm from 'node:vm';

const layout = readFileSync(new URL('../src/layouts/Layout.astro', import.meta.url), 'utf8');
const script = layout.match(/<script is:inline>\s*(\(function \(\) \{\s*var DASHBOARD_HOST = 'app\.warmbly\.com';[\s\S]*?)<\/script>/)[1];
assert.match(layout, /cookieless_mode: 'always'/);
assert.match(layout, /person_profiles: 'never'/);
assert.doesNotMatch(script, /localStorage|sessionStorage|document\.cookie/);
// Nothing on the site writes a cookie, partner referrals included.
assert.doesNotMatch(layout, /document\.cookie/);

const replaced = [];
function visit(href, referrer = '', targets = [], loading = false) {
  const location = new URL(href);
  const links = targets.map((target) => ({ href: new URL(target, href).href }));
  const listeners = {};
  vm.runInNewContext(script, {
    URL, URLSearchParams, location,
    history: { state: null, replaceState: (_state, _title, url) => replaced.push(url) },
    document: {
      referrer,
      readyState: loading ? 'loading' : 'complete',
      querySelectorAll: () => links,
      addEventListener: (name, callback) => { (listeners[name] ||= []).push(callback); },
    },
  });
  if (loading) listeners.DOMContentLoaded.forEach((callback) => callback());
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
  listeners[name].forEach((callback) => callback({ target: { closest: () => link }, button: 0, preventDefault() {} }));
  const url = new URL(link.href);
  assert.equal(url.searchParams.get('utm_source'), 'reddit');
  assert.equal(url.searchParams.get('wb_lp'), '/first');
  assert.equal(url.searchParams.get('plan'), 'pro');
}
const privatePath = visit('https://warmbly.com/?wb_lp=/pricing%3Femail=private%23section&wb_ref=https%3A%2F%2FWWW.REDDIT.COM%2Fr%2Femail', '', ['https://app.warmbly.com/']).links[0];
assert.equal(privatePath.searchParams.get('wb_lp'), '/pricing');
assert.equal(privatePath.searchParams.get('wb_ref'), 'www.reddit.com');
const samePath = visit('https://warmbly.com/pricing?utm_source=ad', '', ['/pricing?interval=year', '#faq']);
assert.equal(samePath.links[0].searchParams.get('utm_source'), 'ad');
assert.equal(samePath.links[0].searchParams.get('wb_lp'), '/pricing');
assert.equal(samePath.links[0].searchParams.get('interval'), 'year');
assert.equal(samePath.links[1].href, 'https://warmbly.com/pricing?utm_source=ad#faq');
const invalidPath = visit(`https://warmbly.com/pricing?wb_lp=${encodeURIComponent('/\\evil.example/offer')}`, '', ['https://app.warmbly.com/']).links[0];
assert.equal(invalidPath.searchParams.get('wb_lp'), '/pricing');
// The link service's dub_id is shown as wb_click at once and carried under that name.
replaced.length = 0;
const partner = visit('https://warmbly.com/pricing?dub_id=cl1ck_ID-9&utm_source=x#faq', '', ['/features', 'https://app.warmbly.com/']).links;
assert.deepEqual(replaced, ['/pricing?utm_source=x&wb_click=cl1ck_ID-9#faq']);
for (const link of partner) {
  assert.equal(link.searchParams.get('wb_click'), 'cl1ck_ID-9');
  assert.equal(link.searchParams.has('dub_id'), false);
}
assert.equal(visit(partner[0].href, '', ['https://app.warmbly.com/']).links[0].searchParams.get('wb_click'), 'cl1ck_ID-9');
replaced.length = 0;
assert.equal(visit('https://warmbly.com/?dub_id=a%20b', '', ['https://app.warmbly.com/']).links[0].searchParams.has('wb_click'), false);
assert.deepEqual(replaced, ['/']);
replaced.length = 0;
visit('https://warmbly.com/pricing?utm_source=x', '', []);
assert.deepEqual(replaced, []);
// ?via= travels until its click is recorded, then gives way to wb_click.
const via = visit('https://warmbly.com/?via=john', '', ['/pricing']).links[0];
assert.equal(via.searchParams.get('via'), 'john');
const recorded = visit('https://warmbly.com/pricing?via=john&wb_click=abc123', '', ['/features?via=stale', 'https://app.warmbly.com/']).links;
for (const link of recorded) {
  assert.equal(link.searchParams.get('wb_click'), 'abc123');
  assert.equal(link.searchParams.has('via'), false);
}
assert.equal(visit('https://warmbly.com/?via=%3Cb%3E', '', ['/pricing']).links[0].searchParams.has('via'), false);

// The partner script records one click per via and puts its id in the address bar.
const partnerScript = layout.match(/<script is:inline define:vars=\{\{ partnerLinkDomain \}\}>([\s\S]*?)<\/script>/)[1];
async function partnerClick(href, answer = { ok: true, json: async () => ({ clickId: 'cl1ck_ID-9' }) }) {
  const calls = [];
  const urls = [];
  vm.runInNewContext(partnerScript, {
    URLSearchParams, location: new URL(href), document: { referrer: 'https://x.com/' }, partnerLinkDomain: 'go.warmbly.com', window: {},
    history: { state: null, replaceState: (_s, _t, url) => urls.push(url) },
    fetch: (url, init) => { calls.push({ url, body: JSON.parse(init.body) }); return Promise.resolve(answer); },
  });
  await new Promise((resolve) => setTimeout(resolve, 0));
  return { calls, urls };
}
const tracked = await partnerClick('https://warmbly.com/pricing?via=john&utm_source=x#faq');
assert.deepEqual(tracked.calls, [{ url: 'https://api.dub.co/track/click', body: { domain: 'go.warmbly.com', key: 'john', url: 'https://warmbly.com/pricing?via=john&utm_source=x#faq', referrer: 'https://x.com/' } }]);
assert.deepEqual(tracked.urls, ['/pricing?utm_source=x&wb_click=cl1ck_ID-9#faq']);
assert.deepEqual((await partnerClick('https://warmbly.com/?utm_source=x')).calls, []);
assert.deepEqual((await partnerClick('https://warmbly.com/?via=john&wb_click=abc')).calls, []);
assert.deepEqual((await partnerClick('https://warmbly.com/?via=%3Cb%3E')).calls, []);
const refused = await partnerClick('https://warmbly.com/?via=john', { ok: false, json: async () => ({}) });
assert.equal(refused.calls.length, 1);
assert.deepEqual(refused.urls, []);
assert.deepEqual((await partnerClick('https://warmbly.com/?via=john', { ok: true, json: async () => ({ clickId: '<b>' }) })).urls, []);

// A dashboard click waits for an in-flight partner click and leaves with its id.
{
  const location = new URL('https://warmbly.com/?via=john');
  const window = {};
  const listeners = {};
  let release;
  window.wbPartnerClick = new Promise((resolve) => { release = resolve; });
  const assigned = [];
  location.assign = (url) => assigned.push(url);
  vm.runInNewContext(script, {
    URL, URLSearchParams, location, window, setTimeout,
    history: { state: null, replaceState() {} },
    document: { referrer: '', readyState: 'complete', querySelectorAll: () => [], addEventListener: (name, callback) => { (listeners[name] ||= []).push(callback); } },
  });
  const link = { href: 'https://app.warmbly.com/auth/register', target: '', hasAttribute: () => false };
  let prevented = false;
  const event = { target: { closest: () => link }, button: 0, preventDefault() { prevented = true; } };
  listeners.click.forEach((callback) => callback(event));
  assert.equal(prevented, true);
  assert.deepEqual(assigned, []);
  // The click is recorded: the address bar now carries wb_click.
  location.search = '?wb_click=cl1ck_ID-9';
  release();
  await new Promise((resolve) => setTimeout(resolve, 0));
  assert.equal(new URL(assigned[0]).searchParams.get('wb_click'), 'cl1ck_ID-9');

  // A new-tab click is never held.
  prevented = false;
  listeners.click.forEach((callback) => callback({ ...event, metaKey: true }));
  assert.equal(prevented, false);
}

console.log('Cookieless marketing acquisition regressions passed');
