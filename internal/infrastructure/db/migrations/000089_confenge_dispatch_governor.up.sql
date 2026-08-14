-- CONFENGE global dispatch governor: shared rolling-hour cap for email + WhatsApp.
CREATE TABLE IF NOT EXISTS confenge_dispatch_control (
    id              smallint PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    paused          boolean NOT NULL DEFAULT false,
    pause_reason    text NOT NULL DEFAULT '',
    paused_at       timestamptz,
    paused_by       uuid,
    updated_at      timestamptz NOT NULL DEFAULT now()
);
INSERT INTO confenge_dispatch_control (id, paused, pause_reason)
VALUES (1, false, '') ON CONFLICT (id) DO NOTHING;

CREATE TABLE IF NOT EXISTS confenge_dispatch_sends (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id     uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    channel             text NOT NULL,
    message_key         text NOT NULL,
    draft_id            uuid,
    reservation_id      uuid,
    sent_at             timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT confenge_dispatch_sends_channel_check CHECK (channel IN ('EMAIL', 'WHATSAPP'))
);
CREATE UNIQUE INDEX IF NOT EXISTS confenge_dispatch_sends_message_key_uidx ON confenge_dispatch_sends (message_key);
CREATE INDEX IF NOT EXISTS confenge_dispatch_sends_sent_at_idx ON confenge_dispatch_sends (sent_at DESC);
CREATE INDEX IF NOT EXISTS confenge_dispatch_sends_org_sent_idx ON confenge_dispatch_sends (organization_id, sent_at DESC);

CREATE TABLE IF NOT EXISTS confenge_dispatch_reservations (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id     uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    channel             text NOT NULL,
    message_key         text NOT NULL,
    draft_id            uuid,
    state               text NOT NULL DEFAULT 'reserved',
    reserved_at         timestamptz NOT NULL DEFAULT now(),
    lease_until         timestamptz NOT NULL,
    committed_at        timestamptz,
    worker_token        text NOT NULL DEFAULT '',
    last_error          text NOT NULL DEFAULT '',
    CONSTRAINT confenge_dispatch_reservations_channel_check CHECK (channel IN ('EMAIL', 'WHATSAPP')),
    CONSTRAINT confenge_dispatch_reservations_state_check CHECK (state IN ('reserved', 'committed', 'released', 'failed'))
);
CREATE UNIQUE INDEX IF NOT EXISTS confenge_dispatch_reservations_message_key_uidx ON confenge_dispatch_reservations (message_key);
CREATE INDEX IF NOT EXISTS confenge_dispatch_reservations_active_idx ON confenge_dispatch_reservations (reserved_at) WHERE state = 'reserved';

CREATE TABLE IF NOT EXISTS confenge_dispatch_queue (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id     uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    channel             text NOT NULL,
    draft_id            uuid NOT NULL,
    message_key         text NOT NULL,
    recipient_ref       text NOT NULL DEFAULT '',
    due_at              timestamptz NOT NULL DEFAULT now(),
    priority            int NOT NULL DEFAULT 0,
    status              text NOT NULL DEFAULT 'queued',
    cancel_reason       text NOT NULL DEFAULT '',
    last_error          text NOT NULL DEFAULT '',
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT confenge_dispatch_queue_channel_check CHECK (channel IN ('EMAIL', 'WHATSAPP')),
    CONSTRAINT confenge_dispatch_queue_status_check CHECK (status IN ('queued', 'reserved', 'sent', 'cancelled', 'failed'))
);
CREATE UNIQUE INDEX IF NOT EXISTS confenge_dispatch_queue_message_key_uidx ON confenge_dispatch_queue (message_key);
CREATE INDEX IF NOT EXISTS confenge_dispatch_queue_fair_idx ON confenge_dispatch_queue (status, due_at ASC, priority DESC, created_at ASC) WHERE status = 'queued';
CREATE INDEX IF NOT EXISTS confenge_dispatch_queue_org_status_idx ON confenge_dispatch_queue (organization_id, status);

CREATE TABLE IF NOT EXISTS confenge_dispatch_failures (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id     uuid REFERENCES organizations(id) ON DELETE SET NULL,
    channel             text NOT NULL DEFAULT '',
    message_key         text NOT NULL DEFAULT '',
    draft_id            uuid,
    error_text          text NOT NULL DEFAULT '',
    occurred_at         timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS confenge_dispatch_failures_occurred_idx ON confenge_dispatch_failures (occurred_at DESC);
