-- APNs device tokens for mobile push delivery. One row per device; a token is
-- globally unique and re-registering it moves it to the signing-in user (the
-- device changed accounts). Environment picks the APNs host (sandbox vs prod).
CREATE TABLE device_tokens (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    platform     TEXT NOT NULL DEFAULT 'ios' CHECK (platform IN ('ios')),
    token        TEXT NOT NULL UNIQUE,
    environment  TEXT NOT NULL DEFAULT 'production' CHECK (environment IN ('development', 'production')),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_device_tokens_user ON device_tokens (user_id);
