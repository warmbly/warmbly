-- The AI and data balance moves from integer credits to US dollars, held as
-- integer micro-dollars (millionths of a dollar). Every credit column keeps a
-- dollar twin, and triggers keep each pair in step whichever release wrote it:
-- a write that changes only the credit column (the previous release) mirrors
-- into the dollar column, and a write that changes the dollar column (this
-- release) rewrites the credit column. Rollback and mixed versions therefore
-- see the same balances. A later release drops the credit columns and these
-- triggers. One credit is worth legacy_credit_micro() micro-dollars.

CREATE OR REPLACE FUNCTION legacy_credit_micro() RETURNS bigint
    LANGUAGE sql IMMUTABLE AS 'SELECT 20000::bigint';

-- Credits rounded down from micro-dollars, never negative.
CREATE OR REPLACE FUNCTION micro_to_credits(m bigint) RETURNS integer
    LANGUAGE sql IMMUTABLE AS 'SELECT (GREATEST(m, 0) / legacy_credit_micro())::integer';

-- A positive limit stays positive in credits, so the legacy CHECK (> 0) holds.
CREATE OR REPLACE FUNCTION micro_to_credit_limit(m bigint) RETURNS integer
    LANGUAGE sql IMMUTABLE AS 'SELECT CASE WHEN m IS NULL THEN NULL ELSE GREATEST(m / legacy_credit_micro(), 1)::integer END';

-- ---------------------------------------------------------------- ledger

ALTER TABLE credit_ledger
    ADD COLUMN IF NOT EXISTS balance_micro bigint NOT NULL DEFAULT 0 CHECK (balance_micro >= 0),
    ADD COLUMN IF NOT EXISTS purchased_micro bigint NOT NULL DEFAULT 0 CHECK (purchased_micro >= 0),
    ADD COLUMN IF NOT EXISTS total_purchased_micro bigint NOT NULL DEFAULT 0 CHECK (total_purchased_micro >= 0);

UPDATE credit_ledger SET
    balance_micro = balance::bigint * legacy_credit_micro(),
    purchased_micro = purchased_balance::bigint * legacy_credit_micro(),
    total_purchased_micro = total_purchased::bigint * legacy_credit_micro();

CREATE OR REPLACE FUNCTION credit_ledger_usd_sync() RETURNS trigger
    LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'INSERT' THEN
        IF NEW.balance_micro = 0 AND NEW.balance <> 0 THEN
            NEW.balance_micro := NEW.balance::bigint * legacy_credit_micro();
        ELSE
            NEW.balance := micro_to_credits(NEW.balance_micro);
        END IF;
        IF NEW.purchased_micro = 0 AND NEW.purchased_balance <> 0 THEN
            NEW.purchased_micro := NEW.purchased_balance::bigint * legacy_credit_micro();
        ELSE
            NEW.purchased_balance := micro_to_credits(NEW.purchased_micro);
        END IF;
        IF NEW.total_purchased_micro = 0 AND NEW.total_purchased <> 0 THEN
            NEW.total_purchased_micro := NEW.total_purchased::bigint * legacy_credit_micro();
        ELSE
            NEW.total_purchased := micro_to_credits(NEW.total_purchased_micro);
        END IF;
        RETURN NEW;
    END IF;

    IF NEW.balance_micro IS DISTINCT FROM OLD.balance_micro THEN
        NEW.balance := micro_to_credits(NEW.balance_micro);
    ELSIF NEW.balance IS DISTINCT FROM OLD.balance THEN
        NEW.balance_micro := GREATEST(OLD.balance_micro + (NEW.balance - OLD.balance)::bigint * legacy_credit_micro(), 0);
    END IF;
    IF NEW.purchased_micro IS DISTINCT FROM OLD.purchased_micro THEN
        NEW.purchased_balance := micro_to_credits(NEW.purchased_micro);
    ELSIF NEW.purchased_balance IS DISTINCT FROM OLD.purchased_balance THEN
        NEW.purchased_micro := GREATEST(OLD.purchased_micro + (NEW.purchased_balance - OLD.purchased_balance)::bigint * legacy_credit_micro(), 0);
    END IF;
    IF NEW.total_purchased_micro IS DISTINCT FROM OLD.total_purchased_micro THEN
        NEW.total_purchased := micro_to_credits(NEW.total_purchased_micro);
    ELSIF NEW.total_purchased IS DISTINCT FROM OLD.total_purchased THEN
        NEW.total_purchased_micro := GREATEST(OLD.total_purchased_micro + (NEW.total_purchased - OLD.total_purchased)::bigint * legacy_credit_micro(), 0);
    END IF;
    RETURN NEW;
