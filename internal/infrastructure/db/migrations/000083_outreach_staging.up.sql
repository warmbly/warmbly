-- Outreach staging: multi-tenant company staging queue for intelligence-plane
-- feeds (e.g. extra-cli confenge.outreach.v1). Companies may exist without a
-- validated email; they are not forced into the contacts table until promoted.
-- Feature flag: CONFENGE_OUTREACH_ENABLED (see internal/app/confenge).

CREATE TABLE IF NOT EXISTS outreach_import_runs (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id     uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,

    source_system       text NOT NULL DEFAULT 'extra-cli',
    source_run_id       text NOT NULL DEFAULT '',
    schema_version      text NOT NULL DEFAULT 'confenge.outreach.v1',
    snapshot_hash       text NOT NULL DEFAULT '',
    repo_sha            text NOT NULL DEFAULT '',
    payload_hash        text NOT NULL DEFAULT '',
    profile_id          text NOT NULL DEFAULT '',
    profile_version     text NOT NULL DEFAULT '',

    status              text NOT NULL DEFAULT 'pending',
    dry_run             boolean NOT NULL DEFAULT false,
    started_at          timestamptz NOT NULL DEFAULT now(),
    finished_at         timestamptz,
    cursor_in           text NOT NULL DEFAULT '',
    cursor_out          text NOT NULL DEFAULT '',

    counts              jsonb NOT NULL DEFAULT '{}'::jsonb,
    errors              jsonb NOT NULL DEFAULT '[]'::jsonb,
    warnings            jsonb NOT NULL DEFAULT '[]'::jsonb,

    created_by_user_id  uuid REFERENCES users(id) ON DELETE SET NULL,
    idempotency_key     text NOT NULL DEFAULT '',
    source_uri          text NOT NULL DEFAULT '',

    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT outreach_import_runs_status_check
        CHECK (status IN ('pending', 'running', 'completed', 'failed', 'partial'))
);

CREATE UNIQUE INDEX IF NOT EXISTS outreach_import_runs_org_idempotency_uidx
    ON outreach_import_runs (organization_id, idempotency_key)
    WHERE idempotency_key <> '';

CREATE INDEX IF NOT EXISTS outreach_import_runs_org_started_idx
    ON outreach_import_runs (organization_id, started_at DESC);

-- Company / account staging (no email required).
CREATE TABLE IF NOT EXISTS outreach_accounts (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id     uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,

    source_lead_id      text NOT NULL DEFAULT '',
    cnpj14              text NOT NULL,
    cnpj_root           text NOT NULL DEFAULT '',
    razao_social        text NOT NULL DEFAULT '',
    nome_fantasia       text NOT NULL DEFAULT '',
    municipio           text NOT NULL DEFAULT '',
    uf                  text NOT NULL DEFAULT '',
    website             text NOT NULL DEFAULT '',

    priority_rank       int NOT NULL DEFAULT 0,
    priority_score      double precision NOT NULL DEFAULT 0,
    priority_tier       text NOT NULL DEFAULT '',
    priority_confidence text NOT NULL DEFAULT '',

    moment_code         text NOT NULL DEFAULT '',
    moment_summary      text NOT NULL DEFAULT '',
    moment_observed_at  date,
    moment_confidence   text NOT NULL DEFAULT '',
    moment_evidence_ids jsonb NOT NULL DEFAULT '[]'::jsonb,

    service_code        text NOT NULL DEFAULT '',
    service_name        text NOT NULL DEFAULT '',
    entry_offer         text NOT NULL DEFAULT '',
    offer_rationale     text NOT NULL DEFAULT '',

    fact_to_mention     text NOT NULL DEFAULT '',
    question_to_ask     text NOT NULL DEFAULT '',
    cta                 text NOT NULL DEFAULT '',
    claims_to_avoid     jsonb NOT NULL DEFAULT '[]'::jsonb,

    commercial_state    text NOT NULL DEFAULT 'NEW',
    queue_state         text NOT NULL DEFAULT 'NEEDS_CONTACT',
    human_override      boolean NOT NULL DEFAULT false,
    blocked             boolean NOT NULL DEFAULT false,
    block_reason        text NOT NULL DEFAULT '',
    do_not_contact      boolean NOT NULL DEFAULT false,

    source_system       text NOT NULL DEFAULT 'extra-cli',
    source_run_id       text NOT NULL DEFAULT '',
    last_import_run_id  uuid REFERENCES outreach_import_runs(id) ON DELETE SET NULL,
    last_payload_hash   text NOT NULL DEFAULT '',
    contracts_json      jsonb NOT NULL DEFAULT '[]'::jsonb,
    raw_snapshot        jsonb NOT NULL DEFAULT '{}'::jsonb,

    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT outreach_accounts_cnpj14_check
        CHECK (cnpj14 ~ '^[0-9]{14}$'),
    CONSTRAINT outreach_accounts_queue_state_check
        CHECK (queue_state IN (
            'NEEDS_CONTACT',
            'READY_TO_GENERATE',
            'NEEDS_REVIEW',
            'APPROVED',
            'ENROLLED',
            'SENT',
            'REPLIED',
            'MEETING',
            'PROPOSAL',
            'WON',
            'LOST',
            'BLOCKED',
            'BOUNCED',
            'DO_NOT_CONTACT',
            'SKIPPED'
        ))
);

