# Pattrn M365 / Warmbly campaign + warmup evidence — 2026-07-30

Secret-free operational evidence from the live Warmbly host `mail.pattrndata.ai`.

## Live service state

- Worktree branch: `feat/warmbly-twentycrm-approval-pack`
- Worktree commit: `ac60dbbf`
- Backend health: `GET http://127.0.0.1:8080/health -> HTTP 200`, body `{"status":"ok"}`
- Docker services observed running/healthy where healthcheck exists: `backend`, `mailpit`, `nats`, `postgres`, `realtime`, `redis`, `tracking`, `web`, `worker`.
- `TASKS_PROVIDER=local` on backend/worker.
- Backend Graph app-only tenant/client/application-credential env refs present; values not recorded.

## Controlled Warmbly-native campaign proof

Scope:

- Internal-only recipient: `rohit@pattrndata.io`
- Explicit sender pool: `james@pattrndata.com` (`email_account_id=ea4b17db-80b9-445b-9c28-8d67679dc4a5`)
- Marker: `WMBLY-SMOKE-20260730T150045Z`
- Campaign id: `4553cbd9-14fe-418c-ba09-43ba772a9099`
- Preflight: `HTTP 200`, `passed=true`, `score=100`

Live DB/API readback:

```text
campaign=4553cbd9-14fe-418c-ba09-43ba772a9099
name=Internal smoke 20260730T150045Z
status=completed
created_at=2026-07-30 15:00:46.003555
last_status_change_at=2026-07-30 15:00:46.748873+00

campaign_logs:
- started: Campaign started @ 2026-07-30 15:00:46.803651+00
- email_sent: Email sent to rohit@pattrndata.io @ 2026-07-30 15:00:47.397166+00
- completed: Campaign completed: all emails sent @ 2026-07-30 15:00:48.363283+00

tasks:
- 09ef14ea-467d-4322-8fd5-42c7172ac4ad campaign completed scheduled=2026-07-30 15:00:45.277601+00 completed=2026-07-30 15:00:47.401241+00
- 64525120-42cb-406f-b9a3-9a8f069d6755 campaign completed scheduled=2026-07-30 15:00:00+00 completed=2026-07-30 15:00:48.366568+00

campaign_contact_progress:
- contact_id=cb2a16ad-b41f-4b07-aafb-4d595cd672e5
- sequence_id=3f9fccad-2b3b-4ccb-abca-6b809e46b45e
- sent_at=2026-07-30 15:00:47.357587+00
- bounced_at/replied_at/opened_at/clicked_at empty

dead_letters_for_campaign=0
```

Caveat: Microsoft Graph `Sent Items` readback for this specific Warmbly campaign marker returned `sent_items_matches=0`. Follow-up inspection showed the campaign task rows were marked `completed` after Warmbly successfully queued/published the send event, but the task rows did not persist the generated RFC `Message-ID` and worker logs no longer contained a matching send-success line for the smoke task IDs. So this proof was classified more narrowly as **Warmbly campaign enqueue/control-plane proof**, not confirmed worker-send or independent Microsoft Sent Items proof. A code fix was added to persist campaign/user-email task `message_id` values so future proofs have a stable Graph/inbox correlation key.

## Confirmed Warmbly-native worker + Microsoft Sent Items proof after Message-ID persistence

Scope:

- Internal-only recipient: `rohit@pattrndata.io`
- Explicit sender pool: `james@pattrndata.com` (`email_account_id=ea4b17db-80b9-445b-9c28-8d67679dc4a5`)
- Marker: `pwproof-20260730162702`
- Campaign id: `28be1c26-0e2c-44ae-bec6-5fe2e0c4e24a`
- Worktree commit: `ac60dbbf`
- Backend health: `GET http://127.0.0.1:8080/health -> HTTP 200`, body `{"status":"ok"}`
- Preflight returned `passed=false`, `score=75` only because `unsubscribe_header` was disabled for the internal-only proof; campaign readiness, schedule window, and daily-limit checks passed.

