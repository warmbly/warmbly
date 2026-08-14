-- Outcome outbox: commercial events returned to extra-cli via HMAC webhook.
-- Warmbly never writes the datalake; delivery is async with retries.

CREATE TABLE IF NOT EXISTS outreach_outcome_outbox (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id     uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,

    event_id            uuid NOT NULL DEFAULT gen_random_uuid(),
    idempotency_key     text NOT NULL,
    source_lead_id      text NOT NULL DEFAULT '',
    cnpj14              text NOT NULL DEFAULT '',
    contact_email       text NOT NULL DEFAULT '',
    event_type          text NOT NULL,
    payload             jsonb NOT NULL DEFAULT '{}'::jsonb,
    occurred_at         timestamptz NOT NULL DEFAULT now(),

    attempts            int NOT NULL DEFAULT 0,
    next_attempt_at     timestamptz NOT NULL DEFAULT now(),
    delivered_at        timestamptz,
    last_error          text NOT NULL DEFAULT '',
    dead_letter         boolean NOT NULL DEFAULT false,

    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT outreach_outcome_outbox_type_check
        CHECK (event_type IN (
            'LEAD_IMPORTED',
            'LEAD_REVIEWED',
            'CONTACT_APPROVED',
            'CONTACTED',
            'REPLIED',
            'MEETING',
            'PROPOSAL',
            'WON',
            'LOST',
            'DO_NOT_CONTACT',
            'BOUNCED'
        ))
);

CREATE UNIQUE INDEX IF NOT EXISTS outreach_outcome_outbox_org_idempotency_uidx
    ON outreach_outcome_outbox (organization_id, idempotency_key);

CREATE INDEX IF NOT EXISTS outreach_outcome_outbox_pending_idx
    ON outreach_outcome_outbox (next_attempt_at)
    WHERE delivered_at IS NULL AND dead_letter = false;
