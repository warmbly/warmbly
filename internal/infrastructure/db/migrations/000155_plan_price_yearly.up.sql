-- plans has carried stripe_price_id_yearly since the yearly prices were added,
-- but never the yearly amount, so the number lived only in the dashboard's
-- static catalog and could not be read by anything server-side.
ALTER TABLE plans ADD COLUMN IF NOT EXISTS price_yearly NUMERIC;

-- The amounts already charged by the matching Stripe prices, so the column
-- agrees with what a customer is billed from the moment it exists.
UPDATE plans SET price_yearly = 276  WHERE name = 'Starter'          AND price_yearly IS NULL;
UPDATE plans SET price_yearly = 852  WHERE name = 'Grow'             AND price_yearly IS NULL;
UPDATE plans SET price_yearly = 3156 WHERE name = 'Business'         AND price_yearly IS NULL;
UPDATE plans SET price_yearly = 144  WHERE id = '00000000-0000-0000-0000-000000000002' AND price_yearly IS NULL;
