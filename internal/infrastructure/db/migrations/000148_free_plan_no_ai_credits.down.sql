UPDATE plans
SET monthly_credits = 50,
    updated_at      = NOW()
WHERE id = '00000000-0000-0000-0000-000000000001'
  AND monthly_credits = 0;
