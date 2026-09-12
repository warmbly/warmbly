-- Reverses 000148 by restoring the allowance 000080 originally inserted.
--
-- One case does not round-trip: a free plan that was ALREADY at zero before
-- 000148 ran comes back as 50, because the up migration recorded no prior
-- value to restore. It is guarded as tightly as it can be without keeping
-- migration bookkeeping in a product table — scoped to the one plan id, and
-- only when the value is still the zero the up migration would have left —
-- but a zero it did not write is indistinguishable from one it did.
--
-- Affects one row of plan configuration that an operator can edit in the
-- admin panel, and the practical posture is forward-only (see the upgrade
-- notes in the deployment guide), so the exposure is a rollback immediately
-- followed by setting the value back.
UPDATE plans
SET monthly_credits = 50,
    updated_at      = NOW()
WHERE id = '00000000-0000-0000-0000-000000000001'
  AND monthly_credits = 0;
