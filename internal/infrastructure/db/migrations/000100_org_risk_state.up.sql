-- Per-organization risk state (issue #141).
--
-- Every abuse control was siloed: rate limits per user, warmup health per
-- mailbox, bans per user. Nothing fused them, so an actor weak on several axes
-- at once (fresh disposable signup, scraped list, mailboxes landing in junk)
-- never escalated as one subject. This is that subject.
--
-- Deliberately modelled on the warmup participant health machine, which has
-- worked: a band, a score, a reason, and the evidence behind it.
ALTER TABLE public.organizations
    ADD COLUMN risk_state text NOT NULL DEFAULT 'trusted',
    ADD COLUMN risk_score integer NOT NULL DEFAULT 0,
    ADD COLUMN risk_reason text,
    -- signals is append-only evidence for admin review, never the source of
    -- the verdict: the state and score are.
    ADD COLUMN risk_signals jsonb NOT NULL DEFAULT '{}'::jsonb,
    ADD COLUMN risk_evaluated_at timestamptz;

ALTER TABLE public.organizations
    ADD CONSTRAINT organizations_risk_state_check
    CHECK (risk_state IN ('trusted', 'watch', 'restricted', 'suspended'));

ALTER TABLE public.organizations
    ADD CONSTRAINT organizations_risk_score_check
    CHECK (risk_score >= 0 AND risk_score <= 100);

-- The rebalancer and the admin list both filter by band.
CREATE INDEX idx_organizations_risk_state
    ON public.organizations USING btree (risk_state)
    WHERE risk_state <> 'trusted';

COMMENT ON COLUMN public.organizations.risk_state IS
    'Fused abuse posture: trusted | watch | restricted | suspended. restricted lowers send caps and forces the free warmup pool; suspended stops sending.';
