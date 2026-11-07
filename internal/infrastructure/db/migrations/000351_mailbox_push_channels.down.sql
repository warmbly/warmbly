BEGIN;
DROP TABLE mailbox_push_channels;
ALTER TABLE fleet_nodes DROP COLUMN sync_wake_protocol;
COMMIT;