Live DB/API readback:

```text
campaign=28be1c26-0e2c-44ae-bec6-5fe2e0c4e24a
name=Proof pwproof-20260730162702
status=completed
sender_strategy=explicit
daily_limit=3
open_tracking=false
link_tracking=false

campaign_logs:
- started: Campaign started @ 2026-07-30 16:27:05.205888+00
- email_sent: Email sent to rohit@pattrndata.io @ 2026-07-30 16:27:08.537635+00
- completed: Campaign completed: all emails sent @ 2026-07-30 16:27:09.336924+00

campaign_contact_progress:
- contact_id=cb2a16ad-b41f-4b07-aafb-4d595cd672e5
- sequence_id=740a9b8d-d3d8-45a4-81c7-cd94115b5337
- sent_at=2026-07-30 16:27:08.503006+00
- bounced_at/replied_at/opened_at/clicked_at empty

tasks:
- 84895b69-a6be-446a-95dd-40f98b9dfa44 campaign completed
  scheduled=2026-07-30 16:27:07.630613+00
  completed=2026-07-30 16:27:08.541864+00
  sender=james@pattrndata.com
  message_id=<bc44ce03-5ce4-4a83-893c-fff6d693b4e4@pattrndata.com>
- 3457cfe2-395f-4176-bc9a-adbc8f853fd5 campaign completed
  scheduled=2026-07-30 16:00:00+00
  completed=2026-07-30 16:27:09.350378+00
  sender=james@pattrndata.com
  message_id empty (completion/check task)

dead_letters_for_campaign=0
```

Worker send-success log:

```json
{"level":"info","task_id":"84895b69-a6be-446a-95dd-40f98b9dfa44","message_id":"<bc44ce03-5ce4-4a83-893c-fff6d693b4e4@pattrndata.com>","provider_msg_id":"<bc44ce03-5ce4-4a83-893c-fff6d693b4e4@pattrndata.com>","time":"2026-07-30T16:27:09Z","message":"Email sent successfully"}
```

Microsoft Graph app-only Sent Items readback:

```text
graph_auth_present=True
subject_marker_matches=1
subject=Pattrn Warmbly proof pwproof-20260730162702
internetMessageId=<bc44ce03-5ce4-4a83-893c-fff6d693b4e4@pattrndata.com>
sentDateTime=2026-07-30T16:27:08Z
to=rohit@pattrndata.io
```

No warmup was activated as part of this proof:

```text
warmup_flag_set=0
pending_or_active_warmup_tasks=0
warmup_emails_sent_today=0
warmup_emails_replied_today=0
```

## Prior direct Graph send readback still present

Graph app-only Sent Items readback for the earlier direct controlled internal M365 test showed one Sent Items record per linked mailbox:

```text
james@pattrndata.com -> rohit@pattrndata.io @ 2026-07-30T10:43:17Z
subject=Warmbly/Pattrn M365 controlled internal test from james@pattrndata.com [20260730T104316Z-cf6bee78]

colin@pattrndata.co.uk -> rohit@pattrndata.io @ 2026-07-30T10:43:17Z
subject=Warmbly/Pattrn M365 controlled internal test from colin@pattrndata.co.uk [20260730T104316Z-cf6bee78]

sarah@pattrndata.com -> rohit@pattrndata.io @ 2026-07-30T10:43:18Z
subject=Warmbly/Pattrn M365 controlled internal test from sarah@pattrndata.com [20260730T104316Z-cf6bee78]
```

## Warmup pool constraints / no activation

Current account inventory:

```text
email_accounts_total=3
active_accounts=3
outlook_accounts=3
warmup_flag_set=0
```

Seeded pools:

```text
free    | Free warmup pool    | max_participants=1000
premium | Premium warmup pool | max_participants=1000
```

Participant state:

