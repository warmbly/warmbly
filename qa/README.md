# Proof harness

Recorded proof for pull requests. A flow is a short scripted Playwright walk
through the dashboard. Running it produces a 1920x1080 H.264 walkthrough and
named full-resolution stills, and `pnpm share` puts them on the PR:

- **first publish**: a Proof section appended to the PR description, stills
  inline, walkthrough videos last
- **every publish after that**: a comment with before/after pairs for only the
  stills that changed since the previous publish, the commits in between, and
  the videos of the flows that changed. Nothing changed means nothing is posted

It is not a test gate and CI does not run it (CI only typechecks it and checks
the flows load). It exists so a reviewer can see a change without checking it
out.

## When to record

Only for a UI change worth watching. Most PRs need no proof.

- **record**: a new page, dialog, drawer or multi-step flow; a redesigned or
  re-laid-out screen; a changed interaction (new controls, new states, a
  different path through a task); a visible bug fix where the before was
  visibly broken; or when the user asks for it
- **skip**: anything without a visible change (backend, API, migrations,
  workers, tests, CI, docs, refactors, dependency bumps) and small visual
  tweaks (copy, a label, an icon, spacing or colour nudges). Describe those in
  the PR text. When unsure, skip
- **follow-ups**: re-record only when the commit changes what the proof shows

## Why scripted

The model never looks at the page while the browser runs. You write the flow
from the code you just changed (you know the routes, labels and fields), run
it, and read one line: pass, or the failing step. A 15-step flow costs a few
thousand tokens to write and nothing to re-run. Driving a browser step by step
from screenshots or snapshots costs that much on every step of every run, and
leaves dead air in the video while the model thinks.

## Be frugal with the machine

Several agents share this machine, and every stack and browser holds real
memory. The harness enforces the expensive parts; the rest is on you.

- **one recording at a time, machine-wide.** `pnpm proof` (and `pnpm aria`)
  take a lock in the system temp directory; a second one waits for the first
  instead of starting its own Chromium. Never fan recording out to subagents:
  they would only queue behind each other while each holds a context
- **one stack per worktree**, in the smallest mode the flow needs. `lite` is
  enough for anything that only reads or edits records
- **stop it when you are done**: `pnpm stack down` after `pnpm share`. A stack
  nobody records against stops itself after 60 minutes (`QA_STACK_IDLE_MIN`),
  but do not lean on that
- the browser and the encoder never run at once: frames are spooled to disk
  during the run and encoded after the browser exits, one flow at a time, with
  bounded encoder threads

Measured on this repo's dashboard:

| | memory |
| --- | --- |
| `lite` stack (backend, dashboard, Redis, NATS, Mailpit) | ~300 MB idle, ~500 MB once used |
| `full` stack (+ consumer, worker, realtime) | ~560 MB idle |
| `sandbox` stack (+ tracking, Dovecot, simulator) | ~760 MB idle |
| Chromium during a recording (proportional set size) | ~800 MB |
| ffmpeg encoding, after Chromium has exited | ~580 MB |

## One-time setup

```bash
make infra              # repo root: the shared postgres (each stack gets its own database in it)
cd qa && pnpm bootstrap # deps, Playwright's Chromium, the small stack images, then `pnpm check`
```

`pnpm check` verifies node 22.18+, ffmpeg with libx264, Chromium (and that it
launches), gh 2.99+ and signed in, docker, the shared postgres, the images each
mode needs, and this worktree's stack. Each failure prints its fix.

## The loop

```bash
pnpm stack up                 # this worktree's own stack (lite unless told otherwise)
pnpm proof contacts           # run the flows whose file or title matches
git push && gh pr create ...  # the PR has to exist first
pnpm share                    # publish the last run to this branch's PR
pnpm stack down               # free the memory
```

After a follow-up commit: push, record the flows your change touched again,
and `pnpm share`. It posts a comment, never a second description section.
Record follow-ups with `pnpm proof:fresh <filter>`, which re-seeds the stack
(about 15 seconds) before recording, so the comparison shows what the commit
changed and not what background jobs did to the data in the meantime.

`pnpm share --dry-run` prints the body and the gh command without uploading
anything, and works before the PR exists. `--no-video` posts stills only,
`--force` comments even when no still changed, `--replace-body` rewrites the
description's Proof section instead of commenting (for a re-record nobody has
reviewed yet), `--allow-failed` publishes a run with failing flows.

