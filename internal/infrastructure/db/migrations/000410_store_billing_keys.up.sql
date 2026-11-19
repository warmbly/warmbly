-- Ties every store refund to the charge it gives back, so a month, a renewal or an
-- order is refunded once per charge and never past what it took. Additive only.

-- The ledger key and amount of the charge that collected the month ending at
-- paid_through, and the amount reserved with pending_charge_key.
ALTER TABLE public.store_mailboxes
    ADD COLUMN IF NOT EXISTS paid_key text,
    ADD COLUMN IF NOT EXISTS paid_micros bigint NOT NULL DEFAULT 0 CHECK (paid_micros >= 0),
    ADD COLUMN IF NOT EXISTS pending_charge_micros bigint NOT NULL DEFAULT 0 CHECK (pending_charge_micros >= 0);

-- A renewal charged and not yet renewed or refunded, reconciled whatever auto_renew says.
ALTER TABLE public.store_domains
    ADD COLUMN IF NOT EXISTS renewal_charge_key text;
CREATE INDEX IF NOT EXISTS idx_store_domains_renewal_charge ON public.store_domains (expires_at)
    WHERE renewal_charge_key IS NOT NULL;

-- An order whose refund did not go through yet; the reconciler retries it.
ALTER TABLE public.store_orders
    ADD COLUMN IF NOT EXISTS refund_pending boolean NOT NULL DEFAULT false;
CREATE INDEX IF NOT EXISTS idx_store_orders_refund_pending ON public.store_orders (updated_at)
    WHERE refund_pending;

-- The quote an expired quote was priced again as, so one quote yields at most one order.
ALTER TABLE public.store_quotes
    ADD COLUMN IF NOT EXISTS replaced_by uuid;
