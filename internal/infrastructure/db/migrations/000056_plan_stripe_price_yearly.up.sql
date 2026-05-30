-- Add a yearly Stripe price to plans so a single plan tier can be billed
-- either monthly (stripe_price_id) or annually (stripe_price_id_yearly).
-- The interval the customer picks in billing decides which price is used at
-- checkout; the existing duration column still records the row's primary cadence.
ALTER TABLE plans ADD COLUMN stripe_price_id_yearly VARCHAR(255);

CREATE INDEX idx_plans_stripe_price_yearly ON plans(stripe_price_id_yearly);
