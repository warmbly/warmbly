DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM sync_arrival_outbox) THEN
        RAISE EXCEPTION 'Cannot downgrade with unresolved sync arrival delivery';
    END IF;
END $$;

DROP TABLE sync_arrival_outbox;
