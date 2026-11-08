-- Alone in its file for CONCURRENTLY. The pending set is the contacts whose
-- inputs changed since the sweep last read them; segments filter on the rest.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_contacts_location_pending
    ON contacts (id)
    WHERE location_fingerprint IS DISTINCT FROM contact_location_fingerprint(custom_fields, phone, email);
