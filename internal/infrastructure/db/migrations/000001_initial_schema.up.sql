CREATE TABLE users (
    id UUID PRIMARY KEY NOT NULL DEFAULT gen_random_uuid (),
    first_name VARCHAR(255) NOT NULL,
    last_name VARCHAR(255) NOT NULL,
    email TEXT UNIQUE NOT NULL,
    password_hash TEXT,
    created_at TIMESTAMPTZ DEFAULT now (),
    updated_at TIMESTAMPTZ DEFAULT now ()
);

CREATE TABLE sessions (
    id UUID PRIMARY KEY NOT NULL DEFAULT gen_random_uuid (),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,

    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ,

    last_refreshed_at TIMESTAMPTZ,
    refresh_nonce TEXT,
    access_nonce TEXT,

    location_city TEXT,
    location_region TEXT,
    location_country TEXT,
    location_country_code TEXT,
    location_postal_code TEXT,

    os_name TEXT,
    browser_name TEXT
);
