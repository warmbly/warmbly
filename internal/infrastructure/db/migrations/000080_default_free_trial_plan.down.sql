-- Only removes the plan when nothing references it; a deployment that has been
-- running has subscriptions pointing at this row.
DELETE FROM plans
WHERE id = '00000000-0000-0000-0000-000000000001'
  AND NOT EXISTS (
      SELECT 1 FROM subscriptions WHERE plan_id = '00000000-0000-0000-0000-000000000001'
  );
