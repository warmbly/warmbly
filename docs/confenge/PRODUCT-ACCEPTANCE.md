# CONFENGE product acceptance

Version: 1.2  
Date: 2026-08-07  
Branch: `feat/confenge-product-acceptance`  
Scope: prove that `extra-cli` + Warmbly form a local commercial engine for CONFENGE multichannel outreach (email + eligible WhatsApp) with factual personalization and human approval of every message.

## Preconditions

All required foundation surfaces are **PRESENT** on the integration tip (orchestration #6 + grounded copy #7 + governor #8 + per-touch #9 + reply cockpit #10 + local-first #11).

If any surface were missing, this matrix would mark product readiness **BLOCKED** rather than PASS.

## Acceptance matrix

| Area | Result | Evidence |
| --- | --- | --- |
| Intelligence | PASS | Multi-company `confenge.outreach.v1` import; service codes + fact-to-mention; grounded generate (`TestProductAcceptanceMultichannelSum` 1–3) |
| Contact | PASS | Exact recipient on draft/touchpoint; WA public phone blocked on generate; no invented opt-in |
| Email | PASS | SMTP delivery of human-approved body to **Mailpit** (API search by marker). Local compose `:11025`/`:18025` or CI service `:1025`/`:8025` via `CONFENGE_MAILPIT_*` |
| WhatsApp | PASS | `GenerateWhatsAppDraft` blocks public/no-opt-in; consented path goes through `SendApprovedWhatsApp` and records on **mock Evolution provider** (not pure policy-only) |
| Approval | PASS | `CanTransport` / `ApplyHumanApproval` / edit invalidates; `requireTouchTransport` binds approved content hash |
| Pacing | PASS | Shared email+WhatsApp governor cap 10/60min; 11th blocked; restart no burst (`dispatch` + product E2E 9–11) |
| Replies | PASS | `ProcessInboundHandoff` cancels open **touchpoints** and drafts, sets queue REPLIED; `ListAttention` without test re-flagging |
| Outcomes | PASS | HMAC sign/verify + test receiver idempotency; reimport preserves DNC |
| Local startup | PASS | `cmd/confenge`, preflight, readiness, kill switch, `docs/confenge/local-ops.md` |
| Security | PASS | Fail-closed flags; AI cannot approve/send; multi-tenant org scoping; no secrets in fixtures |
| No-real-send tests | PASS | Mailpit (local/CI service) + WhatsApp mock provider + httptest outcome receiver; no real leads |
| UI (Playwright) | PARTIAL | Spec + config ship (`web/e2e/confenge-product-acceptance.spec.ts`); **CI/static PASS** via `TestConfengeUIAcceptanceAffordancesPresent` (required data-testids + route). **Live browser run BLOCKED** in this environment: `web:5173` not listening; Playwright attempt failed with `net::ERR_ABORTED` on `/login` (captured under scratch `playwright-confenge.log`). Service E2E remains the behavioral gate for approval/quota/Needs attention. |

## Scenario bullets (20)

Driven by `TestProductAcceptanceMultichannelSum` (and `TestProcessInboundHandoffCancelsOpenTouchpoints` for handoff cadence):

1. Multi-company import — PASS  
2. Distinct dossiers/services — PASS  
3. Different messages per account — PASS  
4. Exact recipient visible — PASS  
5. No message before approval — PASS (`CanTransport` before approve)  
6. Approval by exact content hash — PASS  
7. Edit invalidates approval — PASS  
8. Approved and queued — PASS (`CASQueueTouchpoint`)  
9. Governor max 10 outbound / 60 min across channels — PASS  
10. 11th remains blocked/queued — PASS  
11. Restart does not burst — PASS  
12. Email appears in **Mailpit** with approved content — PASS (real SMTP + Mailpit HTTP API)  
13. WhatsApp only eligible/consented — PASS (`SendApprovedWhatsApp` → mock provider)  
14. Public phone without opt-in blocked — PASS (`GenerateWhatsAppDraft` error)  
15. Inbound reply pauses cadence — PASS (`ProcessInboundHandoff` cancels open touchpoints; test does **not** call Cancel itself)  
16. DNC cancels next touches — PASS (`NoteDNC` product path)  
17. Reply → Needs attention — PASS (`ListAttention` after handoff queue state only)  
18. Reply draft not sent without new approval — PASS  
19. Outcomes via HMAC, idempotent — PASS  
20. Reimport preserves DNC — PASS  

## Real provider non-claims (operator smoke, not code failure)

CI without production secrets does **not** prove:

- Real inbox deliverability (Gmail / Hostinger reputation)
- Real Hostinger SMTP/IMAP send + IMAP reply sync for
  `tiago.sasaki@confenge.com.br` (operator self-smoke; not Mailpit)
- Real WhatsApp Business Account (WABA) / Evolution production
- Meta-approved marketing template
- Opt-in provenance for real leads

CONFENGE go-live does **not** depend on Microsoft 365 / Graph. The production
mailbox is Hostinger SMTP/IMAP. Treat real-channel checks as **operator smoke**
on owned addresses only (`scripts/confenge_self_smoke.sh`). Never send to real
leads in CI or acceptance automation.

## Operator smoke (Tiago)

Use only self-owned destinations. Do not message real leads.

### One email to your own address

```bash
# make infra  # includes Mailpit (UI often http://localhost:18025, SMTP :11025)
export CONFENGE_ENABLED=true
export CONFENGE_REQUIRE_HUMAN_APPROVAL=true
# Import fixture, plan/generate, then approve in /app/confenge
# Confirm the exact approved body in Mailpit matches the review pane
```

### One WhatsApp to your own consented number

```bash
export CONFENGE_WHATSAPP_ENABLED=true
# Only after Evolution sandbox + YOUR number has USER_INITIATED or OPTED_IN
# with provenance_ok. Never free-text cold to public phone book numbers.
```

### Local outcome receptor

```bash
export CONFENGE_MAILPIT_SMTP=127.0.0.1:11025
export CONFENGE_MAILPIT_API=http://127.0.0.1:18025
go test ./internal/app/confenge/ -run TestProductAcceptanceMultichannelSum -count=1 -v

# Production-shaped env for worker delivery to extra-cli:
export CONFENGE_OUTCOME_WEBHOOK_URL=https://127.0.0.1:9999/outcomes
export CONFENGE_OUTCOME_WEBHOOK_SECRET=whsec_local_only_not_committed
```

## Playwright UI

Scoped exception for this acceptance front only.

### What ships

| Artifact | Path |
| --- | --- |
| Spec | `web/e2e/confenge-product-acceptance.spec.ts` |
| Config | `web/playwright.config.ts` |
| Dep | `@playwright/test` in `web/package.json` |
| Static CI gate | `TestConfengeUIAcceptanceAffordancesPresent` |

### UI steps covered by the spec

1. Open `/app/confenge` (after login)  
2. Review evidence (`confenge-evidence`)  
3. Edit body (`confenge-body-input`)  
4. Approve without dispatch (`confenge-approve`)
5. See quota (`confenge-dispatch-quota`)  
6. See sent counter (`confenge-stat-sent`)  
7. Optional inject reply via generate-reply API when `CONFENGE_E2E_TOKEN` + account set  
8. See Needs attention (`confenge-needs-attention`)

### Honest run status (this environment)

```text
web:DOWN on :5173
api:up on :8080 (running binary may not expose confenge routes)
CONFENGE_E2E=1 → FAIL page.goto /login net::ERR_ABORTED (30s timeout)
Evidence: scratch playwright-confenge.log + ui-presence.log
```

Do **not** treat the live Playwright failure as a product logic failure. Treat it as **environment BLOCKED**. Behavioral truth for approve/quota/reply is in the Go product E2E.

### Operator run (when stack is up)

```bash
# Terminal A: make infra && make backend && make web  (CONFENGE_ENABLED=true)
cd web
export CONFENGE_E2E=1
export CONFENGE_E2E_BASE_URL=http://127.0.0.1:5173
export CONFENGE_E2E_EMAIL=dev@warmbly.com
export CONFENGE_E2E_PASSWORD=password123
# optional reply inject:
# export CONFENGE_E2E_TOKEN=... CONFENGE_E2E_ACCOUNT_ID=... CONFENGE_E2E_API=http://127.0.0.1:8080
npx playwright test -c playwright.config.ts
```

## How to re-run

```bash
export CONFENGE_MAILPIT_SMTP=127.0.0.1:11025   # or :1025 in CI
export CONFENGE_MAILPIT_API=http://127.0.0.1:18025  # or :8025 in CI
go test ./internal/app/confenge/ -run 'TestProductAcceptanceMultichannelSum|TestProcessInboundHandoffCancelsOpenTouchpoints|TestConfengeUIAcceptanceAffordancesPresent' -count=1 -v
go test ./internal/app/confenge/dispatch/ -run 'TestCap10|TestRestart|TestEmailAndWhatsApp' -count=1 -v
go test ./internal/app/confenge/ -count=1
```

## Dependencies

This PR is integration/acceptance only. It depends on open foundation and feature PRs #4–#11. After deps merge, rebase onto updated `main`.

## Shipped fix in this acceptance pass

`ProcessInboundHandoff` now cancels open touchpoints (and governor queue by recipient) in addition to draft stop, so reply/DNC truly pause per-touch cadence on the product path.
