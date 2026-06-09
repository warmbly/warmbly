DROP INDEX IF EXISTS idx_meeting_bookings_scheduled;

ALTER TABLE meeting_bookings
    DROP COLUMN IF EXISTS status,
    DROP COLUMN IF EXISTS end_time,
    DROP COLUMN IF EXISTS join_url,
    DROP COLUMN IF EXISTS location,
    DROP COLUMN IF EXISTS cancel_url,
    DROP COLUMN IF EXISTS reschedule_url,
    DROP COLUMN IF EXISTS event_type,
    DROP COLUMN IF EXISTS canceled_reason,
    DROP COLUMN IF EXISTS updated_at;
