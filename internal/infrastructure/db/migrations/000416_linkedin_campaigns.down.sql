ALTER TABLE public.linkedin_messages DROP COLUMN IF EXISTS reply_handled_at;
DROP TABLE IF EXISTS public.linkedin_imports;
DROP TABLE IF EXISTS public.linkedin_contact_state;
DROP TABLE IF EXISTS public.linkedin_actions;
DROP TABLE IF EXISTS public.campaign_linkedin_accounts;
