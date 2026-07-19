-- Undo send: instant sends are held for a short per-user window so the
-- sender can cancel before the mail actually leaves. Bounds mirror
-- config.UndoSendSeconds{Min,Max}; the app clamps, the CHECK backstops.
ALTER TABLE users ADD COLUMN undo_send_seconds INT NOT NULL DEFAULT 30
    CONSTRAINT users_undo_send_seconds_range CHECK (undo_send_seconds >= 5 AND undo_send_seconds <= 120);
