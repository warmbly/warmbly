-- Meeting bookings: full lifecycle + richer detail.
--
-- The baseline meeting_bookings table only recorded the create event with a
-- name, an invitee, and a scheduled time. A booked call is a high-value signal,
-- so we now track its full lifecycle (booked -> rescheduled / canceled) and the
-- detail a sales rep actually needs: the join link, the call window, and the
-- cancel/reschedule links the provider gives us. This powers the Meetings page,
-- the contact timeline, and live dashboard updates.

ALTER TABLE meeting_bookings
    ADD COLUMN status text NOT NULL DEFAULT 'booked'
        CONSTRAINT meeting_bookings_status_check
        CHECK (status = ANY (ARRAY['booked'::text, 'rescheduled'::text, 'canceled'::text, 'completed'::text, 'no_show'::text])),
    ADD COLUMN end_time timestamp with time zone,
    ADD COLUMN join_url text NOT NULL DEFAULT '',
    ADD COLUMN location text NOT NULL DEFAULT '',
    ADD COLUMN cancel_url text NOT NULL DEFAULT '',
    ADD COLUMN reschedule_url text NOT NULL DEFAULT '',
    ADD COLUMN event_type text NOT NULL DEFAULT '',
    ADD COLUMN canceled_reason text NOT NULL DEFAULT '',
    ADD COLUMN updated_at timestamp with time zone NOT NULL DEFAULT now();

-- Upcoming-meetings queries (Meetings page, sidebar count) filter by org and
-- order by the scheduled time, so index that path.
CREATE INDEX idx_meeting_bookings_scheduled
    ON meeting_bookings (organization_id, scheduled_for);