`share` refuses a run with failing flows and a run recorded at a different
commit than HEAD, and warns when the tree was dirty or HEAD is not pushed.

## The stack

`pnpm stack up [lite|full|sandbox]` runs this worktree's Warmbly natively
against the shared postgres, with everything else private to it: its own
database (`warmbly_qa_<worktree>`), Redis, NATS and Mailpit, on ports picked
once per worktree and remembered in `.artifacts/stack/ports`. Nothing crosses
between stacks: no cached profile, no worker event, no login code. The Go
services run from binaries built once per `up` (`go build`, cached), not from
one `go run` toolchain per service. Linux and WSL.

| Mode | Runs | Seed and account |
| --- | --- | --- |
| `lite` | backend, dashboard | rich seed (`SEED_RICH` + `SEED_FULL`), `dev@warmbly.com` |
| `full` | lite + consumer, worker, realtime | same seed; sends, syncs and live updates work |
| `sandbox` | full + tracking, Dovecot, simulator | Sunrise Labs (`sandbox@warmbly.test`): live mailboxes, campaign mail, opens, clicks, replies |

`lite` and `full` share a seed and switch freely. The sandbox is a different
organization, so moving to or from it needs `pnpm stack reset-data <mode>`;
`up` refuses rather than mix them. In the sandbox the simulator keeps changing
data while you record, so its stills differ from run to run by design.

Other commands: `status` (what runs, ports, memory), `logs [name]`, `restart`
(rebuilds the binaries; the dashboard hot-reloads on its own), `reset-data
[mode]` (drop the database, fresh Redis, reseed), `ls` (every QA stack on the
machine), `down`.

Realtime and tracking run from the local `ghcr.io/warmbly/warmbly/*:prod`
images. `up` warns when one was built before the last commit to its source,
with the command that rebuilds it.

Sign-in happens once per stack through the API and the login code in the
stack's Mailpit; the first-run wizard is completed through its own endpoint,
and the session is saved under `.artifacts/auth`. A flow that lands on the
sign-in page, onboarding or the workspace picker fails and says why.

Recording against a stack the harness did not start is possible but explicit:
`QA_WEB_URL` and `QA_API_URL` (and `QA_MAILPIT_URL`, `QA_EMAIL`,
`QA_PASSWORD`). Without either, `pnpm proof` refuses rather than record
whatever happens to answer on the default ports, which is usually another
session's stack.

## Unibox Drafts proof fixtures

Apply `fixtures/unibox-drafts.sql` to this worktree's isolated rich-seed database,
then run `QA_UNIBOX_DRAFTS_FIXTURES=1 pnpm proof unibox-drafts`. The fixture adds
one inert pending agent reply and places one synthetic mailbox message in Drafts.
The walkthrough opens the agent reply for review without sending it.

## Fleet admin proof fixtures

`flows/fleet-telemetry.flow.ts` is opt-in: apply `fixtures/fleet-telemetry.sql`
to this worktree's isolated rich-seed database immediately before recording.
It deliberately makes the capacity view stale while current worker rows hold
the planning counts. All addresses, readings and send counters are synthetic.

The lite stack serves the customer dashboard. Start `admin/` separately with
`VITE_API_URL` pointing to this stack's API, and set `QA_WEB_URL` to its localhost
origin. The seeded session needs view-workers permission (16), verified admin
MFA, and the token JSON stored as `warmbly_admin_token` at that origin. These
are local authentication fixtures, not MFA proof. Invalidate the seeded user's
and session's isolated Redis cache entries after SQL permission/session changes.
Never use production accounts, tokens or databases.

Run `QA_FLEET_FIXTURES=1 pnpm proof fleet-telemetry` in `qa/`. Stop the additional
admin server as well as the stack afterward. This proof does not validate real
worker sampling, network egress, mail delivery or inbox placement.

## Billing plans proof

`flows/billing-agency-plans.flow.ts` records the agency plans on a workspace's Plans tab. The default stack
runs with billing off, where the billing page has no Plans tab, so the flow is opt-in: start the stack with
`QA_STRIPE_SECRET_KEY` set to a Stripe test-mode key (nothing is charged; the flow only reads plans). The
seeded account must be staff of no agency and its workspace attached to none, which a fresh seed is: the link
the flow looks for reads "Set up an agency" only then. Run `QA_BILLING=1 pnpm proof billing-agency-plans`.

