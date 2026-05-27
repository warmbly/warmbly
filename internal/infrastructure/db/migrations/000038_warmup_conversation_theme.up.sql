-- Carry the conversation theme on each warmup send so reply bodies stay
-- topically coherent with the original. Previously the body for a reply
-- was picked from a random conversation, which produced strong
-- fingerprints ("Re: Quick question about time-blocking" answered with
-- a paragraph about travel).

ALTER TABLE warmup_tokens
    ADD COLUMN conversation_theme TEXT NOT NULL DEFAULT '';

CREATE INDEX idx_warmup_tokens_sender_recipient
    ON warmup_tokens (sender_account_id, recipient_account_id);
