ALTER TABLE email_accounts
    ADD COLUMN test_mode text CHECK (test_mode IN ('legacy','diagnostic','off')),
    ADD COLUMN test_send_enabled boolean NOT NULL DEFAULT false,
    ADD COLUMN test_receive_enabled boolean NOT NULL DEFAULT false,
    ADD COLUMN shared_daily_limit integer CHECK (shared_daily_limit > 0),
    ADD COLUMN rolling_recipient_limit integer CHECK (rolling_recipient_limit > 0);

ALTER TABLE email_accounts ALTER COLUMN test_mode SET DEFAULT 'off';
ALTER TABLE tasks
    ADD COLUMN send_reserved_at timestamptz,
    ADD COLUMN send_business_day date,
    ADD COLUMN send_recipients text[],
    ADD COLUMN send_released_at timestamptz,
    ADD COLUMN send_executor_nonce uuid,
    ADD COLUMN send_executor_worker uuid REFERENCES fleet_nodes(id) ON DELETE SET NULL,
    ADD COLUMN send_executor_started_at timestamptz,
    ADD COLUMN send_executor_result jsonb;
CREATE INDEX tasks_send_reservations ON tasks(email_account_id,send_reserved_at) WHERE send_reserved_at IS NOT NULL;

CREATE TABLE diagnostic_auth_verifications (
    task_id uuid NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    email_account_id uuid NOT NULL REFERENCES email_accounts(id) ON DELETE CASCADE,
    worker_id uuid REFERENCES fleet_nodes(id) ON DELETE SET NULL,
    nonce uuid NOT NULL,
    message_id text NOT NULL,
    authorized_at timestamptz NOT NULL DEFAULT NOW(),
    result jsonb CHECK (result IS NULL OR result->>'dkim' IN ('unknown','pass','fail')),
    verified_at timestamptz,
    PRIMARY KEY(task_id,email_account_id)
);

CREATE TABLE send_recovery_resolutions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    email_account_id uuid NOT NULL REFERENCES email_accounts(id) ON DELETE CASCADE,
    recovery_task_id uuid REFERENCES tasks(id) ON DELETE SET NULL,
    evidence_task_id uuid REFERENCES tasks(id) ON DELETE SET NULL,
    previous_reason text NOT NULL CHECK(previous_reason IN ('authentication','permanent','conflict')),
    evidence_type text NOT NULL CHECK(evidence_type IN ('authentication_repaired','operator_provider_confirmation')),
    confirmation_reference text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT NOW()
);

CREATE TABLE send_result_effects (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    task_id uuid NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    effect_key text NOT NULL,
    organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    kind text NOT NULL CHECK(kind IN ('webhook','notification')),
    payload jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT NOW(),
    delivered_at timestamptz,
    lease uuid,
    locked_until timestamptz,
    attempts integer NOT NULL DEFAULT 0,
    UNIQUE(task_id,effect_key)
);
CREATE INDEX send_result_effects_pending ON send_result_effects(created_at) WHERE delivered_at IS NULL;
