-- Refuse rather than discard LinkedIn seats workspaces paid for.
LOCK TABLE public.linkedin_accounts, public.linkedin_connect_sessions IN ACCESS EXCLUSIVE MODE;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM public.linkedin_accounts WHERE removed_at IS NULL)
       OR EXISTS (SELECT 1 FROM public.linkedin_connect_sessions WHERE charged_micros > 0 AND refunded_at IS NULL AND account_id IS NULL) THEN
        RAISE EXCEPTION 'linkedin tables hold connected or charged seats; they cannot be dropped by a rollback';
    END IF;
END $$;

DROP TABLE IF EXISTS public.linkedin_saved_searches;
DROP TABLE IF EXISTS public.linkedin_messages;
DROP TABLE IF EXISTS public.linkedin_conversations;
DROP TABLE IF EXISTS public.linkedin_connect_sessions;
DROP TABLE IF EXISTS public.linkedin_accounts;
DROP TABLE IF EXISTS public.linkedin_workspaces;
DROP TRIGGER IF EXISTS contacts_linkedin ON public.contacts;
DROP FUNCTION IF EXISTS public.contacts_linkedin();
DROP FUNCTION IF EXISTS public.linkedin_public_id(text);
ALTER TABLE public.contacts
    DROP COLUMN IF EXISTS linkedin_url,
    DROP COLUMN IF EXISTS linkedin_public_id;
