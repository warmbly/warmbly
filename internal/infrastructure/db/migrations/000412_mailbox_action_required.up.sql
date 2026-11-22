-- The step a purchased mailbox waits on before it can connect, one only a
-- person can take: today, the Microsoft 365 tenant's administrator approving
-- this instance's app. NULL is nothing to do. A reader that predates a value
-- must handle it before anything writes it, so the CHECK lists only known ones.
ALTER TABLE public.partner_mailboxes
    ADD COLUMN action_required text CHECK (action_required IN ('microsoft_consent'));

ALTER TABLE public.store_mailboxes
    ADD COLUMN action_required text CHECK (action_required IN ('microsoft_consent'));

-- Why a store mailbox's step fell to a person: what stopped the automatic
-- approval with the domain's admin mailbox. NULL while it has not been tried.
ALTER TABLE public.store_mailboxes
    ADD COLUMN action_reason text CHECK (action_reason IN ('mfa_required', 'wrong_password', 'not_admin',
        'wrong_tenant', 'sign_in_blocked', 'no_admin_sign_in', 'failed'));
