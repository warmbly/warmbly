# CONFENGE dynamic priority queue (Warmbly)

## Split of responsibility

| Layer | Owns |
|-------|------|
| **extra-cli** | Facts, contracts, authoritative target-fit, activation state/score, why-now, hot set |
| **Warmbly** | Import, target-fit enforcement, human approval, DNC, governor, dispatch, operational readiness |

Warmbly **does not** recompute commercial intelligence from contracts.

## Target-fit authority

Activation and contact readiness do not authorize outreach by themselves. Every outbound path requires a persisted current extra-cli decision with:

- `target_fit_class=TARGET_CONFIRMED`
- `target_fit_fresh=true`
- non-empty version and source watermark
- a parsed decision observation time
- an eligible imported send tier when a tier is supplied

Missing, stale, out-of-scope, failed, recompute-required, contradictory, and downgraded decisions fail closed. Warmbly never derives target-fit from names, CNAE text, contracts, email availability, or historical scores.

Migration `000092_confenge_target_fit_authority` marks pre-contract records `TARGET_FIT_MISSING`, preserves their account and message history, and revokes unsent work. Each new ineligible import runs the same invalidation automatically. The final campaign gate reloads the exact enrollment candidate and account before worker or SMTP publication.

## Historical root cause

The original Warmbly `FeedLead` contract consumed the legacy send tier and email readiness but omitted the published target-fit class, freshness, version, watermark, and evidence fields. Go JSON decoding therefore discarded the extra-cli decision. `DefaultQueueState` then promoted an account when a contact email had an enrollable verification status, and the Agora query filtered activation timing without requiring target-fit or imported send readiness. Approval treated an empty send tier as no blocker, while enrollment and the final campaign gate had no authoritative target-fit check. Together, those paths made historical `enrollable=true` behave like durable commercial authorization.

The corrected path stores the complete extra-cli decision, rejects temporal regression in the database upsert, and applies one shared authorization predicate from import through final transport.

## Feature flags

| Env | Default | Effect |
|-----|---------|--------|
| `CONFENGE_DYNAMIC_PRIORITY_ENABLED` | `false` | When on, `/app/confenge` work queue uses activation timing |
| `CONFENGE_FEED_SYNC_ENABLED` | `false` | Continuous manifest pull |
| `CONFENGE_EXTRA_CLI_MANIFEST_URL` | empty | HTTPS or `file://` manifest |
| `CONFENGE_FEED_SYNC_INTERVAL` | `15m` | Sync cadence |
| `CONFENGE_EXTRA_CLI_FEED_TOKEN` | empty | Bearer for remote fetch |
| `CONFENGE_EXTRA_CLI_ALLOWED_HOSTS` | empty | Required in prod with remote URL |

Shadow mode: flag off still **imports** activation fields; queue order stays legacy `priority_rank`.

## Activation fields on `outreach_accounts`

Migration `000089_confenge_activation_priority`:

- `activation_state`, `activation_score`, `activation_reason_codes`
- `next_best_action_at`, `activation_expires_at`
- `activation_source_hash`, `message_context_hash`

`queue_state` remains local execution (NEEDS_CONTACT, READY_TO_GENERATE, …).

## Working queue precedence

1. Human-dominant: DNC, blocked, bounce, replied, meeting, proposal, won, lost
2. Active cadence (no duplicate first touch)
3. New outbound when:
   - target-fit is current and eligible
   - `activation_state == ACTIONABLE_NOW`
   - `next_best_action_at <= now`
   - not expired
   - company and contact are email send-ready
   - no local suppression, DNC, reply, or bounce

Lanes: Needs attention → Agora → Needs contact → Review → Approved → Aguardar.

## Stale message context (release-blocking)

`message_context_hash` covers moment / offer / messaging / evidence / recipient / trigger identity.

It does **not** include pure rank or activation score.

On generate: store `generated_context_hash` on the touchpoint.

On queue / final dispatch:

```text
generated_context_hash == account.message_context_hash
```

Mismatch → fail closed, clear approval, force regenerate + human re-approve.

## Sync

- No SQL access to extra-cli
- SSRF-hardened HTTPS + host allowlist
- Idempotent by snapshot / payload hash
- Partial snapshot never marked success
- Deactivations applied without wiping DNC
- Target-fit downgrades revoke open touchpoints, drafts, enrollment, and dispatch items
- Older target-fit watermarks cannot overwrite newer restrictions

## Reconciliation

After migrations are applied, operators can preview and apply the idempotent historical reconciliation:

```bash
go run ./cmd/confenge reconcile-target-fit --dry-run --org-id <uuid>
go run ./cmd/confenge reconcile-target-fit --org-id <uuid>
```

The JSON report includes before and after operational counts, suppression counts by reason, and revoked touchpoint, draft, enrollment, and dispatch totals. Keep the CONFENGE kill switch engaged during production reconciliation.

## Governor unchanged

`CONFENGE_GLOBAL_SENDS_PER_HOUR=10` remains absolute for email + WhatsApp.
Capacity metrics in the UI are planning only.
