-- Client reports: a saved, scoped view of a workspace's results that can be shared by link and emailed on a schedule.
CREATE TABLE client_reports (
    id                    uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id       uuid NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    created_by            uuid REFERENCES users (id) ON DELETE SET NULL,
    name                  text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 64),
    -- Same shape as a member's access: workspace, or only the granted folders, campaigns and mailboxes.
    access_scope          text NOT NULL DEFAULT 'workspace' CHECK (access_scope IN ('workspace', 'restricted')),
    campaign_folder_ids   uuid[] NOT NULL DEFAULT '{}' CHECK (cardinality(campaign_folder_ids) <= 500),
    campaign_ids          uuid[] NOT NULL DEFAULT '{}' CHECK (cardinality(campaign_ids) <= 500),
    email_account_ids     uuid[] NOT NULL DEFAULT '{}' CHECK (cardinality(email_account_ids) <= 500),
    show_overview         boolean NOT NULL DEFAULT true,
    show_campaigns        boolean NOT NULL DEFAULT true,
    show_replies          boolean NOT NULL DEFAULT true,
    show_positive_replies boolean NOT NULL DEFAULT true,
    show_mailboxes        boolean NOT NULL DEFAULT true,
    show_daily_chart      boolean NOT NULL DEFAULT true,
    show_reply_snippets   boolean NOT NULL DEFAULT false,
    date_range            text NOT NULL DEFAULT 'last_30_days'
        CHECK (date_range IN ('last_7_days', 'last_30_days', 'this_month', 'last_month')),
    -- sha256 of the 32-byte token in the share URL; NULL while sharing is off.
    token_hash            bytea UNIQUE CHECK (token_hash IS NULL OR octet_length(token_hash) = 32),
    token_hint            text NOT NULL DEFAULT '',
    link_created_at       timestamptz,
    link_expires_at       timestamptz,
    revoked_at            timestamptz,
    created_at            timestamptz NOT NULL DEFAULT now(),
    updated_at            timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX client_reports_org_idx ON client_reports (organization_id, created_at DESC, id DESC);

-- One schedule per report.
CREATE TABLE client_report_schedules (
    report_id       uuid PRIMARY KEY REFERENCES client_reports (id) ON DELETE CASCADE,
    organization_id uuid NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    recipients      text[] NOT NULL CHECK (cardinality(recipients) BETWEEN 1 AND 10),
    cadence         text NOT NULL CHECK (cadence IN ('weekly', 'monthly')),
    weekday         smallint NOT NULL DEFAULT 1 CHECK (weekday BETWEEN 0 AND 6),
    month_day       smallint NOT NULL DEFAULT 1 CHECK (month_day BETWEEN 1 AND 28),
    hour            smallint NOT NULL DEFAULT 9 CHECK (hour BETWEEN 0 AND 23),
    timezone        text NOT NULL DEFAULT 'UTC',
    paused          boolean NOT NULL DEFAULT false,
    last_sent_at    timestamptz,
    next_run_at     timestamptz NOT NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX client_report_schedules_due_idx ON client_report_schedules (next_run_at) WHERE NOT paused;

-- One row per scheduled run: the unique key makes each period send once, and its token is the email's own report link.
CREATE TABLE client_report_deliveries (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    report_id       uuid NOT NULL REFERENCES client_reports (id) ON DELETE CASCADE,
    organization_id uuid NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    run_at          timestamptz NOT NULL,
    date_range      text NOT NULL CHECK (date_range IN ('last_7_days', 'last_30_days', 'this_month', 'last_month')),
    recipients      text[] NOT NULL,
    sent_to         text[] NOT NULL DEFAULT '{}',
    token_hash      bytea NOT NULL UNIQUE CHECK (octet_length(token_hash) = 32),
    expires_at      timestamptz NOT NULL,
    revoked_at      timestamptz,
    attempts        integer NOT NULL DEFAULT 0,
    claimed_at      timestamptz,
    sent_at         timestamptz,
    last_error      text NOT NULL DEFAULT '',
    created_at      timestamptz NOT NULL DEFAULT now(),
    UNIQUE (report_id, run_at)
);

CREATE INDEX client_report_deliveries_pending_idx ON client_report_deliveries (created_at) WHERE sent_at IS NULL;
CREATE INDEX client_report_deliveries_report_idx ON client_report_deliveries (report_id, run_at DESC);
