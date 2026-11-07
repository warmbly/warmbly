-- A referral discount is redeemable only while its referral code exists, so
-- removing the code (directly or with its owner) disables the discount.
CREATE OR REPLACE FUNCTION disable_referral_discount() RETURNS trigger AS $$
BEGIN
    IF OLD.discount_code_id IS NOT NULL THEN
        UPDATE discount_codes SET status = 'disabled', updated_at = now()
         WHERE id = OLD.discount_code_id AND status = 'active';
    END IF;
    RETURN OLD;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS referral_codes_disable_discount ON referral_codes;
CREATE TRIGGER referral_codes_disable_discount
    AFTER DELETE ON referral_codes
    FOR EACH ROW EXECUTE FUNCTION disable_referral_discount();

-- Referral discounts whose code is already gone.
UPDATE discount_codes dc SET status = 'disabled', updated_at = now()
 WHERE dc.status = 'active'
   AND dc.description = 'Referral reward: 10% off for 3 months'
   AND NOT EXISTS (SELECT 1 FROM referral_codes rc WHERE rc.discount_code_id = dc.id);
