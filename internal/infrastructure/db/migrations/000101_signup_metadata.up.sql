-- Signup-time risk metadata (issue #142).
--
-- RegistrationStart already received the source address and discarded it after
-- the CAPTCHA check, so nothing downstream could correlate accounts by where
-- they were opened from or notice a throwaway address.
--
-- signup_email_normalized collapses plus-tags and Gmail dots to the identity
-- behind the address, so one person opening several accounts that look
-- unrelated is visible. It is for correlation only and never for login.
ALTER TABLE public.users
    ADD COLUMN signup_ip inet,
    ADD COLUMN signup_user_agent text,
    ADD COLUMN signup_email_risk integer NOT NULL DEFAULT 0,
    ADD COLUMN signup_email_normalized text;

ALTER TABLE public.users
    ADD CONSTRAINT users_signup_email_risk_check
    CHECK (signup_email_risk >= 0 AND signup_email_risk <= 100);

-- The velocity and clustering queries (#148) group by these.
CREATE INDEX idx_users_signup_ip ON public.users USING btree (signup_ip) WHERE signup_ip IS NOT NULL;
CREATE INDEX idx_users_signup_email_normalized
    ON public.users USING btree (signup_email_normalized)
    WHERE signup_email_normalized IS NOT NULL;

COMMENT ON COLUMN public.users.signup_email_normalized IS
    'Plus-tags and Gmail dots collapsed, for correlating accounts opened by one person. Never used for authentication.';
