ALTER TABLE warmup_received
    ADD COLUMN first_landing text CHECK (first_landing IN ('inbox','tabs','spam','unknown','archive','custom')),
    ADD COLUMN first_folder text,
    ADD COLUMN first_flags text[],
    ADD COLUMN observed_at timestamptz,
    ADD COLUMN evidence jsonb;

ALTER TABLE warmup_placement_daily
    ADD COLUMN unknown integer NOT NULL DEFAULT 0 CHECK (unknown >= 0),
    ADD COLUMN archived integer NOT NULL DEFAULT 0 CHECK (archived >= 0),
    ADD COLUMN custom integer NOT NULL DEFAULT 0 CHECK (custom >= 0),
    ADD COLUMN instrumented integer NOT NULL DEFAULT 0 CHECK (instrumented >= 0);

ALTER TABLE placement_results
    ADD COLUMN first_landing text CHECK (first_landing IN ('inbox','promotions','other','spam','unknown','archive','custom')),
    ADD COLUMN first_folder text,
    ADD COLUMN first_flags text[],
    ADD COLUMN observed_at timestamptz,
    ADD COLUMN evidence jsonb,
    DROP CONSTRAINT placement_results_folder_check,
    ADD CONSTRAINT placement_results_folder_check CHECK (folder IN ('pending','inbox','promotions','other','spam','missing','failed','cancelled','unknown','archive','custom'));
