-- Referral program. Three concerns, all built on existing primitives:
--
--   1. plans.referral_reward_percent — how much of the invitee's first-month
--      plan price the referrer earns as credit (100 = a full month-equivalent).
--
--   2. referral_codes — one canonical share code per user. The code string is
--      ALSO a discount_codes row (10% off, repeating, 3 months) so the invitee's
--      checkout reuses the existing discount path with no new Stripe mechanics;
--      this table only adds ownership + reverse lookup for attribution.
--
--   3. referral_attributions + referral_earnings_ledger/_transactions — who
--      referred whom, and the referrer's dollar credit ledger. The ledger is
--      cents (real money) and is deliberately separate from the integer AI
--      credit_ledger; it is mirrored to the referrer's Stripe customer balance
--      so it nets off their invoices automatically.

-- 1. Per-plan reward percentage. 100 = full first-month-equivalent price.
ALTER TABLE plans
    ADD COLUMN IF NOT EXISTS referral_reward_percent smallint NOT NULL DEFAULT 100
        CHECK (referral_reward_percent BETWEEN 0 AND 100);

-- 2. One referral code per user. code mirrors a discount_codes.code so the same
-- string both attributes the signup and grants the invitee the 10%/3mo discount.
CREATE TABLE IF NOT EXISTS referral_codes (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_user_id    uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    owner_org_id     uuid NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    code             text NOT NULL,
    discount_code_id uuid REFERENCES discount_codes (id) ON DELETE SET NULL,
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT uq_referral_codes_owner UNIQUE (owner_user_id),
    CONSTRAINT uq_referral_codes_code UNIQUE (code)
);
CREATE INDEX IF NOT EXISTS idx_referral_codes_org ON referral_codes (owner_org_id);

-- 3a. Attribution: invitee org -> referrer. One row per referred org; the
-- UNIQUE (invitee_org_id) is the "an org can only be referred once" guard.
CREATE TABLE IF NOT EXISTS referral_attributions (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    referral_code_id uuid REFERENCES referral_codes (id) ON DELETE SET NULL,
    referrer_user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    referrer_org_id  uuid NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    invitee_org_id   uuid NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    invitee_user_id  uuid REFERENCES users (id) ON DELETE SET NULL,
    status           text NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'qualified', 'rewarded', 'void')),
    reward_cents     bigint NOT NULL DEFAULT 0,
    reward_currency  text NOT NULL DEFAULT 'usd',
    qualified_at     timestamptz,
    rewarded_at      timestamptz,
    voided_at        timestamptz,
    void_reason      text,
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT uq_referral_attr_invitee_org UNIQUE (invitee_org_id)
);
CREATE INDEX IF NOT EXISTS idx_referral_attr_referrer ON referral_attributions (referrer_org_id, status);
CREATE INDEX IF NOT EXISTS idx_referral_attr_status ON referral_attributions (status);

-- 3b. Dollar earnings ledger for referrers (cents). balance_cents is the net
-- credit earned (rewards - clawbacks); stripe_pushed_cents tracks how much of
-- that net we have mirrored onto the Stripe customer balance, so a referrer who
-- earns before subscribing gets flushed once they have a customer. No
-- non-negative CHECK: a clawback may temporarily exceed the available balance.
CREATE TABLE IF NOT EXISTS referral_earnings_ledger (
    org_id                uuid PRIMARY KEY REFERENCES organizations (id) ON DELETE CASCADE,
    balance_cents         bigint NOT NULL DEFAULT 0,
    lifetime_earned_cents bigint NOT NULL DEFAULT 0,
    stripe_pushed_cents   bigint NOT NULL DEFAULT 0,
    currency              text NOT NULL DEFAULT 'usd',
    created_at            timestamptz NOT NULL DEFAULT now(),
    updated_at            timestamptz NOT NULL DEFAULT now()
);

-- 3c. Append-only ledger trail. amount_cents > 0 = reward, < 0 = clawback.
-- idempotency_key is the Stripe event id so a retried webhook never double-grants.
CREATE TABLE IF NOT EXISTS referral_earnings_transactions (
    id                             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id                         uuid NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    attribution_id                 uuid REFERENCES referral_attributions (id) ON DELETE SET NULL,
    amount_cents                   bigint NOT NULL,
    currency                       text NOT NULL DEFAULT 'usd',
    reason                         text NOT NULL,
    balance_after_cents            bigint NOT NULL,
    stripe_customer_balance_txn_id text,
    idempotency_key                text,
    created_at                     timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_referral_txns_org ON referral_earnings_transactions (org_id, created_at DESC);
CREATE UNIQUE INDEX IF NOT EXISTS uq_referral_txns_idempotency
    ON referral_earnings_transactions (idempotency_key)
    WHERE idempotency_key IS NOT NULL;
