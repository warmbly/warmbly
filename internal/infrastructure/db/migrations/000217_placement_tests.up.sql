-- Inbox placement tests for workspaces. A test renders a template (or a
-- campaign step) the way a campaign would, sends one copy per seed mailbox
-- from a real sender mailbox, paced as scheduled tasks, and reads where each
-- copy landed from the seed's synced mail, matched on Message-ID.

-- One probe send per seed is a task of its own, so it shares the min-gap clock
-- and the daily counts every other send from the mailbox uses. Postgres cannot
-- drop an enum value, so the down migration leaves it.
ALTER TYPE public.task_type ADD VALUE IF NOT EXISTS 'placement';

-- Who a seed serves: the instance panel every workspace tests against, or one
-- workspace's own test inboxes. NULL is an ordinary mailbox.
ALTER TABLE email_accounts ADD COLUMN seed_scope text;
UPDATE email_accounts SET seed_scope = 'instance' WHERE is_seed;
ALTER TABLE email_accounts
    ADD CONSTRAINT email_accounts_seed_scope_check
    CHECK (seed_scope IS NULL OR seed_scope IN ('instance', 'workspace'));
DROP INDEX IF EXISTS idx_email_accounts_is_seed;
ALTER TABLE email_accounts DROP COLUMN is_seed;
CREATE INDEX idx_email_accounts_seed_scope
    ON email_accounts (seed_scope, organization_id)
    WHERE seed_scope IS NOT NULL;

-- Scheduled re-tests of an active campaign's first step, rotating through its
-- sending mailboxes, with an alert when the inbox rate drops.
CREATE TABLE placement_monitors (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id uuid NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    campaign_id     uuid NOT NULL UNIQUE REFERENCES campaigns (id) ON DELETE CASCADE,
    created_by      uuid REFERENCES users (id) ON DELETE SET NULL,
    enabled         boolean NOT NULL DEFAULT true,
    interval_days   integer NOT NULL DEFAULT 7 CHECK (interval_days BETWEEN 1 AND 30),
    panel           text NOT NULL DEFAULT 'instance' CHECK (panel IN ('instance', 'workspace', 'cloud')),
    alert_below     integer NOT NULL DEFAULT 70 CHECK (alert_below BETWEEN 0 AND 100),
    pause_on_alert  boolean NOT NULL DEFAULT false,
    next_run_at     timestamptz NOT NULL DEFAULT NOW(),
    last_run_at     timestamptz,
    last_test_id    uuid,
    last_sender_id  uuid REFERENCES email_accounts (id) ON DELETE SET NULL,
    last_alert_at   timestamptz,
    last_error      text NOT NULL DEFAULT '',
    created_at      timestamptz NOT NULL DEFAULT NOW(),
    updated_at      timestamptz NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_placement_monitors_due ON placement_monitors (next_run_at) WHERE enabled;
CREATE INDEX idx_placement_monitors_org ON placement_monitors (organization_id);

-- A copy is matched on its Message-ID now, so the subject token goes. A test
-- the cloud runs for a linked instance has no local sender.
ALTER TABLE placement_tests
    DROP COLUMN token,
    ALTER COLUMN sender_account_id DROP NOT NULL,
    ADD COLUMN created_by         uuid REFERENCES users (id) ON DELETE SET NULL,
    ADD COLUMN campaign_id        uuid REFERENCES campaigns (id) ON DELETE SET NULL,
    ADD COLUMN sequence_id        uuid REFERENCES sequences (id) ON DELETE SET NULL,
    ADD COLUMN contact_id         uuid REFERENCES contacts (id) ON DELETE SET NULL,
    ADD COLUMN monitor_id         uuid REFERENCES placement_monitors (id) ON DELETE SET NULL,
    ADD COLUMN open_tracking      boolean NOT NULL DEFAULT false,
    ADD COLUMN link_tracking      boolean NOT NULL DEFAULT false,
    ADD COLUMN compare_group_id   uuid,
    -- Existing rows were all run from the admin panel.
    ADD COLUMN origin             text NOT NULL DEFAULT 'admin',
    ADD COLUMN panel              text NOT NULL DEFAULT 'instance',
    ADD COLUMN sender_email       text NOT NULL DEFAULT '',
    ADD COLUMN remote_instance_id uuid,
    ADD COLUMN remote_test_id     uuid,
    ADD COLUMN error              text NOT NULL DEFAULT '';

UPDATE placement_tests SET status = 'running' WHERE status = 'pending';

ALTER TABLE placement_tests
    ALTER COLUMN status SET DEFAULT 'running',
    ALTER COLUMN origin SET DEFAULT 'manual',
    ADD CONSTRAINT placement_tests_status_check
        CHECK (status IN ('running', 'completed', 'cancelled', 'failed')),
    ADD CONSTRAINT placement_tests_origin_check
        CHECK (origin IN ('manual', 'monitor', 'admin', 'remote')),
    ADD CONSTRAINT placement_tests_panel_check
        CHECK (panel IN ('instance', 'workspace', 'cloud'));

ALTER TABLE placement_monitors
    ADD CONSTRAINT placement_monitors_last_test_fkey
    FOREIGN KEY (last_test_id) REFERENCES placement_tests (id) ON DELETE SET NULL;

CREATE INDEX idx_placement_tests_running ON placement_tests (created_at) WHERE status = 'running';
CREATE INDEX idx_placement_tests_campaign ON placement_tests (campaign_id, created_at DESC) WHERE campaign_id IS NOT NULL;
CREATE INDEX idx_placement_tests_group ON placement_tests (compare_group_id) WHERE compare_group_id IS NOT NULL;

-- A result names its seed by address and host family as well as by id, so a
-- cloud seed (no local mailbox) and an archive import (the seed stays behind)
-- still read. 'missing' is a copy that never arrived, 'failed' one that never
-- left, 'other' a Gmail tab other than Promotions.
ALTER TABLE placement_results
    DROP CONSTRAINT IF EXISTS placement_results_folder_check,
    ALTER COLUMN seed_account_id DROP NOT NULL,
    ADD COLUMN seed_address     text NOT NULL DEFAULT '',
    ADD COLUMN remote_seed_id   uuid,
    ADD COLUMN task_id          uuid REFERENCES tasks (id) ON DELETE SET NULL,
    ADD COLUMN message_id       text NOT NULL DEFAULT '',
    ADD COLUMN scheduled_at     timestamptz,
    ADD COLUMN sent_at          timestamptz,
    ADD COLUMN remote_synced_at timestamptz,
    ADD COLUMN error            text NOT NULL DEFAULT '';

UPDATE placement_results SET folder = 'missing' WHERE folder = 'other' AND raw_flags = 'timeout';
UPDATE placement_results pr SET seed_address = ea.email
FROM email_accounts ea WHERE ea.id = pr.seed_account_id;

ALTER TABLE placement_results
    ADD CONSTRAINT placement_results_folder_check
        CHECK (folder IN ('pending', 'inbox', 'promotions', 'other', 'spam', 'missing', 'failed', 'cancelled'));

CREATE UNIQUE INDEX idx_placement_results_test_address ON placement_results (test_id, lower(seed_address));
CREATE UNIQUE INDEX idx_placement_results_task ON placement_results (task_id) WHERE task_id IS NOT NULL;