END $$;

DROP TRIGGER IF EXISTS credit_ledger_usd_sync ON credit_ledger;
CREATE TRIGGER credit_ledger_usd_sync BEFORE INSERT OR UPDATE ON credit_ledger
    FOR EACH ROW EXECUTE FUNCTION credit_ledger_usd_sync();

-- ---------------------------------------------------------- transactions
-- Rows written before this release keep NULL dollar columns; readers take
-- COALESCE(x_micro, x * legacy_credit_micro()), so nothing is backfilled.

ALTER TABLE credit_ledger_transactions
    ADD COLUMN IF NOT EXISTS amount_micro bigint,
    ADD COLUMN IF NOT EXISTS balance_after_micro bigint,
    ADD COLUMN IF NOT EXISTS purchased_delta_micro bigint,
    ADD COLUMN IF NOT EXISTS purchased_balance_after_micro bigint,
    ADD COLUMN IF NOT EXISTS input_tokens integer NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS output_tokens integer NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS quantity integer NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS cost_micro bigint NOT NULL DEFAULT 0;

CREATE OR REPLACE FUNCTION credit_txn_usd_sync() RETURNS trigger
    LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.amount_micro IS NULL THEN
        NEW.amount_micro := NEW.amount::bigint * legacy_credit_micro();
        NEW.balance_after_micro := NEW.balance_after::bigint * legacy_credit_micro();
        NEW.purchased_delta_micro := NEW.purchased_delta::bigint * legacy_credit_micro();
        NEW.purchased_balance_after_micro := NEW.purchased_balance_after::bigint * legacy_credit_micro();
    ELSE
        NEW.amount := round(NEW.amount_micro::numeric / legacy_credit_micro())::integer;
        NEW.balance_after := micro_to_credits(NEW.balance_after_micro);
        NEW.purchased_delta := round(COALESCE(NEW.purchased_delta_micro, 0)::numeric / legacy_credit_micro())::integer;
        NEW.purchased_balance_after := micro_to_credits(NEW.purchased_balance_after_micro);
        NEW.tokens_used := NEW.input_tokens + NEW.output_tokens;
    END IF;
    RETURN NEW;
END $$;

DROP TRIGGER IF EXISTS credit_txn_usd_sync ON credit_ledger_transactions;
CREATE TRIGGER credit_txn_usd_sync BEFORE INSERT ON credit_ledger_transactions
    FOR EACH ROW EXECUTE FUNCTION credit_txn_usd_sync();

-- ----------------------------------------------------------------- plans
-- What each plan includes each month, in dollars.

ALTER TABLE plans
    ADD COLUMN IF NOT EXISTS monthly_allowance_micro bigint NOT NULL DEFAULT 0 CHECK (monthly_allowance_micro >= 0);

UPDATE plans SET monthly_allowance_micro = CASE
    WHEN id = '00000000-0000-0000-0000-000000000110' OR lower(name) LIKE 'starter%' THEN 5000000
    WHEN id IN ('00000000-0000-0000-0000-000000000120', '00000000-0000-0000-0000-000000000121') OR lower(name) LIKE 'grow%' THEN 15000000
    WHEN id = '00000000-0000-0000-0000-000000000130' OR lower(name) LIKE 'business%' THEN 50000000
    ELSE monthly_credits::bigint * legacy_credit_micro()
END;

-- The previous release refills a monthly pool from monthly_credits, so it has
-- to grant the same dollars.
UPDATE plans SET monthly_credits = (monthly_allowance_micro / legacy_credit_micro())::integer
WHERE monthly_credits::bigint * legacy_credit_micro() <> monthly_allowance_micro;

-- ------------------------------------------------------- spend settings

