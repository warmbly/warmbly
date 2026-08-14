# WhatsApp channel (policy-gated)

## Architecture

```text
extra-cli → intelligence → Warmbly
                            ├─ Email channel
                            └─ WhatsApp channel
                                 └─ Provider interface
                                      └─ Evolution adapter
                                           └─ WhatsApp Cloud API (production)
```

Inbound:

```text
WhatsApp → Evolution webhook → POST /api/v1/webhooks/evolution/:instance
        → auth + size limit → normalize → idempotency → CRM / consent / stop sequences
```

## Fundamental rule

**A public phone number is not consent.**

```text
phone found → normalize → consent status → eligibility → template/session type → review gate → send
```

Never: `phone found → send WhatsApp`.

## Feature flags (all default false / safe)

```env
CONFENGE_WHATSAPP_ENABLED=false
CONFENGE_WHATSAPP_AUTO_SEND_ENABLED=false
CONFENGE_WHATSAPP_AUTO_REPLY_ENABLED=false
WHATSAPP_PROVIDER=evolution
WHATSAPP_EVOLUTION_BASE_URL=
WHATSAPP_EVOLUTION_API_KEY=
WHATSAPP_EVOLUTION_INSTANCE=
WHATSAPP_EVOLUTION_ALLOW_BAILEYS=false
WHATSAPP_WEBHOOK_SECRET=
CONFENGE_CROSS_CHANNEL_MIN_INTERVAL_HOURS=24
WHATSAPP_SERVICE_WINDOW_HOURS=24
```

## Consent statuses

| Status | Automated send |
|--------|----------------|
| UNKNOWN / NO_OPT_IN | Blocked |
| OPTED_IN (+ provenance) | Allowed per window/template rules |
| USER_INITIATED | Free text inside service window |
| OPTED_OUT / DO_NOT_CONTACT | Always blocked (sticky) |

## Packages

| Path | Role |
|------|------|
| `internal/app/whatsapp` | Domain: provider interface, eligibility, phone, opt-out, service |
| `internal/app/whatsapp/evolution` | Sole Evolution HTTP adapter + webhook normalizer |
| `internal/models/whatsapp.go` | Persistence models |
| `internal/repository/pg_whatsapp.go` | Postgres repository |
| migration `000080_whatsapp_channel` | Schema |

## Provider boundary

Domain code depends only on `whatsapp.Provider`. Evolution DTOs stay inside `evolution/`. Future Meta Cloud API direct or other BSP adapters implement the same interface.

## Testing

- Unit tests use `MockProvider` and `httptest` for Evolution client.
- Never send real WhatsApp to leads during development.
- Operator-controlled numbers / sandbox only for live smoke.

## External blockers (channel not live until)

1. Meta Business / WABA verification
2. Cloud API phone number connected on Evolution instance
3. Official templates approved for cold-start messaging
4. Webhook URL reachable by Evolution with valid secret
5. `CONFENGE_WHATSAPP_ENABLED=true` only after the above

## CONFENGE orchestration (W2)

Deterministic channel orchestrator: `internal/app/confenge/whatsapp_orchestrator.go`.

| Case | Situation | Result |
|------|-----------|--------|
| A | Public phone, no opt-in | Email allowed; WhatsApp blocked |
| B | Reply requests WhatsApp / opt-in with provenance | WhatsApp eligible (review gate) |
| C | User-initiated inbound | Service window + USER_INITIATED |
| D | Site form opt-in with provenance | Template outside window; free text inside |
| E | Opt-out / DNC | Stop all WhatsApp automation (sticky) |

Feed extension (backward compatible):

```json
{
  "phone": "(48) 99999-9999",
  "phone_detail": { "raw": "...", "e164": "+55...", "source_kind": "official_company_site" },
  "whatsapp": { "consent_status": "UNKNOWN", "consent_source": null, "consent_at": null }
}
```

extra-cli supplies facts; Warmbly applies eligibility. Migrations `000083` (channel state) and `000084` (draft channel + candidate phone/consent columns).
