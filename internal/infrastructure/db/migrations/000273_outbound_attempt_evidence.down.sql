DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM outbound_attempts WHERE attempted_at>NOW()-INTERVAL '24 hours') THEN
        RAISE EXCEPTION 'recent provider attempts must be retained until their rolling rate window expires';
    END IF;
END $$;
ALTER TABLE diagnostic_auth_verifications DROP COLUMN attempts;
DROP TABLE outbound_attempts;
