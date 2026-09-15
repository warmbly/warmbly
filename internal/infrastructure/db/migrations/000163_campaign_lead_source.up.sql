-- Where a lead came from, so detaching a linked segment can take back the
-- audience it brought without touching a lead somebody chose by hand.
--
-- Before this, linking segment A, changing your mind and linking segment B
-- left the campaign holding A ∪ B: the links are a live audience but detaching
-- one only ever stopped future enrolment, so the wrong list stayed and got
-- emailed (issue #510).
--
-- 'manual' is the default, which is also the grandfathering: a lead that
-- predates this column was written by a path we cannot re-derive, and the safe
-- reading of an unknown provenance is that a person meant it. Only the
-- automatic enrolment of a linked segment writes 'segment', and only that is
-- ever withdrawn automatically.
ALTER TABLE campaign_leads
    ADD COLUMN source text NOT NULL DEFAULT 'manual';

-- NOT VALID, and deliberately never validated: a NOT VALID check is still
-- enforced on every insert and update, which is the whole job of a
-- discriminator constraint, and the column is one statement old, so every
-- existing row already holds the default it allows. Validating would scan the
-- largest table in the product under ACCESS EXCLUSIVE to learn that. Splitting
-- the validation into the same file would not help either: golang-migrate runs
-- one file in one transaction, so the lock is held across both statements.
ALTER TABLE campaign_leads
    ADD CONSTRAINT campaign_leads_source_check
    CHECK (source IN ('manual', 'segment')) NOT VALID;
