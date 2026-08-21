-- Make the text of a message searchable, not just its subject and preview.
--
-- unibox_emails.search_tsv is generated from subject and snippet, and a snippet
-- is a truncated one-line preview. Searching the unibox for a phrase that
-- appears in the third paragraph of an email therefore returned nothing, which
-- reads as "search is broken" rather than "search only covers the first line".
--
-- Message bodies live in object storage by design (they are large, and the
-- worker writes them there directly), so the searchable text is a bounded
-- plain-text rendering kept alongside the row: enough to find a message by
-- what it says, without turning Postgres into the body store.
ALTER TABLE unibox_emails
    ADD COLUMN body_text text NOT NULL DEFAULT '';

COMMENT ON COLUMN unibox_emails.body_text IS
    'Bounded plain-text rendering of the message body, for full-text search only. The full body lives in object storage.';

-- An expression index rather than a second generated column: adding a stored
-- generated column rewrites the whole table, while this builds against a column
-- that is empty on every existing row and so costs almost nothing here. The
-- query must use this exact expression to match the index.
CREATE INDEX idx_unibox_emails_body_search
    ON unibox_emails USING gin (to_tsvector('english'::regconfig, body_text));
