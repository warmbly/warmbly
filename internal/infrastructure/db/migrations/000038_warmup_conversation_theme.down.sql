DROP INDEX IF EXISTS idx_warmup_tokens_sender_recipient;
ALTER TABLE warmup_tokens DROP COLUMN IF EXISTS conversation_theme;
