-- Multichannel commercial actions wrap the email pipeline. They never
-- replace outreach_touchpoints (EMAIL/WHATSAPP approval remains fail-closed).

CREATE TABLE IF NOT EXISTS outreach_commercial_actions (
    id                      uuid PRIMARY KEY,
    organization_id         uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    account_id              uuid NOT NULL REFERENCES outreach_accounts(id) ON DELETE CASCADE,
    contact_candidate_id    uuid REFERENCES outreach_contact_candidates(id) ON DELETE SET NULL,
    parent_action_id        uuid REFERENCES outreach_commercial_actions(id) ON DELETE SET NULL,
    followup_action_id      uuid REFERENCES outreach_commercial_actions(id) ON DELETE SET NULL,
    source_lead_id          text NOT NULL DEFAULT '',
    person_name             text NOT NULL DEFAULT '',
    observed_role           text NOT NULL DEFAULT '',
    target_role             text NOT NULL DEFAULT '',
    action_type             text NOT NULL,
    reachability_class      text NOT NULL DEFAULT '',
    mapping_version         text NOT NULL DEFAULT 'confenge.reachability.v1',
    route_type              text NOT NULL DEFAULT '',
    route_relation          text NOT NULL DEFAULT '',
    channel_value           text NOT NULL DEFAULT '',
    channel_display         text NOT NULL DEFAULT '',
    why_now                 text NOT NULL DEFAULT '',
    factual_hook            text NOT NULL DEFAULT '',
    recommended_action      text NOT NULL DEFAULT '',
    service_code            text NOT NULL DEFAULT '',
    service_context         text NOT NULL DEFAULT '',
    confidence              text NOT NULL DEFAULT '',
    evidence_ids            jsonb NOT NULL DEFAULT '[]'::jsonb,
    warnings                jsonb NOT NULL DEFAULT '[]'::jsonb,
    state                   text NOT NULL DEFAULT 'PLANNED',
    lane                    text NOT NULL DEFAULT '',
    priority_rank           int NOT NULL DEFAULT 0,
    priority_score          double precision NOT NULL DEFAULT 0,
    actionable              boolean NOT NULL DEFAULT false,
    email_sendable          boolean NOT NULL DEFAULT false,
    dispatchable            boolean NOT NULL DEFAULT false,
    person_fingerprint      text NOT NULL DEFAULT '',
    route_fingerprint       text NOT NULL DEFAULT '',
    content_hash            text NOT NULL DEFAULT '',
    snapshot_hash           text NOT NULL DEFAULT '',
    idempotency_key         text NOT NULL DEFAULT '',
    human_actor             text NOT NULL DEFAULT '',
    human_notes             text NOT NULL DEFAULT '',
    outcome_code            text NOT NULL DEFAULT '',
    outcome_notes           text NOT NULL DEFAULT '',
    target_reached          boolean,
    conversation_started    boolean NOT NULL DEFAULT false,
    interest_state          text NOT NULL DEFAULT '',
    next_action_type        text NOT NULL DEFAULT '',
    next_action_at          timestamptz,
    route_quality_feedback  text NOT NULL DEFAULT '',
    person_relevance_feedback text NOT NULL DEFAULT '',
    message_feedback        text NOT NULL DEFAULT '',
    content_json            jsonb NOT NULL DEFAULT '{}'::jsonb,
    correction_json         jsonb NOT NULL DEFAULT '[]'::jsonb,
    blocked_person          boolean NOT NULL DEFAULT false,
    blocked_route           boolean NOT NULL DEFAULT false,
    stale_warning           text NOT NULL DEFAULT '',
    requires_fresh          boolean NOT NULL DEFAULT false,
    company_name            text NOT NULL DEFAULT '',
    created_at              timestamptz NOT NULL DEFAULT now(),
    updated_at              timestamptz NOT NULL DEFAULT now(),
    started_at              timestamptz,
    completed_at            timestamptz,
    CONSTRAINT outreach_commercial_actions_type_check CHECK (action_type IN (
        'DIRECT_EMAIL','INFERRED_EMAIL_REVIEW','ROLE_EMAIL','GENERIC_EMAIL',
        'DIRECT_CALL','ROUTED_CALL','WHATSAPP','PROFESSIONAL_SOCIAL',
        'CONTACT_FORM','OTHER_MANUAL')),
    CONSTRAINT outreach_commercial_actions_state_check CHECK (state IN (
        'PLANNED','READY','IN_PROGRESS','COMPLETED','FAILED','SKIPPED','BLOCKED','NEEDS_FOLLOWUP'))
);

CREATE UNIQUE INDEX IF NOT EXISTS outreach_commercial_actions_org_idempotency_uidx
    ON outreach_commercial_actions (organization_id, idempotency_key)
    WHERE idempotency_key <> '';

CREATE UNIQUE INDEX IF NOT EXISTS outreach_commercial_actions_org_open_route_uidx
    ON outreach_commercial_actions (organization_id, account_id, action_type, route_fingerprint)
    WHERE state IN ('PLANNED','READY','IN_PROGRESS','NEEDS_FOLLOWUP');

CREATE INDEX IF NOT EXISTS outreach_commercial_actions_today_idx
    ON outreach_commercial_actions (organization_id, state, lane, priority_score DESC);

CREATE INDEX IF NOT EXISTS outreach_commercial_actions_account_idx
    ON outreach_commercial_actions (organization_id, account_id, created_at DESC);

CREATE INDEX IF NOT EXISTS outreach_commercial_actions_history_idx
    ON outreach_commercial_actions (organization_id, action_type, reachability_class, outcome_code);