`flows/plans-popup.flow.ts` records the plan chooser popup, opened from **Settings > Billing**, and the Plans
tab's cards, on the same stack: `QA_BILLING=1 pnpm proof plans-popup`.

## Admin agency proof fixtures

`flows/admin-agencies.flow.ts` records an operator giving an agency a plan, a pool of its own and extra sends
in the admin panel. Start the lite stack with `QA_STRIPE_SECRET_KEY` set (with billing off every agency already
holds everything, so a given plan changes nothing), then serve `admin/` separately with `VITE_API_URL` pointing
to the stack's API and set `QA_WEB_URL` to that localhost admin origin. Sign the seeded account in once so its
session exists, apply `fixtures/admin-agencies.sql`, and clear the stack's own Redis so the new permissions
and session state are read. The fixture grants every admin permission and marks the session as having passed
a second factor: local authentication fixtures, not MFA proof.

Run `QA_ADMIN_AGENCY_FIXTURES=1 pnpm proof admin-agencies` in `qa/`. The flow asserts through the API that the
client workspace receives the plan and that the pool totals are what the page shows. Then, without `QA_WEB_URL`,
`QA_ADMIN_AGENCY_FIXTURES=1 pnpm proof agency-arranged-plan` records the agency's own billing page for the plan
just given. Stop the admin server and the stack afterward.

`flows/agency-branding-look.flow.ts` uses the same fixture, which leaves the agency without a brand: it records
**Agency > Branding** from nothing set to a finished look, picking a colour, font, corners and a sign-in layout
and watching the preview follow. Run it against the dashboard, without `QA_WEB_URL`:
`QA_ADMIN_AGENCY_FIXTURES=1 pnpm proof agency-branding-look`.

`flows/agency-extra-sends.flow.ts` records **Agency > Billing**: the pack price, the automatic top-ups of sends
and of the AI balance with what each charges, and what happens when sends run out. It needs the same fixture with Agency Pro in force on the
agency (set `plan_id` to the Agency Pro plan, `status = 'active'` and a current period on its row; the flow expects
that plan's $60 pack). No step charges a card:
`QA_ADMIN_AGENCY_FIXTURES=1 pnpm proof agency-extra-sends`.

`flows/agency-mailboxes.flow.ts` records **Agency > Mailboxes**: every client's mailboxes, a vendor account the agency
connects once, and that account opened in a client's import. It needs the same fixture (apply it again before each
recording: it removes the vendor account the flow connects) and the mock inbox vendors, which the `lite` stack runs
beside itself when started with `QA_VENDOR_MOCK=1 pnpm stack up lite`. Then:
`QA_ADMIN_AGENCY_FIXTURES=1 pnpm proof agency-mailboxes`.