ALTER TABLE org_ai_settings
    ADD COLUMN IF NOT EXISTS spend_limit_daily_micro bigint CHECK (spend_limit_daily_micro IS NULL OR spend_limit_daily_micro > 0),
    ADD COLUMN IF NOT EXISTS spend_limit_weekly_micro bigint CHECK (spend_limit_weekly_micro IS NULL OR spend_limit_weekly_micro > 0),
    ADD COLUMN IF NOT EXISTS spend_limit_monthly_micro bigint CHECK (spend_limit_monthly_micro IS NULL OR spend_limit_monthly_micro > 0),
    ADD COLUMN IF NOT EXISTS member_limit_daily_micro bigint CHECK (member_limit_daily_micro IS NULL OR member_limit_daily_micro > 0),
    ADD COLUMN IF NOT EXISTS member_limit_weekly_micro bigint CHECK (member_limit_weekly_micro IS NULL OR member_limit_weekly_micro > 0),
    ADD COLUMN IF NOT EXISTS member_limit_monthly_micro bigint CHECK (member_limit_monthly_micro IS NULL OR member_limit_monthly_micro > 0),
    ADD COLUMN IF NOT EXISTS low_balance_threshold_micro bigint NOT NULL DEFAULT 500000 CHECK (low_balance_threshold_micro >= 0),
    ADD COLUMN IF NOT EXISTS auto_reload_enabled boolean NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS auto_reload_threshold_micro bigint NOT NULL DEFAULT 5000000
        CHECK (auto_reload_threshold_micro BETWEEN 10000 AND 1000000000),
    ADD COLUMN IF NOT EXISTS auto_reload_amount_micro bigint NOT NULL DEFAULT 25000000
        CHECK (auto_reload_amount_micro BETWEEN 5000000 AND 5000000000),
    ADD COLUMN IF NOT EXISTS auto_reload_monthly_limit_micro bigint NOT NULL DEFAULT 250000000
        CHECK (auto_reload_monthly_limit_micro BETWEEN 5000000 AND 50000000000),
    ADD COLUMN IF NOT EXISTS auto_reload_failure_code text
        CHECK (auto_reload_failure_code IS NULL OR auto_reload_failure_code IN
            ('card_declined', 'authentication_required', 'no_payment_method', 'payment_failed', 'unavailable')),
    ADD COLUMN IF NOT EXISTS auto_reload_failed_at timestamptz,
    ADD COLUMN IF NOT EXISTS balance_out_notified_at timestamptz;

-- A reload that does not lift the balance over its own trigger would loop.
ALTER TABLE org_ai_settings
    DROP CONSTRAINT IF EXISTS org_ai_settings_auto_reload_shape,
    ADD CONSTRAINT org_ai_settings_auto_reload_shape CHECK (
        auto_reload_amount_micro > auto_reload_threshold_micro
        AND auto_reload_monthly_limit_micro >= auto_reload_amount_micro
    );

UPDATE org_ai_settings SET
    spend_limit_daily_micro = spend_limit_daily::bigint * legacy_credit_micro(),
    spend_limit_weekly_micro = spend_limit_weekly::bigint * legacy_credit_micro(),
    spend_limit_monthly_micro = spend_limit_monthly::bigint * legacy_credit_micro(),
    member_limit_daily_micro = member_limit_daily::bigint * legacy_credit_micro(),
    member_limit_weekly_micro = member_limit_weekly::bigint * legacy_credit_micro(),
    member_limit_monthly_micro = member_limit_monthly::bigint * legacy_credit_micro(),
    low_balance_threshold_micro = low_balance_threshold::bigint * legacy_credit_micro();

-- An existing pack-based auto top-up keeps its shape: the pack becomes the
-- reload amount, the credit threshold the dollar one, and the monthly count
-- times the amount the monthly limit. It moves to auto_reload_enabled, and the
-- legacy switch goes off, so the previous release never charges a pack from
-- a setting this release owns.
UPDATE org_ai_settings SET
    auto_reload_enabled = true,
    auto_topup_enabled = false,
    auto_reload_amount_micro = GREATEST(CASE auto_topup_pack
        WHEN 'pack_2000' THEN 2000 WHEN 'pack_10000' THEN 10000 ELSE 500 END::bigint * legacy_credit_micro(), 5000000),
    auto_reload_threshold_micro = LEAST(GREATEST(auto_topup_threshold::bigint * legacy_credit_micro(), 10000),
        GREATEST(CASE auto_topup_pack WHEN 'pack_2000' THEN 2000 WHEN 'pack_10000' THEN 10000 ELSE 500 END::bigint * legacy_credit_micro(), 5000000) - 10000),
    auto_reload_monthly_limit_micro = GREATEST(CASE auto_topup_pack
        WHEN 'pack_2000' THEN 2000 WHEN 'pack_10000' THEN 10000 ELSE 500 END::bigint * legacy_credit_micro(), 5000000)
        * GREATEST(auto_topup_max_per_month, 1)
WHERE auto_topup_enabled;

