-- Exempt one account from the emailed login code.
--
-- AUTH_LOGIN_CODE is instance-wide and boot-only, so the only way to let a
-- vendor's reviewer sign in (Google's OAuth verification, a security
-- questionnaire, a pen test) was to turn codes off for everyone, for weeks.
-- This narrows that to the one account that needs it.
--
-- Who, why and when, like organizations.risk_override_* and
-- subscriptions.managed_*: an exemption nobody can explain is the thing this
-- is meant to avoid creating.
ALTER TABLE public.users
    ADD COLUMN login_code_exempt        boolean NOT NULL DEFAULT false,
    ADD COLUMN login_code_exempt_reason text,
    ADD COLUMN login_code_exempt_by     uuid REFERENCES public.users(id) ON DELETE SET NULL,
    ADD COLUMN login_code_exempt_at     timestamptz;

-- An exemption without a reason is not answerable later, so it is refused.
ALTER TABLE public.users
    ADD CONSTRAINT users_login_code_exempt_reason_check
    CHECK (
        login_code_exempt = false
        OR (login_code_exempt_reason IS NOT NULL AND btrim(login_code_exempt_reason) <> '')
    );

-- The instance check lists exempt accounts on every run, and a partial index
-- keeps that a lookup rather than a scan of every user on the instance.
CREATE INDEX idx_users_login_code_exempt
    ON public.users (id)
    WHERE login_code_exempt;
