-- Human sending behaviour: per-mailbox randomisation of the things that make
-- automated sending look automated.
--
-- Two tables, deliberately split:
--
--   email_account_behavior   the RANGES a mailbox is allowed to roll inside
--                            (config the customer edits)
--   email_account_daily_plan the values actually ROLLED for one local day
--                            (server-owned, immutable once written)
--
-- The plan matters as much as the ranges. The scheduler recomputes a mailbox's
-- next slot many times a day; if the daily cap, work start/end and lunch were
-- re-rolled on every call the mailbox would jitter around instead of behaving
-- like one person with one workday. Rolling once per (mailbox, local date) and
-- persisting it makes the day stable, inspectable in the dashboard, and
-- reproducible when someone asks "why did this send at 17:41".
--
-- Everything is expressed in MINUTES SINCE LOCAL MIDNIGHT, in the mailbox's own
-- timezone (email_accounts.timezone). That is what makes the schedule
-- timezone-based rather than server-clock-based: a London mailbox and a Denver
-- mailbox on the same worker each keep their own 9-to-5.

CREATE TABLE IF NOT EXISTS email_account_behavior (
    email_account_id  uuid PRIMARY KEY REFERENCES email_accounts(id) ON DELETE CASCADE,

    -- Off by default. An existing mailbox keeps its current fixed cap and gap
    -- until someone opts it in, so this migration changes no sending behaviour
    -- on its own.
    enabled           boolean NOT NULL DEFAULT false,

    -- Daily cold-send target, re-rolled each day inside [min, max]. Applied
    -- via min() against the mailbox cold cap and the campaign daily limit, so
    -- it can only ever LOWER volume — the mailbox-first safety invariant.
    daily_limit_min   integer NOT NULL DEFAULT 30,
    daily_limit_max   integer NOT NULL DEFAULT 45,

    -- Per-hour cold-send ceiling, rolled once per day. Shapes burstiness
    -- within the workday; the daily target still governs the total.
    hourly_limit_min  integer NOT NULL DEFAULT 5,
    hourly_limit_max  integer NOT NULL DEFAULT 9,

    -- Spacing between two sends from this mailbox. Replaces the flat
    -- min_wait_time while behaviour is enabled; each gap is drawn fresh so the
    -- sequence of intervals is irregular, not a fixed metronome.
    gap_min_seconds   integer NOT NULL DEFAULT 90,
    gap_max_seconds   integer NOT NULL DEFAULT 420,

    -- Workday boundaries. The start is drawn from [work_start_min,
    -- work_start_max] and the end from [work_end_min, work_end_max], so the
    -- mailbox does not begin and finish at the same clock minute every day.
    -- Defaults: 09:03-09:27 and 17:18-17:56.
    work_start_min    integer NOT NULL DEFAULT 543,
    work_start_max    integer NOT NULL DEFAULT 567,
    work_end_min      integer NOT NULL DEFAULT 1038,
    work_end_max      integer NOT NULL DEFAULT 1076,

    -- Lunch: a hole in the middle of the day with a drawn start and length.
    -- lunch_latest is the latest the break may START, not end.
    lunch_enabled     boolean NOT NULL DEFAULT true,
    lunch_earliest    integer NOT NULL DEFAULT 720,
    lunch_latest      integer NOT NULL DEFAULT 810,
    lunch_min_minutes integer NOT NULL DEFAULT 30,
    lunch_max_minutes integer NOT NULL DEFAULT 60,

    -- Working days as a MONDAY-indexed bitmask (bit 0 = Monday .. bit 6 =
    -- Sunday), matching campaigns.days and the dashboard's Monday-first grid.
    -- Default 31 = Mon-Fri.
    weekdays          smallint NOT NULL DEFAULT 31,

    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT behavior_daily_range  CHECK (daily_limit_min  >= 1 AND daily_limit_max  <= 500  AND daily_limit_min  <= daily_limit_max),
    CONSTRAINT behavior_hourly_range CHECK (hourly_limit_min >= 1 AND hourly_limit_max <= 200  AND hourly_limit_min <= hourly_limit_max),
    CONSTRAINT behavior_gap_range    CHECK (gap_min_seconds  >= 30 AND gap_max_seconds <= 86400 AND gap_min_seconds <= gap_max_seconds),
    CONSTRAINT behavior_start_range  CHECK (work_start_min   >= 0 AND work_start_max <= 1439 AND work_start_min <= work_start_max),
    CONSTRAINT behavior_end_range    CHECK (work_end_min     >= 0 AND work_end_max   <= 1439 AND work_end_min   <= work_end_max),
    -- The latest possible start must still leave a usable workday.
    CONSTRAINT behavior_day_order    CHECK (work_start_max < work_end_min),
    CONSTRAINT behavior_lunch_window CHECK (lunch_earliest >= 0 AND lunch_latest <= 1439 AND lunch_earliest <= lunch_latest),
    CONSTRAINT behavior_lunch_length CHECK (lunch_min_minutes >= 0 AND lunch_max_minutes <= 240 AND lunch_min_minutes <= lunch_max_minutes),
    CONSTRAINT behavior_weekdays     CHECK (weekdays >= 0 AND weekdays <= 127)
);

