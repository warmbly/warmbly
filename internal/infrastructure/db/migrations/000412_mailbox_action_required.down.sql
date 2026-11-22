ALTER TABLE public.store_mailboxes DROP COLUMN IF EXISTS action_reason;
ALTER TABLE public.store_mailboxes DROP COLUMN IF EXISTS action_required;
ALTER TABLE public.partner_mailboxes DROP COLUMN IF EXISTS action_required;
