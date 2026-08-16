-- Make "no timezone chosen" representable on a mailbox.
--
-- The campaign scheduler already gates its mailbox business-hours check on
-- `acct.Timezone != ""`, so an empty timezone was always meant to mean "not
-- configured, do not apply a second window on top of the campaign's own". The
-- column default defeated that: it was 'UTC', which the scheduler cannot tell
-- apart from a deliberate choice of UTC.
--
-- The effect was that a freshly connected mailbox looked like it had been
-- placed in UTC on purpose, so it was compared against the campaign timezone
-- (default 'Europe/London') and dropped whenever the current UTC hour was
-- outside 08:00-20:00. With one mailbox that empties the candidate pool and the
-- campaign refuses to start.
ALTER TABLE email_accounts ALTER COLUMN timezone SET DEFAULT '';

-- Existing rows holding 'UTC' are the old default, not a choice: until this
-- migration there was no API field, dashboard control or onboarding path that
-- could set email_accounts.timezone at all. The only writer was this default
-- and the seed fixtures, which set explicit non-UTC zones.
--
-- Rows deliberately set to UTC from here on are preserved: this runs once.
UPDATE email_accounts SET timezone = '' WHERE timezone = 'UTC';
