# Outcome contract `confenge.outcome.v1`

Warmbly returns commercial outcomes to extra-cli through an **outbox + HTTPS
HMAC webhook**. Warmbly never writes the datalake database.

## Envelope

```json
{
  "schema_version": "confenge.outcome.v1",
  "event_id": "uuid",
  "idempotency_key": "string",
  "occurred_at": "RFC3339",
  "source": "warmbly",
  "source_lead_id": "string",
  "cnpj14": "string",
  "contact_email": "string",
  "event_type": "REPLIED",
  "campaign_id": "string",
  "message_id": "string",
  "metadata": {}
}
```

## Event types

`LEAD_IMPORTED`, `LEAD_REVIEWED`, `CONTACT_APPROVED`, `CONTACTED`, `REPLIED`,
`MEETING`, `PROPOSAL`, `WON`, `LOST`, `DO_NOT_CONTACT`, `BOUNCED`.

Additive commercial-action fields (optional, ignored by older consumers):
`action_id`, `action_type`, `reachability_class`, `outcome_code`,
`target_reached`, `conversation_started`, `interest_state`,
`person_relevance_feedback`, `route_validity`, `referral`, `new_person`,
`new_role`, `new_route`, `preferred_channel`. WON is never inferred from
these fields.

## Transport

| Setting | Purpose |
| --- | --- |
| `CONFENGE_OUTCOME_WEBHOOK_URL` | HTTPS endpoint on extra-cli |
| `CONFENGE_OUTCOME_WEBHOOK_SECRET` | HMAC shared secret |

Header: `X-Warmbly-Signature: t=<unix>,v1=<hex(hmac_sha256(secret, "<unix>." + body))>`

Receivers must:

1. Reject non-HTTPS in production
2. Verify signature with constant-time compare
3. Reject timestamps outside a short skew window (for example 5 minutes)
4. Deduplicate on `idempotency_key` / `event_id`

## Delivery semantics

- Enqueue is transactional with the user action path (outbox table).
- A background worker (or manual replay) drains pending rows with exponential
  backoff and a dead-letter flag after repeated failure.
- Replays are safe: same idempotency key must not create a second ledger entry
  in extra-cli Decision & Outcome Memory.

## Mapping to extra-cli commercial states

| Outcome | Suggested extra-cli state |
| --- | --- |
| LEAD_IMPORTED | IMPORTED / known in Warmbly |
| CONTACT_APPROVED | READY |
| CONTACTED | CONTACTED |
| REPLIED | REPLIED |
| MEETING | MEETING |
| PROPOSAL | PROPOSAL |
| WON | WON (human only) |
| LOST | LOST (human or explicit rule) |
| DO_NOT_CONTACT | DNC |
| BOUNCED | BOUNCED |

Do not auto-mark WON from machine classification alone.