`flows/client-connect-vendor.flow.ts` records a client connecting many mailboxes from its own vendor account through
a connect link: the link is made in the dashboard, opened on the portal, and the mock vendor's mailboxes are picked
and imported. It needs the same fixture (it marks the product tour as seen), the mock inbox vendors
(`QA_VENDOR_MOCK=1 pnpm stack up lite`) and the portal service in front of this stack, started as described under
[agency clients and the portal domain](#agency-clients-and-the-portal-domain); the dashboard build and preview there
are not needed for this flow. Then: `QA_PORTAL_URL=http://localhost:8095 pnpm proof client-connect-vendor`. Stop the
portal process afterwards along with the stack.

## Member access proof fixtures

`flows/member-access.flow.ts` is opt-in: apply `fixtures/member-access.sql` to
this worktree's rich-seed database, which adds a teammate restricted to the
Outbound folder and one mailbox (`client@acme-agency.test`, `password123`).
The first flow saves a change to that scope, so apply the fixture again before
every recording.

```bash
docker exec -i warmbly-postgres-1 psql -U warmbly -d warmbly_qa_<worktree> < fixtures/member-access.sql
QA_MEMBER_ACCESS_FIXTURES=1 pnpm proof member-access
```

## Campaign estimate proof fixtures

`flows/campaign-estimate.flow.ts` is opt-in: apply `fixtures/campaign-estimate.sql`
to this worktree's rich-seed database, which adds a 300-lead **Agencies** list on
the reserved `.example` TLD, gives any active sender without warmup history three
weeks of it, and marks the owner's product tour finished so it does not cover the
page. Clear the stack's private Redis afterwards so the cached user is re-read.

```bash
docker exec -i warmbly-postgres-1 psql -U warmbly -d warmbly_qa_<worktree> < fixtures/campaign-estimate.sql
docker exec wqa-<worktree>-redis redis-cli FLUSHDB
QA_CAMPAIGN_ESTIMATE_FIXTURES=1 pnpm proof campaign-estimate
```

## Sign-in methods proof fixtures

`flows/signin-methods.flow.ts` is opt-in: apply `fixtures/signin-methods.sql` to
this worktree's rich-seed database after the stack has signed in. It turns the
seeded owner into an account with Apple and Google linked and no password, and
stamps the saved session as freshly confirmed for five minutes, so record right
away. Clear the stack's private Redis afterwards, and `pnpm stack reset-data`
once you are done, because the owner no longer has a password.

```bash
docker exec -i warmbly-postgres-1 psql -U warmbly -d warmbly_qa_<worktree> < fixtures/signin-methods.sql
docker exec wqa-<worktree>-redis redis-cli FLUSHDB
QA_SIGNIN_METHODS_FIXTURES=1 pnpm proof signin-methods
```

## Tester sample data proof

The `tester-sample-data` flow uses the real local tester-provisioning and sample-data endpoints. Start the worktree's lite stack, then serve `admin/` separately against its API. Set `QA_WEB_URL` to that localhost admin origin. Prepare a seeded admin holding `view_users` and `manage_testers`, with a locally MFA-verified session; clear only that user's and session's isolated Redis entries after SQL fixture changes. These authentication fixtures are not proof of real MFA. Run `QA_TESTER_SAMPLE_FIXTURES=1 pnpm proof tester-sample-data`.

The flow creates a disposable dedicated reviewer workspace without displaying its password, confirms the sample-data dialog, checks persisted status and a no-op retry, and revokes the reviewer afterward. It does not connect a mailbox, send email, launch a scan or populate a customer workspace. Stop the additional admin server and the QA stack when finished.

## Writing a flow

Flows live in `flows/<area>.flow.ts`, one `test` per walkthrough. Extend the
area's file when your change lands in it; add one when it does not exist.

```ts
import { expect, test } from "../lib/proof.ts";

test("search contacts and open a contact's details", async ({ page, proof }) => {
  await page.goto("/app/contacts");
  await expect(page.getByRole("table")).toBeVisible();
  await proof.chapter("Contacts", "Search the list, then open one contact");

  await page.getByRole("textbox", { name: /Search by name/ }).pressSequentially("beth", { delay: 60 });
  await expect(page.getByRole("row", { name: /Beth Chen/ })).toBeVisible();
  await proof.shot("search-results", { caption: "Search narrowed to one contact" });
});
```

- `proof.chapter(title, description?)` shows a title card in the video
- `proof.shot(name, { caption?, target?, ignore? })` saves a still. The name is
  kebab-case and is the key follow-up comments diff by, so keep it stable
  across commits. `target` shoots one element. `ignore` takes locators whose
  content changes on its own (relative times, live counters): the still is
  published untouched, and those regions are skipped when it is compared with
  the previous publish
- `proof.dwell(ms?)` holds a result on screen long enough to read
- `test.use({ seed: "sandbox" })` for a flow written against the Sunrise Labs
  data. A flow is skipped, with the command that fixes it, when the stack holds
  the other seed
- `test.use({ signedIn: false })` for a flow that starts on the sign-in pages
- `test.use({ account: { email, password } })` for a flow recorded as another
  seeded or fixture account. It signs in once through Mailpit like the stack's
  own account, and keeps its session in a file of its own

Conventions:

- `expect(...)` the content before every `chapter` and `shot`, so neither lands
  on a loading state
- locate by role and accessible name (`getByRole`, `getByLabel`, `getByText`);
  no CSS classes
- `pressSequentially(text, { delay: 60 })` where typing should be visible,
  `fill` where it should not
- no `waitForTimeout` except through `dwell`
- keep a walkthrough under about 30 seconds; split longer stories into tests
- the title reads as a sentence about what the reviewer sees; it heads the PR
  section

When you need a selector you cannot read off the code, print the page's
accessibility tree with the saved session instead of opening a browser:

```bash
pnpm aria /app/contacts            # whole page
pnpm aria /app/contacts main       # one region, fewer tokens
```

Do not read the video or screenshot your way around the page. Read a still
only when the change is a visual judgment (spacing, colour, layout) that the
assertions cannot make.

## When a flow fails

The list reporter prints the failing step and its locator. Failures keep a
Playwright trace (DOM snapshots, network, console) and the reporter prints its
path; open it with `pnpm exec playwright show-trace <trace.zip>` when the error
line is not enough. A failed flow's frames are dropped instead of encoded
(`QA_VIDEO_ON_FAIL=1` keeps them).

## Recording settings

1920x1080 at 30 fps, H.264 CRF 18 (`slow` preset), yuv420p so every browser's
player shows it. A clip over 10 MB is re-encoded smaller. Frames come from
Playwright's screencast at full size and are encoded here, because Playwright's
built-in `video` option is VP8 at 1 Mbit/s and smears text. The video starts at
the first painted frame, not on the blank page before the app renders. The
animated cursor, click ripples and action labels are Playwright's
`screencast.showActions`. Trace screenshots stay off: they would start a
smaller screencast first, and the recorder refuses frames below 1080p.

| Variable | Default | |
| --- | --- | --- |
| `QA_FPS` | `30` | frame rate |
| `QA_CRF` | `18` | quality, lower is better and bigger |
| `QA_PRESET` | `slow` | x264 preset |
| `QA_ENCODE_THREADS` | `4` | encoder threads, which also bound its memory |
| `QA_SLOWMO` | `250` | ms between actions, so a viewer can follow |
| `QA_MAX_VIDEO_MB` | `10` | re-encode above this |
| `QA_VIDEO_ON_FAIL` | | `1` encodes failed flows too |
| `QA_STACK_IDLE_MIN` | `60` | stop an unused stack after this long; `0` never |
| `QA_LOCK_WAIT_MIN` | `30` | how long a recording waits for another to finish |
| `QA_WEB_URL`, `QA_API_URL`, `QA_MAILPIT_URL` | this worktree's stack | record a stack the harness did not start |
| `QA_EMAIL`, `QA_PASSWORD` | the mode's seeded account | account to sign in as |
| `QA_ALLOW_HOST` | | extra hostnames allowed besides localhost |

## Safety

The repository is public and so is everything attached to its pull requests.
The harness refuses to record anything but localhost (or a host named in
`QA_ALLOW_HOST`), so recordings only ever show seed data. Never point it at
production or a customer workspace, and never attach media to a PR any other
way: no image hosts, no commits of screenshots, no other repositories.
Everything the harness writes stays under the ignored `.artifacts/`.

## Warmup placement proof fixtures

`flows/warmup-placement.flow.ts` is opt-in: apply `fixtures/warmup-placement.sql`
to this worktree's rich-seed database. It adds an A/B arm to the entry step of the
seeded **RevOps outreach - July** campaign, two pool peers (Google Workspace and
Microsoft 365, on the reserved `.example` TLD) and synthetic warmup openings of that
step with the receipts they filed, and marks the owner's product tour finished. It
is re-runnable. Clear the stack's private Redis afterwards.

```bash
docker exec -i warmbly-postgres-1 psql -U warmbly -d warmbly_qa_<worktree> < fixtures/warmup-placement.sql
docker exec wqa-<worktree>-redis redis-cli FLUSHDB
QA_WARMUP_PLACEMENT_FIXTURES=1 pnpm proof warmup-placement
```

The placement numbers are fixture data, not a measurement of real delivery.

## Warmup filtering proof fixtures

`flows/warmup-filtering.flow.ts` is opt-in: apply `fixtures/warmup-filtering.sql`
to this worktree's rich-seed database. It marks the owner's product tour
finished, clears the workspace's `external_warmup` settings and the seeded
`dev.outbound@warmbly.test` filter tag, so the walkthrough starts from the
defaults. It is re-runnable. Clear the stack's private Redis afterwards.

```bash
docker exec -i warmbly-postgres-1 psql -U warmbly -d warmbly_qa_<worktree> < fixtures/warmup-filtering.sql
docker exec wqa-<worktree>-redis redis-cli FLUSHDB
QA_WARMUP_FILTERING_FIXTURES=1 pnpm proof warmup-filtering
```

## Regions and timezones proof fixtures

`flows/regions-and-timezones.flow.ts` is opt-in: apply `fixtures/regions-and-timezones.sql`
to this worktree's rich-seed database. It adds 120 leads on the reserved `.example` TLD
whose custom fields carry a country, state, city, location or timezone in the shapes
exports use (a tenth carry none), a **Prospects worldwide** segment holding them, and a
draft **Worldwide outreach** campaign on New York time with the same leads. It is
re-runnable: it removes the region segments a previous run created and resets the
campaign to its own timezone. The stack's backend works the locations out within a minute.

```bash
docker exec -i warmbly-postgres-1 psql -U warmbly -d warmbly_qa_<worktree> < fixtures/regions-and-timezones.sql
docker exec wqa-<worktree>-redis redis-cli FLUSHDB
QA_REGIONS_FIXTURES=1 pnpm proof regions-and-timezones
```

## Agency proof fixtures

`flows/agency.flow.ts` is opt-in: apply `fixtures/agency.sql` to this worktree's rich-seed
database. It makes the seeded owner the owner of a **Northwind Growth** agency on Agency Pro, with
the seeded workspace as its first client and this month's sends in the shared pool. The flow adds two
more client workspaces (**Bluebird Dental**, **Acme Robotics**) through the API and copies a campaign
into one, so the account then belongs to several workspaces and every test opens the seeded one from
the workspace picker. It is re-runnable. A self-host has no pool cap, so start the stack with billing on to show the
pool meter; no step calls Stripe, so a placeholder test key is enough.

```bash
QA_STRIPE_SECRET_KEY=sk_test_placeholder pnpm stack restart
docker exec -i warmbly-postgres-1 psql -U warmbly -d warmbly_qa_<worktree> < fixtures/agency.sql
docker exec wqa-<worktree>-redis redis-cli FLUSHDB
QA_AGENCY_FIXTURES=1 pnpm proof agency
```

### Agency clients and the portal domain

`flows/agency-portal.flow.ts` records what an agency's clients see. Apply `fixtures/agency-clients.sql`
after `fixtures/agency.sql`: it gives Northwind Growth a brand, a portal theme and a verified portal at `portal.localhost`,
and adds Casey Client (`casey@client.example`, `password123`), the client's own teammate in Dev's
Organization. The first test needs nothing more.

The portal tests need the portal service in front of a built dashboard, the way an agency's domain is
served. The stack's backend expects the portal at `localhost:8095` with the dev portal token (see
`.artifacts/stack/backend.sh`). Build the dashboard into a scratch directory (the dev server's module paths
are not proxied), serve it, and run the portal Worker against this stack (ports from `.artifacts/stack/ports`):

