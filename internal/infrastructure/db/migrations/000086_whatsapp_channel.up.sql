-- WhatsApp channel: consent/eligibility state, messages, instance mapping,
-- official templates cache, and webhook idempotency.
-- Evolution API is external; nothing here stores Evolution as a CRM.

-- Per-contact (or pre-contact) WhatsApp channel state.
CREATE TABLE IF NOT EXISTS whatsapp_contact_state (
    id                      uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id         uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    contact_id              uuid REFERENCES contacts(id) ON DELETE SET NULL,
    outreach_candidate_id   uuid,

    phone_raw               text NOT NULL DEFAULT '',
    phone_e164              text NOT NULL DEFAULT '',
    phone_country           text NOT NULL DEFAULT '',
    phone_valid             boolean NOT NULL DEFAULT false,
    phone_source            text NOT NULL DEFAULT '',
    phone_source_url        text NOT NULL DEFAULT '',
    phone_verified_at       timestamptz,

    whatsapp_consent_status text NOT NULL DEFAULT 'UNKNOWN',
    whatsapp_consent_source text NOT NULL DEFAULT '',
    whatsapp_consent_at     timestamptz,
    whatsapp_consent_scope  text NOT NULL DEFAULT '',
    whatsapp_consent_provenance_ok boolean NOT NULL DEFAULT false,
    whatsapp_consent_form_version text NOT NULL DEFAULT '',
    whatsapp_consent_recorded_by uuid REFERENCES users(id) ON DELETE SET NULL,

    whatsapp_last_inbound_at timestamptz,
    whatsapp_service_window_until timestamptz,
    whatsapp_channel_status text NOT NULL DEFAULT '',
    whatsapp_opt_out_at     timestamptz,
    do_not_contact          boolean NOT NULL DEFAULT false,

    last_email_outbound_at    timestamptz,
    last_whatsapp_outbound_at timestamptz,

    created_at              timestamptz NOT NULL DEFAULT now(),
    updated_at              timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT whatsapp_contact_state_consent_check CHECK (
        whatsapp_consent_status IN (
            'UNKNOWN', 'NO_OPT_IN', 'OPTED_IN', 'USER_INITIATED',
            'OPTED_OUT', 'DO_NOT_CONTACT'
        )
    )
);

CREATE UNIQUE INDEX IF NOT EXISTS whatsapp_contact_state_org_contact_uidx
    ON whatsapp_contact_state (organization_id, contact_id)
    WHERE contact_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS whatsapp_contact_state_org_phone_idx
    ON whatsapp_contact_state (organization_id, phone_e164)
    WHERE phone_e164 <> '';

CREATE INDEX IF NOT EXISTS whatsapp_contact_state_org_consent_idx
    ON whatsapp_contact_state (organization_id, whatsapp_consent_status);

-- Message history (CRM-facing; separate from email threads).
CREATE TABLE IF NOT EXISTS whatsapp_messages (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id     uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    contact_id          uuid REFERENCES contacts(id) ON DELETE SET NULL,
    thread_key          text NOT NULL,
    direction           text NOT NULL,
    channel             text NOT NULL DEFAULT 'WHATSAPP',
    provider            text NOT NULL DEFAULT 'evolution',
    provider_message_id text NOT NULL DEFAULT '',
    idempotency_key     text NOT NULL,
    body_text           text NOT NULL DEFAULT '',
    template_name       text NOT NULL DEFAULT '',
    template_language   text NOT NULL DEFAULT '',
    status              text NOT NULL DEFAULT 'received',
    failure_code        text NOT NULL DEFAULT '',
    draft_id            uuid,
    campaign_id         uuid,
    reviewed_by         uuid REFERENCES users(id) ON DELETE SET NULL,
    approved_at         timestamptz,
    sent_at             timestamptz,
    occurred_at         timestamptz NOT NULL DEFAULT now(),
    created_at          timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT whatsapp_messages_direction_check CHECK (direction IN ('inbound', 'outbound')),
    CONSTRAINT whatsapp_messages_status_check CHECK (
        status IN ('received', 'queued', 'sent', 'delivered', 'read', 'failed', 'blocked')
    )
);

CREATE UNIQUE INDEX IF NOT EXISTS whatsapp_messages_org_idem_uidx
    ON whatsapp_messages (organization_id, idempotency_key);

CREATE INDEX IF NOT EXISTS whatsapp_messages_org_thread_idx
    ON whatsapp_messages (organization_id, thread_key, occurred_at DESC);

CREATE INDEX IF NOT EXISTS whatsapp_messages_org_contact_idx
    ON whatsapp_messages (organization_id, contact_id, occurred_at DESC)
    WHERE contact_id IS NOT NULL;

-- Provider instance → organization mapping (Evolution instance name).
CREATE TABLE IF NOT EXISTS whatsapp_instances (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id  uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    provider         text NOT NULL DEFAULT 'evolution',
    instance_name    text NOT NULL,
    integration_mode text NOT NULL DEFAULT 'WHATSAPP-BUSINESS',
    phone_e164       text NOT NULL DEFAULT '',
    webhook_secret   text NOT NULL DEFAULT '',
    status           text NOT NULL DEFAULT 'unknown',
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT whatsapp_instances_mode_check CHECK (
        integration_mode IN ('WHATSAPP-BUSINESS', 'WHATSAPP-BAILEYS')
    )
);

CREATE UNIQUE INDEX IF NOT EXISTS whatsapp_instances_provider_name_uidx
    ON whatsapp_instances (provider, instance_name);

CREATE INDEX IF NOT EXISTS whatsapp_instances_org_idx
    ON whatsapp_instances (organization_id);

-- Official template approval cache (Meta/BSP is source of truth).
CREATE TABLE IF NOT EXISTS whatsapp_templates (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id  uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    provider         text NOT NULL DEFAULT 'evolution',
    external_id      text NOT NULL DEFAULT '',
    name             text NOT NULL,
    language         text NOT NULL DEFAULT 'pt_BR',
    category         text NOT NULL DEFAULT '',
    status           text NOT NULL DEFAULT 'PENDING',
    variables        jsonb NOT NULL DEFAULT '[]'::jsonb,
    last_sync_at     timestamptz,
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT whatsapp_templates_status_check CHECK (
        status IN ('APPROVED', 'PAUSED', 'REJECTED', 'PENDING', 'MISSING')
    )
);

CREATE UNIQUE INDEX IF NOT EXISTS whatsapp_templates_org_name_lang_uidx
    ON whatsapp_templates (organization_id, name, language);

-- Webhook idempotency log (duplicate provider events must not double-apply).
CREATE TABLE IF NOT EXISTS whatsapp_webhook_events (
    id                   uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id      uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    provider             text NOT NULL DEFAULT 'evolution',
    idempotency_key      text NOT NULL,
    event_type           text NOT NULL DEFAULT '',
    external_message_id  text NOT NULL DEFAULT '',
    payload_hash         text NOT NULL DEFAULT '',
    processed_at         timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS whatsapp_webhook_events_org_idem_uidx
    ON whatsapp_webhook_events (organization_id, idempotency_key);

CREATE INDEX IF NOT EXISTS whatsapp_webhook_events_processed_idx
    ON whatsapp_webhook_events (processed_at);
