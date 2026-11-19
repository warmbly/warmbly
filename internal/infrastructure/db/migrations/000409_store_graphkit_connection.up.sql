-- The GraphKit connection an operator saved in the admin panel; GRAPHKIT_* in the environment wins over it.
-- One row at most. The three secrets are sealed under CREDENTIALS_ENCRYPTION_KEY.
CREATE TABLE IF NOT EXISTS public.store_graphkit_connection (
    id boolean PRIMARY KEY DEFAULT true CHECK (id),
    api_url text NOT NULL,
    api_key text NOT NULL,
    webhook_secret text,
    webhook_secret_previous text,
    -- When webhook_secret_previous was replaced; it verifies for 24 hours after.
    webhook_secret_rotated_at timestamptz,
    updated_by uuid REFERENCES public.users (id) ON DELETE SET NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);
