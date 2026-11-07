-- A booked meeting ends a lead's cold sequence like a reply does. On for every
-- campaign: no branch condition can route on a booking, so no existing flow was
-- built to keep mailing a contact who booked.
ALTER TABLE campaigns ADD COLUMN IF NOT EXISTS stop_on_meeting boolean NOT NULL DEFAULT true;
