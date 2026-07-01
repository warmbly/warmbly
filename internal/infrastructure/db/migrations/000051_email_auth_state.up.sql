-- Persist each mailbox's sending-domain authentication (SPF/DKIM/DMARC) so the
-- platform can surface unauthenticated domains and warn on them in the
-- background, instead of only checking on demand from the dashboard.
--
-- Observe-only for now: the state is recorded and shown, it is NOT yet a hard
-- send/warmup gate. 'unknown' deliberately distinguishes "not checked yet or the
-- DNS lookup failed transiently" from a real 'failing', so a resolver hiccup
-- never looks like a misconfiguration.
ALTER TABLE email_accounts
    ADD COLUMN IF NOT EXISTS auth_state text NOT NULL DEFAULT 'unknown'
        CHECK (auth_state IN ('unknown', 'passing', 'failing')),
    ADD COLUMN IF NOT EXISTS auth_spf boolean NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS auth_dkim boolean NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS auth_dmarc boolean NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS auth_dmarc_policy text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS auth_reason text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS auth_checked_at timestamptz;

-- The sweep claims the oldest-checked active mailboxes first; this index keeps
-- that ordered scan cheap as the table grows.
CREATE INDEX IF NOT EXISTS idx_email_accounts_auth_checked_at
    ON email_accounts (auth_checked_at NULLS FIRST)
    WHERE status = 'active';