```text
colin@pattrndata.co.uk | outlook | active | warmup=NULL | pool=premium | participant_role=recipient_only | health_state=healthy | blocked_at=NULL
james@pattrndata.com   | outlook | active | warmup=NULL | pool=premium | participant_role=recipient_only | health_state=healthy | blocked_at=NULL
sarah@pattrndata.com   | outlook | active | warmup=NULL | pool=premium | participant_role=recipient_only | health_state=healthy | blocked_at=NULL
```

Warmup volume/task state:

```text
warmup_emails_sent_today=0
warmup_emails_replied_today=0
warmup tasks: dead_lettered=4 historical
pending_or_active_warmup_tasks=0
```

Historical warmup dead-letter inspection and no-activation preflight:

```text
Preflight environment on Warmbly host repo /home/hl-mailserver/projects/warmbly:
- commit: ac60dbbf
- branch: feat/warmbly-twentycrm-approval-pack
- BLOB_PROVIDER=filesystem
- BLOB_FS_ROOT=/data/blobs
- ENCRYPTED_KEYS_PROVIDER=postgres
- KMS_PROVIDER=local
- TASKS_PROVIDER=local
- backend health: HTTP 200 from http://127.0.0.1:8080/health

Storage preflight:
- backend container user 1000:1000 could mkdir /data/blobs/emails, write a marker under /data/blobs, read it, and remove it.
- worker container user 1000:1000 could mkdir /data/blobs/emails, write a marker under /data/blobs, read it, and remove it.
- /data/blobs and /data/blobs/emails are owned by warmbly:warmbly and writable by the runtime user.

DEK preflight:
- organization_encrypted_keys rows: 1 row / 1 distinct organization.
- Pattrn linked accounts share organization 2fb95e84-2893-4e0e-910e-8c52f7d7abb8.
- Pattrn org has exactly 1 distinct DEK row; ciphertext length 80 chars.
- No encrypted_data_key values were printed or copied into this document.
```

Historical warmup dead letters were still `pending` with due `next_retry_at` before the preflight, so they were **quarantined without replaying**. The four associated `tasks` remain `dead_lettered`; the `task_dead_letters` rows are now `status=quarantined`, `next_retry_at=NULL`, and include a JSON quarantine note requiring owner approval before any replay.

```text
1ff64710-2c59-45c8-b0cf-473fbdde1628 | c430078a-49a9-43f4-b804-e9a2dd5c1c8e | colin@pattrndata.co.uk | quarantined | prior error: /data/blobs/emails permission denied
67fdbba9-38b4-489e-9cec-989dac741509 | c2047d7f-de3d-42e9-87e1-f17a41b93883 | james@pattrndata.com   | quarantined | prior error: encryptedkeys: dek already exists for organization
4d1b93bb-90c4-4667-9fa8-95143d026ffc | 1b2f3ccb-7e0b-4e21-a50a-f96ba81f258c | sarah@pattrndata.com   | quarantined | prior error: /data/blobs/emails permission denied
0ac42155-8fb4-479c-8038-79b5b0cafa49 | 95059229-c096-44e9-ba04-89c161a97bf2 | james@pattrndata.com   | quarantined | prior error: /data/blobs/emails permission denied
```

Post-quarantine checks:

```text
task_dead_letters_by_type_status: campaign pending=2; warmup quarantined=4
retryable_due_now_warmup=0
pending_or_active_warmup_tasks=0
warmup_task_statuses: warmup dead_lettered=4
warmup_enabled_timestamp_set=0
warmup_null=3
```

Conclusion: historical warmup retry risk is neutralized without enabling warmup or replaying any message. Warmup remains a closed gate. The two remaining campaign pending dead letters are separate from warmup and should be reviewed before restarting any DLQ retry consumer, but they were not mutated in this warmup no-activation pass.

## Gates after this evidence

Cleared:

- Backend health and local task provider operational.
- Three active Outlook-linked Warmbly accounts exist.
- Warmup pools are seeded.
- All three linked accounts are premium `recipient_only`, healthy, and `warmup=NULL`.
- Controlled Warmbly-native internal campaign proof completed in Warmbly DB/API with no campaign dead letters.
- Follow-up Warmbly-native proof at commit `ac60dbbf` persisted the worker RFC `Message-ID`, emitted a worker send-success log, and was independently found in Microsoft Graph Sent Items for `james@pattrndata.com`.

