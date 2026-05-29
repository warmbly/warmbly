-- Autonomous fleet management: cloud credentials, worker env profiles,
-- saved provisioning templates (every Hetzner option exposed), in-flight +
-- historical provisioning jobs as a state machine, budget caps + auto-
-- provision toggle, and a decision_log of every automated action.
--
-- The pluggable-storage migration (000039) already covers KMS / BlobStore /
-- EventBus / EncryptedKeys selection. This migration adds the operational
-- surface so the admin UI can orchestrate the actual fleet.

-- ---------------------------------------------------------------------------
-- Cloud credentials. encrypted_token holds the cipher-encrypted API token
-- (the same cipher service that wraps mailbox creds). last_test_* are
-- populated by the admin "Test connection" button so the operator sees
-- whether the token still works without having to dig into logs.
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS cloud_credentials (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    provider        TEXT NOT NULL CHECK (provider IN ('hetzner')),
    name            TEXT NOT NULL,
    encrypted_token TEXT NOT NULL,
    last_used_at    TIMESTAMPTZ,
    last_test_at    TIMESTAMPTZ,
    last_test_ok    BOOLEAN,
    last_test_error TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ---------------------------------------------------------------------------
-- NOTE: worker_profiles is intentionally NOT created here. It is owned by
-- migration 000029 (worker_credentials), which models the Kafka/Redis/AWS
-- connection bundle that pg_credentials.go reads and writes. An earlier merge
-- accidentally re-declared a second, conflicting worker_profiles in this file
-- (env_template/tier/egress_kind); because it used CREATE TABLE IF NOT EXISTS
-- it silently no-op'd against 029's table, so those columns never existed and
-- nothing reads them. The provisioning surface only needs worker_profiles to
-- EXIST so the provisioning_templates.worker_profile_id FK below can reference
-- it; the tier/egress_kind the provisioner cares about live on
-- provisioning_templates itself.
-- ---------------------------------------------------------------------------

-- ---------------------------------------------------------------------------
-- Provisioning templates: saved configs for one-click provisioning.
--
-- Every Hetzner option the admin form exposes lives here so the admin can
-- pick "cheapest US single-IP single-VPS" as a template once, then never
-- touch the form again. The scale loop picks the row with is_auto_template
-- = true for the relevant tier when auto-provisioning.
--
-- A partial unique index enforces "exactly one auto-template per tier".
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS provisioning_templates (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name              TEXT NOT NULL UNIQUE,
    description       TEXT,
    provider          TEXT NOT NULL,
    location          TEXT NOT NULL,
    datacenter        TEXT,
    server_type       TEXT NOT NULL,
    image             TEXT NOT NULL DEFAULT 'ubuntu-22.04',
    server_count      INT NOT NULL DEFAULT 1 CHECK (server_count >= 1 AND server_count <= 100),
    ipv4_per_server   INT NOT NULL DEFAULT 1 CHECK (ipv4_per_server >= 1 AND ipv4_per_server <= 64),
    ipv6_per_server   INT NOT NULL DEFAULT 1,
    worker_profile_id UUID REFERENCES worker_profiles(id) ON DELETE SET NULL,
    tier              TEXT NOT NULL CHECK (tier IN ('shared_free','shared_premium','dedicated')),
    egress_kind       TEXT NOT NULL DEFAULT 'cold_smtp'
                      CHECK (egress_kind IN ('cold_smtp','oauth_api','warmup_only')),
    labels            JSONB NOT NULL DEFAULT '{}'::jsonb,
    placement_group   TEXT,
    private_network   TEXT,
    firewall          TEXT,
    is_auto_template  BOOLEAN NOT NULL DEFAULT FALSE,
    est_monthly_cost  NUMERIC(10,2),
    est_cost_currency TEXT DEFAULT 'EUR',
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS provisioning_templates_auto_per_tier
    ON provisioning_templates (tier)
    WHERE is_auto_template;

-- ---------------------------------------------------------------------------
-- Provisioning policy: per-provider budget caps and the AUTO_PROVISION toggle.
-- One row per provider. Pre-seeded with safe defaults for Hetzner.
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS provisioning_policy (
    provider         TEXT PRIMARY KEY,
    enabled          BOOLEAN NOT NULL DEFAULT TRUE,
    auto_provision   BOOLEAN NOT NULL DEFAULT FALSE,
    max_per_day      INT NOT NULL DEFAULT 2,
    max_per_month    INT NOT NULL DEFAULT 30,
    monthly_budget   NUMERIC(10,2) DEFAULT 500,
    budget_currency  TEXT DEFAULT 'EUR',
    cooldown_min     INT NOT NULL DEFAULT 60,
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

INSERT INTO provisioning_policy (provider) VALUES ('hetzner')
ON CONFLICT (provider) DO NOTHING;

-- ---------------------------------------------------------------------------
-- Provisioning jobs: every provisioning attempt as a state machine row.
--
-- state transitions:
--   pending -> creating_server -> creating_ips -> assigning_ips
--           -> setting_rdns -> installing -> verifying
--           -> completed
--
-- on failure at any step: -> rolling_back -> failed
--
-- config is the snapshot of the template (or inline custom config) at the
-- time of submission. Mutating the template later doesn't retroactively
-- change in-flight or historical jobs.
--
-- provider_server_id / provider_ip_ids / ips / worker_ids populate as the
-- state machine progresses. They're what rollback uses to clean up.
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS provisioning_jobs (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    state              TEXT NOT NULL DEFAULT 'pending'
                       CHECK (state IN ('pending','creating_server','creating_ips',
                                        'assigning_ips','setting_rdns','installing',
                                        'verifying','completed','failed','rolling_back')),
    triggered_by       TEXT NOT NULL,
    provider           TEXT NOT NULL,
    credential_id      UUID REFERENCES cloud_credentials(id) ON DELETE SET NULL,
    template_id        UUID REFERENCES provisioning_templates(id) ON DELETE SET NULL,
    config             JSONB NOT NULL,
    provider_server_id TEXT,
    provider_ip_ids    TEXT[],
    ips                INET[],
    worker_ids         UUID[],
    est_monthly_cost   NUMERIC(10,2),
    cost_currency      TEXT DEFAULT 'EUR',
    error              TEXT,
    attempts           INT NOT NULL DEFAULT 0,
    last_step_at       TIMESTAMPTZ,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at       TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_provisioning_jobs_state
    ON provisioning_jobs (state);
CREATE INDEX IF NOT EXISTS idx_provisioning_jobs_created
    ON provisioning_jobs (created_at DESC);
CREATE INDEX IF NOT EXISTS idx_provisioning_jobs_active
    ON provisioning_jobs (created_at DESC)
    WHERE state NOT IN ('completed','failed');

-- ---------------------------------------------------------------------------
-- Decision log: every automated action the system takes (assignment,
-- rebalance, quarantine, provisioning, IP rotation) is one row here.
-- Powers the admin "Decisions" page and lets us answer "why did the system
-- do X" after the fact.
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS decision_log (
    id           BIGSERIAL PRIMARY KEY,
    kind         TEXT NOT NULL,
    worker_id    UUID,
    mailbox_id   UUID,
    before       JSONB,
    after        JSONB,
    reason       TEXT,
    triggered_by TEXT,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_decision_log_kind_time
    ON decision_log (kind, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_decision_log_worker_time
    ON decision_log (worker_id, created_at DESC);
