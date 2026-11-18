-- Validates the pause_source CHECK 000401 re-added NOT VALID. A VALIDATE only
-- takes a SHARE UPDATE EXCLUSIVE lock, so writes continue while it scans.
ALTER TABLE public.campaign_leads VALIDATE CONSTRAINT campaign_leads_pause_source_check;
