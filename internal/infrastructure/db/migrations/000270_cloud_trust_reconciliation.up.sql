CREATE TABLE public.cloud_managed_consents (
    id uuid PRIMARY KEY,
    organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    user_id uuid REFERENCES users(id) ON DELETE SET NULL,
    instance_id uuid,
    remote_id uuid,
    session_hash text,
    kind text NOT NULL CHECK (kind IN ('oauth', 'adopt')),
    provider public.email_provider NOT NULL CHECK (provider IN ('gmail', 'outlook')),
    cloud_account_id uuid,
    planned_account_id uuid,
    email_account_id uuid REFERENCES email_accounts(id) ON DELETE SET NULL,
    consent_state text NOT NULL DEFAULT 'revoked'
        CHECK (consent_state IN ('pending', 'active', 'pending_remove', 'revoked', 'expired', 'unknown')),
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    UNIQUE (instance_id, remote_id),
    UNIQUE (instance_id, session_hash)
);

CREATE TABLE public.pool_link_managed_operations (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    instance_id uuid REFERENCES pool_link_instances(id) ON DELETE SET NULL,
    remote_id uuid,
    session_hash text,
    kind text NOT NULL CHECK (kind IN ('oauth', 'adopt', 'revocation')),
    provider public.email_provider CHECK (provider IN ('gmail', 'outlook')),
    planned_account_id uuid,
    email_account_id uuid REFERENCES email_accounts(id) ON DELETE SET NULL,
    consent_state text NOT NULL DEFAULT 'revoked'
        CHECK (consent_state IN ('pending', 'exchanging', 'completed', 'unknown', 'failed', 'revoked')),
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    completed_at timestamptz,
    activation_pending boolean NOT NULL DEFAULT false,
    CHECK (provider IS NOT NULL OR (kind = 'revocation' AND consent_state = 'revoked')),
    UNIQUE (instance_id, remote_id),
    UNIQUE (instance_id, session_hash)
);

ALTER TABLE public.cloud_link
    ADD COLUMN disconnect_pending boolean NOT NULL DEFAULT false;

ALTER TABLE public.cloud_link_mailboxes
    ADD COLUMN enrollment_state text NOT NULL DEFAULT 'active',
    ADD COLUMN standing_observed_at timestamptz,
    ADD CONSTRAINT cloud_link_mailboxes_enrollment_state_check
        CHECK (enrollment_state IN ('active', 'pending_enroll', 'pending_remove'));

-- Existing standing remains evidence; fresh availability requires a new observation.

ALTER TABLE public.warmup_reputation_ledger
    ADD COLUMN cloud_health_state text,
    ADD COLUMN cloud_blocked_until timestamptz,
    ADD COLUMN cloud_health_reason text,
    ADD COLUMN cloud_health_score double precision NOT NULL DEFAULT 0,
    ADD COLUMN cloud_source_account_id uuid,
    ADD COLUMN cloud_source_instance_id uuid,
    ADD CONSTRAINT warmup_reputation_ledger_cloud_state_check
        CHECK (cloud_health_state IN ('quarantined', 'blocked'));

CREATE OR REPLACE FUNCTION public.warmup_reputation_mirror() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    v_org uuid;
    v_email text;
    worst record;
BEGIN
    SELECT a.organization_id, lower(btrim(a.email)) INTO v_org, v_email
    FROM public.email_accounts a JOIN public.organizations o ON o.id = a.organization_id
    WHERE a.id = NEW.email_account_id;
    IF v_org IS NULL THEN RETURN NULL; END IF;

    SELECT p.health_state, p.blocked_at, p.blocked_until, p.blocked_reason,
           p.last_health_score, p.last_health_reason INTO worst
    FROM public.warmup_pool_participants p JOIN public.email_accounts a ON a.id = p.email_account_id
    WHERE a.organization_id = v_org AND lower(btrim(a.email)) = v_email
    ORDER BY CASE p.health_state WHEN 'blocked' THEN 5 WHEN 'quarantined' THEN 4
                 WHEN 'throttled' THEN 3 WHEN 'watch' THEN 2 WHEN 'healthy' THEN 1 ELSE 0 END DESC,
             (p.health_state = 'blocked' AND p.blocked_until IS NULL) DESC,
             p.blocked_until DESC NULLS LAST LIMIT 1;

    IF worst.health_state IS NULL OR worst.health_state = 'healthy' THEN
        DELETE FROM public.warmup_reputation_ledger
        WHERE organization_id = v_org AND email = v_email AND cloud_health_state IS NULL;
        UPDATE public.warmup_reputation_ledger SET health_state = 'healthy', blocked_at = NULL,
            blocked_until = NULL, blocked_reason = NULL, last_health_score = 0, last_health_reason = NULL
        WHERE organization_id = v_org AND email = v_email AND cloud_health_state IS NOT NULL;
        RETURN NULL;
    END IF;

    INSERT INTO public.warmup_reputation_ledger
        (organization_id, email, health_state, blocked_at, blocked_until, blocked_reason,
         last_health_score, last_health_reason, recorded_at, standing_until)
    VALUES (v_org, v_email, worst.health_state, worst.blocked_at, worst.blocked_until, worst.blocked_reason,
            worst.last_health_score, worst.last_health_reason, now(),
            CASE WHEN worst.health_state = 'blocked' AND worst.blocked_until IS NULL THEN NULL
                 ELSE GREATEST(COALESCE(worst.blocked_until, now()), now()) END)
    ON CONFLICT (organization_id, email) DO UPDATE SET
        health_state = EXCLUDED.health_state, blocked_at = EXCLUDED.blocked_at,
        blocked_until = EXCLUDED.blocked_until, blocked_reason = EXCLUDED.blocked_reason,
        last_health_score = EXCLUDED.last_health_score, last_health_reason = EXCLUDED.last_health_reason,
        recorded_at = EXCLUDED.recorded_at, standing_until = EXCLUDED.standing_until;
    RETURN NULL;
END;
$$;

INSERT INTO public.warmup_reputation_ledger
    (organization_id, email, cloud_health_state, cloud_blocked_until, cloud_health_reason,
     cloud_health_score, cloud_source_account_id, cloud_source_instance_id, standing_until)
SELECT DISTINCT ON (ea.organization_id, lower(btrim(ea.email)))
    ea.organization_id, lower(btrim(ea.email)), clm.health_state, clm.blocked_until,
    clm.health_reason, clm.health_score, ea.id, clm.instance_id, now()
FROM public.cloud_link_mailboxes clm JOIN public.email_accounts ea ON ea.id = clm.email_account_id
WHERE ea.organization_id IS NOT NULL AND clm.health_state IN ('quarantined', 'blocked')
    AND (clm.blocked_until IS NULL OR clm.blocked_until > now())
ORDER BY ea.organization_id, lower(btrim(ea.email)),
    CASE clm.health_state WHEN 'blocked' THEN 2 ELSE 1 END DESC,
    (clm.blocked_until IS NULL) DESC, clm.blocked_until DESC
ON CONFLICT (organization_id, email) DO UPDATE SET
    cloud_health_state = EXCLUDED.cloud_health_state, cloud_blocked_until = EXCLUDED.cloud_blocked_until,
    cloud_health_reason = EXCLUDED.cloud_health_reason, cloud_health_score = EXCLUDED.cloud_health_score,
    cloud_source_account_id = EXCLUDED.cloud_source_account_id, cloud_source_instance_id = EXCLUDED.cloud_source_instance_id;
