ALTER TABLE public.store_quotes DROP COLUMN IF EXISTS replaced_by;
DROP INDEX IF EXISTS public.idx_store_orders_refund_pending;
ALTER TABLE public.store_orders DROP COLUMN IF EXISTS refund_pending;
DROP INDEX IF EXISTS public.idx_store_domains_renewal_charge;
ALTER TABLE public.store_domains DROP COLUMN IF EXISTS renewal_charge_key;
ALTER TABLE public.store_mailboxes
    DROP COLUMN IF EXISTS pending_charge_micros,
    DROP COLUMN IF EXISTS paid_micros,
    DROP COLUMN IF EXISTS paid_key;
