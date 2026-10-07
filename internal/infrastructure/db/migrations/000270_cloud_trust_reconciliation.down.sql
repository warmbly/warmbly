DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM public.cloud_link WHERE disconnect_pending)
       OR EXISTS (SELECT 1 FROM public.cloud_link_mailboxes WHERE enrollment_state <> 'active') THEN
        RAISE EXCEPTION 'Reconcile pending cloud lifecycle operations before downgrading';
    END IF;
END $$;

DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM public.cloud_managed_consents)
       OR EXISTS (SELECT 1 FROM public.pool_link_managed_operations) THEN
        RAISE EXCEPTION 'Managed consent history cannot be represented by the older schema';
    END IF;
END $$;

DROP TABLE public.cloud_managed_consents;
DROP TABLE public.pool_link_managed_operations;

UPDATE public.warmup_reputation_ledger
SET health_state = cloud_health_state,
    blocked_at = recorded_at, blocked_until = cloud_blocked_until,
    blocked_reason = cloud_health_reason, last_health_reason = cloud_health_reason,
    last_health_score = cloud_health_score,
    standing_until = CASE WHEN cloud_blocked_until IS NULL THEN NULL ELSE cloud_blocked_until END
WHERE cloud_health_state IN ('quarantined', 'blocked')
    AND (cloud_blocked_until IS NULL OR cloud_blocked_until > now())
    AND (CASE cloud_health_state WHEN 'blocked' THEN 5 ELSE 4 END >
         CASE health_state WHEN 'blocked' THEN 5 WHEN 'quarantined' THEN 4
              WHEN 'throttled' THEN 3 WHEN 'watch' THEN 2 ELSE 1 END
         OR (cloud_health_state = health_state AND cloud_blocked_until IS NULL));

UPDATE public.warmup_pool_participants p
SET health_state = l.health_state,
    blocked_at = l.blocked_at, blocked_until = l.blocked_until,
    blocked_reason = l.blocked_reason, last_health_reason = l.last_health_reason,
    last_health_score = l.last_health_score
FROM public.warmup_reputation_ledger l JOIN public.email_accounts a
    ON a.organization_id = l.organization_id AND lower(btrim(a.email)) = l.email
WHERE p.email_account_id = a.id AND l.cloud_health_state IS NOT NULL
    AND (l.cloud_blocked_until IS NULL OR l.cloud_blocked_until > now());

ALTER TABLE public.cloud_link_mailboxes
    DROP CONSTRAINT cloud_link_mailboxes_enrollment_state_check,
    DROP COLUMN standing_observed_at,
    DROP COLUMN enrollment_state;
ALTER TABLE public.cloud_link DROP COLUMN disconnect_pending;

ALTER TABLE public.warmup_reputation_ledger
    DROP CONSTRAINT warmup_reputation_ledger_cloud_state_check,
    DROP COLUMN cloud_health_state,
    DROP COLUMN cloud_blocked_until,
    DROP COLUMN cloud_health_reason,
    DROP COLUMN cloud_health_score,
    DROP COLUMN cloud_source_account_id,
    DROP COLUMN cloud_source_instance_id;

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
        DELETE FROM public.warmup_reputation_ledger WHERE organization_id = v_org AND email = v_email;
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
