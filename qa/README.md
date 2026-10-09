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
