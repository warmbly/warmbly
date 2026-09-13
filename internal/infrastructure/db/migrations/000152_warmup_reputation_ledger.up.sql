-- A penalty follows the address, not the row. Everything warmup knows about a
-- mailbox (its pool row holding spam_score, health_state and blocked_until,
-- its statistics, spam reports and token attempts) cascades off
-- email_accounts, and the pool row alone dies on paths that never touch the
-- mailbox (leaving the pool on an auth error, a lapsed plan, warmup toggled
-- off). Every one of those handed the same address a clean standing on its
-- next join. This table is a mirror of the address's standing, and the trigger
-- below is the only writer, so no caller can bypass it: every write to a pool
-- row (the health sweep, a tampering block, an admin unblock, the seed on
-- join) keeps it current, and it is already current when the row dies.
--
-- The mirror holds the worst standing among the address's live pool rows
-- (two members of one workspace can each connect the same address) and is
-- deleted when that standing is healthy with no score, so it only ever holds
-- a penalty. standing_until is when the standing itself ends: blocked_until
-- for a timed block, the time of writing for anything else, NULL for a block
-- that requires review and never lapses. The retention window on top of that
-- lives in Go (config.WarmupReputationLedgerDays) and is applied by the purge,
-- which also never removes a row while a live pool row still backs it.
-- recorded_at is bumped when a mailbox leaves the pool or is removed, so the
-- window is counted from the later of the standing's end and the removal.
--
-- warmupRepository.MoveToPool seeds a new pool row from it when the same
-- address rejoins in the same workspace, and never consumes it: the seed is
-- itself a write, and the trigger re-mirrors it.
CREATE TABLE public.warmup_reputation_ledger (
    organization_id    uuid NOT NULL REFERENCES public.organizations(id) ON DELETE CASCADE,
    email              text NOT NULL,
    spam_score         integer NOT NULL DEFAULT 0,
    health_state       text NOT NULL DEFAULT 'healthy',
    blocked_at         timestamptz,
    blocked_until      timestamptz,
    blocked_reason     text,
    last_health_score  double precision NOT NULL DEFAULT 0,
    last_health_reason text,
    recorded_at        timestamptz NOT NULL DEFAULT now(),
    standing_until     timestamptz,
    PRIMARY KEY (organization_id, email),
    CONSTRAINT warmup_reputation_ledger_email_normalized CHECK (email = lower(btrim(email))),
    CONSTRAINT warmup_reputation_ledger_health_state_check
        CHECK (health_state IN ('healthy', 'watch', 'throttled', 'quarantined', 'blocked')),
    CONSTRAINT warmup_reputation_ledger_spam_score_check CHECK (spam_score >= 0 AND spam_score <= 100)
);

CREATE INDEX warmup_reputation_ledger_standing_until ON public.warmup_reputation_ledger (standing_until);

CREATE FUNCTION public.warmup_reputation_mirror() RETURNS trigger
    LANGUAGE plpgsql
AS $$
DECLARE
    v_org   uuid;
    v_email text;
    worst   record;
    v_score integer;
BEGIN
    -- The organization join matters: when a workspace is deleted its mailboxes
    -- cascade while the organization row is already gone, and a mirror row
    -- written then would violate its own foreign key and abort the deletion.
    SELECT a.organization_id, lower(btrim(a.email))
      INTO v_org, v_email
      FROM public.email_accounts a
      JOIN public.organizations o ON o.id = a.organization_id
     WHERE a.id = NEW.email_account_id;
    IF v_org IS NULL THEN
        RETURN NULL;
    END IF;

    SELECT p.health_state, p.blocked_at, p.blocked_until, p.blocked_reason,
           p.last_health_score, p.last_health_reason
      INTO worst
      FROM public.warmup_pool_participants p
      JOIN public.email_accounts a ON a.id = p.email_account_id
     WHERE a.organization_id = v_org AND lower(btrim(a.email)) = v_email
     ORDER BY CASE p.health_state
                  WHEN 'blocked' THEN 5
                  WHEN 'quarantined' THEN 4
                  WHEN 'throttled' THEN 3
                  WHEN 'watch' THEN 2
                  WHEN 'healthy' THEN 1
                  ELSE 0
              END DESC,
              (p.health_state = 'blocked' AND p.blocked_until IS NULL) DESC,
              p.blocked_until DESC NULLS LAST
     LIMIT 1;

    SELECT COALESCE(MAX(p.spam_score), 0)
      INTO v_score
      FROM public.warmup_pool_participants p
      JOIN public.email_accounts a ON a.id = p.email_account_id
     WHERE a.organization_id = v_org AND lower(btrim(a.email)) = v_email;

    IF worst.health_state IS NULL OR (worst.health_state = 'healthy' AND v_score = 0) THEN
        DELETE FROM public.warmup_reputation_ledger
         WHERE organization_id = v_org AND email = v_email;
        RETURN NULL;
    END IF;

    INSERT INTO public.warmup_reputation_ledger
        (organization_id, email, spam_score, health_state, blocked_at, blocked_until, blocked_reason,
         last_health_score, last_health_reason, recorded_at, standing_until)
    VALUES
        (v_org, v_email, v_score, worst.health_state, worst.blocked_at, worst.blocked_until, worst.blocked_reason,
         worst.last_health_score, worst.last_health_reason, now(),
         CASE WHEN worst.health_state = 'blocked' AND worst.blocked_until IS NULL THEN NULL
              ELSE GREATEST(COALESCE(worst.blocked_until, now()), now())
         END)
    ON CONFLICT (organization_id, email) DO UPDATE SET
        spam_score         = EXCLUDED.spam_score,
        health_state       = EXCLUDED.health_state,
        blocked_at         = EXCLUDED.blocked_at,
        blocked_until      = EXCLUDED.blocked_until,
        blocked_reason     = EXCLUDED.blocked_reason,
        last_health_score  = EXCLUDED.last_health_score,
        last_health_reason = EXCLUDED.last_health_reason,
        recorded_at        = EXCLUDED.recorded_at,
        standing_until     = EXCLUDED.standing_until;
    RETURN NULL;
END;
$$;

CREATE TRIGGER warmup_reputation_mirror
    AFTER INSERT OR UPDATE ON public.warmup_pool_participants
    FOR EACH ROW EXECUTE FUNCTION public.warmup_reputation_mirror();

-- Backfill: a no-op update of every penalised pool row runs the mirror once,
-- so a mailbox blocked before this migration is covered from the first sweep.
UPDATE public.warmup_pool_participants
   SET health_state = health_state
 WHERE spam_score > 0 OR health_state <> 'healthy';
