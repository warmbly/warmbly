-- A penalty follows the address, not the row. Everything warmup knows about a
-- mailbox (its pool row with spam_score, health_state and blocked_until, its
-- statistics, spam reports and invalid-token attempts) cascades off
-- email_accounts, so removing a blocked mailbox and adding it back gave the
-- same address a clean standing. This holds the standing between the two rows.
--
-- Written by emailRepository.Delete inside the delete's own transaction, only
-- when there is a penalty to carry; a mailbox in good standing leaves no row.
-- Consumed by warmupRepository.MoveToPool when the same address rejoins a pool
-- in the same workspace. Rows outlive their use by a fixed window (see
-- config.WarmupReputationLedgerDays) and are purged by the health sweep, so
-- the address of a removed mailbox is not kept indefinitely.
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
    removed_at         timestamptz NOT NULL DEFAULT now(),
    expires_at         timestamptz NOT NULL,
    PRIMARY KEY (organization_id, email),
    CONSTRAINT warmup_reputation_ledger_email_normalized CHECK (email = lower(btrim(email))),
    CONSTRAINT warmup_reputation_ledger_health_state_check
        CHECK (health_state IN ('healthy', 'watch', 'throttled', 'quarantined', 'blocked')),
    CONSTRAINT warmup_reputation_ledger_spam_score_check CHECK (spam_score >= 0 AND spam_score <= 100)
);

CREATE INDEX warmup_reputation_ledger_expires_at ON public.warmup_reputation_ledger (expires_at);