```bash
(cd ../portal && pnpm build)
(cd ../web && VITE_APP_URL=http://localhost:<WEB> VITE_API_URL=http://localhost:<API> \
  pnpm exec vite build --outDir /tmp/wqa-portal/web-dist --emptyOutDir)
(cd ../web && VITE_APP_URL=http://localhost:<WEB> VITE_API_URL=http://localhost:<API> \
  pnpm exec vite preview --outDir /tmp/wqa-portal/web-dist --port 15291 --host 127.0.0.1) &
(cd .. && pnpm dlx wrangler@4.45.0 dev --config deploy/cloudflare/portal/wrangler.jsonc --ip 127.0.0.1 --port 8095 \
  --show-interactive-dev-session=false --var API_ORIGIN:http://localhost:<API> --var PORTAL_DOMAIN:localhost:8095 \
  --var PORTAL_EDGE_TOKEN:<token from backend.sh> --var DASHBOARD_ORIGIN:http://127.0.0.1:15291) &
```

Create a connect link (`POST /v1/emails/client-links`) and a report link (`POST /v1/reports/:id/link`) in
Dev's Organization (both are built on `portal.localhost`), then record with their tokens:

```bash
QA_AGENCY_FIXTURES=1 QA_PORTAL_URL=http://portal.localhost:8095 QA_PORTAL_CONNECT=<token> \
  QA_PORTAL_REPORT=<token> pnpm proof agency
```

Stop the preview and portal processes afterwards along with the stack.

## Marketing site agency pages

`flows/site-agencies.flow.ts` records the agency sections of the marketing site (pricing cards, cost per
client, what every agency plan includes, the agencies page). Run `pnpm dev --host 127.0.0.1 --port <port>` in
`site/`, keep this worktree's stack up (the harness checks the backend), then:

```bash
QA_WEB_URL=http://127.0.0.1:<port> QA_API_URL=http://localhost:<API> QA_MAILPIT_URL=http://localhost:<MAILPIT> \
  QA_SITE=1 pnpm proof site-agencies
```
