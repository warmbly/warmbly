-- Advisor: continuously-evaluated, org-scoped findings about deliverability,
-- mailbox config, warmup, campaign performance, copy, and list hygiene.
--
-- Detection is deterministic Go (see internal/app/advisor); the LLM only
-- narrates a finding's title/detail/remedy from its evidence, and that
-- narration is cached per (detector, evidence shape) so a whole org costs a
-- handful of completions a day. A finding is identified by its fingerprint
-- (detector key + subject entity), so re-running the engine updates the same
-- row instead of duplicating advice.

CREATE TABLE IF NOT EXISTS advisor_findings (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id   uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,

    -- Stable identity across runs: <detector_key>:<entity_type>:<entity_id>.
    fingerprint       text NOT NULL,
    detector_key      text NOT NULL,
    category          text NOT NULL,
    severity          text NOT NULL,
    -- Dashboard nav tab the fix lives on, so the UI can badge the right tab
    -- and render the finding on the page where the problem actually is.
    surface           text NOT NULL DEFAULT 'deliverability',

    entity_type       text NOT NULL DEFAULT '',
    entity_id         uuid,
    entity_label      text NOT NULL DEFAULT '',
    -- The entity this one belongs to, when they differ: a copy problem lives on
    -- a sequence step, but the person looking for it is on the campaign page.
    -- Entity-scoped reads match either column, so a finding surfaces on the
    -- page where someone would go looking for it.
    parent_type       text NOT NULL DEFAULT '',
    parent_id         uuid,

    status            text NOT NULL DEFAULT 'open',
    -- 0-100 ranking weight; severity breaks ties on equal impact.
    impact            smallint NOT NULL DEFAULT 0,

    title             text NOT NULL,
    -- How this finding names itself when several of its kind are shown
    -- together ("{count} mailboxes are capped above the safe band"). A
    -- workspace that misconfigured twenty mailboxes the same way should get
    -- one card, not twenty; empty means this finding never collapses.
    group_title       text NOT NULL DEFAULT '',
    detail            text NOT NULL DEFAULT '',
    remedy            text NOT NULL DEFAULT '',
    -- The ordered manual how-to, set only by checks with no one-click fix.
    -- Empty means the remedy prose is the whole answer.
    steps             text[] NOT NULL DEFAULT '{}',
    -- False while the row still carries the deterministic fallback copy, so a
    -- later run can upgrade it once AI is configured / credits exist.
    narrated          boolean NOT NULL DEFAULT false,

    evidence          jsonb NOT NULL DEFAULT '{}'::jsonb,
    -- Bucketed hash of the evidence: narration is reused while this is stable,
    -- and re-generated when the numbers move enough to change the advice.
    evidence_hash     text NOT NULL DEFAULT '',
    -- One-click fix descriptor: {tool, args, label, preview:[{field,from,to}]}.
    -- NULL means the finding is informational and has no automated remedy.
    action            jsonb,

    first_seen_at     timestamptz NOT NULL DEFAULT now(),
    last_seen_at      timestamptz NOT NULL DEFAULT now(),
    resolved_at       timestamptz,

    snoozed_until     timestamptz,
    dismissed_at      timestamptz,
    dismissed_by      uuid REFERENCES users(id) ON DELETE SET NULL,
    dismiss_reason    text NOT NULL DEFAULT '',

    applied_at        timestamptz,
    applied_by        uuid REFERENCES users(id) ON DELETE SET NULL,
    applied_result    text NOT NULL DEFAULT '',

    CONSTRAINT advisor_findings_status_check
        CHECK (status IN ('open', 'snoozed', 'dismissed', 'applied', 'resolved')),
    CONSTRAINT advisor_findings_severity_check
        CHECK (severity IN ('critical', 'high', 'medium', 'low')),
    CONSTRAINT advisor_findings_org_fingerprint_key
        UNIQUE (organization_id, fingerprint)
);

-- The two hot reads: the badge counts per surface, and the strip on an entity.
CREATE INDEX IF NOT EXISTS idx_advisor_findings_open
    ON advisor_findings (organization_id, surface, severity)
    WHERE status IN ('open', 'snoozed');
CREATE INDEX IF NOT EXISTS idx_advisor_findings_entity
    ON advisor_findings (organization_id, entity_type, entity_id)
    WHERE status IN ('open', 'snoozed');
CREATE INDEX IF NOT EXISTS idx_advisor_findings_parent
    ON advisor_findings (organization_id, parent_type, parent_id)
    WHERE status IN ('open', 'snoozed') AND parent_id IS NOT NULL;
-- Snooze expiry sweep.
CREATE INDEX IF NOT EXISTS idx_advisor_findings_snoozed
    ON advisor_findings (snoozed_until)
    WHERE status = 'snoozed';

-- Narration cache. Keyed per org because the copy names the org's own
-- campaigns/mailboxes and follows its voice grounding.
CREATE TABLE IF NOT EXISTS advisor_narrations (
    organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    cache_key       text NOT NULL,
    title           text NOT NULL,
    detail          text NOT NULL,
    remedy          text NOT NULL,
    model           text NOT NULL DEFAULT '',
    created_at      timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (organization_id, cache_key)
);

-- Was this advice useful? Feeds detector tuning and lets a member's "not
-- useful" mute a detector for the org without hiding it from admins.
CREATE TABLE IF NOT EXISTS advisor_feedback (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    finding_id      uuid NOT NULL REFERENCES advisor_findings(id) ON DELETE CASCADE,
    detector_key    text NOT NULL,
    user_id         uuid REFERENCES users(id) ON DELETE SET NULL,
    helpful         boolean NOT NULL,
    reason          text NOT NULL DEFAULT '',
    created_at      timestamptz NOT NULL DEFAULT now(),
    UNIQUE (finding_id, user_id)
);

CREATE INDEX IF NOT EXISTS idx_advisor_feedback_detector
    ON advisor_feedback (organization_id, detector_key, helpful);

-- Per-org advisor controls. A row is created lazily on first write; absence
-- means "all defaults" (enabled, nothing muted).
CREATE TABLE IF NOT EXISTS advisor_settings (
    organization_id  uuid PRIMARY KEY REFERENCES organizations(id) ON DELETE CASCADE,
    enabled          boolean NOT NULL DEFAULT true,
    -- Categories the org has switched off entirely.
    muted_categories text[] NOT NULL DEFAULT '{}',
    -- Individual detector keys the org has switched off.
    muted_detectors  text[] NOT NULL DEFAULT '{}',
    -- Lowest severity the org wants surfaced.
    min_severity     text NOT NULL DEFAULT 'low',
    updated_at       timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT advisor_settings_min_severity_check
        CHECK (min_severity IN ('critical', 'high', 'medium', 'low'))
);

-- One evaluation pass, for observability and to rate-limit manual refreshes.
CREATE TABLE IF NOT EXISTS advisor_runs (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    trigger         text NOT NULL DEFAULT 'schedule',
    started_at      timestamptz NOT NULL DEFAULT now(),
    finished_at     timestamptz,
    findings_new    integer NOT NULL DEFAULT 0,
    findings_open   integer NOT NULL DEFAULT 0,
    findings_closed integer NOT NULL DEFAULT 0,
    narrated        integer NOT NULL DEFAULT 0,
    error           text NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_advisor_runs_org
    ON advisor_runs (organization_id, started_at DESC);
