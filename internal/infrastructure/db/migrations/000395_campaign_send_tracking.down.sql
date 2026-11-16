-- Refuse rather than discard which sends carried tracking; the rates read it and nothing can rebuild it.
-- The lock holds off a send recording its tracking between the check and the drop.
LOCK TABLE campaign_contact_progress IN SHARE ROW EXCLUSIVE MODE;

DO $$
DECLARE
    recorded boolean;
BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.columns
               WHERE table_schema = current_schema() AND table_name = 'campaign_contact_progress' AND column_name = 'open_tracked') THEN
        EXECUTE 'SELECT EXISTS (SELECT 1 FROM campaign_contact_progress WHERE open_tracked IS NOT NULL OR click_tracked IS NOT NULL)' INTO recorded;
        IF recorded THEN
            RAISE EXCEPTION 'campaign sends recorded their tracking after 000395; roll forward instead of downgrading past it';
        END IF;
    END IF;
END $$;

ALTER TABLE campaign_contact_progress
    DROP COLUMN IF EXISTS open_tracked,
    DROP COLUMN IF EXISTS click_tracked;
