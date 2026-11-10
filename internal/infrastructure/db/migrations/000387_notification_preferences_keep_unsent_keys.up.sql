-- A save from a release that does not know a category keeps that category's saved value.
CREATE OR REPLACE FUNCTION users_keep_notification_preferences() RETURNS trigger AS $$
BEGIN
	NEW.notification_preferences := OLD.notification_preferences || NEW.notification_preferences;
	RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS users_keep_notification_preferences ON users;
CREATE TRIGGER users_keep_notification_preferences
	BEFORE UPDATE OF notification_preferences ON users
	FOR EACH ROW
	WHEN (jsonb_typeof(OLD.notification_preferences) = 'object' AND jsonb_typeof(NEW.notification_preferences) = 'object')
	EXECUTE FUNCTION users_keep_notification_preferences();
