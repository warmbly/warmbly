-- Compose needs "conversations with this address" lookups that match either
-- side of the exchange. from_addr already has a GIN index; mirror it on
-- to_addr so the address filter and mailbox-affinity scoring stay indexed.
CREATE INDEX IF NOT EXISTS idx_unibox_emails_to ON public.unibox_emails USING gin (to_addr);
