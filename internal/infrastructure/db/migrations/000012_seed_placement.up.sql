-- Seed inbox-placement testing.
--
-- A "seed" mailbox is an ordinary connected + synced email_account that we
-- flag with is_seed. Because it syncs like any other mailbox, mail it receives
-- lands in unibox_emails — which is exactly where the placement poller looks
-- up the per-test token and reads the folder/flags to classify where the test
-- message landed (Inbox / Spam / Promotions / other), per provider.

ALTER TABLE email_accounts
    ADD COLUMN is_seed boolean NOT NULL DEFAULT false;

-- Cheap lookup of the active seed panel.
CREATE INDEX idx_email_accounts_is_seed
    ON email_accounts (is_seed)
    WHERE is_seed = true;

-- A placement test: one tokenized copy of a template is sent from a chosen
-- sender mailbox to every active seed. Status is "pending" while results are
-- still being classified, "completed" once all resolve or the timeout passes.
CREATE TABLE placement_tests (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id   uuid REFERENCES organizations (id) ON DELETE CASCADE,
    sender_account_id uuid NOT NULL REFERENCES email_accounts (id) ON DELETE CASCADE,
    subject           text NOT NULL,
    body_plain        text NOT NULL DEFAULT '',
    body_html         text NOT NULL DEFAULT '',
    token             text NOT NULL UNIQUE,
    status            text NOT NULL DEFAULT 'pending',
    created_at        timestamptz NOT NULL DEFAULT NOW(),
    finished_at       timestamptz
);

CREATE INDEX idx_placement_tests_status ON placement_tests (status);
CREATE INDEX idx_placement_tests_org ON placement_tests (organization_id, created_at DESC);

-- One row per (test, seed). folder starts "pending" and is set when the
-- token is found in that seed's unibox entries. provider records the seed
-- mailbox's provider so results roll up per provider, not one flat rate.
CREATE TABLE placement_results (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    test_id         uuid NOT NULL REFERENCES placement_tests (id) ON DELETE CASCADE,
    seed_account_id uuid NOT NULL REFERENCES email_accounts (id) ON DELETE CASCADE,
    provider        text NOT NULL DEFAULT '',
    folder          text NOT NULL DEFAULT 'pending'
        CHECK (folder IN ('inbox', 'promotions', 'spam', 'other', 'pending')),
    detected_at     timestamptz,
    raw_flags       text NOT NULL DEFAULT '',
    UNIQUE (test_id, seed_account_id)
);

CREATE INDEX idx_placement_results_test ON placement_results (test_id);
CREATE INDEX idx_placement_results_pending
    ON placement_results (folder)
    WHERE folder = 'pending';