-- One rolled workday per mailbox. Written once, then read; the scheduler never
-- updates a plan in place, so a day's shape cannot drift under it.
CREATE TABLE IF NOT EXISTS email_account_daily_plan (
    email_account_id   uuid NOT NULL REFERENCES email_accounts(id) ON DELETE CASCADE,
    -- The LOCAL calendar date in `timezone`, not a UTC date.
    plan_date          date NOT NULL,
    timezone           text NOT NULL,

    -- False on a non-working weekday: the row still exists so the dashboard can
    -- say "not a sending day" instead of showing nothing.
    is_working_day     boolean NOT NULL,

    daily_limit        integer NOT NULL,
    hourly_limit       integer NOT NULL,
    work_start_minute  integer NOT NULL,
    work_end_minute    integer NOT NULL,
    -- NULL when this day has no break.
    lunch_start_minute integer,
    lunch_end_minute   integer,
    gap_min_seconds    integer NOT NULL,
    gap_max_seconds    integer NOT NULL,

    created_at         timestamptz NOT NULL DEFAULT now(),

    PRIMARY KEY (email_account_id, plan_date),
    CONSTRAINT plan_lunch_pair CHECK (
        (lunch_start_minute IS NULL AND lunch_end_minute IS NULL)
        OR (lunch_start_minute IS NOT NULL AND lunch_end_minute IS NOT NULL
            AND lunch_start_minute < lunch_end_minute)
    )
);

-- The retention sweep deletes plans older than a couple of weeks; this keeps
-- that scan from touching the whole table.
CREATE INDEX IF NOT EXISTS idx_email_account_daily_plan_date
    ON email_account_daily_plan (plan_date);

-- Campaign auto-pause guardrails. Rates are evaluated over a rolling window on
-- campaign_contact_progress and the campaign is paused the moment a band is
-- breached, rather than waiting for a mailbox provider to react first.
ALTER TABLE campaigns
    ADD COLUMN IF NOT EXISTS guardrail_enabled boolean NOT NULL DEFAULT false,
    -- Percent. Bounce rate at or above this pauses the campaign. 0 disables
    -- the rule. Default 5% is Amazon SES's review threshold.
    ADD COLUMN IF NOT EXISTS guardrail_bounce_rate_max numeric(5,2) NOT NULL DEFAULT 5.00,
    -- Percent. Recipient spam complaints at or above this pause the campaign.
    -- Default 0.10% is Google's "never reach 0.30%" band, one third in.
    ADD COLUMN IF NOT EXISTS guardrail_complaint_rate_max numeric(5,2) NOT NULL DEFAULT 0.10,
    -- Percent. Reply rate BELOW this pauses the campaign: a campaign at volume
    -- that nobody answers is spending reputation for nothing. 0 disables it,
    -- which is the default because a floor needs a deliberate choice.
    ADD COLUMN IF NOT EXISTS guardrail_reply_rate_min numeric(5,2) NOT NULL DEFAULT 0,
    -- No rule fires below this many sends in the window. A single bounce out of
    -- four sends is not a 25% bounce rate worth pausing a campaign over.
    ADD COLUMN IF NOT EXISTS guardrail_min_sample integer NOT NULL DEFAULT 50,
    -- Rolling window in days. 0 means the campaign's whole history.
    ADD COLUMN IF NOT EXISTS guardrail_window_days integer NOT NULL DEFAULT 7,
    ADD COLUMN IF NOT EXISTS guardrail_tripped_at timestamptz,
    ADD COLUMN IF NOT EXISTS guardrail_reason text NOT NULL DEFAULT '';

ALTER TABLE campaigns
    ADD CONSTRAINT campaigns_guardrail_bounds CHECK (
        guardrail_bounce_rate_max    >= 0 AND guardrail_bounce_rate_max    <= 100 AND
        guardrail_complaint_rate_max >= 0 AND guardrail_complaint_rate_max <= 100 AND
        guardrail_reply_rate_min     >= 0 AND guardrail_reply_rate_min     <= 100 AND
        guardrail_min_sample         >= 1 AND guardrail_min_sample         <= 100000 AND
        guardrail_window_days        >= 0 AND guardrail_window_days        <= 365
    );

-- The sweep scans active campaigns with guardrails on.
CREATE INDEX IF NOT EXISTS idx_campaigns_guardrail_active
    ON campaigns (id) WHERE guardrail_enabled;

-- New terminal-ish pause reason, so "paused because bounces spiked" is
-- distinguishable from a human pause and can be resumed explicitly.
ALTER TYPE public.campaign_status ADD VALUE IF NOT EXISTS 'paused_guardrail';
