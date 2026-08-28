DROP INDEX IF EXISTS public.idx_organizations_risk_state;

ALTER TABLE public.organizations
    DROP CONSTRAINT IF EXISTS organizations_risk_score_check,
    DROP CONSTRAINT IF EXISTS organizations_risk_state_check;

ALTER TABLE public.organizations
    DROP COLUMN IF EXISTS risk_evaluated_at,
    DROP COLUMN IF EXISTS risk_signals,
    DROP COLUMN IF EXISTS risk_reason,
    DROP COLUMN IF EXISTS risk_score,
    DROP COLUMN IF EXISTS risk_state;