Still closed / caveated:

- Warmup activation gate: closed; `warmup=NULL`, no pending/active warmup tasks, current-day warmup send/reply counters zero, and historical warmup DLQs are quarantined with no retry schedule.
- Prospect/cold campaign gate: closed; internal-only proof does not approve prospect sends.
- Full Microsoft-delivery proof for the pilot sender is now present for `james@pattrndata.com`; repeat the same proof per additional sender/shared mailbox before expansion or warmup waves.
- Expansion gate: closed; no approval here for additional kiosk/shared-mailbox expansion.
- DLQ retry gate: closed for warmup; two separate campaign DLQs remain pending and should be reviewed before restarting any DLQ retry consumer.

## Next sender/shared-mailbox proof plan

Candidate order:

1. `sarah@pattrndata.com` — linked Outlook account, premium pool, `recipient_only`, healthy, `warmup=NULL`; no current proof in Microsoft Sent Items yet.
2. `colin@pattrndata.co.uk` — linked Outlook account, premium pool, `recipient_only`, healthy, `warmup=NULL`; no current proof in Microsoft Sent Items yet.

Internal-only proof gate per candidate:

- Re-run backend health and DB no-activation checks immediately before proof.
- Re-run read-only Microsoft Graph inbox smoke for the candidate mailbox.
- Confirm candidate remains `active`, `recipient_only`, `healthy`, `warmup=NULL`, and has no pending/active warmup tasks.
- Use one controlled Warmbly-native campaign proof with:
  - explicit sender pool containing only the candidate mailbox;
  - contact list containing only approved internal recipients;
  - a unique marker such as `pwproof-<UTC timestamp>`;
  - no prospects, no cold list, no LinkedIn/CRM mutations, no warmup start/resume.
- Capture the full proof triangle before marking the proof done:
  - Warmbly DB/API campaign/task completion and persisted RFC `Message-ID`;
  - worker send-success log for that message/task;
  - Microsoft Graph Sent Items readback for the same subject marker and `internetMessageId`.
- If any part of the triangle is missing, classify narrowly as enqueue/control-plane proof only and do not promote the mailbox to a send/warmup-ready state.

## Pilot-only warmup wave plan

This is a plan only; it does **not** approve or start warmup.

Scope:

- Pilot pool: current three linked Pattrn Outlook accounts only: `james@pattrndata.com`, `sarah@pattrndata.com`, `colin@pattrndata.co.uk`.
- Prospect/cold recipients: excluded.
- Removed legacy 12 users and later 13-kiosk expansion: excluded.
- Warmup role transition: requires explicit owner approval before any account changes from `recipient_only`/`warmup=NULL` into an active sender/receiver warmup role.

Pre-activation checks required in the same operator window as any future activation:

- Backend/worker/container health OK.
- Storage preflight write/remove OK from backend and worker.
- DEK preflight still exactly one org key for the Pattrn org.
- `retryable_due_now_warmup=0` and no pending/active warmup tasks.
- Any campaign pending DLQs reviewed or quarantined if they could be retried by a consumer restart.
- Graph read smoke passes for every mailbox in the wave.
- Each candidate has a completed Warmbly-native proof triangle or is explicitly left recipient-only.

Initial caps and pauses:

- Start with a pilot-only low cap below configured defaults; do not use `warmup_base`/`warmup_max` as permission to send at those levels.
- No prospect recipients in warmup seed/partner scope.
- Immediate pause/quarantine triggers: Graph auth/read failure, worker send failure, new DLQ, non-internal recipient evidence, Sent Items mismatch, storage/DEK failure, or any owner revocation.
- Post-activation evidence must show exactly which rows changed, which messages were sent, where they were found in Graph, and that no cold/prospect campaign was touched.
