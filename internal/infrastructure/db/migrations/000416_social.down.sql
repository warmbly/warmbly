-- Refuse rather than discard social seats workspaces paid for.
LOCK TABLE public.social_accounts, public.social_connect_sessions IN ACCESS EXCLUSIVE MODE;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM public.social_accounts WHERE removed_at IS NULL)
       OR EXISTS (SELECT 1 FROM public.social_connect_sessions WHERE charged_micros > 0 AND refunded_at IS NULL AND account_id IS NULL) THEN
        RAISE EXCEPTION 'social tables hold connected or charged seats; they cannot be dropped by a rollback';
    END IF;
END $$;

DROP TABLE IF EXISTS public.contact_social_identities;
DROP TABLE IF EXISTS public.linkedin_saved_searches;
DROP TABLE IF EXISTS public.social_messages;
DROP TABLE IF EXISTS public.social_conversations;
DROP TABLE IF EXISTS public.social_connect_sessions;
DROP TABLE IF EXISTS public.social_accounts;
DROP TABLE IF EXISTS public.social_workspaces;
