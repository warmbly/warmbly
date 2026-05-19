-- Threat-level segregation.
--
-- Two new concepts:
--
--   workers.risk_pool — buckets shared workers by acceptable risk level.
--     'clean'      = healthy mailboxes only; the IPs we want to protect
--     'risky'      = mailboxes flagged by warmup health (watch / throttled)
--     'quarantine' = anything currently blocked or in active reputation
--                    recovery; these workers exist purely so bad mailboxes
--                    don't poison the clean pool. Operators can choose not
--                    to run a quarantine pool at all — having none just
--                    means flagged mailboxes get stopped, not sent.
--
--   email_accounts.risk_band — the per-mailbox classification that the
--     rebalancer matches against worker risk_pool. Derived from
--     warmup_health_state by the periodic job; never set directly by users.
--
-- Dedicated workers are exempt — they serve one customer, so cross-tenant
-- contamination isn't a concern, and the customer's mailboxes stay together
-- regardless of risk.

CREATE TYPE worker_risk_pool AS ENUM ('clean', 'risky', 'quarantine');
CREATE TYPE email_risk_band  AS ENUM ('clean', 'risky', 'quarantine');

ALTER TABLE workers
    ADD COLUMN risk_pool worker_risk_pool NOT NULL DEFAULT 'clean';

ALTER TABLE email_accounts
    ADD COLUMN risk_band       email_risk_band NOT NULL DEFAULT 'clean',
    ADD COLUMN risk_evaluated_at TIMESTAMPTZ;

CREATE INDEX idx_workers_risk_pool ON workers(risk_pool) WHERE worker_type = 'shared';
CREATE INDEX idx_email_accounts_risk_band ON email_accounts(risk_band) WHERE risk_band <> 'clean';
