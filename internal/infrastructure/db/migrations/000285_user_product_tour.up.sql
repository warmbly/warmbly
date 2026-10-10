-- When the member finished or skipped the dashboard's product tour; NULL shows it once.
ALTER TABLE users ADD COLUMN IF NOT EXISTS product_tour_completed_at TIMESTAMPTZ;
