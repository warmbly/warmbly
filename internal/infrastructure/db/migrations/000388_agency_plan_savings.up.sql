-- Agency plans are monthly plan rows like every other paid plan, whose savings is 0;
-- the yearly price is in price_yearly. Nothing reads the column for them.
UPDATE plans SET savings = 0
WHERE id IN ('00000000-0000-0000-0000-0000000000a1', '00000000-0000-0000-0000-0000000000a2') AND savings = 20;