-- Each limit pair: the side this write changed wins; an insert fills the
-- dollar side from credits only when the writer set credits alone.
CREATE OR REPLACE FUNCTION org_ai_settings_usd_sync() RETURNS trigger
    LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'INSERT' THEN
        IF NEW.spend_limit_daily_micro IS NULL AND NEW.spend_limit_daily IS NOT NULL THEN NEW.spend_limit_daily_micro := NEW.spend_limit_daily::bigint * legacy_credit_micro(); ELSE NEW.spend_limit_daily := micro_to_credit_limit(NEW.spend_limit_daily_micro); END IF;
        IF NEW.spend_limit_weekly_micro IS NULL AND NEW.spend_limit_weekly IS NOT NULL THEN NEW.spend_limit_weekly_micro := NEW.spend_limit_weekly::bigint * legacy_credit_micro(); ELSE NEW.spend_limit_weekly := micro_to_credit_limit(NEW.spend_limit_weekly_micro); END IF;
        IF NEW.spend_limit_monthly_micro IS NULL AND NEW.spend_limit_monthly IS NOT NULL THEN NEW.spend_limit_monthly_micro := NEW.spend_limit_monthly::bigint * legacy_credit_micro(); ELSE NEW.spend_limit_monthly := micro_to_credit_limit(NEW.spend_limit_monthly_micro); END IF;
        IF NEW.member_limit_daily_micro IS NULL AND NEW.member_limit_daily IS NOT NULL THEN NEW.member_limit_daily_micro := NEW.member_limit_daily::bigint * legacy_credit_micro(); ELSE NEW.member_limit_daily := micro_to_credit_limit(NEW.member_limit_daily_micro); END IF;
        IF NEW.member_limit_weekly_micro IS NULL AND NEW.member_limit_weekly IS NOT NULL THEN NEW.member_limit_weekly_micro := NEW.member_limit_weekly::bigint * legacy_credit_micro(); ELSE NEW.member_limit_weekly := micro_to_credit_limit(NEW.member_limit_weekly_micro); END IF;
        IF NEW.member_limit_monthly_micro IS NULL AND NEW.member_limit_monthly IS NOT NULL THEN NEW.member_limit_monthly_micro := NEW.member_limit_monthly::bigint * legacy_credit_micro(); ELSE NEW.member_limit_monthly := micro_to_credit_limit(NEW.member_limit_monthly_micro); END IF;
        NEW.low_balance_threshold := micro_to_credits(NEW.low_balance_threshold_micro);
        RETURN NEW;
    END IF;

    IF NEW.spend_limit_daily_micro IS DISTINCT FROM OLD.spend_limit_daily_micro THEN NEW.spend_limit_daily := micro_to_credit_limit(NEW.spend_limit_daily_micro);
    ELSIF NEW.spend_limit_daily IS DISTINCT FROM OLD.spend_limit_daily THEN NEW.spend_limit_daily_micro := NEW.spend_limit_daily::bigint * legacy_credit_micro(); END IF;
    IF NEW.spend_limit_weekly_micro IS DISTINCT FROM OLD.spend_limit_weekly_micro THEN NEW.spend_limit_weekly := micro_to_credit_limit(NEW.spend_limit_weekly_micro);
    ELSIF NEW.spend_limit_weekly IS DISTINCT FROM OLD.spend_limit_weekly THEN NEW.spend_limit_weekly_micro := NEW.spend_limit_weekly::bigint * legacy_credit_micro(); END IF;
    IF NEW.spend_limit_monthly_micro IS DISTINCT FROM OLD.spend_limit_monthly_micro THEN NEW.spend_limit_monthly := micro_to_credit_limit(NEW.spend_limit_monthly_micro);
    ELSIF NEW.spend_limit_monthly IS DISTINCT FROM OLD.spend_limit_monthly THEN NEW.spend_limit_monthly_micro := NEW.spend_limit_monthly::bigint * legacy_credit_micro(); END IF;
    IF NEW.member_limit_daily_micro IS DISTINCT FROM OLD.member_limit_daily_micro THEN NEW.member_limit_daily := micro_to_credit_limit(NEW.member_limit_daily_micro);
    ELSIF NEW.member_limit_daily IS DISTINCT FROM OLD.member_limit_daily THEN NEW.member_limit_daily_micro := NEW.member_limit_daily::bigint * legacy_credit_micro(); END IF;
    IF NEW.member_limit_weekly_micro IS DISTINCT FROM OLD.member_limit_weekly_micro THEN NEW.member_limit_weekly := micro_to_credit_limit(NEW.member_limit_weekly_micro);
    ELSIF NEW.member_limit_weekly IS DISTINCT FROM OLD.member_limit_weekly THEN NEW.member_limit_weekly_micro := NEW.member_limit_weekly::bigint * legacy_credit_micro(); END IF;
    IF NEW.member_limit_monthly_micro IS DISTINCT FROM OLD.member_limit_monthly_micro THEN NEW.member_limit_monthly := micro_to_credit_limit(NEW.member_limit_monthly_micro);
    ELSIF NEW.member_limit_monthly IS DISTINCT FROM OLD.member_limit_monthly THEN NEW.member_limit_monthly_micro := NEW.member_limit_monthly::bigint * legacy_credit_micro(); END IF;
    IF NEW.low_balance_threshold_micro IS DISTINCT FROM OLD.low_balance_threshold_micro THEN NEW.low_balance_threshold := micro_to_credits(NEW.low_balance_threshold_micro);
    ELSIF NEW.low_balance_threshold IS DISTINCT FROM OLD.low_balance_threshold THEN NEW.low_balance_threshold_micro := NEW.low_balance_threshold::bigint * legacy_credit_micro(); END IF;
    RETURN NEW;
