-- Alone in its file for CONCURRENTLY. Serves the campaign router's has_booked check per lead.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_meeting_bookings_contact_created
    ON public.meeting_bookings (contact_id, created_at)
    WHERE contact_id IS NOT NULL;
