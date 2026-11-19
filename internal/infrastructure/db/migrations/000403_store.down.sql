-- Refuse rather than discard what workspaces bought and paid for.
-- The locks hold off a purchase written between the check and the drop.
LOCK TABLE public.store_orders, public.store_domains, public.store_mailboxes IN ACCESS EXCLUSIVE MODE;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM public.store_orders)
       OR EXISTS (SELECT 1 FROM public.store_domains)
       OR EXISTS (SELECT 1 FROM public.store_mailboxes) THEN
        RAISE EXCEPTION 'store tables hold purchased domains or mailboxes; they cannot be dropped by a rollback';
    END IF;
END $$;

ALTER TABLE public.mailbox_domain_grants DROP COLUMN IF EXISTS purchased;
-- A store card charge reads as the top-up it settled as.
DROP INDEX IF EXISTS public.credit_auto_topup_attempts_charge_key;
UPDATE public.credit_auto_topup_attempts SET kind = 'top_up' WHERE kind = 'store';
ALTER TABLE public.credit_auto_topup_attempts DROP CONSTRAINT IF EXISTS credit_auto_topup_attempts_kind_check;
ALTER TABLE public.credit_auto_topup_attempts
    ADD CONSTRAINT credit_auto_topup_attempts_kind_check CHECK (kind IN ('auto_reload', 'top_up')),
    DROP COLUMN IF EXISTS charge_key,
    DROP COLUMN IF EXISTS description;
DROP TABLE IF EXISTS public.store_webhook_events;
DROP TABLE IF EXISTS public.store_mailboxes;
DROP TABLE IF EXISTS public.store_domains;
DROP TABLE IF EXISTS public.store_orders;
DROP TABLE IF EXISTS public.store_quotes;
DROP TABLE IF EXISTS public.store_accounts;
