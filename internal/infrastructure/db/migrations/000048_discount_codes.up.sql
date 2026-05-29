-- Discount / promo codes. Codes are validated and managed entirely in our
-- database (the source of truth); at redemption time the billing layer mints a
-- one-off Stripe coupon (for money discounts) or sets trial days (for trial
-- extensions) and attaches it to the checkout session / subscription. We do not
-- pre-create Stripe coupons or promotion codes at admin-create time.
--
-- A code carries exactly one discount kind keyed on `type`:
--   percent          -> percent_off (1..100)
--   fixed            -> amount_off + currency
--   trial_extension  -> trial_extension_days (extra free-trial days at checkout)
--
-- `duration` mirrors Stripe coupon duration for money codes (once / repeating
-- N months / forever). Plan eligibility is many-to-many via discount_code_plans;
-- a code applies to every plan when applies_to_all_plans = true and there are no
-- rows in discount_code_plans.

CREATE TYPE discount_type AS ENUM ('percent', 'fixed', 'trial_extension');
CREATE TYPE discount_duration AS ENUM ('once', 'repeating', 'forever');
CREATE TYPE discount_code_status AS ENUM ('active', 'disabled', 'expired');
CREATE TYPE discount_redemption_status AS ENUM ('pending', 'applied', 'canceled');

CREATE TABLE discount_codes (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    code VARCHAR(64) NOT NULL UNIQUE,
    description TEXT NOT NULL DEFAULT '',

    type discount_type NOT NULL,
    percent_off SMALLINT,
    amount_off DECIMAL(10, 2),
    currency VARCHAR(3),
    trial_extension_days INT,

    duration discount_duration NOT NULL DEFAULT 'once',
    duration_in_months INT,

    max_redemptions INT,
    times_redeemed INT NOT NULL DEFAULT 0,
    per_account_limit INT NOT NULL DEFAULT 1,

    applies_to_all_plans BOOLEAN NOT NULL DEFAULT true,

    status discount_code_status NOT NULL DEFAULT 'active',
    starts_at TIMESTAMPTZ,
    expires_at TIMESTAMPTZ,

    created_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT discount_value_matches_type CHECK (
        (type = 'percent' AND percent_off IS NOT NULL AND amount_off IS NULL AND trial_extension_days IS NULL)
        OR (type = 'fixed' AND amount_off IS NOT NULL AND currency IS NOT NULL AND percent_off IS NULL AND trial_extension_days IS NULL)
        OR (type = 'trial_extension' AND trial_extension_days IS NOT NULL AND percent_off IS NULL AND amount_off IS NULL)
    ),
    CONSTRAINT discount_percent_bounds CHECK (percent_off IS NULL OR (percent_off BETWEEN 1 AND 100)),
    CONSTRAINT discount_amount_positive CHECK (amount_off IS NULL OR amount_off > 0),
    CONSTRAINT discount_trial_positive CHECK (trial_extension_days IS NULL OR trial_extension_days > 0),
    CONSTRAINT discount_per_account_positive CHECK (per_account_limit >= 1),
    CONSTRAINT discount_max_redemptions_positive CHECK (max_redemptions IS NULL OR max_redemptions > 0),
    CONSTRAINT discount_repeating_months CHECK (duration <> 'repeating' OR duration_in_months IS NOT NULL)
);

CREATE INDEX idx_discount_codes_status ON discount_codes(status, created_at DESC);
CREATE INDEX idx_discount_codes_code ON discount_codes(code);

-- Plan eligibility. Presence of any row restricts the code to those plans.
CREATE TABLE discount_code_plans (
    discount_code_id UUID NOT NULL REFERENCES discount_codes(id) ON DELETE CASCADE,
    plan_id UUID NOT NULL REFERENCES plans(id) ON DELETE CASCADE,

    PRIMARY KEY (discount_code_id, plan_id)
);

CREATE INDEX idx_discount_code_plans_plan ON discount_code_plans(plan_id);

-- Redemptions. A row is created (pending) when a code-bearing checkout session is
-- built, then flipped to applied on the checkout.session.completed webhook (or
-- created directly as applied for a plan change). Caps are enforced by counting
-- pending+applied rows; times_redeemed denormalizes the applied count.
CREATE TABLE discount_redemptions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    discount_code_id UUID NOT NULL REFERENCES discount_codes(id) ON DELETE CASCADE,
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    redeemed_by UUID REFERENCES users(id) ON DELETE SET NULL,
    subscription_id UUID REFERENCES subscriptions(id) ON DELETE SET NULL,
    plan_id UUID REFERENCES plans(id) ON DELETE SET NULL,

    stripe_coupon_id VARCHAR(255),
    stripe_checkout_session_id VARCHAR(255) UNIQUE,

    -- snapshot of the discount at redemption time
    type discount_type NOT NULL,
    percent_off SMALLINT,
    amount_off DECIMAL(10, 2),
    currency VARCHAR(3),
    trial_extension_days INT,

    status discount_redemption_status NOT NULL DEFAULT 'pending',
    redeemed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    applied_at TIMESTAMPTZ
);

CREATE INDEX idx_discount_redemptions_code ON discount_redemptions(discount_code_id, redeemed_at DESC);
CREATE INDEX idx_discount_redemptions_org ON discount_redemptions(organization_id, redeemed_at DESC);
