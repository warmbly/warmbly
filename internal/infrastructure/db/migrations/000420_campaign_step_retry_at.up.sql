-- When a step that could not run for one contact is offered again; routing passes the contact over until then.
ALTER TABLE public.campaign_contact_progress ADD COLUMN IF NOT EXISTS retry_at timestamptz;
