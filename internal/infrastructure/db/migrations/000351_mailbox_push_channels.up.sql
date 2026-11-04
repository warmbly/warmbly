BEGIN;
-- Workers that act on SYNC_WAKE report 1; older workers stay at 0 and are never sent one.
ALTER TABLE fleet_nodes ADD COLUMN sync_wake_protocol integer NOT NULL DEFAULT 0;

-- The provider push channel a worker holds open for a mailbox, so a reload renews it.
CREATE TABLE mailbox_push_channels (
    email_account_id uuid PRIMARY KEY REFERENCES email_accounts(id) ON DELETE CASCADE,
    provider text NOT NULL CHECK (provider IN ('gmail','graph')),
    subscription_id text,
    credential text,
    -- Lowercased mailbox address: a Gmail notification names the mailbox only by it.
    address text NOT NULL,
    expires_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX mailbox_push_channels_gmail_address ON mailbox_push_channels (address) WHERE provider = 'gmail';
COMMIT;
