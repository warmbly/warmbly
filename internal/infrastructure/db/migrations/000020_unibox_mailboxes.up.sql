-- Migrate unibox email messages and mailboxes from Cassandra to PostgreSQL

CREATE TABLE unibox_emails (
    id UUID NOT NULL,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    email_id UUID NOT NULL REFERENCES email_accounts(id) ON DELETE CASCADE,
    mailbox INTEGER NOT NULL DEFAULT 0,
    thread_id TEXT NOT NULL DEFAULT '',
    message_id TEXT NOT NULL DEFAULT '',
    gmail_id TEXT NOT NULL DEFAULT '',
    parent_id TEXT NOT NULL DEFAULT '',
    uid INTEGER NOT NULL DEFAULT 0,
    mod_seq BIGINT NOT NULL DEFAULT 0,
    flags TEXT[] NOT NULL DEFAULT '{}',
    bcc TEXT[] NOT NULL DEFAULT '{}',
    cc TEXT[] NOT NULL DEFAULT '{}',
    from_addr TEXT[] NOT NULL DEFAULT '{}',
    in_reply_to TEXT[] NOT NULL DEFAULT '{}',
    reply_to TEXT[] NOT NULL DEFAULT '{}',
    to_addr TEXT[] NOT NULL DEFAULT '{}',
    subject TEXT NOT NULL DEFAULT '',
    size BIGINT NOT NULL DEFAULT 0,
    internal_date TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    sent_date TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    snippet TEXT NOT NULL DEFAULT '',
    seen BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (id)
);

-- Primary lookup: user's inbox sorted by date
CREATE INDEX idx_unibox_emails_user_date ON unibox_emails(user_id, internal_date DESC);

-- Lookup by email account
CREATE INDEX idx_unibox_emails_user_email ON unibox_emails(user_id, email_id, internal_date DESC);

-- Thread lookups
CREATE INDEX idx_unibox_emails_thread ON unibox_emails(user_id, email_id, thread_id, internal_date DESC);

-- Unseen count (partial index for efficiency)
CREATE INDEX idx_unibox_emails_unseen ON unibox_emails(user_id) WHERE seen = FALSE;
CREATE INDEX idx_unibox_emails_unseen_account ON unibox_emails(user_id, email_id) WHERE seen = FALSE;

-- Sender lookups via GIN on from_addr array
CREATE INDEX idx_unibox_emails_from ON unibox_emails USING GIN(from_addr);

-- Full-text search on subject and snippet
ALTER TABLE unibox_emails ADD COLUMN search_tsv tsvector
    GENERATED ALWAYS AS (to_tsvector('english', coalesce(subject, '') || ' ' || coalesce(snippet, ''))) STORED;
CREATE INDEX idx_unibox_emails_search ON unibox_emails USING GIN(search_tsv);

-- Mailboxes (IMAP mailbox state per email account)
CREATE TABLE unibox_mailboxes (
    email_id UUID NOT NULL REFERENCES email_accounts(id) ON DELETE CASCADE,
    uid_validity INTEGER NOT NULL,
    mailbox TEXT NOT NULL DEFAULT '',
    attributes TEXT[] NOT NULL DEFAULT '{}',
    highestmodseq BIGINT NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (email_id, uid_validity)
);
