-- The free plan is the connect-and-see-it-work tier: mailboxes, sync and a
-- couple of small campaigns. AI is a paid feature, so the allowance goes to
-- zero and the trial grant in internal/app/trial stops firing (it only grants
-- when monthly_credits > 0).
--
-- Existing balances are deliberately left alone. Someone who already received
-- the allowance keeps what they have; taking credits back from an account that
-- was given them is a worse surprise than the inconsistency.
UPDATE plans
SET monthly_credits = 0,
    updated_at      = NOW()
WHERE id = '00000000-0000-0000-0000-000000000001'
  AND monthly_credits <> 0;
