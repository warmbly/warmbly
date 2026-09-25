-- The campaign a verdict was made with, so a re-check asks again only about
-- verdicts made without one. A constant default adds the column without a rewrite.
ALTER TABLE inbox_tag_results
    ADD COLUMN IF NOT EXISTS campaign text NOT NULL DEFAULT '';
