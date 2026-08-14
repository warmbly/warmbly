-- Persist extra-cli reachability on candidates so CollectToday re-plan
-- cannot invent R1/VALIDATED after a process restart.
ALTER TABLE outreach_contact_candidates
    ADD COLUMN IF NOT EXISTS reachability_class text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS route_type text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS route_relation text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS channel_value text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS channel_display text NOT NULL DEFAULT '';

COMMENT ON COLUMN outreach_contact_candidates.reachability_class IS
'Imported extra-cli reachability token. Empty means current contact-tier contract; Warmbly never invents a class.';
COMMENT ON COLUMN outreach_contact_candidates.route_relation IS
'Imported route relation (BELONGS_TO_NAMED_PERSON, ROUTES_TO_NAMED_PERSON, ROLE_MAILBOX, CORPORATE_GENERIC).';
