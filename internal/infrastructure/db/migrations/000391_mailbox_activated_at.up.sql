-- When a mailbox last started running here: connected, imported, or switched back on.
-- The unreachable-mailbox sweep measures its silence from this, so switching one on starts a fresh window.
-- now() is evaluated once, so existing rows take the migration time without a table rewrite.
ALTER TABLE email_accounts ADD COLUMN activated_at timestamptz NOT NULL DEFAULT now();

-- A trigger rather than callers, so every path that switches a mailbox on (the toggle, bulk, reconnect,
-- the API, warmblyctl) restarts the clock and retires the sweep's own switch-off notice.
CREATE FUNCTION email_accounts_activated() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    NEW.activated_at := now();
    UPDATE email_account_errors SET resolved_at = now(), resolved_by = 'switched on'
    WHERE email_account_id = NEW.id AND error_code = 'MAILBOX_UNREACHABLE' AND resolved_at IS NULL;
    RETURN NEW;
END $$;

CREATE TRIGGER email_accounts_activated
    BEFORE UPDATE OF status ON email_accounts
    FOR EACH ROW
    WHEN (NEW.status = 'active' AND OLD.status IS DISTINCT FROM NEW.status)
    EXECUTE FUNCTION email_accounts_activated();
