-- Reply classification: persist the layered classifier verdict for each inbound
-- reply on the per-(campaign, contact, step) progress row. This is what reply
-- branching ("reply_positive" / "reply_negative" / ...) reads at schedule time,
-- and what lets stop_on_reply / the plain "replied" condition IGNORE automated
-- replies (auto_reply / out_of_office) instead of treating them as a human reply.
--
-- reply_class    enum string: positive | negative | neutral | auto_reply |
--                out_of_office | unsubscribe | unknown ('' / NULL when unset).
-- reply_confidence  classifier confidence in [0,1].
-- reply_source   which layer decided: header | lexicon | model | '' (unset).
--
-- Kept as typed text columns (not jsonb) because they are queried/filtered in
-- SQL by the branch evaluator and the OOO-aware reply checks.
ALTER TABLE campaign_contact_progress
    ADD COLUMN IF NOT EXISTS reply_class text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS reply_confidence real NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS reply_source text NOT NULL DEFAULT '';

-- Discriminator guards so a bad write can't poison branch routing.
ALTER TABLE campaign_contact_progress
    ADD CONSTRAINT campaign_contact_progress_reply_class_chk
    CHECK (reply_class IN ('', 'positive', 'negative', 'neutral', 'auto_reply', 'out_of_office', 'unsubscribe', 'unknown'));

ALTER TABLE campaign_contact_progress
    ADD CONSTRAINT campaign_contact_progress_reply_source_chk
    CHECK (reply_source IN ('', 'header', 'lexicon', 'model'));

-- Partial index for the reply-branch evaluator, which only ever reads rows that
-- carry a non-empty class.
CREATE INDEX IF NOT EXISTS idx_campaign_progress_reply_class
    ON campaign_contact_progress (campaign_id, contact_id)
    WHERE reply_class <> '';
