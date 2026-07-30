# Pattrn M365 / Warmbly campaign + warmup evidence — 2026-07-30

Secret-free operational evidence from the live Warmbly host `mail.pattrndata.ai`.

## Live service state

- Worktree branch: `feat/warmbly-twentycrm-approval-pack`
- Worktree commit: `044a7530`
- Backend health: `GET http://127.0.0.1:8080/health -> HTTP 200`, body `{"status":"ok"}`
- Docker services observed running/healthy where healthcheck exists: `backend`, `mailpit`, `nats`, `postgres`, `realtime`, `redis`, `tracking`, `web`, `worker`.
- `TASKS_PROVIDER=local` on backend/worker.
- Backend Graph env key presence: `BOX_OUTLOOK_TENANT_ID`, `BOX_OUTLOOK_CLIENT_ID`, `BOX_OUTLOOK_CLIENT_SECRET` present; values not recorded.

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

Caveat: Microsoft Graph `Sent Items` readback for this specific Warmbly campaign marker returned `sent_items_matches=0`. Follow-up inspection showed the campaign task rows were marked `completed` after Warmbly successfully queued/published the send event, but the task rows did not persist the generated RFC `Message-ID` and worker logs no longer contained a matching send-success line for the smoke task IDs. So this proof is now classified more narrowly as **Warmbly campaign enqueue/control-plane proof**, not confirmed worker-send or independent Microsoft Sent Items proof. A code fix was added to persist campaign/user-email task `message_id` values so future proofs have a stable Graph/inbox correlation key.

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

Historical warmup dead-letter inspection:

```text
c430078a-49a9-43f4-b804-e9a2dd5c1c8e | failed to upload email body to S3: mkdir /data/blobs/emails: permission denied
c2047d7f-de3d-42e9-87e1-f17a41b93883 | failed to get cipher: encryptedkeys: dek already exists for organization
1b2f3ccb-7e0b-4e21-a50a-f96ba81f258c | failed to upload email body to S3: mkdir /data/blobs/emails: permission denied
95059229-c096-44e9-ba04-89c161a97bf2 | failed to upload email body to S3: mkdir /data/blobs/emails: permission denied
```

Decision: do **not** replay or clear these as part of this no-activation pass. They are historical failed warmup send attempts from before the current closed gate, not pending/active work. They should stay as an audit trail until a separate warmup activation approval; before activation, run a storage/DEK preflight and either mark these stale rows superseded/quarantined or clear them with an explicit DB migration note.

Conclusion: warmup remains safe as a recipient-only readiness setup; no warmup activation was performed. Do not enable warmup sending until the historical dead-lettered warmup tasks are explicitly handled and an explicit warmup activation approval exists.

## Gates after this evidence

Cleared:

- Backend health and local task provider operational.
- Three active Outlook-linked Warmbly accounts exist.
- Warmup pools are seeded.
- All three linked accounts are premium `recipient_only`, healthy, and `warmup=NULL`.
- Controlled Warmbly-native internal campaign proof completed in Warmbly DB/API with no campaign dead letters, but follow-up evidence narrows this to enqueue/control-plane proof because worker-send/Sent Items proof was not available for the marker.

Still closed / caveated:

- Warmup activation gate: closed; `warmup=NULL`, no pending/active warmup tasks, current-day warmup send/reply counters zero.
- Prospect/cold campaign gate: closed; internal-only proof does not approve prospect sends.
- Warmbly-native campaign proof lacks independent Graph Sent Items readback for the campaign marker; investigate sender implementation / save-to-sent behavior before treating this as full Microsoft-delivery proof.
- Expansion gate: closed; no approval here for additional kiosk/shared-mailbox expansion.
