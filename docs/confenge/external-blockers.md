# External blockers (honest status)

These items cannot be completed from this agent session without operator /
maintainer action. Code paths are implemented and unit-tested; production
proof remains open.

## 1. GitHub Actions on warmbly/warmbly#91

| Item | Status |
| --- | --- |
| Upstream PR | https://github.com/warmbly/warmbly/pull/91 |
| CI run | https://github.com/warmbly/warmbly/actions/runs/31142042410 |
| Conclusion | `action_required` |

First-time contributor workflows from the fork require **maintainer approval**
before jobs run. This agent cannot approve Actions on `warmbly/warmbly`
(403 admin rights). After approval, CI should run `make lint` + `go test -race
./...` and web typecheck.

## 2. Fork CI (`tjsasakifln/warmbly`)

`.github/workflows/ci.yml` only triggers on:

```yaml
push:
  branches: [main]
pull_request:
  branches: [main]
```

Stacked PRs targeting intermediate branches (review → import, ops → review)
therefore **do not** run GitHub Actions. PR #3 (import → `main`) is the only
fork PR eligible for CI. Actions history on the fork showed zero runs at last
check; if that persists after a new push to a `main`-targeted PR, check
Actions enablement and billing for the personal fork.

**Workaround for full-stack CI:** open or use a tip PR
`feat/confenge-outcome-ops` → `main` so the complete commit stack is tested
against the same workflow as production PRs.

## 3. Netcup VPS deploy

No live inventory of the Netcup box (ports, compose projects, reverse proxy,
extra-cli containers) was available in this environment.  
`docs/confenge/netcup-ops.md` describes an isolated `warmbly-confenge` project.
Do **not** declare production until preflight + smoke on the real VPS.

## 4. Email channel (Hostinger, not M365)

**Fact:** `tiago.sasaki@confenge.com.br` is **Hostinger-hosted** (SMTP/IMAP).
It is **not** Microsoft 365 / Exchange Online. CONFENGE go-live does **not**
require Azure app registration, Graph client ID, or client secret.

| Item | Value |
| --- | --- |
| SMTP | `smtp.hostinger.com:587` STARTTLS |
| IMAP | `imap.hostinger.com:993` SSL |
| Env | `CONFENGE_SMTP_*` / `CONFENGE_IMAP_*` / `CONFENGE_MAILBOX_*` (local `.env.confenge` only) |
| Connect | `scripts/confenge_hostinger_connect.sh` |
| Smoke | `scripts/confenge_self_smoke.sh` (operator address only; no leads) |
| Mailpit | Local tests only |

Warmbly still has optional Outlook OAuth (`BOX_OUTLOOK_*`) for other products;
it is **not** a CONFENGE go-live dependency. Do not put Hostinger passwords on
the VPS.

## 5. extra-cli feed / outcome receptor

Legacy import works (`leads.json` / commercial run shapes).  
Native `confenge.outreach.v1` exporter and HMAC outcome endpoint on extra-cli
are **not** required for Warmbly unit tests; they remain optional separate PRs
on `tjsasakifln/extra-cli` if production wants HTTPS feed + Decision & Outcome
Memory writeback.

## 6. Frontend typecheck in this environment

Host Node is v20; pnpm 11 requires Node ≥ 22.13. Web CI uses Node 20 + pnpm 10
and is the authoritative frontend gate once Actions run. Local
`pnpm typecheck` may fail on this machine for toolchain reasons, not app code.

## Local verification that did pass

```bash
go test ./internal/app/confenge/ -count=1   # import, drafts, enroll, CRM, e2e, HMAC
make lint
go test ./cmd/backend/ ./cmd/consumer/ ./internal/api/handler/ -count=0
```

## What is NOT a blocker for merging code review

- Intelligence / execution separation (feed + outbox only)
- Multi-tenant staging + DNC preservation
- Validators + template AI fallback
- Campaign bootstrap + enroll
- Outcome HMAC outbox worker
- CRM pipeline bootstrap + reply-class mapping