END $$;

DROP TRIGGER IF EXISTS org_ai_settings_usd_sync ON org_ai_settings;
CREATE TRIGGER org_ai_settings_usd_sync BEFORE INSERT OR UPDATE ON org_ai_settings
    FOR EACH ROW EXECUTE FUNCTION org_ai_settings_usd_sync();

-- ------------------------------------------------------- saved-card charges
-- One row per charge on the saved card, an automatic reload or a one-click
-- top-up. The id derives every Stripe idempotency key, so a retried job never
-- charges twice, and a pending row counts against the monthly reload limit.

ALTER TABLE credit_auto_topup_attempts
    ADD COLUMN IF NOT EXISTS kind text NOT NULL DEFAULT 'auto_reload' CHECK (kind IN ('auto_reload', 'top_up')),
    ADD COLUMN IF NOT EXISTS amount_micro bigint NOT NULL DEFAULT 0 CHECK (amount_micro >= 0),
    ADD COLUMN IF NOT EXISTS amount_cents bigint NOT NULL DEFAULT 0 CHECK (amount_cents >= 0),
    ADD COLUMN IF NOT EXISTS currency text NOT NULL DEFAULT 'usd',
    ADD COLUMN IF NOT EXISTS invoice_id text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS customer_id text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS payment_method_id text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS tries integer NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS retry_at timestamptz,
    ADD COLUMN IF NOT EXISTS failure_code text NOT NULL DEFAULT '';

UPDATE credit_auto_topup_attempts SET amount_micro = credits::bigint * legacy_credit_micro() WHERE amount_micro = 0;

CREATE UNIQUE INDEX IF NOT EXISTS credit_auto_topup_attempts_invoice
    ON credit_auto_topup_attempts (invoice_id)
    WHERE invoice_id <> '';

-- ------------------------------------------------- per-feature charge records
-- Placement tests, placement batches and research runs record what they cost
-- in micro-dollars beside their credit columns. This release writes both; a
-- row the previous release writes carries credits only and readers fall back
-- to them.

ALTER TABLE placement_tests
    ADD COLUMN IF NOT EXISTS charged_micro bigint NOT NULL DEFAULT 0 CHECK (charged_micro >= 0),
    ADD COLUMN IF NOT EXISTS refunded_micro bigint NOT NULL DEFAULT 0 CHECK (refunded_micro >= 0);

UPDATE placement_tests SET
    charged_micro = credits_charged::bigint * legacy_credit_micro(),
    refunded_micro = credits_refunded::bigint * legacy_credit_micro()
WHERE credits_charged > 0 AND charged_micro = 0;

ALTER TABLE placement_batches
    ADD COLUMN IF NOT EXISTS max_micro bigint NOT NULL DEFAULT 0 CHECK (max_micro >= 0),
    ADD COLUMN IF NOT EXISTS spent_micro bigint NOT NULL DEFAULT 0 CHECK (spent_micro >= 0);

UPDATE placement_batches SET
    max_micro = max_credits::bigint * legacy_credit_micro(),
    spent_micro = credits_spent::bigint * legacy_credit_micro()
WHERE (max_credits > 0 OR credits_spent > 0) AND max_micro = 0 AND spent_micro = 0;

ALTER TABLE contact_research_runs
    ADD COLUMN IF NOT EXISTS charged_micro bigint NOT NULL DEFAULT 0 CHECK (charged_micro >= 0);

UPDATE contact_research_runs SET charged_micro = credits_charged::bigint * legacy_credit_micro()
WHERE credits_charged > 0 AND charged_micro = 0;

ALTER TABLE lead_enrichment_runs
    ADD COLUMN IF NOT EXISTS charged_micro bigint NOT NULL DEFAULT 0 CHECK (charged_micro >= 0);
