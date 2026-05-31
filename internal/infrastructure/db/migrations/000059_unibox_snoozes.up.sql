-- Snoozes hide a thread from the inbox until snoozed_until passes.
-- Per (user, thread) so the same thread snoozed twice updates in place.
-- The unibox query treats a row as snoozed when a matching entry exists
-- with snoozed_until > now(); once the time passes, the row becomes
-- inert (no need to delete it) and the thread reappears in the inbox.

CREATE TABLE unibox_snoozes (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id       UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    thread_id     TEXT NOT NULL,
    snoozed_until TIMESTAMPTZ NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (user_id, thread_id)
);

CREATE INDEX unibox_snoozes_user_until
    ON unibox_snoozes (user_id, snoozed_until);
