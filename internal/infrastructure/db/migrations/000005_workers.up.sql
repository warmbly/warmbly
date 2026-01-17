CREATE TABLE workers (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    ip_addr TEXT NOT NULL,
    public_key TEXT NOT NULL,
    active BOOLEAN DEFAULT FALSE,
    created_at TIMESTAMPTZ DEFAULT now(),
    updated_at TIMESTAMPTZ DEFAULT now(),
    PRIMARY KEY (id)
);