CREATE UNIQUE INDEX IF NOT EXISTS outreach_accounts_org_cnpj14_uidx
    ON outreach_accounts (organization_id, cnpj14);

CREATE INDEX IF NOT EXISTS outreach_accounts_org_queue_idx
    ON outreach_accounts (organization_id, queue_state);

CREATE INDEX IF NOT EXISTS outreach_accounts_org_source_lead_idx
    ON outreach_accounts (organization_id, source_lead_id)
    WHERE source_lead_id <> '';

-- Contact candidates (may lack email or verification).
CREATE TABLE IF NOT EXISTS outreach_contact_candidates (
    id                      uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id         uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    account_id              uuid NOT NULL REFERENCES outreach_accounts(id) ON DELETE CASCADE,

    source_contact_id       text NOT NULL DEFAULT '',
    name                    text NOT NULL DEFAULT '',
    role                    text NOT NULL DEFAULT '',
    email                   text NOT NULL DEFAULT '',
    phone                   text NOT NULL DEFAULT '',
    linkedin_url            text NOT NULL DEFAULT '',
    source_url              text NOT NULL DEFAULT '',
    source_document         text NOT NULL DEFAULT '',
    source_date             date,
    verification_status     text NOT NULL DEFAULT 'NOT_FOUND',
    confidence              text NOT NULL DEFAULT '',
    recommended             boolean NOT NULL DEFAULT false,

    warmbly_contact_id      uuid REFERENCES contacts(id) ON DELETE SET NULL,
    promoted_at             timestamptz,
    blocked                 boolean NOT NULL DEFAULT false,
    block_reason            text NOT NULL DEFAULT '',
    do_not_contact          boolean NOT NULL DEFAULT false,
    bounced                 boolean NOT NULL DEFAULT false,

    last_import_run_id      uuid REFERENCES outreach_import_runs(id) ON DELETE SET NULL,
    created_at              timestamptz NOT NULL DEFAULT now(),
    updated_at              timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT outreach_contact_candidates_verification_check
        CHECK (verification_status IN (
            'OFFICIAL_SOURCE',
            'PUBLIC_DOCUMENT_RECENT',
            'MULTIPLE_PUBLIC_SOURCES',
            'INSTITUTIONAL_GENERIC',
            'PUBLIC_POSSIBLY_STALE',
            'CANDIDATE_UNVERIFIED',
            'NOT_FOUND',
            'INVALID',
            'BOUNCED',
            'DO_NOT_CONTACT'
        ))
);

CREATE INDEX IF NOT EXISTS outreach_contact_candidates_account_idx
    ON outreach_contact_candidates (organization_id, account_id);

CREATE UNIQUE INDEX IF NOT EXISTS outreach_contact_candidates_org_source_uidx
    ON outreach_contact_candidates (organization_id, account_id, source_contact_id)
    WHERE source_contact_id <> '';

CREATE INDEX IF NOT EXISTS outreach_contact_candidates_email_idx
    ON outreach_contact_candidates (organization_id, lower(email))
    WHERE email <> '';

-- Normalized evidence per account (sanitized text only; no raw HTML).
CREATE TABLE IF NOT EXISTS outreach_evidence (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id     uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    account_id          uuid NOT NULL REFERENCES outreach_accounts(id) ON DELETE CASCADE,

    source_evidence_id  text NOT NULL,
    evidence_type       text NOT NULL DEFAULT '',
    title               text NOT NULL DEFAULT '',
    url                 text NOT NULL DEFAULT '',
    document            text NOT NULL DEFAULT '',
    evidence_date       date,
    location            text NOT NULL DEFAULT '',
    excerpt             text NOT NULL DEFAULT '',
    synthesis           text NOT NULL DEFAULT '',
    epistemic_class     text NOT NULL DEFAULT 'COMMERCIAL_HYPOTHESIS',
    reliability         text NOT NULL DEFAULT '',
    consulted_at        date,

    last_import_run_id  uuid REFERENCES outreach_import_runs(id) ON DELETE SET NULL,
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT outreach_evidence_epistemic_check
        CHECK (epistemic_class IN (
            'CONFIRMED_FACT',
            'STRONG_INFERENCE',
            'WEAK_INFERENCE',
            'COMMERCIAL_HYPOTHESIS',
            'NOT_FOUND',
            'REQUIRES_COMPANY_CONFIRMATION',
            'CONTRADICTORY_EVIDENCE'
        ))
);

CREATE UNIQUE INDEX IF NOT EXISTS outreach_evidence_org_source_uidx
    ON outreach_evidence (organization_id, account_id, source_evidence_id);

CREATE INDEX IF NOT EXISTS outreach_evidence_account_idx
    ON outreach_evidence (organization_id, account_id);
