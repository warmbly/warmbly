-- Net-new campaign send controls: sender rotation/weighting, per-campaign daily
-- ramp-up, ESP/provider matching, new-lead cap + priority, and a campaign-scoped
-- tracking-domain override.
--
-- Every change is ADDITIVE and the defaults reproduce today's behavior exactly:
-- tag-based sender selection, flat daily_limit (ramp off), no ESP matching,
-- unlimited new leads, and the mailbox/default tracking domain. Existing
-- campaigns are therefore unaffected until a control is explicitly turned on.

-- 1. Sending-account rotation / weighting --------------------------------
ALTER TABLE campaigns ADD COLUMN sender_strategy text NOT NULL DEFAULT 'tags'
    CHECK (sender_strategy IN ('tags', 'explicit'));
ALTER TABLE campaigns ADD COLUMN rotation_mode text NOT NULL DEFAULT 'weighted'
    CHECK (rotation_mode IN ('weighted', 'round_robin', 'least_recently_used'));

-- 2. Per-campaign daily ramp-up ------------------------------------------
ALTER TABLE campaigns ADD COLUMN ramp_enabled boolean NOT NULL DEFAULT false;
ALTER TABLE campaigns ADD COLUMN ramp_start integer NOT NULL DEFAULT 10
    CHECK (ramp_start >= 1 AND ramp_start <= 100);
ALTER TABLE campaigns ADD COLUMN ramp_increment integer NOT NULL DEFAULT 5
    CHECK (ramp_increment >= 0 AND ramp_increment <= 100);
ALTER TABLE campaigns ADD COLUMN ramp_ceiling integer NOT NULL DEFAULT 50
    CHECK (ramp_ceiling >= 1 AND ramp_ceiling <= 100);
-- ramp_level is the persisted per-mailbox level (0 = not started); pause/resume
-- keeps progress. ramp_level_date day-gates the once-per-UTC-day advance.
ALTER TABLE campaigns ADD COLUMN ramp_level integer NOT NULL DEFAULT 0;
ALTER TABLE campaigns ADD COLUMN ramp_level_date date;

-- 3. ESP / provider matching ---------------------------------------------
ALTER TABLE campaigns ADD COLUMN esp_match_mode text NOT NULL DEFAULT 'off'
    CHECK (esp_match_mode IN ('off', 'prefer', 'strict'));

-- 4. New-lead cap + priority ---------------------------------------------
ALTER TABLE campaigns ADD COLUMN max_new_leads_per_day integer NOT NULL DEFAULT 0
    CHECK (max_new_leads_per_day >= 0 AND max_new_leads_per_day <= 1000); -- 0 = unlimited
ALTER TABLE campaigns ADD COLUMN prioritize_new_leads boolean NOT NULL DEFAULT false;

-- 5. Campaign-scoped tracking domain (honored only when verified) ---------
ALTER TABLE campaigns ADD COLUMN tracking_domain text NOT NULL DEFAULT ''; -- '' = mailbox/default fallback
ALTER TABLE campaigns ADD COLUMN tracking_domain_verified boolean NOT NULL DEFAULT false;
ALTER TABLE campaigns ADD COLUMN tracking_domain_verified_at timestamptz;

-- Explicit per-campaign sender list (feature 1). Used only when
-- sender_strategy = 'explicit'; an empty set makes the scheduler fall back to
-- the existing tag-based selection, preserving backward compatibility.
CREATE TABLE campaign_senders (
    campaign_id       uuid NOT NULL REFERENCES campaigns (id) ON DELETE CASCADE,
    email_account_id  uuid NOT NULL REFERENCES email_accounts (id) ON DELETE CASCADE,
    weight            integer NOT NULL DEFAULT 1 CHECK (weight >= 1 AND weight <= 100),
    rotation_position integer NOT NULL DEFAULT 0,  -- monotonic cursor for round_robin
    last_sent_at      timestamptz,                 -- drives least_recently_used
    enabled           boolean NOT NULL DEFAULT true,
    created_at        timestamptz NOT NULL DEFAULT NOW(),
    PRIMARY KEY (campaign_id, email_account_id)
);
CREATE INDEX idx_campaign_senders_campaign ON campaign_senders (campaign_id) WHERE enabled = true;

-- Per-(campaign, day) counters; back the ramp advance and the new-lead cap
-- (features 2 & 4). A "new lead" is a contact receiving sequence position 1.
CREATE TABLE campaign_daily_sends (
    campaign_id       uuid NOT NULL REFERENCES campaigns (id) ON DELETE CASCADE,
    send_date         date NOT NULL,
    emails_sent       integer NOT NULL DEFAULT 0,
    new_leads_started integer NOT NULL DEFAULT 0,
    PRIMARY KEY (campaign_id, send_date)
);

-- Recipient ESP cache (feature 3). Derived from the recipient domain in the
-- control plane at selection time — never an MX dial on the send hot path.
ALTER TABLE contacts ADD COLUMN esp_provider text NOT NULL DEFAULT ''; -- '' | 'gmail' | 'outlook' | 'other'
ALTER TABLE contacts ADD COLUMN esp_resolved_at timestamptz;
