-- Label automated opens (Apple Mail Privacy Protection prefetches, UA-less
-- fetchers) so analytics can separate human opens from machine opens instead
-- of silently inflating open rates. A later human open clears the flag.
ALTER TABLE campaign_contact_progress
    ADD COLUMN IF NOT EXISTS opened_machine boolean NOT NULL DEFAULT false;
