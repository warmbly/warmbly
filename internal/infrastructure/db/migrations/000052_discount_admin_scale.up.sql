-- The admin discount-code manager lists codes ordered by
-- (created_at DESC, id DESC) by default. Without a matching index a large
-- discount_codes table falls back to a seqscan + sort on every page load,
-- which gets slow once an org runs many named influencer/promo codes.
CREATE INDEX IF NOT EXISTS idx_discount_codes_created_id
    ON public.discount_codes (created_at DESC, id DESC);
