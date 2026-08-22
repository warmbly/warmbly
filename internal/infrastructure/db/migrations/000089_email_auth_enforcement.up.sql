-- Turn the persisted SPF/DKIM/DMARC state (migration 000067) from an
-- observe-only badge into a real send/warmup gate.
--
-- The gate is deliberately not "auth_state = 'failing' means stop now".
-- auth_failing_since records when the domain ENTERED the failing state, and
-- enforcement only begins once it has been continuously failing for the
-- operator's grace window. That makes two whole classes of outage impossible
-- by construction: a resolver returning a spurious NXDOMAIN cannot stop a
-- customer's campaign within the hour, and an owner who breaks a record gets
-- warned days before anything stops sending.
--
-- The clock is cleared on 'passing' and PRESERVED on 'unknown'. Clearing it on
-- 'unknown' would let a genuinely broken domain dodge the gate forever by
-- flapping through a transient DNS error; the gate already keys on 'failing',
-- so 'unknown' is fail-open without needing the timestamp reset.
ALTER TABLE email_accounts
    ADD COLUMN IF NOT EXISTS auth_failing_since timestamptz;

COMMENT ON COLUMN email_accounts.auth_failing_since IS
    'When the sending domain entered auth_state=''failing''. NULL when passing or never failed. Enforcement starts only after the operator grace window has elapsed from this point.';

-- Existing failing rows start their grace window now, not retroactively, so
-- nobody is gated the instant this deploys.
UPDATE email_accounts
SET auth_failing_since = NOW()
WHERE auth_state = 'failing'
  AND auth_failing_since IS NULL;

-- The DMARC check now falls back to the organizational domain (RFC 7489
-- 6.6.3), so a dedicated sending subdomain covered by its parent's record is
-- no longer misread as missing DMARC. Every stored 'failing' verdict predates
-- that fix and has to be re-derived before it can gate anything.
--
-- Only 'failing' rows need it: the fallback can only ever turn DMARCFound
-- false -> true, so a 'passing' verdict cannot change. Clearing auth_checked_at
-- puts them at the head of the sweep's NULLS FIRST scan.
UPDATE email_accounts
SET auth_checked_at = NULL
WHERE auth_state = 'failing';

-- The warmup task parks a gated mailbox under its own status so an operator can
-- tell "skipped because the domain is unauthenticated" apart from the health
-- and access skips it would otherwise be lumped in with.
ALTER TYPE public.task_status ADD VALUE IF NOT EXISTS 'skipped_domain_auth';

-- The gate reads auth_state + auth_failing_since for every mailbox in a
-- campaign's sender pool on every scheduling pass, so keep the failing set
-- cheap to find.
CREATE INDEX IF NOT EXISTS idx_email_accounts_auth_failing
    ON email_accounts (auth_failing_since)
    WHERE auth_state = 'failing';
