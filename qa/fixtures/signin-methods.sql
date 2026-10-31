-- The seeded owner as an account created through Apple and Google, for the isolated rich-seed QA database, never production.
BEGIN;
DO $$
BEGIN
  IF left(current_database(), 11) <> 'warmbly_qa_' THEN
    RAISE EXCEPTION 'sign-in methods proof fixtures require an isolated warmbly_qa_ database';
  END IF;
  IF NOT EXISTS (SELECT 1 FROM users WHERE id = '11111111-0000-0000-0000-000000000001') THEN
    RAISE EXCEPTION 'sign-in methods proof fixtures require the rich QA seed';
  END IF;
END $$;

-- No password and no passkey, so the providers are the only way in.
UPDATE users SET password_hash = NULL WHERE id = '11111111-0000-0000-0000-000000000001';
-- The product tour would cover the page.
UPDATE users SET product_tour_completed_at = now()
WHERE id = '11111111-0000-0000-0000-000000000001' AND product_tour_completed_at IS NULL;
DELETE FROM webauthn_credentials WHERE user_id = '11111111-0000-0000-0000-000000000001';

INSERT INTO user_identities (user_id, provider, issuer, subject, email, created_at, last_login_at)
VALUES
  ('11111111-0000-0000-0000-000000000001', 'apple', 'https://appleid.apple.com', 'qa-apple-owner',
   '[EMAIL]', now() - interval '40 days', now() - interval '2 days'),
  ('11111111-0000-0000-0000-000000000001', 'google', 'https://accounts.google.com', 'qa-google-owner',
   '[EMAIL]', now() - interval '12 days', now() - interval '12 days')
ON CONFLICT (issuer, subject) DO NOTHING;

-- Removing a provider needs a fresh confirmation; the saved session counts as one for five minutes.
UPDATE sessions SET reauth_at = now() WHERE user_id = '11111111-0000-0000-0000-000000000001';
COMMIT;
