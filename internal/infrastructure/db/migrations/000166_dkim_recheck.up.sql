-- A DKIM key sits at a selector its owner chose, and DNS offers no way to list
-- the selectors under a domain. The check probed nine generic names, stored the
-- miss as auth_dkim = false, and every surface then reported it as a MISSING
-- record: the mailbox drawer drew a red "Missing" row, the summary said
-- "missing or unverifiable: DKIM", and the Advisor raised a card telling the
-- owner to publish a record that was already published. Every self-hoster whose
-- provider uses any other selector saw it on all of their mailboxes at once.
--
-- The probe now derives selectors from the domain's own SPF and MX records
-- (which name the provider, whose selector is fixed) on top of a wider default
-- set, and a miss is reported as unverified rather than missing.
--
-- Stored verdicts predate both halves, so put every domain that has not proven
-- DKIM back at the head of the sweep's "auth_checked_at NULLS FIRST" scan and
-- let it re-derive. Nothing else is touched: auth_state is unaffected by DKIM
-- either way, so no mailbox's send gate moves because of this.
UPDATE email_accounts
SET auth_checked_at = NULL
WHERE auth_dkim = false;

COMMENT ON COLUMN email_accounts.auth_dkim IS
    'True only when a DKIM key was positively found at a probed selector. False means unverified, NOT missing: selectors are not discoverable from DNS. It never gates sending.';
