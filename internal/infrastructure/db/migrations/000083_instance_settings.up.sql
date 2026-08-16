-- Instance settings: the small database-backed configuration tier.
--
-- The environment is authoritative for everything else, so this table holds
-- only keys that NO environment variable owns. That disjointness is the whole
-- design: there is no precedence to resolve, no write-back hazard, and no
-- locked-by-environment form field anywhere in the admin panel.
--
-- The document is jsonb because it is a small, evolving, read-then-apply blob
-- that is never filtered in SQL. It is kept type-safe at the app boundary by
-- internal/app/instancesettings (a Go struct plus clamping on read and write).
--
-- Singleton enforced with the boolean-primary-key idiom: id can only ever be
-- true, so a second row is impossible.

CREATE TABLE IF NOT EXISTS instance_settings (
    id         boolean PRIMARY KEY DEFAULT true CHECK (id),
    doc        jsonb NOT NULL DEFAULT '{}'::jsonb,
    updated_by uuid REFERENCES users (id) ON DELETE SET NULL,
    updated_at timestamptz NOT NULL DEFAULT NOW()
);

-- Seed the row empty. An absent key resolves to its compiled default, so an
-- empty document and a missing row behave identically.
INSERT INTO instance_settings (id, doc)
VALUES (true, '{}'::jsonb)
ON CONFLICT (id) DO NOTHING;
