-- A dismissed import is hidden from the workspace's recent list; its rows and history stay.
ALTER TABLE mailbox_imports ADD COLUMN dismissed_at timestamp with time zone;
