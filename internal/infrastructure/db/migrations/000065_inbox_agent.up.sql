-- Inbox agent (M10). A paid + opt-in feature: on an inbound human reply the
-- backend drafts a suggested reply and stores it here awaiting a human
-- Approve-and-send / Edit / Discard in the unibox. The agent NEVER sends; a
-- draft is inert until a person acts on it.

-- Per-org opt-in. Off by default; an admin turns it on under settings.
ALTER TABLE organizations
    ADD COLUMN IF NOT EXISTS inbox_agent_enabled BOOLEAN NOT NULL DEFAULT false;

CREATE TABLE IF NOT EXISTS ai_thread_drafts (
    id                UUID PRIMARY KEY,
    organization_id   UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    email_account_id  UUID NOT NULL,
    -- Mailbox owner: the send goes out from their account when approved.
    owner_user_id     UUID NOT NULL,
    thread_id         TEXT NOT NULL,
    -- The inbound message this drafts a reply to. Used to dedupe reprocessed
    -- events (never draft the same inbound message twice) and to thread the send.
    source_message_id UUID,
    contact_id        UUID,
    campaign_id       UUID,
    to_addr           TEXT NOT NULL,
    subject           TEXT NOT NULL DEFAULT '',
    -- RFC Message-Id of the inbound reply, so the approved send references it.
    in_reply_to       TEXT NOT NULL DEFAULT '',
    body              TEXT NOT NULL DEFAULT '',
    intent_class      TEXT NOT NULL DEFAULT '',
    confidence        DOUBLE PRECISION NOT NULL DEFAULT 0,
    model             TEXT NOT NULL DEFAULT '',
    status            TEXT NOT NULL DEFAULT 'pending'
                          CHECK (status IN ('pending', 'approved', 'discarded')),
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- At most one pending draft per thread (the human resolves it before another).
CREATE UNIQUE INDEX IF NOT EXISTS ai_thread_drafts_one_pending
    ON ai_thread_drafts (organization_id, thread_id)
    WHERE status = 'pending';

-- Never draft the same inbound message twice, even across reprocessed events.
CREATE UNIQUE INDEX IF NOT EXISTS ai_thread_drafts_source_msg
    ON ai_thread_drafts (source_message_id)
    WHERE source_message_id IS NOT NULL;

-- Pending list + badge count per org, newest first.
CREATE INDEX IF NOT EXISTS ai_thread_drafts_org_pending
    ON ai_thread_drafts (organization_id, created_at DESC)
    WHERE status = 'pending';
