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

ALTER TABLE campaign_leads
    ADD CONSTRAINT campaign_leads_source_check
    CHECK (source IN ('manual', 'segment'));
