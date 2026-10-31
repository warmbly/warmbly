-- A non-NULL users.password_expires_at marks an issued initial password: capped at 48 hours from the write
-- that issued it, and moved later or cleared only together with a new password hash, whatever writes the row.
CREATE OR REPLACE FUNCTION users_initial_password_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'UPDATE'
       AND NEW.password_hash IS NOT DISTINCT FROM OLD.password_hash
       AND OLD.password_expires_at IS NOT NULL THEN
        IF NEW.password_expires_at IS NULL OR NEW.password_expires_at > OLD.password_expires_at THEN
            RAISE EXCEPTION 'an issued password cannot be extended; issue a new one'
                USING ERRCODE = 'check_violation', CONSTRAINT = 'users_initial_password_not_extended';
        END IF;
        RETURN NEW;
    END IF;
    IF NEW.password_expires_at IS NOT NULL THEN
        NEW.password_expires_at := LEAST(NEW.password_expires_at, now() + interval '48 hours');
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS users_initial_password_guard ON users;
CREATE TRIGGER users_initial_password_guard
    BEFORE INSERT OR UPDATE OF password_hash, password_expires_at ON users
    FOR EACH ROW EXECUTE FUNCTION users_initial_password_guard();
