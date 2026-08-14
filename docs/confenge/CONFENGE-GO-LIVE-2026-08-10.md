# CONFENGE go-live card — 2026-08-10 09:00 America/Sao_Paulo

> Historical card from 2026-08-10. Superseded by the PR #45 operational
> validation on 2026-08-13. This file is not authorization to resume dispatch
> or send. The current state is 8 generic recipients in `NEEDS_REVIEW`, 22
> accounts blocked by `recipient_evidence_date_missing`, 0 approved, 0 sent,
> and dispatch paused. Issues #39, #41, #42, and #43 remain the active gates.

## Verdict

```text
GO_FOR_CONTROLLED_PILOT
```

Binary decision. Bound live on **continuous Hostinger IMAP poll** evidence (not JetStream force, not ADD_EMAIL reseed-after-send).

---

## Blockers

**NONE** for a supervised EMAIL-only pilot at 10/h with dispatch currently PAUSED (operator resumes at 09:00 SP after human copy review).

### Closed this cycle (were blockers)

1. **Continuous IMAP stop-on-reply** — After worker first attach settled, new Hostinger self-seed + `Re: CONFENGE CONT_POLL …` landed via the 1m IMAP poll without backend/worker restart. Unibox stored Hostinger From form `{" (tiago.sasaki@confenge.com.br)"}`. Touchpoints reseeded to PLANNED after seed, then Re: moved them to `REPLIED` / `stop_reason=REPLY` (ids `b27a338b…`, `db221658…`). Evidence: `reply-stop-continuous-poll.log`.

2. **From normalization** — `emailaddr.ExtractFirst` handles Hostinger ` (a@b)` Unibox forms (main).

3. **IMAP CONDSTORE stall** — Sync planner uses SELECT EXISTS/UIDNEXT when HighestModSeq is flat; LIST-STATUS-only planning was insufficient on Hostinger (PRs #31/#32 → `0e74f972`).

4. **Outcome outbox** — feed URL + HMAC aligned; 11+ delivered previously.

5. **SSH / deploy identity** — VPS HEAD == `.deployed_sha` == origin/main `0e74f972…`.

---

## SHA audit

| REPO | MAIN SHA | DEPLOYED / PRODUCTION | STATUS |
| --- | --- | --- | --- |
| warmbly | `0e74f9722041383ba36d5d7d6d8dc77f70784163` | VPS HEAD + `.deployed_sha` same | **MATCH** |
| extra-cli | `28a31a1bac44d250f6f9dd26bd9c30aa12ae1263` | feed `:8443` | PARTIAL stock |
| web-cfg | `88d72aeaa72c812fcff7e2bde9c2736f5f22515f` | live build-info | **MATCH** |

---

## What already converged

| Item | Status |
| --- | --- |
| Hostinger SMTP self-smoke | PASS |
| Hostinger continuous IMAP → Unibox | PASS |
| Reply-stop natural (continuous poll) | **PASS** REPLIED/REPLY |
| Outcome delivery | PASS |
| Kill switch / GREEN off / WhatsApp off / dispatch paused | PASS |

---

## Draft sample honesty

10 real-account drafts: why_you / micro_offer empty; template-ish bodies; risk flags include economic_or_legal_claim_language. Human rewrite before send selection.

---

## Monday policy

| Item | Value |
| --- | --- |
| Start | 2026-08-10 09:00 America/Sao_Paulo |
| Channel | EMAIL_ONLY |
| WhatsApp | OFF |
| Initial rate | 10/h |
| GREEN autorun | OFF |
| Dispatch | PAUSED until operator resume |
| Ramp | 10→15→20 only on bounce/auth/queue/health |
| Stop | fail-closed |

### Human actions before 09:00 (≤3)

1. Human-edit/approve only rewritten drafts (not raw template sample).
2. Confirm status still PASS + kill-switch paused until resume.
3. At 09:00 SP: resume dispatch then keep 10/h; kill with pause.

---

## Residuals (non-blocking)

- Outcome receptor `--memory-store`
- Feed stock age 2026-08-08
- First mailbox attach after worker recreate still needs one ADD_EMAIL (normal in-memory worker design); continuous poll after settle is proven

---

## Evidence

`/tmp/grok-goal-16829f704c38/implementer/evidence/` — `reply-stop-continuous-poll.log`, `deploy-imap-select-status.log`, `deploy-sha-transcript.log`, `shas.json`, `sha-audit.md`, `deploy-warmbly.json`, `GO-NO-GO.md`.
