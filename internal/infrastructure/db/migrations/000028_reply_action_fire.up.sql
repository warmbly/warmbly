-- Instant engagement-triggered action chains: when an inbound signal lands for a
-- contact (a reply is classified, or an open / click is tracked), a matching
-- INSTANT branch on the contact's current step fires that branch's action chain
-- IMMEDIATELY (targeted at that one contact) instead of waiting for the contact's
-- next scheduled step boundary.
--
-- instant_fired is the exactly-once idempotency gate for those instant fires,
-- keyed PER EVENT KIND. A contact can open AND click AND reply on the same step,
-- and each distinct instant branch should fire exactly once, so a single
-- "already fired" boolean is not enough. Instead we record which event kinds
-- ("reply" / "open" / "click") have already fired their chain for this (campaign,
-- contact, step) progress row. The firing path claims a kind with an atomic
-- `array_append(...) WHERE NOT ($kind = ANY(instant_fired))` UPDATE, so a
-- redelivered / duplicated event of the same kind (or an auto-reply followed by a
-- human reply on the same step) can never run that kind's chain twice, while a
-- different kind on the same step is still free to fire once. Defaults to '{}'
-- (nothing fired yet) and is NOT NULL so the array operators never see NULL.
ALTER TABLE campaign_contact_progress
    ADD COLUMN IF NOT EXISTS instant_fired text[] NOT NULL DEFAULT '{}';
